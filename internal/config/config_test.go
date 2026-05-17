package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTunnelType_String(t *testing.T) {
	tests := []struct {
		in   TunnelType
		want string
	}{
		{TypeLocal, "local"},
		{TypeRemote, "remote"},
		{TypeDynamic, "dynamic"},
	}
	for _, tc := range tests {
		assert.Equal(t, tc.want, tc.in.String())
	}
}

func TestConfig_TunnelByName(t *testing.T) {
	cfg := &Config{
		Tunnels: map[string]Tunnel{
			"api": {Host: "bastion", Type: TypeLocal, LocalPort: 8443},
		},
	}
	got, ok := cfg.TunnelByName("api")
	assert.True(t, ok)
	assert.Equal(t, "bastion", got.Host)

	_, ok = cfg.TunnelByName("nope")
	assert.False(t, ok)
}
