package config

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoad_validFixture(t *testing.T) {
	cfg, err := Load("../../testdata/tunnels-valid.toml")
	require.NoError(t, err)
	require.NotNil(t, cfg)

	require.Len(t, cfg.Tunnels, 2)

	api, ok := cfg.TunnelByName("api-prod")
	require.True(t, ok)
	assert.Equal(t, "bastion-prod", api.Host)
	assert.Equal(t, TypeLocal, api.Type)
	assert.Equal(t, 8443, api.LocalPort)
	assert.Equal(t, "api.internal", api.RemoteHost)
	assert.Equal(t, 443, api.RemotePort)
	assert.Equal(t, "work-stack", api.Group)

	socks, ok := cfg.TunnelByName("socks-eu")
	require.True(t, ok)
	assert.Equal(t, TypeDynamic, socks.Type)
	assert.Equal(t, 1080, socks.LocalPort)

	require.Len(t, cfg.Groups, 1)
	assert.Equal(t, "Production work stack", cfg.Groups["work-stack"].Description)
}

func TestLoad_missingFile(t *testing.T) {
	_, err := Load("/nonexistent/path.toml")
	require.Error(t, err)
	assert.ErrorContains(t, err, "no such file")
}

func TestLoad_invalidTOML(t *testing.T) {
	tmpFile := t.TempDir() + "/bad.toml"
	require.NoError(t, writeFile(tmpFile, []byte("this is = not [ valid toml")))
	_, err := Load(tmpFile)
	require.Error(t, err)
}

func writeFile(path string, b []byte) error {
	return os.WriteFile(path, b, 0o600)
}
