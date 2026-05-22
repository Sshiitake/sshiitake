package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// helpModel is the state and renderer for the help overlay.
type helpModel struct {
	keys  keyMap
	theme Theme
}

// newHelpModel constructs a help model bound to the given keymap and theme.
func newHelpModel(k keyMap, t Theme) *helpModel {
	return &helpModel{keys: k, theme: t}
}

// tunnelTypeDiagrams documents the three tunnel topologies with
// box-drawing ASCII art. Used by the help overlay.
const tunnelTypeDiagrams = `
Local forward (-L):

  ┌──────────┐   SSH   ┌──────────┐   TCP   ┌──────────┐
  │  ssht    │────────►│  bastion │────────►│  target  │
  │  :8443   │encrypted│          │ direct  │  :443    │
  └──────────┘         └──────────┘         └──────────┘
       ▲
       │
       └── you reach the target by dialling 127.0.0.1:8443

Remote forward (-R):

  ┌──────────┐   SSH   ┌──────────┐         ┌──────────┐
  │  local   │◄────────┤  bastion │◄────────│  others  │
  │  :3000   │encrypted│  :9090   │         │          │
  └──────────┘         └──────────┘         └──────────┘
                            ▲
                            │
                            └── peers reach you via bastion:9090

Dynamic (-D, SOCKS5):

  ┌──────────┐   SSH   ┌──────────┐
  │ browser  │────────►│  bastion │
  │ SOCKS5   │encrypted│  egress  │──────► the internet
  │ :1080    │         │          │
  └──────────┘         └──────────┘
`

// view renders the help overlay: full keymap rows followed by the
// tunnel-type ASCII diagrams.
func (h *helpModel) view() string {
	var b strings.Builder
	b.WriteString(h.theme.GroupHeader.Render("Help"))
	b.WriteString("\n\n")

	for _, row := range h.keys.FullHelp() {
		var parts []string
		for _, k := range row {
			parts = append(parts, lipgloss.JoinHorizontal(lipgloss.Left,
				h.theme.Accent.Render(k.Help().Key),
				" ",
				h.theme.HelpText.Render(k.Help().Desc),
			))
		}
		b.WriteString(strings.Join(parts, "    "))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(h.theme.GroupHeader.Render("Tunnel types"))
	b.WriteString("\n")
	b.WriteString(h.theme.HelpText.Render(tunnelTypeDiagrams))
	b.WriteString("\n")
	b.WriteString(h.theme.HelpText.Render("esc to close help"))
	return b.String()
}
