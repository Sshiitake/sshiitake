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

func TestConfigCheck_validFixture(t *testing.T) {
	var stdout bytes.Buffer
	cmd := rootCmd()
	cmd.SetOut(&stdout)
	cmd.SetArgs([]string{
		"config", "check",
		"--config", "../../testdata/tunnels-valid.toml",
		"--ssh-config", "../../testdata/ssh_config_sample",
	})
	require.NoError(t, cmd.Execute())
	assert.Contains(t, stdout.String(), "OK")
}

func TestConfigCheck_missingFile(t *testing.T) {
	var stderr bytes.Buffer
	cmd := rootCmd()
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{
		"config", "check",
		"--config", "/nonexistent.toml",
	})
	err := cmd.Execute()
	require.Error(t, err)
}
