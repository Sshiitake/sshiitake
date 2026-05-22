package manager

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Sshiitake/sshiitake/internal/config"
)

func TestNew_emptyConfig(t *testing.T) {
	m, err := New(&config.Config{Tunnels: map[string]config.Tunnel{}}, "", Options{})
	require.NoError(t, err)
	require.NotNil(t, m)
	assert.Len(t, m.Tunnels(), 0)
}

func TestNew_singleTunnelLoaded(t *testing.T) {
	cfg := &config.Config{
		Tunnels: map[string]config.Tunnel{
			"api": {Host: "localhost", Type: config.TypeLocal,
				LocalPort: 18443, RemoteHost: "127.0.0.1", RemotePort: 80},
		},
	}
	m, err := New(cfg, "", Options{HostKeyVerification: false})
	require.NoError(t, err)
	assert.Len(t, m.Tunnels(), 1)
	assert.Equal(t, "api", m.Tunnels()[0].Name())
}

func TestNew_unknownNameFails(t *testing.T) {
	cfg := &config.Config{Tunnels: map[string]config.Tunnel{}}
	_, err := New(cfg, "", Options{Selectors: []string{"nope"}})
	require.Error(t, err)
	assert.ErrorContains(t, err, "not found")
}
