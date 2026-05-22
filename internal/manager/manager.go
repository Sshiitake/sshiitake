// Package manager orchestrates multiple tunnels: it loads them from
// config (optionally filtered by selector names or group), drives the
// lifecycle in parallel, and broadcasts events to subscribers.
//
// Phase 2 covers structural support; the consumer wiring (CLI --bare
// stream, future TUI) is in cmd/ssht.
package manager

import (
	"context"
	"fmt"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/sync/errgroup"

	"github.com/Sshiitake/sshiitake/internal/config"
	"github.com/Sshiitake/sshiitake/internal/tunnel"
)

// Options configures Manager construction.
type Options struct {
	// Selectors filters the tunnels to manage. An empty slice means "all
	// tunnels in config". A name matches a single tunnel; a group name
	// expands to its members.
	Selectors []string

	// HostKeyCallback is required for production use. Tests can leave
	// it nil and set HostKeyVerification=false to skip dial-time host
	// key checks (those run inside Tunnel.dial).
	HostKeyCallback ssh.HostKeyCallback

	// HostKeyVerification, when false, lets the Manager construct
	// tunnels without a host-key callback. ONLY for tests.
	HostKeyVerification bool
}

// Manager owns a set of tunnels and exposes lifecycle controls + event
// subscription.
type Manager struct {
	tunnels []*tunnel.Tunnel
	subs    *subscribers
}

// New builds a Manager from config + ssh config path + options.
func New(cfg *config.Config, sshConfigPath string, opts Options) (*Manager, error) {
	names, err := resolveSelectors(cfg, opts.Selectors)
	if err != nil {
		return nil, err
	}

	tunnels := make([]*tunnel.Tunnel, 0, len(names))
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
		tunnels = append(tunnels, tunnel.New(rt, tunnel.Options{
			HostKeyCallback: opts.HostKeyCallback,
		}))
	}

	return &Manager{
		tunnels: tunnels,
		subs:    newSubscribers(),
	}, nil
}

// Tunnels returns the tunnels owned by this manager. The slice is a
// shallow copy; mutating its order is fine.
func (m *Manager) Tunnels() []*tunnel.Tunnel {
	out := make([]*tunnel.Tunnel, len(m.tunnels))
	copy(out, m.tunnels)
	return out
}

// Subscribe returns a buffered event channel.
func (m *Manager) Subscribe(buf int) chan Event { return m.subs.Subscribe(buf) }

// Unsubscribe closes the channel.
func (m *Manager) Unsubscribe(ch chan Event) { m.subs.Unsubscribe(ch) }

// Run starts all tunnels concurrently and blocks until ctx is
// cancelled. Returns the first error from any tunnel that fails fatally
// (e.g. dial error); other tunnels are stopped before Run returns.
// On graceful shutdown (ctx cancel), returns nil.
func (m *Manager) Run(ctx context.Context) error {
	defer m.subs.closeAll()

	g, ctx := errgroup.WithContext(ctx)
	for _, t := range m.tunnels {
		t := t // capture
		g.Go(func() error {
			return m.runOne(ctx, t)
		})
	}
	if err := g.Wait(); err != nil && ctx.Err() == nil {
		return err
	}
	return nil
}

func (m *Manager) runOne(ctx context.Context, t *tunnel.Tunnel) error {
	started := make(chan struct{})

	stateCh := make(chan struct{})
	go func() {
		defer close(stateCh)
		// Watch for the started signal so we can emit the Up event.
		<-started
		m.subs.publish(Event{
			Type:       EventTunnelState,
			TunnelName: t.Name(),
			Timestamp:  time.Now().UTC(),
			Status:     tunnel.StatusUp,
		})
	}()

	err := t.Start(ctx, started)

	// Emit Down state (regardless of error vs graceful).
	m.subs.publish(Event{
		Type:       EventTunnelState,
		TunnelName: t.Name(),
		Timestamp:  time.Now().UTC(),
		Status:     tunnel.StatusDown,
	})

	<-stateCh // make sure the stateCh goroutine is collected
	return err
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
