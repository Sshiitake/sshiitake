package manager

import (
	"context"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"

	"github.com/Sshiitake/sshiitake/internal/config"
	"github.com/Sshiitake/sshiitake/internal/reload"
	"github.com/Sshiitake/sshiitake/internal/tunnel"
)

// runManagerInBackground starts m.Run(ctx) in a goroutine and returns
// a channel that yields the eventual error. Cleans up on test exit.
func runManagerInBackground(t *testing.T, m *Manager, ctx context.Context) chan error {
	t.Helper()
	errCh := make(chan error, 1)
	go func() { errCh <- m.Run(ctx) }()
	t.Cleanup(func() {
		// Drain in case the test cancelled and forgot to read.
		select {
		case <-errCh:
		case <-time.After(3 * time.Second):
		}
	})
	return errCh
}

// tunnelStateTracker records every TunnelState event received on ch so
// tests can assert ordering and presence without depending on the order
// events arrive in (Up events for two concurrent tunnels can interleave
// either way, and a one-event-per-call select would discard the "other"
// tunnel's event).
type tunnelStateTracker struct {
	mu   *sync.Mutex
	seen map[string][]tunnel.Status
}

func newTunnelStateTracker(ctx context.Context, ch chan Event) *tunnelStateTracker {
	t := &tunnelStateTracker{
		mu:   &sync.Mutex{},
		seen: map[string][]tunnel.Status{},
	}
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case e, ok := <-ch:
				if !ok {
					return
				}
				if e.Type != EventTunnelState {
					continue
				}
				t.mu.Lock()
				t.seen[e.TunnelName] = append(t.seen[e.TunnelName], e.Status)
				t.mu.Unlock()
			}
		}
	}()
	return t
}

func (t *tunnelStateTracker) waitForStatus(name string, status tunnel.Status, dur time.Duration) bool {
	deadline := time.After(dur)
	for {
		t.mu.Lock()
		for _, s := range t.seen[name] {
			if s == status {
				t.mu.Unlock()
				return true
			}
		}
		t.mu.Unlock()
		select {
		case <-deadline:
			return false
		case <-time.After(10 * time.Millisecond):
		}
	}
}

// waitForLatestStatus waits until the MOST RECENT status recorded for
// name equals status. Use when checking a tunnel has transitioned away
// from a previous state (e.g. restart-after-Apply: first Down, then Up
// again, must surface as latest=Up).
func (t *tunnelStateTracker) waitForLatestStatus(name string, status tunnel.Status, dur time.Duration) bool {
	deadline := time.After(dur)
	for {
		t.mu.Lock()
		hist := t.seen[name]
		if len(hist) > 0 && hist[len(hist)-1] == status {
			t.mu.Unlock()
			return true
		}
		t.mu.Unlock()
		select {
		case <-deadline:
			return false
		case <-time.After(10 * time.Millisecond):
		}
	}
}

// statusCount returns how many times the given status appears in name's
// history. Used to assert "saw Down at least once" after Apply removes
// a tunnel.
func (t *tunnelStateTracker) statusCount(name string, status tunnel.Status) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	n := 0
	for _, s := range t.seen[name] {
		if s == status {
			n++
		}
	}
	return n
}

// TestApply_addsNewTunnel: start with one tunnel, Apply adds a second.
func TestApply_addsNewTunnel(t *testing.T) {
	sshAddr, hostKey := startTestSSHServer(t)
	echo1 := startEchoServer(t)
	echo2 := startEchoServer(t)

	host, port := splitHostPort(t, sshAddr)
	initial := &config.Config{
		Tunnels: map[string]config.Tunnel{
			"echo1": {Host: host, Type: config.TypeLocal, LocalPort: 0,
				RemoteHost: echoHost(echo1), RemotePort: echoPort(echo1)},
		},
		Groups: map[string]config.Group{},
	}
	sshCfgPath := writeTempSSHConfig(t, host, port)

	m, err := New(initial, sshCfgPath, Options{
		HostKeyCallback: ssh.FixedHostKey(hostKey),
	})
	require.NoError(t, err)
	ch := m.Subscribe(64)
	defer m.Unsubscribe(ch)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tracker := newTunnelStateTracker(ctx, ch)
	runErr := runManagerInBackground(t, m, ctx)

	require.True(t, tracker.waitForStatus("echo1", tunnel.StatusUp, 3*time.Second))

	// Now Apply a new tunnel.
	newCfg := &config.Config{
		Tunnels: map[string]config.Tunnel{
			"echo1": initial.Tunnels["echo1"],
			"echo2": {Host: host, Type: config.TypeLocal, LocalPort: 0,
				RemoteHost: echoHost(echo2), RemotePort: echoPort(echo2)},
		},
		Groups: map[string]config.Group{},
	}
	plan := reload.Diff(initial, newCfg)
	require.Equal(t, []string{"echo2"}, plan.Added)
	require.NoError(t, m.Apply(newCfg, plan))

	require.True(t, tracker.waitForStatus("echo2", tunnel.StatusUp, 3*time.Second))

	// Both tunnels usable.
	tuns := m.Tunnels()
	require.Len(t, tuns, 2)
	for _, tun := range tuns {
		c, err := net.Dial("tcp", tun.LocalAddr())
		require.NoError(t, err, "dial %s", tun.Name())
		_, _ = c.Write([]byte("ping"))
		buf := make([]byte, 4)
		_, _ = c.Read(buf)
		assert.Equal(t, "ping", string(buf))
		_ = c.Close()
	}

	cancel()
	<-runErr
}

