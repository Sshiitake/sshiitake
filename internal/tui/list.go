package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/Sshiitake/sshiitake/internal/manager"
	"github.com/Sshiitake/sshiitake/internal/metrics"
	"github.com/Sshiitake/sshiitake/internal/tunnel"
)

// tunnelRow is the model state for one tunnel in the list view.
type tunnelRow struct {
	Name      string
	Group     string
	Status    tunnel.Status
	LocalAddr string
	BytesIn   uint64
	BytesOut  uint64
	LatencyMs float64
	Latency   []metrics.Sample
}

// listModel is the state and renderer for the tunnels list view.
type listModel struct {
	keys    keyMap
	theme   Theme
	tunnels []tunnelRow
	cursor  int
	filter  string
}

// newListModel constructs a list model bound to the given keymap and theme.
func newListModel(k keyMap, t Theme) *listModel {
	return &listModel{keys: k, theme: t}
}

// applyEvent folds a manager.Event into the list state.
func (l *listModel) applyEvent(e manager.Event) {
	switch e.Type {
	case manager.EventTunnelState:
		r := l.findOrCreate(e.TunnelName)
		r.Status = e.Status
	case manager.EventMetrics:
		r := l.findOrCreate(e.TunnelName)
		r.BytesIn = e.BytesIn
		r.BytesOut = e.BytesOut
		r.LatencyMs = e.LatencyMs
		// Append to local latency history for sparkline.
		r.Latency = append(r.Latency, metrics.Sample{At: e.Timestamp, Value: e.LatencyMs})
		if len(r.Latency) > 60 {
			r.Latency = r.Latency[len(r.Latency)-60:]
		}
	}
}

// findOrCreate returns a pointer to the row for name, creating it if absent.
func (l *listModel) findOrCreate(name string) *tunnelRow {
	for i := range l.tunnels {
		if l.tunnels[i].Name == name {
			return &l.tunnels[i]
		}
	}
	l.tunnels = append(l.tunnels, tunnelRow{Name: name})
	return &l.tunnels[len(l.tunnels)-1]
}

// setFilter records the current filter query (case-insensitive substring)
// and resets the cursor to 0 so the selection points at a visible row.
func (l *listModel) setFilter(q string) {
	l.filter = q
	l.cursor = 0
}

// visibleTunnels returns the rows passing the current filter (or all rows
// when filter is empty).
func (l *listModel) visibleTunnels() []tunnelRow {
	if l.filter == "" {
		return l.tunnels
	}
	q := strings.ToLower(l.filter)
	var out []tunnelRow
	for _, t := range l.tunnels {
		if strings.Contains(strings.ToLower(t.Name), q) ||
			strings.Contains(strings.ToLower(t.Group), q) {
			out = append(out, t)
		}
	}
	return out
}

// cursorUp moves the selection cursor up one row, clamped at 0.
func (l *listModel) cursorUp() {
	if l.cursor > 0 {
		l.cursor--
	}
}

// cursorDown moves the selection cursor down one row, clamped at len-1.
func (l *listModel) cursorDown() {
	if l.cursor < len(l.visibleTunnels())-1 {
		l.cursor++
	}
}

// selected returns the currently-selected row and true if one exists.
func (l *listModel) selected() (tunnelRow, bool) {
	v := l.visibleTunnels()
	if l.cursor < 0 || l.cursor >= len(v) {
		return tunnelRow{}, false
	}
	return v[l.cursor], true
}

// view renders the list as a string.
func (l *listModel) view() string {
	if len(l.tunnels) == 0 {
		return l.theme.HelpText.Render(
			"no tunnels yet. edit ~/.config/sshiitake/tunnels.toml or run `ssht add` to create one interactively.")
	}

	visible := l.visibleTunnels()
	if len(visible) == 0 {
		return l.theme.HelpText.Render(fmt.Sprintf("no tunnels match filter %q", l.filter))
	}

	// Group rows.
	byGroup := map[string][]tunnelRow{}
	var groupNames []string
	for _, t := range visible {
		group := t.Group
		if group == "" {
			group = "ad-hoc"
		}
		if _, ok := byGroup[group]; !ok {
			groupNames = append(groupNames, group)
		}
		byGroup[group] = append(byGroup[group], t)
	}
	sort.Strings(groupNames)

	var b strings.Builder
	idx := 0
	for _, g := range groupNames {
		b.WriteString(l.theme.GroupHeader.Render("▾ " + g))
		b.WriteString("\n")
		for _, t := range byGroup[g] {
			b.WriteString(l.renderRow(t, idx == l.cursor))
			b.WriteString("\n")
			idx++
		}
		b.WriteString("\n")
	}

	// Footer with short help.
	footer := footerHelp(l.keys, l.theme)
	b.WriteString(footer)

	return b.String()
}

// renderRow renders a single tunnel row, optionally with the selection marker.
func (l *listModel) renderRow(t tunnelRow, selected bool) string {
	dot := statusDot(t.Status, l.theme)
	name := l.theme.TunnelName.Render(sanitiseForTerminal(t.Name))
	addr := t.LocalAddr
	if addr == "" {
		addr = l.theme.HelpText.Render("(down)")
	}
	latency := renderLatency(t.LatencyMs, l.theme)
	spark := RenderSparkline(t.Latency, 6)
	prefix := "  "
	if selected {
		prefix = "▸ "
	}
	return fmt.Sprintf("%s%s %s  %-18s  %s  %s", prefix, dot, name, addr, latency, spark)
}

// statusDot returns the styled glyph for a tunnel status.
func statusDot(s tunnel.Status, t Theme) string {
	switch s {
	case tunnel.StatusUp:
		return t.StatusUp.Render("●")
	case tunnel.StatusDown:
		return t.StatusDown.Render("○")
	case tunnel.StatusConnecting:
		return t.StatusConnecting.Render("◐")
	case tunnel.StatusStopping:
		return t.StatusConnecting.Render("⊘")
	default:
		return "?"
	}
}

// renderLatency renders a latency in ms with colour bucketing.
func renderLatency(ms float64, t Theme) string {
	s := fmt.Sprintf("%4.0fms", ms)
	switch {
	case ms == 0:
		return t.HelpText.Render("    -ms")
	case ms < 50:
		return t.LatencyGood.Render(s)
	case ms < 200:
		return t.LatencyWarn.Render(s)
	default:
		return t.LatencyBad.Render(s)
	}
}

// footerHelp renders the short-help line as accent-coloured keys with
// muted descriptions.
func footerHelp(km keyMap, t Theme) string {
	var parts []string
	for _, b := range km.ShortHelp() {
		parts = append(parts, lipgloss.JoinHorizontal(lipgloss.Left,
			t.Accent.Render(b.Help().Key),
			" ",
			t.HelpText.Render(b.Help().Desc),
		))
	}
	return t.HelpText.Render(strings.Join(parts, "  "))
}
