package cmd

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/aeon022/timectl/internal/models"
	"github.com/spf13/cobra"
)

var weekJSON bool

var weekCmd = &cobra.Command{
	Use:   "week",
	Short: "Show this week's time breakdown",
	RunE: func(cmd *cobra.Command, args []string) error {
		s, err := openStore()
		if err != nil {
			return err
		}
		defer s.Close()

		summaries, err := s.WeekSummary()
		if err != nil {
			return err
		}

		if weekJSON {
			return printWeekJSON(summaries)
		}

		printWeekTable(summaries)
		return nil
	},
}

func init() {
	weekCmd.Flags().BoolVar(&weekJSON, "json", false, "Output as JSON")
}

func printWeekTable(summaries []models.DaySummary) {
	var weekTotal time.Duration
	for _, ds := range summaries {
		weekTotal += ds.Total
	}

	// Find max for bar scaling.
	var maxH float64
	for _, ds := range summaries {
		h := ds.Total.Hours()
		if h > maxH {
			maxH = h
		}
	}
	if maxH < 1 {
		maxH = 1
	}

	const barWidth = 24
	for _, ds := range summaries {
		label := ds.Date.Format("Mon 01/02")
		hours := ds.Total.Hours()
		filled := int(hours / maxH * barWidth)
		if hours > 0 && filled == 0 {
			filled = 1
		}
		bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)
		dur := models.FormatDuration(ds.Total)
		fmt.Printf("  %s  %s  %s\n", label, bar, dur)
	}

	fmt.Println("  " + strings.Repeat("─", 55))
	fmt.Printf("  Total:          %s\n", models.FormatDuration(weekTotal))

	// By task breakdown.
	allByTask := map[string]time.Duration{}
	for _, ds := range summaries {
		for task, d := range ds.ByTask {
			allByTask[task] += d
		}
	}

	if len(allByTask) > 0 {
		fmt.Println("\n  By task:")
		for task, d := range allByTask {
			fmt.Printf("    %-30s %s\n", task, models.FormatDuration(d))
		}
	}
}

func printWeekJSON(summaries []models.DaySummary) error {
	type daySummaryJSON struct {
		Date          string            `json:"date"`
		TotalSeconds  int               `json:"total_seconds"`
		TotalHuman    string            `json:"total_human"`
		ByTask        map[string]int    `json:"by_task"`
		EntryCount    int               `json:"entry_count"`
	}

	var out []daySummaryJSON
	var weekTotalSec int
	for _, ds := range summaries {
		byTaskSec := map[string]int{}
		for task, d := range ds.ByTask {
			byTaskSec[task] = int(d.Seconds())
		}
		totalSec := int(ds.Total.Seconds())
		weekTotalSec += totalSec
		out = append(out, daySummaryJSON{
			Date:         ds.Date.Format("2006-01-02"),
			TotalSeconds: totalSec,
			TotalHuman:   models.FormatDuration(ds.Total),
			ByTask:       byTaskSec,
			EntryCount:   len(ds.Entries),
		})
	}

	result := map[string]any{
		"days":               out,
		"week_total_seconds": weekTotalSec,
		"week_total_human":   models.FormatDuration(time.Duration(weekTotalSec) * time.Second),
	}

	b, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(b))
	return nil
}
