// Package logbuffer holds an in-memory ring of per-tunnel log lines.
// Consumed by future TUI detail-view and by the JSON event stream.
package logbuffer

import (
	"sync"
	"time"
)

// Entry is one log line with timestamp.
type Entry struct {
	At      time.Time
	Message string
}

// Buffer is a fixed-capacity ring of Entries, safe for concurrent use.
type Buffer struct {
	mu       sync.Mutex
	capacity int
	entries  []Entry
	next     int
	full     bool
}

// New constructs a Buffer with the given capacity. Capacity must be > 0.
func New(capacity int) *Buffer {
	if capacity <= 0 {
		panic("logbuffer.New: capacity must be > 0")
	}
	return &Buffer{capacity: capacity, entries: make([]Entry, capacity)}
}

// Append records a log line.
func (b *Buffer) Append(at time.Time, msg string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.entries[b.next] = Entry{At: at, Message: msg}
	b.next++
	if b.next == b.capacity {
		b.next = 0
		b.full = true
	}
}

// Snapshot returns a copy of the entries in chronological order.
func (b *Buffer) Snapshot() []Entry {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.full {
		out := make([]Entry, b.next)
		copy(out, b.entries[:b.next])
		return out
	}
	out := make([]Entry, b.capacity)
	copy(out, b.entries[b.next:])
	copy(out[b.capacity-b.next:], b.entries[:b.next])
	return out
}
