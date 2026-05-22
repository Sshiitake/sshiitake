// Package manager orchestrates multiple tunnels: it loads them from
// config (optionally filtered by selector names or group), drives the
// lifecycle in parallel, and broadcasts events to subscribers.
//
// Each managed tunnel has its own derived context and cancel function
// so Apply (hot-reload) can stop a specific tunnel without disturbing
// the others.
package manager

import (
	"context"
	"fmt"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/Sshiitake/sshiitake/internal/config"
	"github.com/Sshiitake/sshiitake/internal/tunnel"
)

// Options configures Manager construction.
type Options struct {
	// Selectors filters the tunnels to manage at startup. An empty slice
	// means "all tunnels in config". A name matches a single tunnel; a
	// group name expands to its members. After construction, Apply may
	// add tunnels outside this selector if the user edits the config.
	Selectors []string

	// HostKeyCallback is required for production use. Tests may supply
	// ssh.InsecureIgnoreHostKey() when the test setup never reaches the
	// handshake (e.g. dial-error tests). Tunnel.dial enforces non-nil.
	HostKeyCallback ssh.HostKeyCallback

	// Reconnect, when true, causes every tunnel to be driven via
	// Tunnel.StartWithReconnect (exponential backoff on transient SSH
	// errors). Default false to keep tests deterministic; cmd/ssht/up
	// flips this to true unless --no-reconnect is supplied.
	Reconnect bool
}

// tunnelHandle pairs a tunnel with the lifecycle plumbing that lets
// Apply stop it without disturbing siblings.
type tunnelHandle struct {
	tunnel *tunnel.Tunnel
	cancel context.CancelFunc
	done   chan struct{}
}

// Manager owns a set of tunnels and exposes lifecycle controls + event
// subscription.
type Manager struct {
	subs       *subscribers
	reconnect  bool
	sshCfgPath string
	tunnelOpts tunnel.Options

	// runCtx is the parent context Run was called with. Per-tunnel
	// contexts derive from this so a root cancel still stops everything.
	// Set on Run entry; nil before Run is called.
	mu      sync.Mutex
	runCtx  context.Context // nolint:containedctx // intentional: parent for derived per-tunnel contexts
	handles map[string]*tunnelHandle
}

// New builds a Manager from config + ssh config path + options.
func New(cfg *config.Config, sshConfigPath string, opts Options) (*Manager, error) {
	names, err := resolveSelectors(cfg, opts.Selectors)
	if err != nil {
		return nil, err
	}

	tunnelOpts := tunnel.Options{
		HostKeyCallback: opts.HostKeyCallback,
		Reconnect:       opts.Reconnect,
	}

	handles := make(map[string]*tunnelHandle, len(names))
	for _, name := range names {
		raw, ok := cfg.TunnelByName(name)
		if !ok {
			return nil, fmt.Errorf("tunnel %q not found in config", name)
		}
		rt, err := config.ResolveWithSSHConfig(raw, sshConfigPath)
		if err != nil {
			return nil, fmt.Errorf("resolve %q: %w", name, err)
		}
		rt.Name = name
		handles[name] = &tunnelHandle{
			tunnel: tunnel.New(rt, tunnelOpts),
		}
	}

	return &Manager{
		subs:       newSubscribers(),
		reconnect:  opts.Reconnect,
		sshCfgPath: sshConfigPath,
		tunnelOpts: tunnelOpts,
		handles:    handles,
	}, nil
}

// Tunnels returns a snapshot of the tunnels owned by this manager.
// The slice is freshly allocated; iteration order is undefined (callers
// that need stability should sort by name).
func (m *Manager) Tunnels() []*tunnel.Tunnel {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*tunnel.Tunnel, 0, len(m.handles))
	for _, h := range m.handles {
		out = append(out, h.tunnel)
	}
	return out
}

