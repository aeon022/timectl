package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var (
	startProject string
	startNotes   string
)

var startCmd = &cobra.Command{
	Use:   "start TASK",
	Short: "Start a timer",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		task := args[0]

		s, err := openStore()
		if err != nil {
			return err
		}
		defer s.Close()

		entry, err := s.Start(task, startProject)
		if err != nil {
			return err
		}

		proj := ""
		if entry.Project != "" {
			proj = " (" + entry.Project + ")"
		}
		fmt.Printf("▶ Started: %s%s\n  %s — running\n",
			entry.Task, proj, entry.StartedAt.Format("15:04:05"))
		return nil
	},
}

func init() {
	startCmd.Flags().StringVarP(&startProject, "project", "p", "", "Project / tag name")
	startCmd.Flags().StringVarP(&startNotes, "notes", "n", "", "Notes")
}
