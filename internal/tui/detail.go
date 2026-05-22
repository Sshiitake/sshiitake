package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/Sshiitake/sshiitake/internal/logbuffer"
	"github.com/Sshiitake/sshiitake/internal/manager"
	"github.com/Sshiitake/sshiitake/internal/metrics"
)

// detailModel is the state and renderer for the per-tunnel detail view.
type detailModel struct {
	keys  keyMap
	theme Theme
	row   tunnelRow
	logs  []logbuffer.Entry
	open  bool // whether anything is being shown
	since time.Time
}

// newDetailModel constructs a detail model bound to the given keymap and theme.
func newDetailModel(k keyMap, t Theme) *detailModel {
	return &detailModel{keys: k, theme: t}
}

// show opens the detail view for the given tunnel row and starts the
// uptime clock.
func (d *detailModel) show(r tunnelRow) {
	d.row = r
	d.open = true
	d.since = time.Now()
}

// close hides the detail view. The last-shown row is retained so the
// next render can still display its name in placeholders if desired.
func (d *detailModel) close() { d.open = false }

// applyEvent folds a manager.Event into the detail state, but only when
// the event is for the currently-shown tunnel.
func (d *detailModel) applyEvent(e manager.Event) {
	if e.TunnelName != d.row.Name {
		return
	}
	switch e.Type {
	case manager.EventTunnelState:
		d.row.Status = e.Status
	case manager.EventMetrics:
		d.row.BytesIn = e.BytesIn
		d.row.BytesOut = e.BytesOut
		d.row.LatencyMs = e.LatencyMs
		d.row.Latency = append(d.row.Latency, metrics.Sample{At: e.Timestamp, Value: e.LatencyMs})
		if len(d.row.Latency) > 60 {
			d.row.Latency = d.row.Latency[len(d.row.Latency)-60:]
		}
	case manager.EventLog:
		d.logs = append(d.logs, logbuffer.Entry{At: e.Timestamp, Message: sanitiseForTerminal(e.Message)})
		if len(d.logs) > 12 {
			d.logs = d.logs[len(d.logs)-12:]
		}
	}
}

// view renders the detail panel as a string.
func (d *detailModel) view() string {
	if !d.open && d.row.Name == "" {
		return d.theme.HelpText.Render("Select a tunnel and press enter for details.")
	}

	var b strings.Builder
	b.WriteString(d.theme.GroupHeader.Render(sanitiseForTerminal(d.row.Name)))
	b.WriteString("  ")
	b.WriteString(statusDot(d.row.Status, d.theme))
	b.WriteString("\n\n")

	fmt.Fprintf(&b, "  Status:           %s\n", d.row.Status)
	fmt.Fprintf(&b, "  Forward:          %s\n", sanitiseForTerminal(d.row.LocalAddr))
	fmt.Fprintf(&b, "  Sent:             %s\n", formatBytes(d.row.BytesOut))
	fmt.Fprintf(&b, "  Received:         %s\n", formatBytes(d.row.BytesIn))
	fmt.Fprintf(&b, "  Latency:          %4.1f ms  %s\n", d.row.LatencyMs, RenderSparkline(d.row.Latency, 24))
	if d.open {
		fmt.Fprintf(&b, "  Uptime:           %s\n", time.Since(d.since).Truncate(time.Second))
	}

	b.WriteString("\n")
	b.WriteString(d.theme.GroupHeader.Render("Recent logs:"))
	b.WriteString("\n")
	if len(d.logs) == 0 {
		b.WriteString(d.theme.HelpText.Render("  (no log entries yet)"))
		b.WriteString("\n")
	} else {
		for _, e := range d.logs {
			fmt.Fprintf(&b, "  %s  %s\n", e.At.Format("15:04:05"), e.Message)
		}
	}

	b.WriteString("\n")
	b.WriteString(d.theme.HelpText.Render("esc back  ?  help  q quit"))
	return b.String()
}

// formatBytes renders a byte count as "B", "KB", "MB", or "GB" with
// one decimal place above the byte threshold.
func formatBytes(n uint64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
	)
	switch {
	case n < KB:
		return fmt.Sprintf("%d B", n)
	case n < MB:
		return fmt.Sprintf("%.1f KB", float64(n)/KB)
	case n < GB:
		return fmt.Sprintf("%.1f MB", float64(n)/MB)
	default:
		return fmt.Sprintf("%.1f GB", float64(n)/GB)
	}
}
