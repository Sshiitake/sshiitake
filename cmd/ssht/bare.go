package main

import (
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/spf13/cobra"

	"github.com/Sshiitake/sshiitake/internal/manager"
	"github.com/Sshiitake/sshiitake/internal/tunnel"
)

// bareEnvelope is the on-wire JSON schema for --bare events.
// Stable across patch versions; minor changes add optional fields.
type bareEnvelope struct {
	Type      string  `json:"type"`
	Tunnel    string  `json:"tunnel,omitempty"`
	Timestamp string  `json:"ts"`
	Status    string  `json:"status,omitempty"`
	BytesIn   uint64  `json:"bytes_in,omitempty"`
	BytesOut  uint64  `json:"bytes_out,omitempty"`
	LatencyMs float64 `json:"latency_ms,omitempty"`
	Message   string  `json:"message,omitempty"`
}

type bareEncoder struct{ w io.Writer }

func newBareEncoder(w io.Writer) *bareEncoder { return &bareEncoder{w: w} }

func (b *bareEncoder) write(e manager.Event) error {
	env := bareEnvelope{
		Type:      e.Type.String(),
		Tunnel:    e.TunnelName,
		Timestamp: e.Timestamp.UTC().Format(time.RFC3339),
	}
	switch e.Type {
	case manager.EventTunnelState:
		env.Status = statusString(e.Status)
	case manager.EventMetrics:
		env.BytesIn = e.BytesIn
		env.BytesOut = e.BytesOut
		env.LatencyMs = e.LatencyMs
	case manager.EventLog:
		env.Message = e.Message
	default:
		return fmt.Errorf("bare: unknown event type %d", e.Type)
	}
	data, err := json.Marshal(env)
	if err != nil {
		return fmt.Errorf("bare: marshal: %w", err)
	}
	if _, err := b.w.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("bare: write: %w", err)
	}
	return nil
}

func statusString(s tunnel.Status) string {
	switch s {
	case tunnel.StatusDown:
		return "down"
	case tunnel.StatusConnecting:
		return "connecting"
	case tunnel.StatusUp:
		return "up"
	case tunnel.StatusStopping:
		return "stopping"
	default:
		return "unknown"
	}
}

// streamBareEvents writes one JSON object per line to stdout for each
// event the manager publishes, until the event channel closes or the
// Run goroutine returns.
func streamBareEvents(cmd *cobra.Command, eventCh chan manager.Event, runErr chan error, _ *manager.Manager) error {
	enc := newBareEncoder(cmd.OutOrStdout())
	for {
		select {
		case e, ok := <-eventCh:
			if !ok {
				return <-runErr
			}
			if err := enc.write(e); err != nil {
				// In bare mode, marshal errors are fatal: the consumer
				// expects a coherent stream.
				return err
			}
		case err := <-runErr:
			return err
		}
	}
}
