package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolve_matchingHost(t *testing.T) {
	tun := Tunnel{
		Host: "bastion-prod", Type: TypeLocal, LocalPort: 8443,
		RemoteHost: "api.internal", RemotePort: 443,
	}
	r, err := ResolveWithSSHConfig(tun, "../../testdata/ssh_config_sample")
	require.NoError(t, err)

	assert.Equal(t, "203.0.113.10", r.SSHHost)
	assert.Equal(t, 2200, r.SSHPort)
	assert.Equal(t, "adam", r.SSHUser)
	assert.Contains(t, r.IdentityFile, "id_ed25519_work")
	assert.Empty(t, r.ProxyJump)

	assert.Equal(t, TypeLocal, r.Type)
	assert.Equal(t, 8443, r.LocalPort)
	assert.Equal(t, "api.internal:443", r.RemoteAddr)
}

func TestResolve_fallbackDefaults(t *testing.T) {
	tun := Tunnel{
		Host: "unmatched-host.example.com", Type: TypeDynamic, LocalPort: 1080,
	}
	r, err := ResolveWithSSHConfig(tun, "../../testdata/ssh_config_sample")
	require.NoError(t, err)

	// No match in ssh_config: SSHHost falls back to the literal hostname.
	assert.Equal(t, "unmatched-host.example.com", r.SSHHost)
	assert.Equal(t, 22, r.SSHPort)
}

func TestResolve_proxyJump(t *testing.T) {
	tun := Tunnel{
		Host: "api.internal", Type: TypeLocal, LocalPort: 8443,
		RemoteHost: "127.0.0.1", RemotePort: 443,
	}
	r, err := ResolveWithSSHConfig(tun, "../../testdata/ssh_config_sample")
	require.NoError(t, err)
	assert.Equal(t, "bastion-prod", r.ProxyJump)
	assert.Equal(t, "svc", r.SSHUser)
}
