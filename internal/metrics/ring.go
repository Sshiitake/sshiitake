// Package metrics provides per-tunnel counters and time-series ring
// buffers used by the manager event stream and (eventually) the TUI
// sparklines. The package has no dependency on tunnel or manager;
// the callers wire it in.
package metrics

import (
	"sync"
	"time"
)

// Sample is one (time, value) point.
type Sample struct {
	At    time.Time
	Value float64
}

// Ring is a fixed-capacity ring buffer of Samples. Safe for concurrent
// Add and Snapshot.
type Ring struct {
	mu       sync.Mutex
	capacity int
	samples  []Sample // length grows up to capacity, then we overwrite
	next     int      // write index for the next Add
	full     bool
}

// NewRing constructs a ring of the given capacity. Capacity must be > 0.
func NewRing(capacity int) *Ring {
	if capacity <= 0 {
		panic("metrics.NewRing: capacity must be > 0")
	}
	return &Ring{capacity: capacity, samples: make([]Sample, capacity)}
}

// Add records a sample.
func (r *Ring) Add(at time.Time, value float64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.samples[r.next] = Sample{At: at, Value: value}
	r.next++
	if r.next == r.capacity {
		r.next = 0
		r.full = true
	}
}

// Snapshot returns a copy of the samples in chronological order.
func (r *Ring) Snapshot() []Sample {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.full {
		out := make([]Sample, r.next)
		copy(out, r.samples[:r.next])
		return out
	}
	out := make([]Sample, r.capacity)
	copy(out, r.samples[r.next:])
	copy(out[r.capacity-r.next:], r.samples[:r.next])
	return out
}
