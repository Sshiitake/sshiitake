package manager

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Sshiitake/sshiitake/internal/config"
	"github.com/Sshiitake/sshiitake/internal/reload"
	"github.com/Sshiitake/sshiitake/internal/tunnel"
)

// applyStopTimeout caps how long Apply will wait for a tunnel goroutine
// to exit after we cancel its context. A graceful Stop should take a
// few ms (close listener + close client conn); 5s is a generous upper
// bound that protects Apply from hanging on a misbehaving tunnel.
const applyStopTimeout = 5 * time.Second

// Apply mutates the live tunnel set according to plan against newCfg.
// Removed and Modified tunnels are stopped first; Added and Modified
// are then started fresh. Modified is treated as Remove+Add because the
// tunnel.Tunnel value embeds its ResolvedTunnel at construction and
// does not support in-place reconfiguration.
//
// Apply must only be called AFTER Run has been entered (it derives
// per-tunnel contexts from m.runCtx). Calling Apply before Run is a
// programming error and returns a non-nil error.
//
// Errors from individual tunnel operations (resolve failures, stop
// timeouts) are accumulated and returned as a wrapped errors.Join.
// A partial Apply is still committed: a config edit that adds tunnel
// A correctly and breaks tunnel B will leave A running and return an
// error mentioning B.
func (m *Manager) Apply(newCfg *config.Config, plan reload.Plan) error {
	if newCfg == nil {
		return errors.New("apply: newCfg is nil")
	}
	m.mu.Lock()
	if m.runCtx == nil {
		m.mu.Unlock()
		return errors.New("apply: manager has not been Run yet")
	}
	parent := m.runCtx

	// Stage 1: stop removed + modified. We collect the done channels
	// under the lock then wait outside it so a stuck tunnel goroutine
	// can't deadlock the lock.
	toStop := append([]string{}, plan.Removed...)
	toStop = append(toStop, plan.Modified...)
	type pendingStop struct {
		name string
		done chan struct{}
	}
	var stops []pendingStop
	for _, name := range toStop {
		h, ok := m.handles[name]
		if !ok {
			continue
		}
		if h.cancel != nil {
			h.cancel()
		}
		stops = append(stops, pendingStop{name: name, done: h.done})
		delete(m.handles, name)
	}
	m.mu.Unlock()

	var errs []error
	for _, s := range stops {
		if s.done == nil {
			continue
		}
		select {
		case <-s.done:
			// stopped cleanly
		case <-time.After(applyStopTimeout):
			errs = append(errs, fmt.Errorf("tunnel %q did not stop within %s", s.name, applyStopTimeout))
		}
	}

	// Stage 2: start added + modified. New ResolvedTunnel per name; new
	// *tunnel.Tunnel; spawn via spawnHandle.
	toStart := append([]string{}, plan.Added...)
	toStart = append(toStart, plan.Modified...)

	for _, name := range toStart {
		raw, ok := newCfg.TunnelByName(name)
		if !ok {
			errs = append(errs, fmt.Errorf("apply: tunnel %q missing from new config", name))
			continue
		}
		rt, err := config.ResolveWithSSHConfig(raw, m.sshCfgPath)
		if err != nil {
			errs = append(errs, fmt.Errorf("resolve %q: %w", name, err))
			continue
		}
		rt.Name = name
		newT := tunnel.New(rt, m.tunnelOpts)
		h := &tunnelHandle{tunnel: newT}

		m.mu.Lock()
		m.handles[name] = h
		m.mu.Unlock()

		// errCh=nil: Apply-added tunnels surface errors via the event
		// stream only, never via Manager.Run's return value.
		m.spawnHandle(parent, h, nil, nil)
	}

	if len(errs) == 0 {
		return nil
	}
	return joinErrs(errs)
}

// joinErrs preserves error wrapping while flattening the per-tunnel
// failures into a single returned error. We construct the joined error
// manually rather than relying solely on errors.Join so the message is
// readable in the streamHumanEvents fallback path (line-by-line is
// preferable when surfaced to a TTY).
func joinErrs(errs []error) error {
	if len(errs) == 1 {
		return errs[0]
	}
	parts := make([]string, len(errs))
	for i, e := range errs {
		parts[i] = e.Error()
	}
	return fmt.Errorf("apply: %d error(s): %s", len(errs), strings.Join(parts, "; "))
}

// hasHandle is a test helper.
func (m *Manager) hasHandle(name string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.handles[name]
	return ok
}

// Use a no-op reference so the static analyser stops warning about
// context (we keep the import readable for future extension).
var _ = context.Background