// TestApply_removesTunnel: start with two, Apply removes one.
func TestApply_removesTunnel(t *testing.T) {
	sshAddr, hostKey := startTestSSHServer(t)
	echo1 := startEchoServer(t)
	echo2 := startEchoServer(t)

	host, port := splitHostPort(t, sshAddr)
	initial := &config.Config{
		Tunnels: map[string]config.Tunnel{
			"echo1": {Host: host, Type: config.TypeLocal, LocalPort: 0,
				RemoteHost: echoHost(echo1), RemotePort: echoPort(echo1)},
			"echo2": {Host: host, Type: config.TypeLocal, LocalPort: 0,
				RemoteHost: echoHost(echo2), RemotePort: echoPort(echo2)},
		},
		Groups: map[string]config.Group{},
	}
	sshCfgPath := writeTempSSHConfig(t, host, port)

	m, err := New(initial, sshCfgPath, Options{
		HostKeyCallback: ssh.FixedHostKey(hostKey),
	})
	require.NoError(t, err)
	ch := m.Subscribe(64)
	defer m.Unsubscribe(ch)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tracker := newTunnelStateTracker(ctx, ch)
	runErr := runManagerInBackground(t, m, ctx)

	require.True(t, tracker.waitForStatus("echo1", tunnel.StatusUp, 3*time.Second))
	require.True(t, tracker.waitForStatus("echo2", tunnel.StatusUp, 3*time.Second))

	// Remove echo2.
	newCfg := &config.Config{
		Tunnels: map[string]config.Tunnel{
			"echo1": initial.Tunnels["echo1"],
		},
		Groups: map[string]config.Group{},
	}
	plan := reload.Diff(initial, newCfg)
	require.Equal(t, []string{"echo2"}, plan.Removed)
	require.NoError(t, m.Apply(newCfg, plan))

	require.True(t, tracker.waitForLatestStatus("echo2", tunnel.StatusDown, 3*time.Second))

	// echo1 still up; echo2 handle gone.
	assert.True(t, m.hasHandle("echo1"))
	assert.False(t, m.hasHandle("echo2"))

	cancel()
	<-runErr
}

