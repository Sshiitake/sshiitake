package logbuffer

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestBuffer_appendThenSnapshot(t *testing.T) {
	b := New(3)
	b.Append(time.Unix(1, 0), "one")
	b.Append(time.Unix(2, 0), "two")

	got := b.Snapshot()
	assert.Len(t, got, 2)
	assert.Equal(t, "one", got[0].Message)
	assert.Equal(t, "two", got[1].Message)
}

func TestBuffer_overCapacityDropsOldest(t *testing.T) {
	b := New(2)
	b.Append(time.Unix(1, 0), "one")
	b.Append(time.Unix(2, 0), "two")
	b.Append(time.Unix(3, 0), "three")

	got := b.Snapshot()
	assert.Len(t, got, 2)
	assert.Equal(t, "two", got[0].Message)
	assert.Equal(t, "three", got[1].Message)
}

func TestBuffer_concurrent(t *testing.T) {
	b := New(100)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			b.Append(time.Now(), "x")
		}()
	}
	wg.Wait()
	assert.Len(t, b.Snapshot(), 50)
}

func TestBuffer_zeroCapacityPanics(t *testing.T) {
	assert.Panics(t, func() { New(0) })
}
