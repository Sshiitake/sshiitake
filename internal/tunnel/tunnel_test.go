package tunnel

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"

	"github.com/Sshiitake/sshiitake/internal/config"
)

func TestTunnel_dial_connects(t *testing.T) {
	addr, hostKey := newTestSSHServer(t)
	host, port := splitHostPort(t, addr)

	rt := config.ResolvedTunnel{
		Name:      "test",
		SSHHost:   host,
		SSHPort:   port,
		SSHUser:   "tester",
		Type:      config.TypeLocal,
		LocalHost: "127.0.0.1",
		LocalPort: 0,
	}
	opts := Options{
		HostKeyCallback: ssh.FixedHostKey(hostKey),
		DialTimeout:     2 * time.Second,
	}
	tun := New(rt, opts)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	client, err := tun.dial(ctx)
	require.NoError(t, err)
	require.NotNil(t, client)
	assert.NoError(t, client.Close())
}

func splitHostPort(t *testing.T, addr string) (string, int) {
	t.Helper()
	host, portStr, err := net.SplitHostPort(addr)
	require.NoError(t, err)
	p, err := strconv.Atoi(portStr)
	require.NoError(t, err)
	return host, p
}

func TestTunnel_lifecycle(t *testing.T) {
	sshAddr, hostKey := newTestSSHServer(t)
	host, port := splitHostPort(t, sshAddr)

	// Echo target.
	echo, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = echo.Close() })
	go func() {
		for {
			c, err := echo.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				_, _ = io.Copy(c, c)
				_ = c.Close()
			}(c)
		}
	}()

	rt := config.ResolvedTunnel{
		Name:       "echo",
		SSHHost:    host,
		SSHPort:    port,
		SSHUser:    "tester",
		Type:       config.TypeLocal,
		LocalHost:  "127.0.0.1",
		LocalPort:  0,
		RemoteAddr: echo.Addr().String(),
	}
	opts := Options{
		HostKeyCallback: ssh.FixedHostKey(hostKey),
		DialTimeout:     2 * time.Second,
	}
	tun := New(rt, opts)

	assert.Equal(t, StatusDown, tun.Status())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	started := make(chan struct{})
	errCh := make(chan error, 1)
	go func() {
		errCh <- tun.Start(ctx, started)
	}()

	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("tunnel did not start in 3s")
	}

	assert.Equal(t, StatusUp, tun.Status())

	c, err := net.Dial("tcp", tun.LocalAddr())
	require.NoError(t, err)
	_, _ = c.Write([]byte("ok"))
	buf := make([]byte, 2)
	_, err = io.ReadFull(c, buf)
	require.NoError(t, err)
	assert.Equal(t, "ok", string(buf))
	_ = c.Close()

	cancel()
	select {
	case err := <-errCh:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("tunnel did not stop")
	}
	assert.Equal(t, StatusDown, tun.Status())
}

// TestTunnel_dial_proxyJumpRefused verifies the HIGH fix from the
// Phase 1 red-team review: dial() must refuse a tunnel with a non-empty
// ProxyJump rather than silently bypass the bastion.
func TestTunnel_dial_proxyJumpRefused(t *testing.T) {
	addr, hostKey := newTestSSHServer(t)
	host, port := splitHostPort(t, addr)

	rt := config.ResolvedTunnel{
		Name:      "viajump",
		SSHHost:   host,
		SSHPort:   port,
		SSHUser:   "tester",
		ProxyJump: "bastion-prod", // <-- this is what should trip the guard
		Type:      config.TypeLocal,
		LocalHost: "127.0.0.1",
		LocalPort: 0,
	}
	opts := Options{
		HostKeyCallback: ssh.FixedHostKey(hostKey),
		DialTimeout:     2 * time.Second,
	}
	tun := New(rt, opts)
	_, err := tun.dial(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "ProxyJump")
	require.Contains(t, err.Error(), "Phase 2")
}

func TestTunnel_metricsAccessible(t *testing.T) {
	sshAddr, hostKey := newTestSSHServer(t)
	host, port := splitHostPort(t, sshAddr)

	echo, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = echo.Close() })
	go func() {
		for {
			c, err := echo.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				_, _ = io.Copy(c, c)
				_ = c.Close()
			}(c)
		}
	}()

	rt := config.ResolvedTunnel{
		Name: "echo", SSHHost: host, SSHPort: port, SSHUser: "tester",
		Type: config.TypeLocal, LocalHost: "127.0.0.1", LocalPort: 0,
		RemoteAddr: echo.Addr().String(),
	}
	tun := New(rt, Options{HostKeyCallback: ssh.FixedHostKey(hostKey), DialTimeout: 2 * time.Second})

	// Metrics tracker is non-nil even before Start.
	require.NotNil(t, tun.Metrics())

	in, out := tun.Metrics().Bytes()
	assert.Equal(t, uint64(0), in)
	assert.Equal(t, uint64(0), out)

	// Start, push bytes through, confirm counters tick up.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{})
	errCh := make(chan error, 1)
	go func() { errCh <- tun.Start(ctx, started) }()
	<-started

	c, err := net.Dial("tcp", tun.LocalAddr())
	require.NoError(t, err)
	_, _ = c.Write([]byte("hello"))
	buf := make([]byte, 5)
	_, err = io.ReadFull(c, buf)
	require.NoError(t, err)
	_ = c.Close()

	require.Eventually(t, func() bool {
		in, out := tun.Metrics().Bytes()
		return in >= 5 && out >= 5
	}, 2*time.Second, 50*time.Millisecond, "tracker did not accumulate via tunnel")

	cancel()
	<-errCh
}

