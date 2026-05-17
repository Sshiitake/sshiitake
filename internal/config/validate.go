package config

import (
	"errors"
	"fmt"
)

// Validate checks the config for internal consistency.
// Returns the first error encountered, with a wrapped message that
// identifies the offending tunnel.
func (c *Config) Validate() error {
	if c == nil {
		return errors.New("config is nil")
	}
	for name, t := range c.Tunnels {
		if name == "" {
			return errors.New("tunnel name must not be empty")
		}
		if err := validateTunnel(t); err != nil {
			return fmt.Errorf("tunnel %q: %w", name, err)
		}
		if t.Group != "" {
			if _, ok := c.Groups[t.Group]; !ok {
				return fmt.Errorf("tunnel %q: references unknown group %q", name, t.Group)
			}
		}
	}
	return nil
}

func validateTunnel(t Tunnel) error {
	if t.Host == "" {
		return errors.New("host must not be empty")
	}
	switch t.Type {
	case TypeLocal, TypeRemote, TypeDynamic:
	default:
		return fmt.Errorf("unknown type %q (want local, remote, or dynamic)", t.Type)
	}
	if !validPort(t.LocalPort) {
		return fmt.Errorf("local_port %d out of range (1-65535)", t.LocalPort)
	}
	switch t.Type {
	case TypeLocal:
		if t.RemoteHost == "" {
			return errors.New("local tunnel requires remote_host")
		}
		if !validPort(t.RemotePort) {
			return fmt.Errorf("remote_port %d out of range (1-65535)", t.RemotePort)
		}
	case TypeRemote:
		if !validPort(t.RemotePort) {
			return fmt.Errorf("remote_port %d out of range (1-65535)", t.RemotePort)
		}
	case TypeDynamic:
		// only local_port required
	}
	return nil
}

func validPort(p int) bool { return p >= 1 && p <= 65535 }
