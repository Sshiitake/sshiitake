package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidate_validFixture(t *testing.T) {
	cfg, err := Load("../../testdata/tunnels-valid.toml")
	require.NoError(t, err)
	require.NoError(t, cfg.Validate())
}

func TestValidate_emptyName(t *testing.T) {
	cfg := &Config{Tunnels: map[string]Tunnel{
		"": {Host: "h", Type: TypeLocal, LocalPort: 1234, RemoteHost: "r", RemotePort: 80},
	}}
	err := cfg.Validate()
	require.Error(t, err)
	assert.ErrorContains(t, err, "tunnel name must not be empty")
}

func TestValidate_unknownType(t *testing.T) {
	cfg := &Config{Tunnels: map[string]Tunnel{
		"x": {Host: "h", Type: "weird", LocalPort: 1234},
	}}
	err := cfg.Validate()
	require.Error(t, err)
	assert.ErrorContains(t, err, "unknown type")
}

func TestValidate_portOutOfRange(t *testing.T) {
	cfg := &Config{Tunnels: map[string]Tunnel{
		"x": {Host: "h", Type: TypeLocal, LocalPort: 70000, RemoteHost: "r", RemotePort: 80},
	}}
	err := cfg.Validate()
	require.Error(t, err)
	assert.ErrorContains(t, err, "local_port")
}

func TestValidate_localMissingRemote(t *testing.T) {
	cfg := &Config{Tunnels: map[string]Tunnel{
		"x": {Host: "h", Type: TypeLocal, LocalPort: 1234},
	}}
	err := cfg.Validate()
	require.Error(t, err)
	assert.ErrorContains(t, err, "remote_host")
}

func TestValidate_dynamicNeedsOnlyLocalPort(t *testing.T) {
	cfg := &Config{Tunnels: map[string]Tunnel{
		"socks": {Host: "h", Type: TypeDynamic, LocalPort: 1080},
	}}
	require.NoError(t, cfg.Validate())
}

func TestValidate_groupReferenceUnknown(t *testing.T) {
	cfg := &Config{
		Tunnels: map[string]Tunnel{
			"x": {Host: "h", Type: TypeDynamic, LocalPort: 1080, Group: "no-such-group"},
		},
	}
	err := cfg.Validate()
	require.Error(t, err)
	assert.ErrorContains(t, err, "no-such-group")
}
