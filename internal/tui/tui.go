package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Sshiitake/sshiitake/internal/manager"
)

// Run starts the TUI bound to the given Manager and theme name. It
// returns when the user quits or ctx is cancelled. Run does NOT own
// the Manager's lifecycle: callers are expected to cancel ctx and join
// Manager.Run separately.
//
// An unknown themeName falls back to the default theme.
func Run(ctx context.Context, m *manager.Manager, themeName string) error {
	theme, ok := ThemeByName(themeName)
	if !ok {
		theme, _ = ThemeByName(DefaultThemeName)
	}

	events := m.Subscribe(256)
	defer m.Unsubscribe(events)

	model := NewModel(events, theme)
	p := tea.NewProgram(model, tea.WithContext(ctx))
	_, err := p.Run()
	return err
}
