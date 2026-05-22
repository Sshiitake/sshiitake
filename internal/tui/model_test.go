package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Sshiitake/sshiitake/internal/manager"
	"github.com/Sshiitake/sshiitake/internal/tunnel"
)

func TestModel_initialViewIsList(t *testing.T) {
	m := NewModel(nil, ThemeByNameOrDefault("dark"), nil)
	assert.Equal(t, viewList, m.currentView)
}

func TestModel_handlesEventMsg(t *testing.T) {
	m := NewModel(nil, ThemeByNameOrDefault("dark"), nil)
	model, _ := m.Update(ManagerEventMsg{E: manager.Event{
		Type: manager.EventTunnelState, TunnelName: "api", Status: tunnel.StatusUp,
	}})
	updated := model.(*Model)
	assert.Len(t, updated.list.tunnels, 1)
}

func TestModel_quitOnQ(t *testing.T) {
	m := NewModel(nil, ThemeByNameOrDefault("dark"), nil)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	assert.NotNil(t, cmd, "quit key should return a tea.Quit cmd")
}

func TestModel_toggleHelp(t *testing.T) {
	m := NewModel(nil, ThemeByNameOrDefault("dark"), nil)
	model, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	assert.Equal(t, viewHelp, model.(*Model).currentView)
}

func TestModel_filterEnterReturnsToList(t *testing.T) {
	m := NewModel(nil, ThemeByNameOrDefault("dark"), nil)
	// Slash enters filter mode.
	model, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	assert.Equal(t, viewFilter, model.(*Model).currentView)
	// Enter exits to list with the filter applied.
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	assert.Equal(t, viewList, model.(*Model).currentView)
}

func TestModel_filterEscRestoresPreviousFilter(t *testing.T) {
	m := NewModel(nil, ThemeByNameOrDefault("dark"), nil)
	// Apply a filter first.
	model, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	mm := model.(*Model)
	mm.filterInput.SetValue("api")
	model, _ = mm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	mm = model.(*Model)
	require.Equal(t, "api", mm.list.filter)
	// Open filter and start typing, then cancel.
	model, _ = mm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	mm = model.(*Model)
	mm.filterInput.SetValue("zzz")
	model, _ = mm.Update(tea.KeyMsg{Type: tea.KeyEsc})
	mm = model.(*Model)
	// Filter on list should still be "api", not "zzz".
	require.Equal(t, "api", mm.list.filter)
	// Filter input value should be restored to the previously-applied "api".
	require.Equal(t, "api", mm.filterInput.Value())
}

// TestModel_toggleKeyTriggersCallback: space on a selected row invokes
// the onToggle callback with that row's name.
func TestModel_toggleKeyTriggersCallback(t *testing.T) {
	var toggled string
	m := NewModel(nil, ThemeByNameOrDefault("dark"), func(name string) {
		toggled = name
	})
	// Seed a row directly so .selected() returns it without depending on
	// the event-driven applyEvent path.
	m.list.tunnels = []tunnelRow{{Name: "api"}}
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(" ")})
	assert.Equal(t, "api", toggled)
}

// TestModel_toggleKeyNoCallbackIsNoop: when onToggle is nil, pressing
// space must not panic and must not change state.
func TestModel_toggleKeyNoCallbackIsNoop(t *testing.T) {
	m := NewModel(nil, ThemeByNameOrDefault("dark"), nil)
	m.list.tunnels = []tunnelRow{{Name: "api"}}
	require.NotPanics(t, func() {
		m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(" ")})
	})
	// View state unchanged.
	assert.Equal(t, viewList, m.currentView)
}
