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

func TestValidate_localHostLoopbackOK(t *testing.T) {
	cfg := &Config{Tunnels: map[string]Tunnel{
		"x": {Host: "h", Type: TypeLocal, LocalHost: "127.0.0.1",
			LocalPort: 1234, RemoteHost: "r", RemotePort: 80},
	}}
	require.NoError(t, cfg.Validate())
}

func TestValidate_localHostNonLoopbackRejected(t *testing.T) {
	cfg := &Config{Tunnels: map[string]Tunnel{
		"x": {Host: "h", Type: TypeLocal, LocalHost: "0.0.0.0",
			LocalPort: 1234, RemoteHost: "r", RemotePort: 80},
	}}
	err := cfg.Validate()
	require.Error(t, err)
	assert.ErrorContains(t, err, "local_host")
	assert.ErrorContains(t, err, "0.0.0.0")
}

func TestValidate_localHostIPv6LoopbackOK(t *testing.T) {
	cfg := &Config{Tunnels: map[string]Tunnel{
		"x": {Host: "h", Type: TypeLocal, LocalHost: "::1",
			LocalPort: 1234, RemoteHost: "r", RemotePort: 80},
	}}
	require.NoError(t, cfg.Validate())
}

func TestValidate_localHostExternalRejected(t *testing.T) {
	cfg := &Config{Tunnels: map[string]Tunnel{
		"x": {Host: "h", Type: TypeLocal, LocalHost: "192.168.1.50",
			LocalPort: 1234, RemoteHost: "r", RemotePort: 80},
	}}
	require.Error(t, cfg.Validate())
}

// TestValidate_localPortZeroOK accepts local_port = 0 as "auto-pick"
// so callers can ask the OS for an ephemeral port and read LocalAddr()
// after Start. Without this, tests would have to reserve a port and
// race the tunnel for binding it.
func TestValidate_localPortZeroOK(t *testing.T) {
	cfg := &Config{Tunnels: map[string]Tunnel{
		"x": {Host: "h", Type: TypeLocal, LocalPort: 0,
			RemoteHost: "r", RemotePort: 80},
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
