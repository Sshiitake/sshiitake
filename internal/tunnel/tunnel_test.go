package tunnel

import (
	"context"
	"io"
	"net"
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
