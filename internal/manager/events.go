package manager

import (
	"sync"
	"time"

	"github.com/Sshiitake/sshiitake/internal/tunnel"
)

// EventType discriminates events on the stream. The numeric values are
// not stable; the JSON encoding uses the string form (see MarshalJSON
// in the cmd/ssht/bare.go encoder).
type EventType int

// Event type values published by the Manager.
const (
	EventUnknown EventType = iota
	EventTunnelState
	EventMetrics
	EventLog
)

// String returns the stable wire name for the event type.
func (et EventType) String() string {
	switch et {
	case EventTunnelState:
		return "tunnel_state"
	case EventMetrics:
		return "metrics"
	case EventLog:
		return "log"
	default:
		return "unknown"
	}
}

// Event is one message on the manager's event stream.
type Event struct {
	Type       EventType
	TunnelName string
	Timestamp  time.Time

	// EventTunnelState
	Status tunnel.Status

	// EventMetrics
	BytesIn   uint64
	BytesOut  uint64
	LatencyMs float64 // 0 if no recent sample

	// EventLog
	Message string
}

// subscribers fan-out events to N consumer channels with per-channel
// drop-on-full semantics.
type subscribers struct {
	mu     sync.Mutex
	chans  map[chan Event]struct{}
	closed bool
}

func newSubscribers() *subscribers {
	return &subscribers{chans: make(map[chan Event]struct{})}
}

// Subscribe returns a buffered channel that will receive events.
// Capacity is bufferSize; when the buffer is full, new events for THIS
// subscriber are dropped silently. Pick capacity according to how
// patient the consumer is (TUI: 256; --bare: 256; tests: small to
// exercise drops).
func (s *subscribers) Subscribe(bufferSize int) chan Event {
	if bufferSize <= 0 {
		bufferSize = 1
	}
	ch := make(chan Event, bufferSize)
	s.mu.Lock()
	s.chans[ch] = struct{}{}
	s.mu.Unlock()
	return ch
}

// Unsubscribe removes ch from the subscriber set and closes it.
func (s *subscribers) Unsubscribe(ch chan Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.chans[ch]; !ok {
		return
	}
	delete(s.chans, ch)
	close(ch)
}

// publish fans out e to all current subscribers. Slow subscribers drop.
func (s *subscribers) publish(e Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	for ch := range s.chans {
		select {
		case ch <- e:
		default:
			// drop
		}
	}
}
