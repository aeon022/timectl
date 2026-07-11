package cmd

import (
	"fmt"
	"os"

	"github.com/aeon022/timectl/internal/store"
	"github.com/aeon022/timectl/internal/tui"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "timectl",
	Short: "Time tracking for developers",
	Long: `timectl — a terminal-first time tracker.

Run without arguments to open the interactive TUI.
Use subcommands for quick CLI access.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		s, err := openStore()
		if err != nil {
			return err
		}
		defer s.Close()
		return tui.Run(s)
	},
}

// Execute is the entry point called from main.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.AddCommand(
		startCmd,
		stopCmd,
		statusCmd,
		todayCmd,
		weekCmd,
		logCmd,
		deleteCmd,
		mcpCmd,
		invoiceCmd,
	)
}

func openStore() (*store.Store, error) {
	path, err := store.DefaultPath()
	if err != nil {
		return nil, fmt.Errorf("resolve db path: %w", err)
	}
	return store.Open(path)
}
