package metrics

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestTracker_bytesCounters(t *testing.T) {
	tk := NewTracker()
	tk.AddBytesIn(100)
	tk.AddBytesIn(50)
	tk.AddBytesOut(200)

	in, out := tk.Bytes()
	assert.Equal(t, uint64(150), in)
	assert.Equal(t, uint64(200), out)
}

func TestTracker_latencyRing(t *testing.T) {
	tk := NewTracker()
	now := time.Unix(0, 0)
	tk.RecordLatency(now, 12*time.Millisecond)
	tk.RecordLatency(now.Add(time.Second), 18*time.Millisecond)

	snap := tk.LatencySnapshot()
	assert.Len(t, snap, 2)
	assert.InDelta(t, 12.0, snap[0].Value, 0.01, "milliseconds stored as float")
}

func TestTracker_concurrentSafe(t *testing.T) {
	tk := NewTracker()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() { defer wg.Done(); tk.AddBytesIn(1) }()
		go func() { defer wg.Done(); tk.AddBytesOut(1) }()
	}
	wg.Wait()

	in, out := tk.Bytes()
	assert.Equal(t, uint64(50), in)
	assert.Equal(t, uint64(50), out)
}
