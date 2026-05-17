package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Sshiitake/sshiitake/internal/config"
)

func configCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "config",
		Short: "Inspect and validate the tunnels.toml file",
	}
	c.AddCommand(configCheckCmd())
	return c
}

func configCheckCmd() *cobra.Command {
	var (
		cfgPath    string
		sshCfgPath string
	)
	cmd := &cobra.Command{
		Use:   "check",
		Short: "Validate tunnels.toml and resolve hosts against ~/.ssh/config",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(cfgPath)
			if err != nil {
				return err
			}
			if err := cfg.Validate(); err != nil {
				return err
			}
			for name, t := range cfg.Tunnels {
				rt, err := config.ResolveWithSSHConfig(t, sshCfgPath)
				if err != nil {
					return fmt.Errorf("resolve %q: %w", name, err)
				}
				_ = rt
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "OK: %d tunnels, %d groups\n",
				len(cfg.Tunnels), len(cfg.Groups))
			return nil
		},
	}
	cmd.Flags().StringVar(&cfgPath, "config", defaultConfigPath(), "path to tunnels.toml")
	cmd.Flags().StringVar(&sshCfgPath, "ssh-config", "", "path to ssh_config (default ~/.ssh/config)")
	return cmd
}
