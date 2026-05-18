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
func TestUp_endToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e in short mode")
	}

	sshAddr, hostKey := newInProcSSHServer(t)
	host, port := splitHostPortHelper(t, sshAddr)

	echoAddr := startEchoServer(t)

	// Reserve a free localhost port for the tunnel's local listener.
	// The config validator rejects local_port = 0, so we pre-bind on
	// an ephemeral port, read it, and immediately release it. There
	// is a small race window before tunnel.Start re-binds, but it's
	// localhost-only and acceptable for a test.
	localPort := reserveLocalPort(t)

	dir := t.TempDir()
	cfgPath := dir + "/tunnels.toml"
	require.NoError(t, os.WriteFile(cfgPath, []byte(fmt.Sprintf(`
[tunnels.echo]
host = "%s"
type = "local"
local_port = %d
remote_host = "%s"
remote_port = %s
`, host, localPort, echoHost(echoAddr), echoPort(echoAddr))), 0o600))

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
	}, 5*time.Second, 50*time.Millisecond, "tunnel forwarding never succeeded")

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

// reserveLocalPort binds an ephemeral localhost port, reads the chosen
// port number, and immediately closes the listener. The caller then
// re-binds that port. Small race, but localhost-only and used solely
// to satisfy the config validator (which rejects port 0) without
// hard-coding a number.
func reserveLocalPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	_, p, err := net.SplitHostPort(ln.Addr().String())
	require.NoError(t, err)
	require.NoError(t, ln.Close())
	pi, err := strconv.Atoi(p)
	require.NoError(t, err)
	return pi
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
