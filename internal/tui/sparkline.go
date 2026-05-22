package tui

import (
	"math"
	"strings"

	"github.com/Sshiitake/sshiitake/internal/metrics"
)

// blockChars is the 8-level set used by RenderSparkline.
var blockChars = []rune{'▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}

// RenderSparkline produces a width-wide sparkline string from the most
// recent up-to-width samples. Returns an all-space string of width if
// samples is empty.
func RenderSparkline(samples []metrics.Sample, width int) string {
	if width <= 0 {
		return ""
	}
	if len(samples) == 0 {
		return strings.Repeat(" ", width)
	}
	// Take the last `width` samples.
	if len(samples) > width {
		samples = samples[len(samples)-width:]
	}

	// Find min/max for normalisation.
	minV, maxV := samples[0].Value, samples[0].Value
	for _, s := range samples[1:] {
		if s.Value < minV {
			minV = s.Value
		}
		if s.Value > maxV {
			maxV = s.Value
		}
	}

	var b strings.Builder
	b.Grow(len(samples) * 3) // average UTF-8 width
	for _, s := range samples {
		b.WriteRune(blockFor(s.Value, minV, maxV))
	}
	// Left-pad with spaces to reach width.
	if pad := width - len(samples); pad > 0 {
		return strings.Repeat(" ", pad) + b.String()
	}
	return b.String()
}

func blockFor(value, minV, maxV float64) rune {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		// Defensive: nothing upstream produces NaN/Inf today, but the
		// sparkline must never panic or silently map them to a block.
		return ' '
	}
	if maxV == minV {
		// All values equal: pick the middle block.
		return blockChars[3]
	}
	frac := (value - minV) / (maxV - minV)
	idx := int(frac * float64(len(blockChars)-1))
	if idx < 0 {
		idx = 0
	}
	if idx >= len(blockChars) {
		idx = len(blockChars) - 1
	}
	return blockChars[idx]
}
