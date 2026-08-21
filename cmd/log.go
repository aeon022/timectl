package cmd

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
)

var (
	logDays    int
	logProject string
	logJSON    bool
)

var logCmd = &cobra.Command{
	Use:   "log",
	Short: "Show recent time entries",
	RunE: func(cmd *cobra.Command, args []string) error {
		s, err := openStore()
		if err != nil {
			return err
		}
		defer s.Close()

		now := time.Now()
		end := now.Add(time.Minute)
		start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).
			AddDate(0, 0, -logDays+1)

		entries, err := s.FilteredRange(start, end, logProject)
		if err != nil {
			return err
		}

		if logJSON {
			return printEntriesJSON(entries)
		}

		if len(entries) == 0 {
			fmt.Printf("  No entries in the last %d day(s).\n", logDays)
			return nil
		}

		printEntriesTable(entries, true)
		return nil
	},
}

func init() {
	logCmd.Flags().IntVarP(&logDays, "days", "d", 7, "Number of days to show")
	logCmd.Flags().StringVarP(&logProject, "project", "p", "", "Filter by project")
	logCmd.Flags().BoolVar(&logJSON, "json", false, "Output as JSON")
}
