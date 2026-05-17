// Package config defines the on-disk schema for tunnels.toml and merges
// it with ~/.ssh/config to produce ResolvedTunnel values ready for
// internal/tunnel to use.
package config

// TunnelType is the kind of port forward.
type TunnelType string

// Supported TunnelType values matching the on-disk schema.
const (
	TypeLocal   TunnelType = "local"
	TypeRemote  TunnelType = "remote"
	TypeDynamic TunnelType = "dynamic"
)

// String makes TunnelType satisfy fmt.Stringer.
func (t TunnelType) String() string { return string(t) }

// Config is the parsed contents of tunnels.toml.
type Config struct {
	Tunnels map[string]Tunnel `toml:"tunnels"`
	Groups  map[string]Group  `toml:"groups"`
}

// Tunnel is a single tunnel definition.
type Tunnel struct {
	Host       string     `toml:"host"`
	Type       TunnelType `toml:"type"`
	LocalHost  string     `toml:"local_host"`
	LocalPort  int        `toml:"local_port"`
	RemoteHost string     `toml:"remote_host"`
	RemotePort int        `toml:"remote_port"`
	Group      string     `toml:"group"`
}

// Group is a named collection of tunnels.
type Group struct {
	Description string `toml:"description"`
}

// TunnelByName returns the named tunnel, or false if not found.
func (c *Config) TunnelByName(name string) (Tunnel, bool) {
	t, ok := c.Tunnels[name]
	return t, ok
}
