package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

func rootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "ssht",
		Short: "A TUI SSH tunnel manager",
		Long: `ssht is a small, focused SSH tunnel manager.
Define your forwards once in ~/.config/sshiitake/tunnels.toml,
bring them up with ` + "`ssht up <name>`" + `.`,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(versionCmd())
	return root
}

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the ssht version",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintf(cmd.OutOrStdout(),
				"ssht %s (commit %s, built %s)\n", version, commit, date)
			return nil
		},
	}
}
