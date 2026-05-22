package manager

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"

	"github.com/Sshiitake/sshiitake/internal/config"
	"github.com/Sshiitake/sshiitake/internal/tunnel"
)

// TestToggle_stopsRunningTunnel: a tunnel that is up is taken down by Toggle.
func TestToggle_stopsRunningTunnel(t *testing.T) {
	sshAddr, hostKey := startTestSSHServer(t)
	echo := startEchoServer(t)

	host, port := splitHostPort(t, sshAddr)
	cfg := &config.Config{
		Tunnels: map[string]config.Tunnel{
			"e": {Host: host, Type: config.TypeLocal, LocalPort: 0,
				RemoteHost: echoHost(echo), RemotePort: echoPort(echo)},
		},
		Groups: map[string]config.Group{},
	}
	sshCfgPath := writeTempSSHConfig(t, host, port)

	m, err := New(cfg, sshCfgPath, Options{
		HostKeyCallback: ssh.FixedHostKey(hostKey),
	})
	require.NoError(t, err)
	ch := m.Subscribe(64)
	defer m.Unsubscribe(ch)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tracker := newTunnelStateTracker(ctx, ch)
	runErr := runManagerInBackground(t, m, ctx)

	require.True(t, tracker.waitForStatus("e", tunnel.StatusUp, 3*time.Second))

	require.NoError(t, m.Toggle("e"))
	require.True(t, tracker.waitForLatestStatus("e", tunnel.StatusDown, 3*time.Second))

	cancel()
	<-runErr
}

// TestToggle_restartsStoppedTunnel: a tunnel that was stopped by Toggle is
// restarted by a second Toggle.
func TestToggle_restartsStoppedTunnel(t *testing.T) {
	sshAddr, hostKey := startTestSSHServer(t)
	echo := startEchoServer(t)

	host, port := splitHostPort(t, sshAddr)
	cfg := &config.Config{
		Tunnels: map[string]config.Tunnel{
			"e": {Host: host, Type: config.TypeLocal, LocalPort: 0,
				RemoteHost: echoHost(echo), RemotePort: echoPort(echo)},
		},
		Groups: map[string]config.Group{},
	}
	sshCfgPath := writeTempSSHConfig(t, host, port)

	m, err := New(cfg, sshCfgPath, Options{
		HostKeyCallback: ssh.FixedHostKey(hostKey),
	})
	require.NoError(t, err)
	ch := m.Subscribe(64)
	defer m.Unsubscribe(ch)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tracker := newTunnelStateTracker(ctx, ch)
	runErr := runManagerInBackground(t, m, ctx)

	require.True(t, tracker.waitForStatus("e", tunnel.StatusUp, 3*time.Second))

	// Stop.
	require.NoError(t, m.Toggle("e"))
	require.True(t, tracker.waitForLatestStatus("e", tunnel.StatusDown, 3*time.Second))

	// Restart.
	require.NoError(t, m.Toggle("e"))
	require.True(t, tracker.waitForLatestStatus("e", tunnel.StatusUp, 3*time.Second))

	// Confirm the restarted tunnel actually forwards traffic.
	tuns := m.Tunnels()
	require.Len(t, tuns, 1)
	c, err := net.Dial("tcp", tuns[0].LocalAddr())
	require.NoError(t, err)
	_, _ = c.Write([]byte("ping"))
	buf := make([]byte, 4)
	_, _ = c.Read(buf)
	assert.Equal(t, "ping", string(buf))
	_ = c.Close()

	cancel()
	<-runErr
}

// TestToggle_unknownNameErrors: Toggle on a name that's not in the handle
// set returns a not-found error.
func TestToggle_unknownNameErrors(t *testing.T) {
	cfg := &config.Config{Tunnels: map[string]config.Tunnel{}}
	m, err := New(cfg, "", Options{})
	require.NoError(t, err)

	// Pretend Run was entered.
	m.mu.Lock()
	m.runCtx = context.Background()
	m.mu.Unlock()

	err = m.Toggle("nope")
	require.Error(t, err)
	assert.ErrorContains(t, err, "not found")
}

// TestToggle_beforeRunFails: programming-error sanity check.
func TestToggle_beforeRunFails(t *testing.T) {
	cfg := &config.Config{
		Tunnels: map[string]config.Tunnel{
			"e": {Host: "h", Type: config.TypeLocal, LocalPort: 1234,
				RemoteHost: "127.0.0.1", RemotePort: 5678},
		},
	}
	m, err := New(cfg, "", Options{})
	require.NoError(t, err)

	err = m.Toggle("e")
	require.Error(t, err)
	assert.ErrorContains(t, err, "has not been Run")
}