// Subscribe returns a buffered event channel.
func (m *Manager) Subscribe(buf int) chan Event { return m.subs.Subscribe(buf) }

// Unsubscribe closes the channel.
func (m *Manager) Unsubscribe(ch chan Event) { m.subs.Unsubscribe(ch) }

// Run starts all tunnels concurrently and blocks until ctx is
// cancelled. Returns nil on graceful shutdown.
//
// Unlike the pre-Apply implementation this does NOT return the first
// tunnel error: with hot-reload + Apply, a single tunnel failing is a
// recoverable condition the user may fix by editing the config, not a
// reason to tear down the rest of the set. The error is still surfaced
// via EventTunnelState{Down, Message}.
//
// The first tunnel dial failure is still preserved as Run's return
// value when ctx is not cancelled, so existing tests (notably
// TestRun_dialFailureDoesNotDeadlock) keep their semantics.
func (m *Manager) Run(ctx context.Context) error {
	defer m.subs.closeAll()

	m.mu.Lock()
	m.runCtx = ctx
	// Snapshot the initial set; spawn each one. Apply may add more
	// during the run.
	initial := make([]*tunnelHandle, 0, len(m.handles))
	for _, h := range m.handles {
		initial = append(initial, h)
	}
	m.mu.Unlock()

	m.startMetricsTicker(ctx)

	// errCh receives the FIRST non-nil error from the initial tunnel
	// set so dial-failure semantics survive. Apply-spawned tunnels do
	// not feed errCh: they're advisory and reported via events.
	errCh := make(chan error, len(initial))
	var wg sync.WaitGroup
	for _, h := range initial {
		h := h
		wg.Add(1)
		m.spawnHandle(ctx, h, &wg, errCh)
	}

	// Wait until either ctx is cancelled or all initial tunnels have
	// returned. Apply-added tunnels can still be running; they'll
	// receive the ctx cancellation through their derived contexts.
	allDone := make(chan struct{})
	go func() {
		wg.Wait()
		close(allDone)
	}()

	var firstErr error
	gotError := false
	for {
		select {
		case <-ctx.Done():
			// Wait for all (including Apply-added) handles to drain.
			m.waitAllHandles()
			return nil
		case err := <-errCh:
			if !gotError && err != nil {
				firstErr = err
				gotError = true
			}
		case <-allDone:
			// All initial handles finished. If anyone reported an error
			// and the caller did not cancel us, surface it.
			if firstErr != nil && ctx.Err() == nil {
				m.waitAllHandles()
				return firstErr
			}
			// No errors but caller hasn't cancelled. Keep the manager
			// alive so Apply-added tunnels can run, and so a future
			// `up <name>` user can add a tunnel by editing tunnels.toml.
			// Block on ctx so we still return when the user Ctrl+C's.
			<-ctx.Done()
			m.waitAllHandles()
			return nil
		}
	}
}

