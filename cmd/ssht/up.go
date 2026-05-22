package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"

	"github.com/Sshiitake/sshiitake/internal/config"
	"github.com/Sshiitake/sshiitake/internal/manager"
	"github.com/Sshiitake/sshiitake/internal/tui"
	"github.com/Sshiitake/sshiitake/internal/tunnel"
)

func upCmd() *cobra.Command {
	var (
		cfgPath        string
		sshCfgPath     string
		knownHostsPath string
		listenFile     string
		bare           bool
		noTUI          bool
		noReconnect    bool
		theme          string
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
				Selectors:       args,
				HostKeyCallback: hostKeyCB,
				Reconnect:       !noReconnect,
			})
			if err != nil {
				return err
			}

			ctx, cancel := signal.NotifyContext(cmd.Context(),
				os.Interrupt, syscall.SIGTERM)
			defer cancel()

			eventCh := m.Subscribe(256)

			runErr := make(chan error, 1)
			go func() { runErr <- m.Run(ctx) }()

			if bare {
				defer m.Unsubscribe(eventCh)
				return streamBareEvents(cmd, eventCh, runErr, m)
			}

			// Decide TUI vs human-friendly stream.
			useTUI := !noTUI && isStdoutTTY(cmd)
			if useTUI {
				// TUI subscribes its own channel; release the local one.
				m.Unsubscribe(eventCh)
				tuiErr := make(chan error, 1)
				go func() { tuiErr <- tui.Run(ctx, m, theme) }()
				select {
				case err := <-tuiErr:
					cancel()
					<-runErr
					return err
				case err := <-runErr:
					cancel()
					<-tuiErr
					return err
				}
			}

			defer m.Unsubscribe(eventCh)
			return streamHumanEvents(cmd, eventCh, runErr, m, listenFile)
		},
	}
	cmd.Flags().StringVar(&cfgPath, "config", defaultConfigPath(), "path to tunnels.toml")
	cmd.Flags().StringVar(&sshCfgPath, "ssh-config", "", "path to ssh_config (default ~/.ssh/config)")
	cmd.Flags().StringVar(&knownHostsPath, "known-hosts", "", "path to known_hosts (default ~/.ssh/known_hosts)")
	cmd.Flags().StringVar(&listenFile, "listen-file", "", "test-only: write first tunnel listen addr here")
	_ = cmd.Flags().MarkHidden("listen-file")
	cmd.Flags().BoolVar(&bare, "bare", false, "stream newline-delimited JSON events to stdout; no human-friendly output")
	cmd.Flags().BoolVar(&noTUI, "no-tui", false, "disable the TUI even when stdout is a TTY; stream human-friendly events instead")
	cmd.Flags().BoolVar(&noReconnect, "no-reconnect", false, "do not auto-reconnect tunnels that drop")
	cmd.Flags().StringVar(&theme, "theme", tui.DefaultThemeName, "TUI theme: dark, light, or high-contrast")
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

// isStdoutTTY returns true when the command's stdout is a terminal.
// During tests cmd.OutOrStdout() is a bytes.Buffer (no Fd method) so
// this returns false and the human-stream path is used.
func isStdoutTTY(cmd *cobra.Command) bool {
	type fder interface{ Fd() uintptr }
	out := cmd.OutOrStdout()
	if f, ok := out.(fder); ok {
		return isatty.IsTerminal(f.Fd())
	}
	return false
}
