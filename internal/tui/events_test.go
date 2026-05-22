package tui

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/Sshiitake/sshiitake/internal/manager"
)

// TestListenForEvents_deliversEventAsMsg verifies the channel-to-Msg
// adapter forwards a manager.Event payload through ManagerEventMsg.
func TestListenForEvents_deliversEventAsMsg(t *testing.T) {
	ch := make(chan manager.Event, 1)
	ch <- manager.Event{Type: manager.EventTunnelState, TunnelName: "x"}
	close(ch)

	cmd := listenForEvents(ch)
	msg := cmd()
	wrapped, ok := msg.(ManagerEventMsg)
	assert.True(t, ok, "expected ManagerEventMsg, got %T", msg)
	assert.Equal(t, "x", wrapped.E.TunnelName)
}

// TestListenForEvents_closedChannelEmitsClosedMsg ensures we surface a
// terminal signal when the manager closes its subscription.
func TestListenForEvents_closedChannelEmitsClosedMsg(t *testing.T) {
	ch := make(chan manager.Event)
	close(ch)
	cmd := listenForEvents(ch)
	msg := cmd()
	_, ok := msg.(managerClosedMsg)
	assert.True(t, ok, "expected managerClosedMsg, got %T", msg)
}

// TestTickEvery_returnsCommand smoke-tests the tick scheduler.
func TestTickEvery_returnsCommand(t *testing.T) {
	cmd := tickEvery(10 * time.Millisecond)
	assert.NotNil(t, cmd, "tickEvery should return a non-nil tea.Cmd")
}
