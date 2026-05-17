package config

import (
	"fmt"

	"github.com/BurntSushi/toml"
)

// Load reads and decodes a tunnels.toml file from path.
// It does NOT validate the config; call (*Config).Validate after loading.
func Load(path string) (*Config, error) {
	var cfg Config
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		return nil, fmt.Errorf("load %s: %w", path, err)
	}
	if cfg.Tunnels == nil {
		cfg.Tunnels = map[string]Tunnel{}
	}
	if cfg.Groups == nil {
		cfg.Groups = map[string]Group{}
	}
	return &cfg, nil
}
