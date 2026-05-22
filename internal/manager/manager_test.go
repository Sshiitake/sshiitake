package manager

import (
	"context"
	"io"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"

	"github.com/Sshiitake/sshiitake/internal/config"
	"github.com/Sshiitake/sshiitake/internal/tunnel"
)

func TestNew_emptyConfig(t *testing.T) {
	m, err := New(&config.Config{Tunnels: map[string]config.Tunnel{}}, "", Options{})
	require.NoError(t, err)
	require.NotNil(t, m)
	assert.Len(t, m.Tunnels(), 0)
}

func TestNew_singleTunnelLoaded(t *testing.T) {
	cfg := &config.Config{
		Tunnels: map[string]config.Tunnel{
			"api": {Host: "localhost", Type: config.TypeLocal,
				LocalPort: 18443, RemoteHost: "127.0.0.1", RemotePort: 80},
		},
	}
	m, err := New(cfg, "", Options{HostKeyVerification: false})
	require.NoError(t, err)
	assert.Len(t, m.Tunnels(), 1)
	assert.Equal(t, "api", m.Tunnels()[0].Name())
}

func TestNew_unknownNameFails(t *testing.T) {
	cfg := &config.Config{Tunnels: map[string]config.Tunnel{}}
	_, err := New(cfg, "", Options{Selectors: []string{"nope"}})
	require.Error(t, err)
	assert.ErrorContains(t, err, "not found")
}

// TestRun_multipleTunnelsStartConcurrently constructs an in-process
// SSH server, two tunnels pointing at two echo servers, runs the
// manager, exercises both tunnels, then cancels the context and
// confirms graceful shutdown.
func TestRun_multipleTunnelsStartConcurrently(t *testing.T) {
	sshAddr, hostKey := startTestSSHServer(t)
	echo1 := startEchoServer(t)
	echo2 := startEchoServer(t)

	host, port := splitHostPort(t, sshAddr)
	cfg := &config.Config{
		Tunnels: map[string]config.Tunnel{
			"echo1": {Host: host, Type: config.TypeLocal, LocalPort: 0,
				RemoteHost: echoHost(echo1), RemotePort: echoPort(echo1)},
			"echo2": {Host: host, Type: config.TypeLocal, LocalPort: 0,
				RemoteHost: echoHost(echo2), RemotePort: echoPort(echo2)},
		},
		Groups: map[string]config.Group{},
	}
	// Inline ssh-config: map "host:port" alias to itself, override SSHPort.
	sshCfgPath := writeTempSSHConfig(t, host, port)

	m, err := New(cfg, sshCfgPath, Options{
		HostKeyCallback:     ssh.FixedHostKey(hostKey),
		HostKeyVerification: true,
	})
	require.NoError(t, err)
	require.Len(t, m.Tunnels(), 2)

	ch := m.Subscribe(64)
	defer m.Unsubscribe(ch)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runErr := make(chan error, 1)
	go func() { runErr <- m.Run(ctx) }()

	// Wait for both tunnels to report Up via the event stream.
	upCount := 0
	deadline := time.After(5 * time.Second)
	for upCount < 2 {
		select {
		case e := <-ch:
			if e.Type == EventTunnelState && e.Status == tunnel.StatusUp {
				upCount++
			}
		case <-deadline:
			t.Fatalf("only %d tunnels reported Up", upCount)
		}
	}

	// Both tunnels should now be usable.
	for _, tun := range m.Tunnels() {
		require.NotEmpty(t, tun.LocalAddr())
		c, err := net.Dial("tcp", tun.LocalAddr())
		require.NoError(t, err)
		_, err = c.Write([]byte("ping"))
		require.NoError(t, err)
		buf := make([]byte, 4)
		_, err = io.ReadFull(c, buf)
		require.NoError(t, err)
		require.Equal(t, "ping", string(buf))
		_ = c.Close()
	}

	cancel()
	select {
	case err := <-runErr:
		require.NoError(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("manager did not stop")
	}
}

func TestRun_emitsMetricsEvents(t *testing.T) {
	sshAddr, hostKey := startTestSSHServer(t)
	echo := startEchoServer(t)
	host, port := splitHostPort(t, sshAddr)
	cfg := &config.Config{
		Tunnels: map[string]config.Tunnel{
			"e": {Host: host, Type: config.TypeLocal, LocalPort: 0,
				RemoteHost: echoHost(echo), RemotePort: echoPort(echo)},
		},
	}
	m, err := New(cfg, writeTempSSHConfig(t, host, port), Options{
		HostKeyCallback: ssh.FixedHostKey(hostKey), HostKeyVerification: true,
	})
	require.NoError(t, err)

	ch := m.Subscribe(64)
	defer m.Unsubscribe(ch)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runErr := make(chan error, 1)
	go func() { runErr <- m.Run(ctx) }()

	// Wait for Up.
	requireEventEventually(t, ch, EventTunnelState, tunnel.StatusUp, 3*time.Second)

	// Push traffic so metrics actually have something to report.
	c, err := net.Dial("tcp", m.Tunnels()[0].LocalAddr())
	require.NoError(t, err)
	_, _ = c.Write(make([]byte, 1024))
	buf := make([]byte, 1024)
	_, _ = io.ReadFull(c, buf)
	_ = c.Close()

	// Expect at least one EventMetrics within a reasonable window.
	deadline := time.After(3 * time.Second)
	for {
		select {
		case e := <-ch:
			if e.Type == EventMetrics && e.BytesIn > 0 {
				cancel()
				<-runErr
				return
			}
		case <-deadline:
			t.Fatal("no EventMetrics with bytes received")
		}
	}
}

func requireEventEventually(t *testing.T, ch chan Event, et EventType, st tunnel.Status, dur time.Duration) {
	t.Helper()
	deadline := time.After(dur)
	for {
		select {
		case e := <-ch:
			if e.Type == et && e.Status == st {
				return
			}
		case <-deadline:
			t.Fatalf("did not see %v / %v", et, st)
		}
	}
}
