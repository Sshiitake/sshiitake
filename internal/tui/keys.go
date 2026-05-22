package tui

import "github.com/charmbracelet/bubbles/key"

// keyMap is the full TUI key set. Bubbles' `key` package gives free
// help integration with `?` overlays.
type keyMap struct {
	Up     key.Binding
	Down   key.Binding
	Enter  key.Binding
	Back   key.Binding
	Filter key.Binding
	Toggle key.Binding
	Help   key.Binding
	Quit   key.Binding
}

var defaultKeys = keyMap{
	Up:     key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
	Down:   key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
	Enter:  key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "details")),
	Back:   key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
	Filter: key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter")),
	Toggle: key.NewBinding(key.WithKeys(" "), key.WithHelp("space", "toggle")),
	Help:   key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
	Quit:   key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
}

// ShortHelp returns key bindings shown in the footer.
func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Toggle, k.Filter, k.Help, k.Quit}
}

// FullHelp returns the grouped key bindings shown in the help overlay.
func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.Enter, k.Back},
		{k.Toggle, k.Filter, k.Help, k.Quit},
	}
}
