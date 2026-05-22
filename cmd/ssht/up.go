package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/Sshiitake/sshiitake/internal/config"
	"github.com/Sshiitake/sshiitake/internal/manager"
	"github.com/Sshiitake/sshiitake/internal/tunnel"
)

func upCmd() *cobra.Command {
	var (
		cfgPath        string
		sshCfgPath     string
		knownHostsPath string
		listenFile     string
		bare           bool
	)
	cmd := &cobra.Command{
		Use:   "up <name|group>...",
		Short: "Bring up one or more tunnels (or a group) and run until interrupted",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(cfgPath)
			if err != nil {
				return err
			}
			if err := cfg.Validate(); err != nil {
				return err
			}

			hostKeyCB, err := buildHostKeyCallback(knownHostsPath)
			if err != nil {
				return err
			}

			m, err := manager.New(cfg, sshCfgPath, manager.Options{
				Selectors:           args,
				HostKeyCallback:     hostKeyCB,
				HostKeyVerification: true,
			})
			if err != nil {
				return err
			}

			ctx, cancel := signal.NotifyContext(cmd.Context(),
				os.Interrupt, syscall.SIGTERM)
			defer cancel()

			eventCh := m.Subscribe(256)
			defer m.Unsubscribe(eventCh)

			runErr := make(chan error, 1)
			go func() { runErr <- m.Run(ctx) }()

			if bare {
				return streamBareEvents(cmd, eventCh, runErr, m)
			}

			return streamHumanEvents(cmd, eventCh, runErr, m, listenFile)
		},
	}
	cmd.Flags().StringVar(&cfgPath, "config", defaultConfigPath(), "path to tunnels.toml")
	cmd.Flags().StringVar(&sshCfgPath, "ssh-config", "", "path to ssh_config (default ~/.ssh/config)")
	cmd.Flags().StringVar(&knownHostsPath, "known-hosts", "", "path to known_hosts (default ~/.ssh/known_hosts)")
	cmd.Flags().StringVar(&listenFile, "listen-file", "", "test-only: write first tunnel listen addr here")
	_ = cmd.Flags().MarkHidden("listen-file")
	cmd.Flags().BoolVar(&bare, "bare", false, "stream newline-delimited JSON events to stdout; no human-friendly output")
	return cmd
}

// streamHumanEvents prints state changes to the user until ctx is done
// or Run returns.
func streamHumanEvents(cmd *cobra.Command, eventCh chan manager.Event, runErr chan error, m *manager.Manager, listenFile string) error {
	out := cmd.OutOrStdout()
	upCount := 0

	for {
		select {
		case e, ok := <-eventCh:
			if !ok {
				return <-runErr
			}
			if e.Type == manager.EventTunnelState {
				switch e.Status {
				case tunnel.StatusUp:
					upCount++
					for _, tun := range m.Tunnels() {
						if tun.Name() == e.TunnelName {
							_, _ = fmt.Fprintf(out, "tunnel %q up on %s\n", e.TunnelName, tun.LocalAddr())
							if listenFile != "" && upCount == 1 {
								_ = os.WriteFile(listenFile, []byte(tun.LocalAddr()), 0o600)
							}
							break
						}
					}
				case tunnel.StatusDown:
					_, _ = fmt.Fprintf(out, "tunnel %q down\n", e.TunnelName)
				}
			}
		case err := <-runErr:
			return err
		}
	}
}
