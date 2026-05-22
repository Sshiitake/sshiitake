package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Sshiitake/sshiitake/internal/manager"
	"github.com/Sshiitake/sshiitake/internal/tunnel"
)

func TestListView_emptyShowsPlaceholder(t *testing.T) {
	l := newListModel(defaultKeys, darkTheme())
	view := l.view()
	assert.Contains(t, view, "no tunnels", "empty list should hint at it")
}

func TestListView_groupsTunnels(t *testing.T) {
	l := newListModel(defaultKeys, darkTheme())
	l.tunnels = []tunnelRow{
		{Name: "api-prod", Group: "work", Status: tunnel.StatusUp, LocalAddr: "127.0.0.1:8443"},
		{Name: "db", Group: "work", Status: tunnel.StatusUp, LocalAddr: "127.0.0.1:5432"},
		{Name: "nas", Group: "personal", Status: tunnel.StatusDown, LocalAddr: ""},
	}
	view := l.view()
	assert.Contains(t, view, "work")
	assert.Contains(t, view, "personal")
	assert.Contains(t, view, "api-prod")
	assert.Contains(t, view, "db")
	assert.Contains(t, view, "nas")
}

func TestListView_appliesEvent(t *testing.T) {
	l := newListModel(defaultKeys, darkTheme())
	l.applyEvent(manager.Event{
		Type: manager.EventTunnelState, TunnelName: "api", Status: tunnel.StatusUp,
	})
	require.Len(t, l.tunnels, 1)
	assert.Equal(t, tunnel.StatusUp, l.tunnels[0].Status)
}

func TestListView_metricsEventUpdatesCounters(t *testing.T) {
	l := newListModel(defaultKeys, darkTheme())
	l.applyEvent(manager.Event{
		Type: manager.EventTunnelState, TunnelName: "api", Status: tunnel.StatusUp,
	})
	l.applyEvent(manager.Event{
		Type: manager.EventMetrics, TunnelName: "api",
		Timestamp: time.Unix(1, 0), BytesIn: 1024, BytesOut: 2048, LatencyMs: 12.3,
	})
	require.Len(t, l.tunnels, 1)
	assert.Equal(t, uint64(1024), l.tunnels[0].BytesIn)
	assert.Equal(t, uint64(2048), l.tunnels[0].BytesOut)
}

func TestListView_filter(t *testing.T) {
	l := newListModel(defaultKeys, darkTheme())
	l.tunnels = []tunnelRow{
		{Name: "api-prod", Group: "work", Status: tunnel.StatusUp},
		{Name: "db", Group: "work", Status: tunnel.StatusUp},
		{Name: "api-staging", Group: "work", Status: tunnel.StatusUp},
	}
	l.setFilter("api")
	visible := l.visibleTunnels()
	require.Len(t, visible, 2)
	for _, v := range visible {
		assert.True(t, strings.Contains(v.Name, "api"))
	}
}

func TestListView_filterResetsCursor(t *testing.T) {
	l := newListModel(defaultKeys, darkTheme())
	l.tunnels = []tunnelRow{
		{Name: "a"}, {Name: "b"}, {Name: "api-c"}, {Name: "d"}, {Name: "e"},
	}
	l.cursor = 3
	l.setFilter("api")
	require.Equal(t, 0, l.cursor)
	row, ok := l.selected()
	require.True(t, ok)
	require.Equal(t, "api-c", row.Name)
}

func TestListView_cursorAndSelection(t *testing.T) {
	l := newListModel(defaultKeys, darkTheme())
	l.tunnels = []tunnelRow{
		{Name: "a", Group: "x", Status: tunnel.StatusUp},
		{Name: "b", Group: "x", Status: tunnel.StatusUp},
		{Name: "c", Group: "x", Status: tunnel.StatusUp},
	}
	// Initial cursor is at 0.
	sel, ok := l.selected()
	require.True(t, ok)
	assert.Equal(t, "a", sel.Name)

	// cursorUp at top clamps.
	l.cursorUp()
	assert.Equal(t, 0, l.cursor)

	// cursorDown moves through rows.
	l.cursorDown()
	l.cursorDown()
	sel, ok = l.selected()
	require.True(t, ok)
	assert.Equal(t, "c", sel.Name)

	// cursorDown at bottom clamps.
	l.cursorDown()
	assert.Equal(t, 2, l.cursor)

	// Empty list returns no selection.
	empty := newListModel(defaultKeys, darkTheme())
	_, ok = empty.selected()
	assert.False(t, ok)
}
