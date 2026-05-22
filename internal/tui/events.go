package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Sshiitake/sshiitake/internal/manager"
)

// ManagerEventMsg wraps a manager.Event for delivery to the tea.Model.
type ManagerEventMsg struct{ E manager.Event }

// listenForEvents returns a tea.Cmd that blocks on the event channel
// once and emits a ManagerEventMsg. The model's Update re-arms it.
func listenForEvents(ch <-chan manager.Event) tea.Cmd {
	return func() tea.Msg {
		e, ok := <-ch
		if !ok {
			return managerClosedMsg{}
		}
		return ManagerEventMsg{E: e}
	}
}

type managerClosedMsg struct{}

// tickMsg fires every refresh interval so we can repaint sparklines.
type tickMsg struct{}

func tickEvery(interval time.Duration) tea.Cmd {
	return tea.Tick(interval, func(_ time.Time) tea.Msg { return tickMsg{} })
}
