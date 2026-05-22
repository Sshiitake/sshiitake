package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/Sshiitake/sshiitake/internal/logbuffer"
	"github.com/Sshiitake/sshiitake/internal/manager"
	"github.com/Sshiitake/sshiitake/internal/metrics"
	"github.com/Sshiitake/sshiitake/internal/tunnel"
)

func TestDetailView_rendersTunnelInfo(t *testing.T) {
	d := newDetailModel(defaultKeys, darkTheme())
	d.row = tunnelRow{
		Name:      "api-prod",
		Group:     "work",
		Status:    tunnel.StatusUp,
		LocalAddr: "127.0.0.1:8443",
		BytesIn:   1024,
		BytesOut:  2048,
		LatencyMs: 12.3,
	}
	v := d.view()
	assert.Contains(t, v, "api-prod")
	assert.Contains(t, v, "127.0.0.1:8443")
	assert.Contains(t, v, "1.0 KB") // formatted bytes
	assert.Contains(t, v, "2.0 KB")
}

func TestDetailView_showsRecentLogs(t *testing.T) {
	d := newDetailModel(defaultKeys, darkTheme())
	d.row = tunnelRow{Name: "x", Status: tunnel.StatusUp}
	d.logs = []logbuffer.Entry{
		{At: time.Unix(0, 0), Message: "connected"},
		{At: time.Unix(1, 0), Message: "proxied: GET /v2/recipes (200, 31ms)"},
	}
	v := d.view()
	assert.Contains(t, v, "connected")
	assert.Contains(t, v, "/v2/recipes")
}

func TestDetailView_metricsEventTracksLatencySamples(t *testing.T) {
	d := newDetailModel(defaultKeys, darkTheme())
	d.show(tunnelRow{Name: "x"})
	for i := 0; i < 5; i++ {
		d.applyEvent(manager.Event{
			Type: manager.EventMetrics, TunnelName: "x",
			Timestamp: time.Unix(int64(i), 0), LatencyMs: float64(i * 10),
		})
	}
	// Latency ring has 5 samples; sparkline rendering won't panic.
	v := d.view()
	assert.NotEmpty(t, v)
	// close() is also part of the contract.
	d.close()
	_ = metrics.Sample{} // import keeper
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		in   uint64
		want string
	}{
		{0, "0 B"},
		{500, "500 B"},
		{1024, "1.0 KB"},
		{1024 * 1024, "1.0 MB"},
		{1500000, "1.4 MB"},
		{1024 * 1024 * 1024, "1.0 GB"},
	}
	for _, tc := range tests {
		assert.Equal(t, tc.want, formatBytes(tc.in), tc.in)
	}
}

func TestDetailView_emptyShowsPlaceholder(t *testing.T) {
	d := newDetailModel(defaultKeys, darkTheme())
	v := d.view()
	assert.Contains(t, strings.ToLower(v), "select")
}
