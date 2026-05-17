package config

import (
	"fmt"
	"os"
	"strconv"

	sshcfg "github.com/kevinburke/ssh_config"
)

// ResolvedTunnel is a Tunnel merged with the relevant identity fields
// from ~/.ssh/config. internal/tunnel consumes this directly.
type ResolvedTunnel struct {
	Name string

	// SSH connection identity (from ssh_config, with fallbacks)
	SSHHost      string
	SSHPort      int
	SSHUser      string
	IdentityFile string
	ProxyJump    string

	// Forward details
	Type       TunnelType
	LocalHost  string
	LocalPort  int
	RemoteAddr string // "host:port" for local/remote, empty for dynamic
}

// ResolveWithSSHConfig merges a tunnel definition with the entries in
// the ssh config file at sshConfigPath. The path may be empty to use
// the default ~/.ssh/config.
func ResolveWithSSHConfig(t Tunnel, sshConfigPath string) (ResolvedTunnel, error) {
	if sshConfigPath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ResolvedTunnel{}, fmt.Errorf("home dir: %w", err)
		}
		sshConfigPath = home + "/.ssh/config"
	}

	cfg, err := openSSHConfig(sshConfigPath)
	if err != nil {
		return ResolvedTunnel{}, err
	}

	host, err := cfg.Get(t.Host, "HostName")
	if err != nil || host == "" {
		host = t.Host
	}
	portStr, _ := cfg.Get(t.Host, "Port")
	port := 22
	if p, err := strconv.Atoi(portStr); err == nil && p > 0 {
		port = p
	}
	user, _ := cfg.Get(t.Host, "User")
	identity, _ := cfg.Get(t.Host, "IdentityFile")
	proxy, _ := cfg.Get(t.Host, "ProxyJump")

	r := ResolvedTunnel{
		SSHHost:      host,
		SSHPort:      port,
		SSHUser:      user,
		IdentityFile: identity,
		ProxyJump:    proxy,
		Type:         t.Type,
		LocalHost:    t.LocalHost,
		LocalPort:    t.LocalPort,
	}

	switch t.Type {
	case TypeLocal:
		r.RemoteAddr = fmt.Sprintf("%s:%d", t.RemoteHost, t.RemotePort)
	case TypeRemote:
		r.RemoteAddr = fmt.Sprintf("%s:%d", t.LocalHost, t.LocalPort)
	}

	if r.LocalHost == "" {
		r.LocalHost = "127.0.0.1"
	}
	return r, nil
}

func openSSHConfig(path string) (*sshcfg.Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open ssh config: %w", err)
	}
	defer func() { _ = f.Close() }()
	return sshcfg.Decode(f)
}
