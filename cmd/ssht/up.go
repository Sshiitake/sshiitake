package main

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/crypto/ssh"

	"github.com/Sshiitake/sshiitake/internal/config"
	"github.com/Sshiitake/sshiitake/internal/tunnel"
)

func upCmd() *cobra.Command {
	var (
		cfgPath    string
		sshCfgPath string
		listenFile string // hidden: test-only, write the actual listen address here
	)
	cmd := &cobra.Command{
		Use:   "up <name>",
		Short: "Bring up a tunnel by name and run until interrupted",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]

			cfg, err := config.Load(cfgPath)
			if err != nil {
				return err
			}
			if err := cfg.Validate(); err != nil {
				return err
			}
			rawTunnel, ok := cfg.TunnelByName(name)
			if !ok {
				return fmt.Errorf("tunnel %q not found in %s", name, cfgPath)
			}
			rt, err := config.ResolveWithSSHConfig(rawTunnel, sshCfgPath)
			if err != nil {
				return err
			}
			rt.Name = name

			hostKeyCB, err := buildHostKeyCallback()
			if err != nil {
				return err
			}

			tun := tunnel.New(rt, tunnel.Options{
				HostKeyCallback: hostKeyCB,
				DialTimeout:     10 * time.Second,
			})

			ctx, cancel := signal.NotifyContext(cmd.Context(),
				os.Interrupt, syscall.SIGTERM)
			defer cancel()

			started := make(chan struct{})

			errCh := make(chan error, 1)
			go func() {
				errCh <- tun.Start(ctx, started)
			}()

			select {
			case <-started:
				fmt.Fprintf(cmd.OutOrStdout(),
					"tunnel %q up on %s\n", name, tun.LocalAddr())
				if listenFile != "" {
					_ = os.WriteFile(listenFile, []byte(tun.LocalAddr()), 0o600)
				}
			case err := <-errCh:
				return err
			case <-ctx.Done():
				return nil
			}

			return <-errCh
		},
	}
	cmd.Flags().StringVar(&cfgPath, "config", defaultConfigPath(), "path to tunnels.toml")
	cmd.Flags().StringVar(&sshCfgPath, "ssh-config", "", "path to ssh_config (default ~/.ssh/config)")
	cmd.Flags().StringVar(&listenFile, "listen-file", "", "test-only: write listen addr to this path")
	_ = cmd.Flags().MarkHidden("listen-file")
	return cmd
}

// buildHostKeyCallback chooses the host-key verification strategy.
//
// In tests, SSHT_TEST_HOSTKEY pins a single base64-encoded host key.
// In production, this will use known_hosts (Phase 4). For Phase 1
// we deliberately FAIL if neither is set, to avoid silently accepting
// any host.
func buildHostKeyCallback() (ssh.HostKeyCallback, error) {
	if pinned := os.Getenv("SSHT_TEST_HOSTKEY"); pinned != "" {
		raw, err := base64.StdEncoding.DecodeString(pinned)
		if err != nil {
			return nil, fmt.Errorf("SSHT_TEST_HOSTKEY: %w", err)
		}
		pub, err := ssh.ParsePublicKey(raw)
		if err != nil {
			return nil, fmt.Errorf("SSHT_TEST_HOSTKEY: %w", err)
		}
		return ssh.FixedHostKey(pub), nil
	}
	return nil, errors.New("host key verification not configured: " +
		"set SSHT_TEST_HOSTKEY for tests, or wait for Phase 4 known_hosts support")
}
