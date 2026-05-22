package tui

import (
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/Sshiitake/sshiitake/internal/manager"
)

// View-state constants for the top-level Model router.
const (
	viewList = iota
	viewDetail
	viewHelp
	viewFilter
)

// Model is the top-level tea.Model for the TUI. It owns the sub-models
// (list, detail, help) and the filter text input, and routes between
// them based on the current view state.
type Model struct {
	keys        keyMap
	theme       Theme
	list        *listModel
	detail      *detailModel
	help        *helpModel
	filterInput textinput.Model
	currentView int

	events <-chan manager.Event

	width, height int
}

// NewModel constructs a fresh Model. events may be nil for tests, in
// which case Init returns no commands.
func NewModel(events <-chan manager.Event, theme Theme) *Model {
	ti := textinput.New()
	ti.Placeholder = "filter..."
	ti.Prompt = "/ "
	return &Model{
		keys:        defaultKeys,
		theme:       theme,
		list:        newListModel(defaultKeys, theme),
		detail:      newDetailModel(defaultKeys, theme),
		help:        newHelpModel(defaultKeys, theme),
		filterInput: ti,
		currentView: viewList,
		events:      events,
	}
}

// ThemeByNameOrDefault returns the named theme, falling back to the
// default when the name is unknown. Test convenience.
func ThemeByNameOrDefault(name string) Theme {
	if t, ok := ThemeByName(name); ok {
		return t
	}
	t, _ := ThemeByName(DefaultThemeName)
	return t
}

// Init implements tea.Model. When events is non-nil it arms the event
// listener and the periodic tick.
func (m *Model) Init() tea.Cmd {
	if m.events != nil {
		return tea.Batch(listenForEvents(m.events), tickEvery(time.Second))
	}
	return nil
}

// Update implements tea.Model. It dispatches on the message type and
// routes key events through handleKey.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case ManagerEventMsg:
		m.list.applyEvent(msg.E)
		if m.detail.row.Name != "" {
			m.detail.applyEvent(msg.E)
		}
		if m.events != nil {
			return m, listenForEvents(m.events)
		}
		return m, nil

	case managerClosedMsg:
		return m, tea.Quit

	case tickMsg:
		if m.events != nil {
			return m, tickEvery(time.Second)
		}
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

// handleKey dispatches key events. Filter mode swallows most keys so the
// text input can capture them; Enter and Esc exit back to the list view
// with the filter applied.
func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.currentView == viewFilter {
		switch msg.Type {
		case tea.KeyEnter:
			m.list.setFilter(m.filterInput.Value())
			m.currentView = viewList
			return m, nil
		case tea.KeyEsc:
			// Cancel: restore the input to the previously-applied filter
			// (so reopening filter mode doesn't lose what was applied earlier).
			m.filterInput.SetValue(m.list.filter)
			m.currentView = viewList
			return m, nil
		default:
			var cmd tea.Cmd
			m.filterInput, cmd = m.filterInput.Update(msg)
			return m, cmd
		}
	}

	switch {
	case key.Matches(msg, m.keys.Quit):
		return m, tea.Quit
	case key.Matches(msg, m.keys.Help):
		if m.currentView == viewHelp {
			m.currentView = viewList
		} else {
			m.currentView = viewHelp
		}
	case key.Matches(msg, m.keys.Filter):
		m.filterInput.Focus()
		m.filterInput.SetValue(m.list.filter)
		m.currentView = viewFilter
	case key.Matches(msg, m.keys.Up):
		m.list.cursorUp()
	case key.Matches(msg, m.keys.Down):
		m.list.cursorDown()
	case key.Matches(msg, m.keys.Enter):
		if row, ok := m.list.selected(); ok {
			m.detail.show(row)
			m.currentView = viewDetail
		}
	case key.Matches(msg, m.keys.Back):
		m.currentView = viewList
	}
	return m, nil
}

// View implements tea.Model.
func (m *Model) View() string {
	switch m.currentView {
	case viewDetail:
		return m.detail.view()
	case viewHelp:
		return m.help.view()
	case viewFilter:
		return m.list.view() + "\n" + m.filterInput.View()
	default:
		return m.list.view()
	}
}
