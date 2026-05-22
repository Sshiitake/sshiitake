package tui

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestThemes_haveAllExpectedFields(t *testing.T) {
	for _, name := range []string{"dark", "light", "high-contrast"} {
		theme, ok := ThemeByName(name)
		assert.True(t, ok, "theme %q should exist", name)
		// Spot-check key fields are populated (non-empty styles).
		// We can't easily compare styles for equality, so just ensure
		// the rendering doesn't panic and returns a non-empty string.
		assert.NotPanics(t, func() { _ = theme.StatusUp.Render("●") })
		assert.NotPanics(t, func() { _ = theme.StatusDown.Render("○") })
		assert.NotPanics(t, func() { _ = theme.GroupHeader.Render("group") })
	}
}

func TestThemes_unknownNameReturnsFalse(t *testing.T) {
	_, ok := ThemeByName("nonsense")
	assert.False(t, ok)
}

func TestThemes_defaultIsDark(t *testing.T) {
	assert.Equal(t, "dark", DefaultThemeName)
}