// TestKeyAuth_refusesOverbroadPerms verifies that keyAuth refuses a
// private key whose file mode permits group/other access, matching
// OpenSSH's stricture.
func TestKeyAuth_refusesOverbroadPerms(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "key")
	// Generate a real RSA key so PEM parsing would succeed if perms
	// passed; this isolates the perm check as the failure cause.
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	pemBlock := &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(rsaKey)}
	require.NoError(t, os.WriteFile(keyPath, pem.EncodeToMemory(pemBlock), 0o644))

	_, err = keyAuth(keyPath)
	require.Error(t, err)
	assert.ErrorContains(t, err, "permissions too open")
}

// TestKeyAuth_errorDoesNotLeakPath ensures parse errors don't echo the
// full key path back to the user; the message stays generic.
func TestKeyAuth_errorDoesNotLeakPath(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "bad-key")
	require.NoError(t, os.WriteFile(keyPath, []byte("not a key"), 0o600))

	_, err := keyAuth(keyPath)
	require.Error(t, err)
	assert.NotContains(t, err.Error(), dir, "error should not leak the file path")
}

// TestBuildAuth_errorsWhenAllAttemptsFailed verifies that buildAuth
// surfaces a wrapped error when at least one auth source was attempted
// AND every attempt failed.
func TestBuildAuth_errorsWhenAllAttemptsFailed(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "")

	dir := t.TempDir()
	missingKey := filepath.Join(dir, "no-such-key")

	tun := &Tunnel{rt: config.ResolvedTunnel{IdentityFile: missingKey}}
	methods, _, err := tun.buildAuth()
	require.Error(t, err)
	assert.Empty(t, methods)
}

// TestBuildAuth_silentEmptyWhenNothingTried verifies that buildAuth
// stays silent (no error, no methods) when nothing was configured.
// The test SSH server uses NoClientAuth, so an empty methods slice is
// the desired outcome there.
func TestBuildAuth_silentEmptyWhenNothingTried(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "")
	tun := &Tunnel{rt: config.ResolvedTunnel{IdentityFile: ""}}
	methods, _, err := tun.buildAuth()
	require.NoError(t, err)
	assert.Empty(t, methods)
}

// TestClassifyAgentDialError covers the categorisation table and (most
// importantly) confirms that the socket path is never present in the
// returned category string. The category leaks into --bare JSON event
// streams; the path must not.
func TestClassifyAgentDialError(t *testing.T) {
	assert.Equal(t, "socket not found", classifyAgentDialError(errors.New("dial unix /tmp/foo: no such file or directory")))
	assert.Equal(t, "agent not listening", classifyAgentDialError(errors.New("connection refused")))
	assert.Equal(t, "permission denied", classifyAgentDialError(errors.New("permission denied")))
	assert.Equal(t, "connect failed", classifyAgentDialError(errors.New("some unexpected error")))
	assert.Equal(t, "ok", classifyAgentDialError(nil))
	// Most importantly: no socket path leaks through.
	out := classifyAgentDialError(errors.New("dial unix /run/user/1000/sock: no such file or directory"))
	assert.NotContains(t, out, "/run/user/1000")
}

// TestBuildAuth_agentErrorHidesSocketPath verifies the wrapping path:
// when SSH_AUTH_SOCK points at a missing socket and IdentityFile also
// fails, the surfaced error must NOT contain the socket path.
func TestBuildAuth_agentErrorHidesSocketPath(t *testing.T) {
	dir := t.TempDir()
	bogusSock := filepath.Join(dir, "missing.sock")
	t.Setenv("SSH_AUTH_SOCK", bogusSock)

	tun := &Tunnel{rt: config.ResolvedTunnel{IdentityFile: filepath.Join(dir, "missing-key")}}
	_, _, err := tun.buildAuth()
	require.Error(t, err)
	assert.NotContains(t, err.Error(), bogusSock, "socket path leaked through buildAuth error")
}
