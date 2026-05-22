// Package main `add` wizard tests.
//
// These tests exercise the testable seam (appendTunnel), not the
// interactive huh form. The form requires a TTY and cannot be driven
// headlessly without significant scaffolding. The form -> appendTunnel
// boundary is a thin variable-binding step; the toml-write logic in
// appendTunnel is what carries the risk worth testing.
package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/BurntSushi/toml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Sshiitake/sshiitake/internal/config"
)

func TestAdd_appendsTunnelToTOML(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "tunnels.toml")
	require.NoError(t, os.WriteFile(cfgPath, []byte(`
[tunnels.existing]
host = "hostA"
type = "local"
local_port = 8000
remote_host = "127.0.0.1"
remote_port = 80
`), 0o600))

	err := appendTunnel(cfgPath, "newone", config.Tunnel{
		Host:       "hostB",
		Type:       config.TypeLocal,
		LocalPort:  9000,
		RemoteHost: "10.0.0.1",
		RemotePort: 443,
	})
	require.NoError(t, err)

	cfg, err := config.Load(cfgPath)
	require.NoError(t, err)
	assert.Contains(t, cfg.Tunnels, "existing")
	assert.Contains(t, cfg.Tunnels, "newone")
	assert.Equal(t, "hostB", cfg.Tunnels["newone"].Host)
	assert.Equal(t, config.TypeLocal, cfg.Tunnels["newone"].Type)
	assert.Equal(t, 9000, cfg.Tunnels["newone"].LocalPort)
}

func TestAdd_duplicateNameRejected(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "tunnels.toml")
	require.NoError(t, os.WriteFile(cfgPath, []byte(`
[tunnels.existing]
host = "h"
type = "dynamic"
local_port = 1080
`), 0o600))

	err := appendTunnel(cfgPath, "existing", config.Tunnel{
		Host: "h2", Type: config.TypeDynamic, LocalPort: 1081,
	})
	require.Error(t, err)
	assert.ErrorContains(t, err, "already exists")
}

func TestAppendTunnel_createsParentDir(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "sub", "nested", "tunnels.toml")
	require.NoError(t, appendTunnel(cfgPath, "x", config.Tunnel{
		Host: "h", Type: config.TypeDynamic, LocalPort: 1080,
	}))
	stat, err := os.Stat(cfgPath)
	require.NoError(t, err)
	assert.False(t, stat.IsDir())
}

func TestAppendTunnel_enforces0600OnExistingFile(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "tunnels.toml")
	// Pre-existing with overbroad perms.
	require.NoError(t, os.WriteFile(cfgPath, []byte{}, 0o644))
	require.NoError(t, appendTunnel(cfgPath, "x", config.Tunnel{
		Host: "h", Type: config.TypeDynamic, LocalPort: 1080,
	}))
	stat, err := os.Stat(cfgPath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), stat.Mode().Perm())
}

func TestAdd_serialisesValidTOML(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "tunnels.toml")
	require.NoError(t, os.WriteFile(cfgPath, []byte{}, 0o600))

	require.NoError(t, appendTunnel(cfgPath, "x", config.Tunnel{
		Host:       "h",
		Type:       config.TypeLocal,
		LocalPort:  1234,
		RemoteHost: "r",
		RemotePort: 80,
	}))

	data, err := os.ReadFile(cfgPath)
	require.NoError(t, err)
	var cfg config.Config
	_, err = toml.Decode(string(data), &cfg)
	require.NoError(t, err)
	assert.Contains(t, cfg.Tunnels, "x")
	assert.False(t, bytes.HasSuffix(data, []byte("\n\n\n")), "no excessive trailing blank lines")
}
