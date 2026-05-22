package manager

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Sshiitake/sshiitake/internal/tunnel"
)

func TestSubscribe_receivesEvents(t *testing.T) {
	subs := newSubscribers()

	ch := subs.Subscribe(8)
	defer subs.Unsubscribe(ch)

	subs.publish(Event{
		Type:       EventTunnelState,
		TunnelName: "api-prod",
		Timestamp:  time.Unix(123, 0),
		Status:     tunnel.StatusUp,
	})

	select {
	case e := <-ch:
		assert.Equal(t, EventTunnelState, e.Type)
		assert.Equal(t, "api-prod", e.TunnelName)
		assert.Equal(t, tunnel.StatusUp, e.Status)
	case <-time.After(time.Second):
		t.Fatal("did not receive event")
	}
}

func TestSubscribe_slowConsumerDropsEvents(t *testing.T) {
	subs := newSubscribers()

	ch := subs.Subscribe(1) // capacity 1
	defer subs.Unsubscribe(ch)

	// Three publishes with a 1-element buffer; two drops expected.
	subs.publish(Event{Type: EventTunnelState, TunnelName: "a"})
	subs.publish(Event{Type: EventTunnelState, TunnelName: "b"})
	subs.publish(Event{Type: EventTunnelState, TunnelName: "c"})

	got := <-ch
	assert.Equal(t, "a", got.TunnelName, "first event is the only one delivered")

	select {
	case <-ch:
		t.Fatal("expected channel empty after drop")
	case <-time.After(50 * time.Millisecond):
		// Expected
	}
}

func TestUnsubscribe_closesChannel(t *testing.T) {
	subs := newSubscribers()
	ch := subs.Subscribe(1)

	subs.Unsubscribe(ch)
	// publish to ensure no panic on closed channel send
	require.NotPanics(t, func() {
		subs.publish(Event{Type: EventTunnelState})
	})
}
