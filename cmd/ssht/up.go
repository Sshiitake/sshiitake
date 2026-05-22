package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"

	"github.com/Sshiitake/sshiitake/internal/config"
	"github.com/Sshiitake/sshiitake/internal/manager"
	"github.com/Sshiitake/sshiitake/internal/reload"
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
		noReload       bool
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

			// Hot-reload watcher: re-loads tunnels.toml on edit and
			// applies the diff. Errors are logged to stderr but never
			// interrupt the running tunnels.
			if !noReload {
				startReloadWatcher(ctx, cmd, cfgPath, sshCfgPath, cfg, m)
			}

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
	cmd.Flags().BoolVar(&noReload, "no-reload", false, "do not watch tunnels.toml for hot-reload")
	cmd.Flags().StringVar(&theme, "theme", tui.DefaultThemeName, "TUI theme: dark, light, or high-contrast")
	return cmd
}

// startReloadWatcher launches a goroutine that watches cfgPath for
// changes and, on each debounced write, reloads tunnels.toml and
// applies the diff to the running Manager.
//
// Errors during reload (invalid TOML, validation failure, partial Apply
// failure) are written to stderr with a [reload] prefix; the running
// tunnels are never torn down because of a bad edit. The user can then
// fix the file and the next save reapplies.
func startReloadWatcher(ctx context.Context, cmd *cobra.Command, cfgPath, sshCfgPath string, initialCfg *config.Config, m *manager.Manager) {
	w, err := reload.New(cfgPath, reload.DefaultDebounce)
	if err != nil {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "[reload] watcher disabled: %v\n", err)
		return
	}
	go func() { _ = w.Run(ctx) }()
	go func() {
		current := initialCfg
		for {
			select {
			case <-ctx.Done():
				return
			case <-w.Changed:
				newCfg, err := config.Load(cfgPath)
				if err != nil {
					_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "[reload] load failed: %v\n", err)
					continue
				}
				if err := newCfg.Validate(); err != nil {
					_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "[reload] validate failed: %v\n", err)
					continue
				}
				plan := reload.Diff(current, newCfg)
				if plan.Empty() {
					continue
				}
				if err := m.Apply(newCfg, plan); err != nil {
					_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "[reload] apply failed: %v\n", err)
					// Even on partial failure we adopt newCfg as the
					// reference for the next diff: handles that
					// succeeded are now part of the live set, and
					// re-trying the same edit twice would otherwise
					// keep emitting the same error.
				}
				current = newCfg
			}
		}
	}()
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
