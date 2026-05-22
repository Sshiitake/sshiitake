package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"
)

func TestVersionCommand(t *testing.T) {
	var stdout bytes.Buffer
	cmd := rootCmd()
	cmd.SetOut(&stdout)
	cmd.SetArgs([]string{"version"})

	require.NoError(t, cmd.Execute())
	assert.Contains(t, stdout.String(), "ssht")
}

func TestRootHelp(t *testing.T) {
	var stdout bytes.Buffer
	cmd := rootCmd()
	cmd.SetOut(&stdout)
	cmd.SetArgs([]string{"--help"})

	require.NoError(t, cmd.Execute())
	out := stdout.String()
	assert.Contains(t, out, "ssht")
	assert.Contains(t, out, "version")
}

func TestConfigCheck_validFixture(t *testing.T) {
	var stdout bytes.Buffer
	cmd := rootCmd()
	cmd.SetOut(&stdout)
	cmd.SetArgs([]string{
		"config", "check",
		"--config", "../../testdata/tunnels-valid.toml",
		"--ssh-config", "../../testdata/ssh_config_sample",
	})
	require.NoError(t, cmd.Execute())
	assert.Contains(t, stdout.String(), "OK")
}

func TestConfigCheck_missingFile(t *testing.T) {
	var stderr bytes.Buffer
	cmd := rootCmd()
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{
		"config", "check",
		"--config", "/nonexistent.toml",
	})
	err := cmd.Execute()
	require.Error(t, err)
}

// TestUp_endToEnd verifies the full bring-up path: load config, dial
// in-process SSH server, open local listener, forward bytes through.
// Manager-driven: a single-tunnel use of the multi-tunnel CLI path.
func TestUp_endToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e in short mode")
	}

	sshAddr, hostKey := newInProcSSHServer(t)
	host, port := splitHostPortHelper(t, sshAddr)

	echoAddr := startEchoServer(t)

	dir := t.TempDir()
	cfgPath := dir + "/tunnels.toml"
	// local_port = 0 asks the OS for an ephemeral port. The listen-file
	// receives the actual bound address once Start succeeds, removing
	// the 18443 collision risk that flaked CI on shared runners.
	require.NoError(t, os.WriteFile(cfgPath, []byte(fmt.Sprintf(`
[tunnels.echo]
host = "%s"
type = "local"
local_port = 0
remote_host = "%s"
remote_port = %s
`, host, echoHost(echoAddr), echoPort(echoAddr))), 0o600))

	sshCfgPath := dir + "/ssh_config"
	require.NoError(t, os.WriteFile(sshCfgPath, []byte(fmt.Sprintf(`
Host %s
    HostName %s
    Port %d
    User tester
`, host, host, port)), 0o600))

	t.Setenv("SSHT_TEST_HOSTKEY", base64HostKey(hostKey))

	listenFile := dir + "/listen.txt"
	cmd := rootCmd()
	cmd.SetArgs([]string{
		"up", "echo",
		"--config", cfgPath,
		"--ssh-config", sshCfgPath,
		"--listen-file", listenFile,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() { errCh <- cmd.ExecuteContext(ctx) }()

	require.Eventually(t, func() bool {
		data, err := os.ReadFile(listenFile)
		if err != nil || len(data) == 0 {
			return false
		}
		c, err := net.Dial("tcp", string(data))
		if err != nil {
			return false
		}
		defer c.Close()
		_, err = c.Write([]byte("ping"))
		if err != nil {
			return false
		}
		buf := make([]byte, 4)
		_, err = io.ReadFull(c, buf)
		return err == nil && string(buf) == "ping"
	}, 8*time.Second, 50*time.Millisecond, "tunnel forwarding never succeeded")

	cancel()
	select {
	case err := <-errCh:
		require.NoError(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("up did not exit on context cancel")
	}
}

// --- Helpers below: duplicate code from internal/tunnel/testserver_test.go
// is intentional; test helpers should not be exported.

func newInProcSSHServer(t *testing.T) (addr string, hostKey ssh.PublicKey) {
	t.Helper()
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	signer, err := ssh.NewSignerFromKey(rsaKey)
	require.NoError(t, err)

	cfg := &ssh.ServerConfig{NoClientAuth: true}
	cfg.AddHostKey(signer)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })

	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go inProcServerHandle(c, cfg)
		}
	}()
	return ln.Addr().String(), signer.PublicKey()
}

func inProcServerHandle(c net.Conn, cfg *ssh.ServerConfig) {
	defer c.Close()
	sshConn, chans, reqs, err := ssh.NewServerConn(c, cfg)
	if err != nil {
		return
	}
	defer sshConn.Close()
	go ssh.DiscardRequests(reqs)
	for ch := range chans {
		if ch.ChannelType() != "direct-tcpip" {
			_ = ch.Reject(ssh.UnknownChannelType, "unsupported")
			continue
		}
		go inProcServeChannel(ch)
	}
}

func inProcServeChannel(ch ssh.NewChannel) {
	var msg struct {
		DestAddr   string
		DestPort   uint32
		OriginAddr string
		OriginPort uint32
	}
	if err := ssh.Unmarshal(ch.ExtraData(), &msg); err != nil {
		_ = ch.Reject(ssh.ConnectionFailed, "bad payload")
		return
	}
	target := net.JoinHostPort(msg.DestAddr, strconv.Itoa(int(msg.DestPort)))
	remote, err := net.Dial("tcp", target)
	if err != nil {
		_ = ch.Reject(ssh.ConnectionFailed, err.Error())
		return
	}
	channel, reqs, err := ch.Accept()
	if err != nil {
		_ = remote.Close()
		return
	}
	go ssh.DiscardRequests(reqs)
	go func() { _, _ = io.Copy(channel, remote); _ = channel.Close() }()
	go func() { _, _ = io.Copy(remote, channel); _ = remote.Close() }()
}

