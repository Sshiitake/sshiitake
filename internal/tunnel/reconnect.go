// Package tunnel reconnect machinery: exponential-backoff loop that
// retries Start on transient SSH failures (EOF, connection reset,
// handshake timeouts) while letting permanent failures (host-key
// mismatch, unsupported options, no usable auth) surface immediately.
package tunnel

import (
	"math/rand/v2"
	"strings"
	"time"
)

// BackoffOptions configures the reconnect backoff schedule. Zero values
// receive sane defaults via applyDefaults, with one deliberate
// exception: Jitter==0 means "no jitter" rather than "use default
// jitter" so tests can run deterministically. Production call sites
// (StartWithReconnect via newBackoff) inject the 10% default themselves
// when the field is untouched at the manager level.
type BackoffOptions struct {
	// InitialDelay is the wait before the first reconnect attempt.
	// Defaults to 1s.
	InitialDelay time.Duration
	// MaxDelay caps the exponential growth. Defaults to 60s.
	MaxDelay time.Duration
	// Multiplier scales the previous delay each step. Defaults to 2.0.
	Multiplier float64
	// Jitter is the fractional bound on per-call randomisation
	// (e.g. 0.1 means +/-10%). Zero disables jitter entirely.
	Jitter float64
	// MaxAttempts is the maximum number of Start calls before giving up.
	// Defaults to 10.
	MaxAttempts int
}

func (o *BackoffOptions) applyDefaults() {
	if o.InitialDelay == 0 {
		o.InitialDelay = time.Second
	}
	if o.MaxDelay == 0 {
		o.MaxDelay = 60 * time.Second
	}
	if o.Multiplier == 0 {
		o.Multiplier = 2.0
	}
	// Jitter: zero means no jitter; no default applied here.
	if o.MaxAttempts == 0 {
		o.MaxAttempts = 10
	}
}

// backoff produces an exponentially growing delay schedule with
// optional jitter, capped at MaxDelay. Not safe for concurrent use.
type backoff struct {
	opts    BackoffOptions
	current time.Duration
}

func newBackoff(opts BackoffOptions) *backoff {
	opts.applyDefaults()
	return &backoff{opts: opts}
}

// next returns the next delay and advances the schedule.
func (b *backoff) next() time.Duration {
	if b.current == 0 {
		b.current = b.opts.InitialDelay
	} else {
		b.current = time.Duration(float64(b.current) * b.opts.Multiplier)
	}
	if b.current > b.opts.MaxDelay {
		b.current = b.opts.MaxDelay
	}
	if b.opts.Jitter == 0 {
		return b.current
	}
	// Apply +/-Jitter * current as a uniform delta.
	delta := (rand.Float64()*2 - 1) * b.opts.Jitter * float64(b.current)
	return b.current + time.Duration(delta)
}

// reset returns the schedule to its initial state so the next call to
// next yields InitialDelay.
func (b *backoff) reset() { b.current = 0 }

// isReconnectableError decides whether an error from Start should
// trigger reconnect. Permanent failures (config errors, host-key
// mismatches, unsupported options) are NOT reconnectable; transient
// network failures are. Unknown errors default to not reconnectable to
// avoid hot-loops on novel failure modes.
//
// Permanent tokens are checked first: an error message containing both
// permanent and transient substrings (e.g. "ssh: handshake failed: host
// key mismatch") classifies as permanent.
func isReconnectableError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	for _, t := range []string{
		"HostKeyCallback required",
		"not yet supported",
		"ProxyJump",
		"local_host",
		"host key",
		"KEY MISMATCH",
		"not in known_hosts",
		"no usable auth methods",
		// Server-side credential rejection. crypto/ssh surfaces these
		// inside a "handshake failed" wrapper, so they must be matched
		// BEFORE the transient "handshake" token below or we'd retry a
		// credential failure ten times before giving up.
		"unable to authenticate",
		"no supported methods remain",
	} {
		if strings.Contains(msg, t) {
			return false
		}
	}
	for _, t := range []string{
		"EOF",
		"connection reset",
		"connection refused",
		"handshake",
		"timeout",
		"broken pipe",
		"no route to host",
		"network is unreachable",
	} {
		if strings.Contains(msg, t) {
			return true
		}
	}
	return false
}
