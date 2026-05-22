package tui

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHelpView_includesKeyBindings(t *testing.T) {
	h := newHelpModel(defaultKeys, darkTheme())
	v := h.view()
	for _, want := range []string{"toggle", "details", "filter", "quit"} {
		assert.Contains(t, v, want, "help should include %q", want)
	}
}

func TestHelpView_includesTunnelTypeDiagrams(t *testing.T) {
	h := newHelpModel(defaultKeys, darkTheme())
	v := h.view()
	// The diagrams use box drawing; spot-check for the local-forward example.
	assert.Contains(t, v, "Local forward")
	assert.Contains(t, v, "Remote forward")
	assert.Contains(t, v, "Dynamic")
	// Box-drawing presence.
	assert.True(t, strings.ContainsAny(v, "┌└─│"), "should contain ASCII box characters")
}
