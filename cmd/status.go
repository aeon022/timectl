package cmd

import (
	"fmt"

	"github.com/aeon022/timectl/internal/models"
	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show the currently running timer",
	RunE: func(cmd *cobra.Command, args []string) error {
		s, err := openStore()
		if err != nil {
			return err
		}
		defer s.Close()

		running, err := s.Running()
		if err != nil {
			return err
		}

		if running == nil {
			fmt.Println("No timer running.")
			return nil
		}

		proj := ""
		if running.Project != "" {
			proj = " (" + running.Project + ")"
		}
		fmt.Printf("▶ %s%s — running for %s\n",
			running.Task, proj, models.FormatDuration(running.ComputedDuration()))
		return nil
	},
}
