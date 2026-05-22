package tui

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDefaultKeys_shortHelpHasFooterBindings(t *testing.T) {
	short := defaultKeys.ShortHelp()
	assert.NotEmpty(t, short, "ShortHelp should populate footer bindings")
}

func TestDefaultKeys_fullHelpHasMultipleRows(t *testing.T) {
	full := defaultKeys.FullHelp()
	assert.GreaterOrEqual(t, len(full), 2, "FullHelp should have at least 2 rows")
	for i, row := range full {
		assert.NotEmpty(t, row, "row %d should have bindings", i)
	}
}
