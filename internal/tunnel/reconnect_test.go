package tunnel

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestBackoff_progression(t *testing.T) {
	b := newBackoff(BackoffOptions{
		InitialDelay: 1 * time.Second,
		MaxDelay:     60 * time.Second,
		Multiplier:   2,
		Jitter:       0,
		MaxAttempts:  20,
	})
	expected := []time.Duration{
		1 * time.Second,
		2 * time.Second,
		4 * time.Second,
		8 * time.Second,
		16 * time.Second,
		32 * time.Second,
		60 * time.Second, // capped
		60 * time.Second,
	}
	for i, want := range expected {
		got := b.next()
		assert.Equal(t, want, got, "step %d", i)
	}
}

func TestBackoff_jitterStaysInBounds(t *testing.T) {
	b := newBackoff(BackoffOptions{
		InitialDelay: 1 * time.Second,
		MaxDelay:     60 * time.Second,
		Multiplier:   2,
		Jitter:       0.5, // +/-50%
		MaxAttempts:  20,
	})
	for i := 0; i < 100; i++ {
		d := b.next()
		// First call returns InitialDelay +/- 50% so the lower bound is
		// 500ms and upper bound is 1.5s. We check the widest possible
		// envelope here (any single call within bounds; reset between
		// calls keeps each draw an independent first-step draw).
		assert.GreaterOrEqual(t, d, 500*time.Millisecond)
		assert.LessOrEqual(t, d, 1500*time.Millisecond)
		b.reset()
	}
}

func TestBackoff_resetReturnsToInitial(t *testing.T) {
	b := newBackoff(BackoffOptions{InitialDelay: time.Second, Multiplier: 2, Jitter: 0})
	b.next()
	b.next()
	b.next()
	b.reset()
	assert.Equal(t, time.Second, b.next())
}

func TestIsReconnectableError(t *testing.T) {
	assert.True(t, isReconnectableError(errors.New("EOF")))
	assert.True(t, isReconnectableError(errors.New("connection reset by peer")))
	assert.True(t, isReconnectableError(errors.New("ssh: handshake failed")))
	assert.False(t, isReconnectableError(nil))
	assert.False(t, isReconnectableError(errors.New("HostKeyCallback required")))
	assert.False(t, isReconnectableError(errors.New("ProxyJump=... is not yet supported")))
}

// TestIsReconnectableError_authRejection guards against the auth-rejection
// retry storm: crypto/ssh wraps server-side credential failures inside a
// "handshake failed" prefix, so the permanent tokens for auth rejection
// must win over the transient "handshake" substring.
func TestIsReconnectableError_authRejection(t *testing.T) {
	assert.False(t, isReconnectableError(errors.New("ssh: handshake failed: ssh: unable to authenticate, attempted methods [none publickey]")))
	assert.False(t, isReconnectableError(errors.New("ssh: handshake failed: no supported methods remain")))
}

// TestIsReconnectableError_handshakeTimeout confirms a genuinely transient
// network-layer handshake failure still classifies as reconnectable.
func TestIsReconnectableError_handshakeTimeout(t *testing.T) {
	assert.True(t, isReconnectableError(errors.New("ssh: handshake failed: timeout")))
}
