package tui

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Sshiitake/sshiitake/internal/metrics"
)

func TestRenderSparkline_emptyReturnsBlank(t *testing.T) {
	out := RenderSparkline(nil, 5)
	assert.Equal(t, "     ", out, "empty samples should produce all-space spacer of the width")
}

func TestRenderSparkline_uniformValuesAreFlat(t *testing.T) {
	now := time.Unix(0, 0)
	samples := []metrics.Sample{
		{At: now, Value: 50},
		{At: now.Add(time.Second), Value: 50},
		{At: now.Add(2 * time.Second), Value: 50},
	}
	out := RenderSparkline(samples, 3)
	// Each sample renders to the same mid-range char (▄ or so).
	for _, r := range out {
		assert.Equal(t, '▄', r, "uniform samples should produce a flat line")
	}
}

func TestRenderSparkline_widthTrimming(t *testing.T) {
	now := time.Unix(0, 0)
	samples := make([]metrics.Sample, 10)
	for i := range samples {
		samples[i] = metrics.Sample{At: now.Add(time.Duration(i) * time.Second), Value: float64(i)}
	}
	out := RenderSparkline(samples, 5)
	// Output is exactly width-5 (last 5 samples).
	assert.Equal(t, 5, runeLen(out))
}

func TestRenderSparkline_minMaxRange(t *testing.T) {
	now := time.Unix(0, 0)
	samples := []metrics.Sample{
		{At: now, Value: 0},
		{At: now.Add(time.Second), Value: 100},
	}
	out := RenderSparkline(samples, 2)
	runes := []rune(out)
	assert.Equal(t, '▁', runes[0], "min value uses ▁")
	assert.Equal(t, '█', runes[1], "max value uses █")
}

// TestRenderSparkline_handlesNaNAndInf verifies that NaN and +Inf
// values render as a space rune rather than panicking or silently
// mapping to a block. Defensive: nothing upstream produces NaN today.
func TestRenderSparkline_handlesNaNAndInf(t *testing.T) {
	now := time.Unix(0, 0)
	samples := []metrics.Sample{
		{At: now, Value: math.NaN()},
		{At: now.Add(time.Second), Value: math.Inf(1)},
		{At: now.Add(2 * time.Second), Value: 50},
	}
	out := RenderSparkline(samples, 3)
	require.NotPanics(t, func() { _ = out })
	// NaN/Inf collapse to space.
	runes := []rune(out)
	require.GreaterOrEqual(t, len(runes), 2)
	assert.Equal(t, ' ', runes[0])
	assert.Equal(t, ' ', runes[1])
}

func runeLen(s string) int {
	n := 0
	for range s {
		n++
	}
	return n
}
