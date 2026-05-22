package metrics

import (
	"sync/atomic"
	"time"
)

// LatencyRingCapacity is the number of latency samples retained per
// tunnel. 60 samples at one-per-second gives a one-minute sparkline.
const LatencyRingCapacity = 60

// Tracker is a per-tunnel metrics container. Bytes counters use atomics
// (high-frequency writes from io.Copy goroutines). Latency uses the Ring.
type Tracker struct {
	bytesIn  atomic.Uint64
	bytesOut atomic.Uint64
	latency  *Ring
}

// NewTracker returns a fresh Tracker.
func NewTracker() *Tracker {
	return &Tracker{latency: NewRing(LatencyRingCapacity)}
}

// AddBytesIn records bytes received from the SSH side (remote -> local).
func (t *Tracker) AddBytesIn(n int64) {
	if n < 0 {
		return
	}
	t.bytesIn.Add(uint64(n))
}

// AddBytesOut records bytes sent to the SSH side (local -> remote).
func (t *Tracker) AddBytesOut(n int64) {
	if n < 0 {
		return
	}
	t.bytesOut.Add(uint64(n))
}

// Bytes returns (in, out).
func (t *Tracker) Bytes() (in, out uint64) {
	return t.bytesIn.Load(), t.bytesOut.Load()
}

// RecordLatency stores a latency sample (stored in milliseconds as float64).
func (t *Tracker) RecordLatency(at time.Time, d time.Duration) {
	t.latency.Add(at, float64(d)/float64(time.Millisecond))
}

// LatencySnapshot returns the latency ring contents in chronological order.
func (t *Tracker) LatencySnapshot() []Sample {
	return t.latency.Snapshot()
}
