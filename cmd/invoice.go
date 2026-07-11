package cmd

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/aeon022/timectl/internal/models"
	"github.com/spf13/cobra"
)

var (
	invoiceMonth string
	invoiceRate  float64
)

var invoiceCmd = &cobra.Command{
	Use:   "invoice",
	Short: "Generate a Markdown invoice for a month",
	Long: `Generate a Markdown time/billing invoice for the given month.

The hourly rate is read from --rate or TIMECTL_HOURLY_RATE env var.
If no rate is set, amounts are shown as "$0.00".`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Resolve hourly rate.
		rate := invoiceRate
		if rate == 0 {
			if v := os.Getenv("TIMECTL_HOURLY_RATE"); v != "" {
				if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 {
					rate = f
				}
			}
		}

		// Resolve month.
		var year int
		var month time.Month
		if invoiceMonth == "" {
			now := time.Now()
			year, month = now.Year(), now.Month()
		} else {
			t, err := time.Parse("2006-01", invoiceMonth)
			if err != nil {
				return fmt.Errorf("invalid month %q: expected YYYY-MM", invoiceMonth)
			}
			year, month = t.Year(), t.Month()
		}

		from := time.Date(year, month, 1, 0, 0, 0, 0, time.Local)
		to := from.AddDate(0, 1, 0)

		s, err := openStore()
		if err != nil {
			return err
		}
		defer s.Close()

		entries, err := s.Range(from, to)
		if err != nil {
			return fmt.Errorf("load entries: %w", err)
		}

		monthLabel := from.Format("January 2006")
		fmt.Printf("# Invoice — %s\n", monthLabel)
		if rate > 0 {
			fmt.Printf("Rate: $%.0f/h\n", rate)
		}
		fmt.Println()

		// Per-entry table.
		fmt.Println("## Time Entries")
		fmt.Println()
		fmt.Println("| Task | Project | Duration | Amount |")
		fmt.Println("|------|---------|----------|--------|")

		var grandTotal time.Duration
		for _, e := range entries {
			d := e.ComputedDuration()
			grandTotal += d
			proj := e.Project
			if proj == "" {
				proj = "—"
			}
			amount := ""
			if rate > 0 {
				amount = fmt.Sprintf("$%.2f", d.Hours()*rate)
			} else {
				amount = "$0.00"
			}
			fmt.Printf("| %s | %s | %s | %s |\n",
				e.Task, proj, models.FormatDuration(d), amount)
		}

		totalAmount := ""
		if rate > 0 {
			totalAmount = fmt.Sprintf("**$%.2f**", grandTotal.Hours()*rate)
		} else {
			totalAmount = "**$0.00**"
		}
		fmt.Printf("| **Total** | | **%s** | %s |\n",
			models.FormatDuration(grandTotal), totalAmount)

		// Project summary (only if any entries have a project).
		projTotals := map[string]time.Duration{}
		for _, e := range entries {
			if e.Project != "" {
				projTotals[e.Project] += e.ComputedDuration()
			}
		}
		if len(projTotals) > 0 {
			fmt.Println()
			fmt.Println("## By Project")
			fmt.Println()
			fmt.Println("| Project | Duration | Amount |")
			fmt.Println("|---------|----------|--------|")

			// Sort projects alphabetically for stable output.
			projs := make([]string, 0, len(projTotals))
			for p := range projTotals {
				projs = append(projs, p)
			}
			sortStrings(projs)

			for _, p := range projs {
				d := projTotals[p]
				amount := ""
				if rate > 0 {
					amount = fmt.Sprintf("$%.2f", d.Hours()*rate)
				} else {
					amount = "$0.00"
				}
				fmt.Printf("| %s | %s | %s |\n", p, models.FormatDuration(d), amount)
			}
		}

		return nil
	},
}

// sortStrings sorts a string slice in place (simple insertion sort to avoid importing sort).
func sortStrings(ss []string) {
	for i := 1; i < len(ss); i++ {
		for j := i; j > 0 && strings.ToLower(ss[j]) < strings.ToLower(ss[j-1]); j-- {
			ss[j], ss[j-1] = ss[j-1], ss[j]
		}
	}
}

func init() {
	invoiceCmd.Flags().StringVar(&invoiceMonth, "month", "", "Month to invoice (YYYY-MM, default: current month)")
	invoiceCmd.Flags().Float64Var(&invoiceRate, "rate", 0, "Hourly rate in $ (overrides TIMECTL_HOURLY_RATE)")
}