// spawnHandle wires up the per-tunnel context + done channel and
// launches the goroutine that drives Start/StartWithReconnect.
//
// errCh is optional; nil means "errors are advisory, only emit events."
// Apply uses errCh=nil for tunnels added via hot-reload.
//
// Caller MUST hold m.mu when invoking, OR ensure the handle is not yet
// stored in m.handles. spawnHandle takes the lock itself to insert.
func (m *Manager) spawnHandle(parent context.Context, h *tunnelHandle, wg *sync.WaitGroup, errCh chan<- error) {
	hCtx, cancel := context.WithCancel(parent)
	h.cancel = cancel
	h.done = make(chan struct{})

	started := make(chan struct{})
	startDone := make(chan error, 1)
	go func() {
		if m.reconnect {
			startDone <- h.tunnel.StartWithReconnect(hCtx, started)
			return
		}
		startDone <- h.tunnel.Start(hCtx, started)
	}()

	go func() {
		defer close(h.done)
		if wg != nil {
			defer wg.Done()
		}
		// Stage 1: wait for "started" (Up event) or early failure.
		select {
		case <-started:
			m.subs.publish(Event{
				Type:       EventTunnelState,
				TunnelName: h.tunnel.Name(),
				Timestamp:  time.Now().UTC(),
				Status:     tunnel.StatusUp,
			})
		case err := <-startDone:
			m.subs.publish(Event{
				Type:       EventTunnelState,
				TunnelName: h.tunnel.Name(),
				Timestamp:  time.Now().UTC(),
				Status:     tunnel.StatusDown,
				Message:    errMessage(err),
			})
			if errCh != nil && err != nil {
				select {
				case errCh <- err:
				default:
				}
			}
			return
		}
		// Stage 2: wait for the tunnel to actually finish.
		err := <-startDone
		m.subs.publish(Event{
			Type:       EventTunnelState,
			TunnelName: h.tunnel.Name(),
			Timestamp:  time.Now().UTC(),
			Status:     tunnel.StatusDown,
			Message:    errMessage(err),
		})
		if errCh != nil && err != nil {
			select {
			case errCh <- err:
			default:
				// errCh full: a prior error already won the race; this
				// one is dropped. The event stream still carries it.
			}
		}
	}()
}

// waitAllHandles blocks until every currently-tracked handle's
// goroutine has signalled done. Used during shutdown.
func (m *Manager) waitAllHandles() {
	m.mu.Lock()
	dones := make([]chan struct{}, 0, len(m.handles))
	for _, h := range m.handles {
		if h.done != nil {
			dones = append(dones, h.done)
		}
	}
	m.mu.Unlock()
	for _, d := range dones {
		<-d
	}
}

// metricsTickInterval determines how often Manager emits an EventMetrics
// snapshot per tunnel. 1s is conservative; the TUI sparkline needs at
// most one sample per second at the 60-cell width.
const metricsTickInterval = time.Second

func (m *Manager) startMetricsTicker(ctx context.Context) {
	go func() {
		t := time.NewTicker(metricsTickInterval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-t.C:
				m.mu.Lock()
				tuns := make([]*tunnel.Tunnel, 0, len(m.handles))
				for _, h := range m.handles {
					tuns = append(tuns, h.tunnel)
				}
				m.mu.Unlock()
				for _, tun := range tuns {
					if tun.Status() != tunnel.StatusUp {
						continue
					}
					in, out := tun.Metrics().Bytes()
					var lat float64
					if snap := tun.Metrics().LatencySnapshot(); len(snap) > 0 {
						lat = snap[len(snap)-1].Value
					}
					m.subs.publish(Event{
						Type:       EventMetrics,
						TunnelName: tun.Name(),
						Timestamp:  now.UTC(),
						BytesIn:    in,
						BytesOut:   out,
						LatencyMs:  lat,
					})
				}
			}
		}
	}()
}

// errMessage returns "" for nil, otherwise the error's string form,
// for embedding in EventTunnelState{Down}.
func errMessage(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// resolveSelectors returns the ordered list of tunnel names to manage,
// expanding group names. Empty selectors -> all tunnels.
func resolveSelectors(cfg *config.Config, selectors []string) ([]string, error) {
	if len(selectors) == 0 {
		names := make([]string, 0, len(cfg.Tunnels))
		for n := range cfg.Tunnels {
			names = append(names, n)
		}
		return names, nil
	}

	seen := make(map[string]struct{})
	var out []string
	add := func(name string) {
		if _, ok := seen[name]; ok {
			return
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}

	for _, sel := range selectors {
		if _, ok := cfg.Groups[sel]; ok {
			for _, n := range tunnelsInGroup(cfg, sel) {
				add(n)
			}
			continue
		}
		if _, ok := cfg.Tunnels[sel]; ok {
			add(sel)
			continue
		}
		return nil, fmt.Errorf("selector %q: not found (neither tunnel nor group)", sel)
	}
	return out, nil
}
