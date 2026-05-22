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

// TestResolve_missingSSHConfigFallsBackToHostLiteral covers the CI
// fallback path: when the configured ssh_config file doesn't exist
// (common on fresh macOS GH runners), resolve treats t.Host as a
// literal SSH hostname rather than erroring.
func TestResolve_missingSSHConfigFallsBackToHostLiteral(t *testing.T) {
	tun := Tunnel{
		Host: "my-host", Type: TypeDynamic, LocalPort: 1080,
	}
	r, err := ResolveWithSSHConfig(tun, "/no/such/path")
	require.NoError(t, err)
	assert.Equal(t, "my-host", r.SSHHost)
	assert.Equal(t, 22, r.SSHPort)
	assert.Equal(t, "127.0.0.1", r.LocalHost)
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
