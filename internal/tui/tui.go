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
// An unknown themeName falls back to the default theme. The space-to-
// toggle binding is wired to Manager.Toggle via a callback dispatched
// off the Bubble Tea event loop (Toggle is potentially blocking; running
// it inline would freeze the UI for up to toggleStopTimeout).
func Run(ctx context.Context, m *manager.Manager, themeName string) error {
	theme, ok := ThemeByName(themeName)
	if !ok {
		theme, _ = ThemeByName(DefaultThemeName)
	}

	events := m.Subscribe(256)
	defer m.Unsubscribe(events)

	onToggle := func(name string) {
		// Errors are surfaced via the event stream (Down events carry the
		// error message). Toggle is best-effort from the TUI's POV.
		go func() { _ = m.Toggle(name) }()
	}

	model := NewModel(events, theme, onToggle)
	p := tea.NewProgram(model, tea.WithContext(ctx))
	_, err := p.Run()
	return err
}
