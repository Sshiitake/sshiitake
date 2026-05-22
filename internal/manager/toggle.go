package manager

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Sshiitake/sshiitake/internal/tunnel"
)

// toggleStopTimeout caps how long Toggle waits for a tunnel goroutine to
// exit after we cancel its context. Mirrors applyStopTimeout: a graceful
// Stop should take a few ms; 5s is a generous upper bound.
const toggleStopTimeout = 5 * time.Second

// Toggle starts or stops the named tunnel based on its current state.
//
// If the tunnel handle's goroutine is still running, Toggle cancels its
// context and waits up to toggleStopTimeout for it to exit. If the
// handle is stopped (done channel closed, or never started), Toggle
// constructs a fresh *tunnel.Tunnel from the retained ResolvedTunnel and
// spawns it.
//
// Toggle must only be called after Run has been entered (it derives the
// per-tunnel context from m.runCtx). Calling Toggle on an unknown name
// returns an error.
func (m *Manager) Toggle(name string) error {
	m.mu.Lock()
	if m.runCtx == nil {
		m.mu.Unlock()
		return errors.New("toggle: manager has not been Run yet")
	}
	h, ok := m.handles[name]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("toggle: tunnel %q not found", name)
	}
	parent := m.runCtx
	running := handleRunning(h)

	if running {
		// Capture the cancel + done under the lock; the wait happens
		// outside so a stuck goroutine cannot deadlock the manager.
		cancel := h.cancel
		done := h.done
		m.mu.Unlock()
		return waitForStop(name, cancel, done)
	}

	// Restart: reconstruct *tunnel.Tunnel from the retained
	// ResolvedTunnel and spawn under the parent context. initHandle
	// reassigns h.ctx + h.cancel + h.done atomically under the lock so
	// a concurrent Toggle / Apply / waitAllHandles always sees a fully
	// initialised handle.
	newT := tunnel.New(h.rt, m.tunnelOpts)
	h.tunnel = newT
	initHandle(parent, h)
	m.mu.Unlock()

	m.spawnHandle(h, nil, nil)
	return nil
}

// handleRunning reports whether h's goroutine is currently active.
// A handle is considered running when it has a non-nil done channel that
// has not yet been closed. A nil done channel means Toggle (or Apply)
// has not spawned this handle yet; a closed done channel means the
// previous spawn has fully exited.
func handleRunning(h *tunnelHandle) bool {
	if h.done == nil {
		return false
	}
	select {
	case <-h.done:
		return false
	default:
		return true
	}
}

// waitForStop cancels and waits for done up to toggleStopTimeout.
// Returns an error if the goroutine doesn't exit in time.
func waitForStop(name string, cancel context.CancelFunc, done <-chan struct{}) error {
	if cancel != nil {
		cancel()
	}
	if done == nil {
		return nil
	}
	select {
	case <-done:
		return nil
	case <-time.After(toggleStopTimeout):
		return fmt.Errorf("tunnel %q did not stop within %s", name, toggleStopTimeout)
	}
}