// TestApply_modifiedTunnelRestartsWithNewConfig: change the remote port
// of an existing tunnel; the old goroutine stops and a new one starts.
func TestApply_modifiedTunnelRestartsWithNewConfig(t *testing.T) {
	sshAddr, hostKey := startTestSSHServer(t)
	echoA := startEchoServer(t)
	echoB := startEchoServer(t)

	host, port := splitHostPort(t, sshAddr)
	initial := &config.Config{
		Tunnels: map[string]config.Tunnel{
			"e": {Host: host, Type: config.TypeLocal, LocalPort: 0,
				RemoteHost: echoHost(echoA), RemotePort: echoPort(echoA)},
		},
		Groups: map[string]config.Group{},
	}
	sshCfgPath := writeTempSSHConfig(t, host, port)

	m, err := New(initial, sshCfgPath, Options{
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

	// Modify "e" to point at echoB.
	mod := initial.Tunnels["e"]
	mod.RemoteHost = echoHost(echoB)
	mod.RemotePort = echoPort(echoB)
	newCfg := &config.Config{
		Tunnels: map[string]config.Tunnel{"e": mod},
		Groups:  map[string]config.Group{},
	}
	plan := reload.Diff(initial, newCfg)
	require.Equal(t, []string{"e"}, plan.Modified)
	require.NoError(t, m.Apply(newCfg, plan))

	// Sequence: old "e" goes Down, then new "e" Up. Track via history
	// so we assert at least one Down was observed and the LATEST status
	// is Up.
	require.True(t, tracker.waitForLatestStatus("e", tunnel.StatusUp, 3*time.Second))
	assert.GreaterOrEqual(t, tracker.statusCount("e", tunnel.StatusDown), 1, "expected at least one Down between Up events")

	// Verify the live tunnel actually forwards to echoB by checking the
	// resolved remote address. We can't easily assert WHICH echo server
	// served the response (both just echo), but we can confirm the
	// connection works.
	tuns := m.Tunnels()
	require.Len(t, tuns, 1)
	c, err := net.Dial("tcp", tuns[0].LocalAddr())
	require.NoError(t, err)
	_, _ = c.Write([]byte("xyz"))
	buf := make([]byte, 3)
	_, _ = c.Read(buf)
	assert.Equal(t, "xyz", string(buf))
	_ = c.Close()

	cancel()
	<-runErr
}

// TestApply_emptyPlanIsNoop: a no-op Apply must not disturb running
// tunnels.
func TestApply_emptyPlanIsNoop(t *testing.T) {
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

	// Apply with an empty plan (same config in/out).
	plan := reload.Diff(cfg, cfg)
	require.True(t, plan.Empty())
	require.NoError(t, m.Apply(cfg, plan))

	// Confirm "e" is still up by checking its local addr is still set
	// and we can still dial it.
	tuns := m.Tunnels()
	require.Len(t, tuns, 1)
	assert.NotEmpty(t, tuns[0].LocalAddr())
	c, err := net.Dial("tcp", tuns[0].LocalAddr())
	require.NoError(t, err)
	_ = c.Close()

	cancel()
	<-runErr
}

// TestApply_beforeRunFails: programming-error sanity check.
func TestApply_beforeRunFails(t *testing.T) {
	cfg := &config.Config{Tunnels: map[string]config.Tunnel{}}
	m, err := New(cfg, "", Options{})
	require.NoError(t, err)
	err = m.Apply(cfg, reload.Plan{Added: []string{"x"}})
	require.Error(t, err)
	assert.ErrorContains(t, err, "has not been Run")
}

// TestApply_nilNewCfgErrors: defensive guard.
func TestApply_nilNewCfgErrors(t *testing.T) {
	cfg := &config.Config{Tunnels: map[string]config.Tunnel{}}
	m, err := New(cfg, "", Options{})
	require.NoError(t, err)
	// Pretend Run was entered.
	m.mu.Lock()
	m.runCtx = context.Background()
	m.mu.Unlock()
	err = m.Apply(nil, reload.Plan{})
	require.Error(t, err)
	assert.ErrorContains(t, err, "newCfg is nil")
}

// TestApply_handleAlwaysHasDoneChan asserts the spawnHandle invariant:
// every handle reachable through m.handles must have a non-nil done
// channel and cancel func. A regression in the publish-before-init
// ordering would surface here as a flaky false from
// allHandlesFullyInitialised under -race.
func TestApply_handleAlwaysHasDoneChan(t *testing.T) {
	sshAddr, hostKey := startTestSSHServer(t)
	echo := startEchoServer(t)
	host, port := splitHostPort(t, sshAddr)
	cfg := &config.Config{Tunnels: map[string]config.Tunnel{
		"seed": {Host: host, Type: config.TypeLocal, LocalPort: 0,
			RemoteHost: echoHost(echo), RemotePort: echoPort(echo)},
	}}
	m, err := New(cfg, writeTempSSHConfig(t, host, port), Options{
		HostKeyCallback: ssh.FixedHostKey(hostKey),
	})
	require.NoError(t, err)
	ch := m.Subscribe(64)
	defer m.Unsubscribe(ch)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tracker := newTunnelStateTracker(ctx, ch)
	runErr := runManagerInBackground(t, m, ctx)

	require.True(t, tracker.waitForStatus("seed", tunnel.StatusUp, 3*time.Second))

	// Hammer Apply with a steady stream of add/remove plans while
	// concurrently asserting the invariant. If a handle is ever
	// published before initHandle ran, we'll observe done==nil here.
	stop := make(chan struct{})
	go func() {
		i := 0
		for {
			select {
			case <-stop:
				return
			default:
			}
			name := fmt.Sprintf("dyn%d", i)
			i++
			newCfg := &config.Config{Tunnels: map[string]config.Tunnel{
				"seed": cfg.Tunnels["seed"],
				name: {Host: host, Type: config.TypeLocal, LocalPort: 0,
					RemoteHost: echoHost(echo), RemotePort: echoPort(echo)},
			}}
			_ = m.Apply(newCfg, reload.Plan{Added: []string{name}})
		}
	}()

	deadline := time.After(200 * time.Millisecond)
	for {
		select {
		case <-deadline:
			close(stop)
			cancel()
			<-runErr
			return
		default:
		}
		assert.True(t, m.allHandlesFullyInitialised(),
			"handle published before initHandle ran")
	}
}