func startEchoServer(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				_, _ = io.Copy(c, c)
				_ = c.Close()
			}(c)
		}
	}()
	return ln.Addr().String()
}

func splitHostPortHelper(t *testing.T, addr string) (string, int) {
	t.Helper()
	h, p, err := net.SplitHostPort(addr)
	require.NoError(t, err)
	pi, err := strconv.Atoi(p)
	require.NoError(t, err)
	return h, pi
}

func echoHost(addr string) string {
	h, _, _ := net.SplitHostPort(addr)
	return h
}

func echoPort(addr string) string {
	_, p, _ := net.SplitHostPort(addr)
	return p
}

func base64HostKey(k ssh.PublicKey) string {
	return base64.StdEncoding.EncodeToString(k.Marshal())
}

func TestExitCode_configError(t *testing.T) {
	code := classifyError(fmt.Errorf("load %s: open: no such file", "tunnels.toml"))
	assert.Equal(t, 1, code)
}

func TestExitCode_sshError(t *testing.T) {
	code := classifyError(fmt.Errorf("dial tcp: handshake failed"))
	assert.Equal(t, 2, code)
}

func TestExitCode_nil(t *testing.T) {
	assert.Equal(t, 0, classifyError(nil))
}

func TestExitCode_keyMismatch(t *testing.T) {
	err := fmt.Errorf("some prefix: %w", ErrKeyMismatch)
	assert.Equal(t, 2, classifyError(err))
}

func TestExitCode_hostNotKnown(t *testing.T) {
	err := fmt.Errorf("some prefix: %w", ErrHostNotInKnownHosts)
	assert.Equal(t, 2, classifyError(err))
}

// TestUp_hotReloadAddsTunnel: bring up `ssht up echo1`, edit tunnels.toml
// to ADD echo2, and verify that the human-stream output reports echo2
// coming up without restarting the process.
func TestUp_hotReloadAddsTunnel(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e in short mode")
	}

	sshAddr, hostKey := newInProcSSHServer(t)
	host, port := splitHostPortHelper(t, sshAddr)

	echo1 := startEchoServer(t)
	echo2 := startEchoServer(t)

	dir := t.TempDir()
	cfgPath := dir + "/tunnels.toml"

	initialTOML := fmt.Sprintf(`
[tunnels.echo1]
host = "%s"
type = "local"
local_port = 0
remote_host = "%s"
remote_port = %s
`, host, echoHost(echo1), echoPort(echo1))
	require.NoError(t, os.WriteFile(cfgPath, []byte(initialTOML), 0o600))

	sshCfgPath := dir + "/ssh_config"
	require.NoError(t, os.WriteFile(sshCfgPath, []byte(fmt.Sprintf(`
Host %s
    HostName %s
    Port %d
    User tester
`, host, host, port)), 0o600))

	t.Setenv("SSHT_TEST_HOSTKEY", base64HostKey(hostKey))

	// concurrentBuffer is io.Writer-safe under concurrent writes;
	// bytes.Buffer is not, and ssht's logger + reload goroutine + main
	// goroutine all write to stdout.
	var stdout concurrentBuffer

	cmd := rootCmd()
	cmd.SetOut(&stdout)
	// --no-tui to force the human-stream path (otherwise even a
	// bytes.Buffer falls back to human stream because isStdoutTTY
	// returns false; but being explicit documents intent).
	cmd.SetArgs([]string{
		"up", "echo1",
		"--config", cfgPath,
		"--ssh-config", sshCfgPath,
		"--no-tui",
		"--no-reconnect",
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() { errCh <- cmd.ExecuteContext(ctx) }()

	// Wait for echo1 to come up via stdout.
	require.Eventually(t, func() bool {
		return bytesContains(stdout.Bytes(), []byte(`tunnel "echo1" up`))
	}, 8*time.Second, 50*time.Millisecond, "echo1 never came up")

	// Now ADD echo2 by rewriting the config file. Use atomic-write
	// so we exercise the same path appendTunnel uses.
	newTOML := initialTOML + fmt.Sprintf(`
[tunnels.echo2]
host = "%s"
type = "local"
local_port = 0
remote_host = "%s"
remote_port = %s
`, host, echoHost(echo2), echoPort(echo2))
	tmpPath := cfgPath + ".tmp"
	require.NoError(t, os.WriteFile(tmpPath, []byte(newTOML), 0o600))
	require.NoError(t, os.Rename(tmpPath, cfgPath))

	// Reload should pick up the change and bring echo2 up.
	require.Eventually(t, func() bool {
		return bytesContains(stdout.Bytes(), []byte(`tunnel "echo2" up`))
	}, 8*time.Second, 50*time.Millisecond, "echo2 never came up via hot reload")

	cancel()
	select {
	case <-errCh:
	case <-time.After(3 * time.Second):
		t.Fatal("up did not exit on context cancel")
	}
}

// concurrentBuffer is a thread-safe write target for cobra's stdout
// when multiple goroutines (main, reload, manager) emit lines.
type concurrentBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *concurrentBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *concurrentBuffer) Bytes() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	// Return a copy so the caller can hold it past the next write.
	out := make([]byte, b.buf.Len())
	copy(out, b.buf.Bytes())
	return out
}

func bytesContains(haystack, needle []byte) bool {
	return bytes.Contains(haystack, needle)
}
