package cmd

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/aeon022/timectl/internal/models"
	"github.com/spf13/cobra"
)

var todayJSON bool

var todayCmd = &cobra.Command{
	Use:   "today",
	Short: "List today's time entries",
	RunE: func(cmd *cobra.Command, args []string) error {
		s, err := openStore()
		if err != nil {
			return err
		}
		defer s.Close()

		entries, err := s.Today()
		if err != nil {
			return err
		}

		if todayJSON {
			return printEntriesJSON(entries)
		}

		printEntriesTable(entries)
		return nil
	},
}

func init() {
	todayCmd.Flags().BoolVar(&todayJSON, "json", false, "Output as JSON")
}

func printEntriesTable(entries []models.Entry) {
	if len(entries) == 0 {
		fmt.Println("  No entries today.")
		return
	}

	var total time.Duration
	for _, e := range entries {
		d := e.ComputedDuration()
		total += d

		start := e.StartedAt.Format("15:04")
		var stop string
		if e.StoppedAt != nil {
			stop = e.StoppedAt.Format("15:04")
		} else {
			stop = "     "
		}

		proj := ""
		if e.Project != "" {
			proj = " [" + e.Project + "]"
		}

		fmt.Printf("  %s – %s  %-7s  %-30s%s\n",
			start, stop, models.FormatDuration(d), e.Task, proj)
	}

	fmt.Println("  " + strings.Repeat("─", 60))
	fmt.Printf("  Total: %s\n", models.FormatDuration(total))
}

func printEntriesJSON(entries []models.Entry) error {
	type jsonEntry struct {
		ID              int64   `json:"id"`
		Task            string  `json:"task"`
		Project         string  `json:"project"`
		StartedAt       string  `json:"started_at"`
		StoppedAt       *string `json:"stopped_at,omitempty"`
		DurationSeconds int     `json:"duration_seconds"`
		DurationHuman   string  `json:"duration_human"`
		Notes           string  `json:"notes"`
		Running         bool    `json:"running"`
	}

	var out []jsonEntry
	var totalSec int
	for _, e := range entries {
		d := e.ComputedDuration()
		totalSec += int(d.Seconds())
		je := jsonEntry{
			ID:              e.ID,
			Task:            e.Task,
			Project:         e.Project,
			StartedAt:       e.StartedAt.Format(time.RFC3339),
			DurationSeconds: int(d.Seconds()),
			DurationHuman:   models.FormatDuration(d),
			Notes:           e.Notes,
			Running:         e.IsRunning(),
		}
		if e.StoppedAt != nil {
			s := e.StoppedAt.Format(time.RFC3339)
			je.StoppedAt = &s
		}
		out = append(out, je)
	}

	result := map[string]any{
		"entries":       out,
		"total_seconds": totalSec,
		"total_human":   models.FormatDuration(time.Duration(totalSec) * time.Second),
	}

	b, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(b))
	return nil
}
