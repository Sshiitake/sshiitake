package metrics

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestRing_belowCapacityKeepsAll(t *testing.T) {
	r := NewRing(5)
	now := time.Unix(0, 0)
	for i := 0; i < 3; i++ {
		r.Add(now.Add(time.Duration(i)*time.Second), float64(i))
	}
	got := r.Snapshot()
	assert.Len(t, got, 3)
	assert.Equal(t, 0.0, got[0].Value)
	assert.Equal(t, 2.0, got[2].Value)
}

func TestRing_overCapacityDropsOldest(t *testing.T) {
	r := NewRing(3)
	now := time.Unix(0, 0)
	for i := 0; i < 5; i++ {
		r.Add(now.Add(time.Duration(i)*time.Second), float64(i))
	}
	got := r.Snapshot()
	assert.Len(t, got, 3)
	assert.Equal(t, 2.0, got[0].Value, "oldest two should be dropped")
	assert.Equal(t, 4.0, got[2].Value)
}

func TestRing_concurrentSafeAddAndSnapshot(t *testing.T) {
	r := NewRing(100)
	done := make(chan struct{})

	// Writer
	go func() {
		now := time.Unix(0, 0)
		for i := 0; i < 1000; i++ {
			r.Add(now.Add(time.Duration(i)*time.Millisecond), float64(i))
		}
		close(done)
	}()

	// Reader hammering Snapshot
	for {
		select {
		case <-done:
			snap := r.Snapshot()
			assert.LessOrEqual(t, len(snap), 100)
			return
		default:
			_ = r.Snapshot()
		}
	}
}

func TestRing_zeroCapacityPanics(t *testing.T) {
	assert.Panics(t, func() { NewRing(0) })
}
