package cmd

import (
	"fmt"

	"github.com/aeon022/timectl/internal/models"
	"github.com/spf13/cobra"
)

var stopNotes string

var stopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the running timer",
	RunE: func(cmd *cobra.Command, args []string) error {
		s, err := openStore()
		if err != nil {
			return err
		}
		defer s.Close()

		entry, err := s.Stop(stopNotes)
		if err != nil {
			return err
		}

		proj := ""
		if entry.Project != "" {
			proj = " (" + entry.Project + ")"
		}

		fmt.Printf("■ Stopped: %s%s\n  %s → %s — %s\n",
			entry.Task,
			proj,
			entry.StartedAt.Format("15:04:05"),
			entry.StoppedAt.Format("15:04:05"),
			models.FormatDuration(entry.Duration),
		)
		return nil
	},
}

func init() {
	stopCmd.Flags().StringVarP(&stopNotes, "notes", "n", "", "Notes to attach")
}
