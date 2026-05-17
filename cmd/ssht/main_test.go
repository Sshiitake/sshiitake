package main

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVersionCommand(t *testing.T) {
	var stdout bytes.Buffer
	cmd := rootCmd()
	cmd.SetOut(&stdout)
	cmd.SetArgs([]string{"version"})

	require.NoError(t, cmd.Execute())
	assert.Contains(t, stdout.String(), "ssht")
}

func TestRootHelp(t *testing.T) {
	var stdout bytes.Buffer
	cmd := rootCmd()
	cmd.SetOut(&stdout)
	cmd.SetArgs([]string{"--help"})

	require.NoError(t, cmd.Execute())
	out := stdout.String()
	assert.Contains(t, out, "ssht")
	assert.Contains(t, out, "version")
}
