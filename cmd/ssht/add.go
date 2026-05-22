package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/Sshiitake/sshiitake/internal/config"
)

// addCmd returns the cobra command for `ssht add`.
func addCmd() *cobra.Command {
	var cfgPath string
	cmd := &cobra.Command{
		Use:   "add",
		Short: "Interactively add a new tunnel to tunnels.toml",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAddWizard(cfgPath)
		},
	}
	cmd.Flags().StringVar(&cfgPath, "config", defaultConfigPath(), "path to tunnels.toml")
	return cmd
}

// runAddWizard drives the interactive huh form, then writes the result
// to cfgPath via appendTunnel.
func runAddWizard(cfgPath string) error {
	var (
		name       string
		host       string
		tunType    string
		localPort  string
		remoteHost string
		remotePort string
		group      string
	)

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Tunnel name").
				Value(&name).
				Validate(huh.ValidateNotEmpty()),
			huh.NewInput().
				Title("Host (must exist in ~/.ssh/config)").
				Value(&host).
				Validate(huh.ValidateNotEmpty()),
			huh.NewSelect[string]().
				Title("Type").
				Options(
					huh.NewOption("Local forward (-L)", "local"),
					huh.NewOption("Remote forward (-R)", "remote"),
					huh.NewOption("Dynamic (SOCKS5, -D)", "dynamic"),
				).
				Value(&tunType),
		),
		huh.NewGroup(
			huh.NewInput().
				Title("Local port").
				Value(&localPort).
				Validate(validatePort),
			huh.NewInput().
				Title("Remote host").
				Value(&remoteHost).
				Validate(func(s string) error {
					if tunType != "dynamic" && s == "" {
						return fmt.Errorf("required for local/remote forwards")
					}
					return nil
				}),
			huh.NewInput().
				Title("Remote port").
				Value(&remotePort).
				Validate(func(s string) error {
					if tunType == "dynamic" {
						return nil
					}
					return validatePort(s)
				}),
			huh.NewInput().
				Title("Group (optional)").
				Value(&group),
		),
	)

	if err := form.Run(); err != nil {
		return err
	}

	t := config.Tunnel{
		Host:       host,
		Type:       config.TunnelType(tunType),
		Group:      group,
		LocalPort:  atoi(localPort),
		RemoteHost: remoteHost,
		RemotePort: atoi(remotePort),
	}
	return appendTunnel(cfgPath, name, t)
}

// validatePort returns nil for ports in 1..65535, error otherwise.
func validatePort(s string) error {
	n := atoi(s)
	if n < 1 || n > 65535 {
		return fmt.Errorf("port must be 1-65535")
	}
	return nil
}

// atoi parses s as a base-10 int, returning 0 on any error. Intended
// only for huh-validated input where we've already gated bad values.
func atoi(s string) int {
	var n int
	_, _ = fmt.Sscanf(s, "%d", &n)
	return n
}

// appendTunnel reads tunnels.toml, adds the named tunnel, and writes
// it back. Refuses to overwrite an existing name. If path doesn't
// exist, starts from an empty config.
//
// Writes are atomic at the directory level: the new content lands in
// a sibling temp file with 0600 perms and is then renamed over the
// target. A crash mid-write leaves the original file untouched.
func appendTunnel(path, name string, t config.Tunnel) error {
	cfg, err := loadOrEmpty(path)
	if err != nil {
		return err
	}
	if _, exists := cfg.Tunnels[name]; exists {
		return fmt.Errorf("tunnel %q already exists in %s", name, path)
	}
	if cfg.Tunnels == nil {
		cfg.Tunnels = map[string]config.Tunnel{}
	}
	cfg.Tunnels[name] = t

	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(cfg); err != nil {
		return fmt.Errorf("encode: %w", err)
	}

	// Ensure parent directory exists.
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("mkdir parent: %w", err)
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), ".tunnels.toml.*")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmpPath := tmp.Name()
	// Safe if already renamed away: Remove on a non-existent path is a
	// no-op we deliberately swallow.
	defer func() { _ = os.Remove(tmpPath) }()

	if err := os.Chmod(tmpPath, 0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod temp: %w", err)
	}
	if _, err := tmp.Write(buf.Bytes()); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp: %w", err)
	}
	return os.Rename(tmpPath, path)
}

// loadOrEmpty returns config.Load(path), or an empty *Config if path
// does not exist.
func loadOrEmpty(path string) (*config.Config, error) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return &config.Config{Tunnels: map[string]config.Tunnel{}}, nil
	} else if err != nil {
		return nil, err
	}
	return config.Load(path)
}
