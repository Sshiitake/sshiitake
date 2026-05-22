package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Sshiitake/sshiitake/internal/manager"
	"github.com/Sshiitake/sshiitake/internal/tunnel"
)

func TestBare_marshalsEvent(t *testing.T) {
	var buf bytes.Buffer
	enc := newBareEncoder(&buf)

	err := enc.write(manager.Event{
		Type:       manager.EventTunnelState,
		TunnelName: "api-prod",
		Timestamp:  time.Unix(1700000000, 0).UTC(),
		Status:     tunnel.StatusUp,
	})
	require.NoError(t, err)

	line := strings.TrimSpace(buf.String())
	var obj map[string]any
	require.NoError(t, json.Unmarshal([]byte(line), &obj))
	assert.Equal(t, "tunnel_state", obj["type"])
	assert.Equal(t, "api-prod", obj["tunnel"])
	assert.Equal(t, "up", obj["status"])
	// Ends with a newline so the consumer can line-buffer.
	assert.True(t, strings.HasSuffix(buf.String(), "\n"))
}

func TestBare_metricsEvent(t *testing.T) {
	var buf bytes.Buffer
	enc := newBareEncoder(&buf)

	err := enc.write(manager.Event{
		Type:       manager.EventMetrics,
		TunnelName: "api-prod",
		Timestamp:  time.Unix(1700000001, 0).UTC(),
		BytesIn:    1024,
		BytesOut:   2048,
		LatencyMs:  12.3,
	})
	require.NoError(t, err)

	var obj map[string]any
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(buf.String())), &obj))
	assert.Equal(t, "metrics", obj["type"])
	assert.InDelta(t, 1024.0, obj["bytes_in"], 0.01)
	assert.InDelta(t, 2048.0, obj["bytes_out"], 0.01)
	assert.InDelta(t, 12.3, obj["latency_ms"], 0.01)
}

func TestBare_unknownEventTypeReturnsError(t *testing.T) {
	var buf bytes.Buffer
	enc := newBareEncoder(&buf)
	err := enc.write(manager.Event{Type: manager.EventUnknown})
	require.Error(t, err)
}
