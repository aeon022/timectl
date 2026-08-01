package cmd

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/aeon022/timectl/internal/models"
	"github.com/spf13/cobra"
)

var remindAfterMin int

var remindCmd = &cobra.Command{
	Use:   "remind",
	Short: "Send a macOS notification if the running timer has gone on too long",
	Long: `Check whether a timer is running and, if it's been running
longer than N minutes (120 by default), send a macOS notification.
Same pattern habctl's own "remind" uses — ideal as a launchd job
running every 15-30 minutes.`,
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
			fmt.Println("No timer running — nothing to remind.")
			return nil
		}

		elapsed := running.ComputedDuration()
		if int(elapsed.Minutes()) < remindAfterMin {
			fmt.Printf("Timer running %s — under the %dm threshold.\n", models.FormatDuration(elapsed), remindAfterMin)
			return nil
		}

		task := running.Task
		if running.Project != "" {
			task = task + " @" + running.Project
		}
		title := "Timer still running"
		body := fmt.Sprintf("%s — %s", task, models.FormatDuration(elapsed))

		script := fmt.Sprintf(`display notification %q with title %q`, body, title)
		out, err := exec.Command("osascript", "-e", script).CombinedOutput()
		if err != nil {
			fmt.Printf("Reminder: %s — %s\n", title, body)
			if len(out) > 0 {
				fmt.Printf("osascript: %s\n", strings.TrimSpace(string(out)))
			}
			return nil
		}

		fmt.Printf("Notified: %s\n", body)
		return nil
	},
}

func init() {
	remindCmd.Flags().IntVar(&remindAfterMin, "after", 120, "Notify if the running timer has gone on longer than this many minutes")
	rootCmd.AddCommand(remindCmd)
}
