package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"

	"github.com/Sshiitake/sshiitake/internal/manager"
	"github.com/Sshiitake/sshiitake/internal/tunnel"
)

func TestModel_initialViewIsList(t *testing.T) {
	m := NewModel(nil, ThemeByNameOrDefault("dark"))
	assert.Equal(t, viewList, m.currentView)
}

func TestModel_handlesEventMsg(t *testing.T) {
	m := NewModel(nil, ThemeByNameOrDefault("dark"))
	model, _ := m.Update(ManagerEventMsg{E: manager.Event{
		Type: manager.EventTunnelState, TunnelName: "api", Status: tunnel.StatusUp,
	}})
	updated := model.(*Model)
	assert.Len(t, updated.list.tunnels, 1)
}

func TestModel_quitOnQ(t *testing.T) {
	m := NewModel(nil, ThemeByNameOrDefault("dark"))
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	assert.NotNil(t, cmd, "quit key should return a tea.Quit cmd")
}

func TestModel_toggleHelp(t *testing.T) {
	m := NewModel(nil, ThemeByNameOrDefault("dark"))
	model, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	assert.Equal(t, viewHelp, model.(*Model).currentView)
}

func TestModel_filterEnterReturnsToList(t *testing.T) {
	m := NewModel(nil, ThemeByNameOrDefault("dark"))
	// Slash enters filter mode.
	model, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	assert.Equal(t, viewFilter, model.(*Model).currentView)
	// Enter exits to list with the filter applied.
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	assert.Equal(t, viewList, model.(*Model).currentView)
}
