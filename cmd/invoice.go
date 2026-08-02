package cmd

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/aeon022/timectl/internal/models"
	"github.com/spf13/cobra"
)

var (
	invoiceMonth  string
	invoiceRate   float64
	invoiceClient string
	invoiceOutput string
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

		var b strings.Builder
		monthLabel := from.Format("January 2006")
		fmt.Fprintf(&b, "# Invoice — %s\n", monthLabel)
		if invoiceClient != "" {
			fmt.Fprintf(&b, "Client: %s\n", invoiceClient)
		}
		if rate > 0 {
			fmt.Fprintf(&b, "Rate: $%.2f/h\n", rate)
		}
		fmt.Fprintln(&b)

		// Per-entry table. The Amount column only makes sense once a rate is
		// known — showing a fake "$0.00" for every row when no rate was set
		// reads as "you earned nothing" rather than "rate not configured".
		b.WriteString("## Time Entries\n\n")
		if rate > 0 {
			b.WriteString("| Task | Project | Duration | Amount |\n")
			b.WriteString("|------|---------|----------|--------|\n")
		} else {
			b.WriteString("| Task | Project | Duration |\n")
			b.WriteString("|------|---------|----------|\n")
		}

		var grandTotal time.Duration
		for _, e := range entries {
			d := e.ComputedDuration()
			grandTotal += d
			proj := e.Project
			if proj == "" {
				proj = "—"
			}
			if rate > 0 {
				fmt.Fprintf(&b, "| %s | %s | %s | $%.2f |\n",
					e.Task, proj, models.FormatDuration(d), d.Hours()*rate)
			} else {
				fmt.Fprintf(&b, "| %s | %s | %s |\n", e.Task, proj, models.FormatDuration(d))
			}
		}

		if rate > 0 {
			fmt.Fprintf(&b, "| **Total** | | **%s** | **$%.2f** |\n",
				models.FormatDuration(grandTotal), grandTotal.Hours()*rate)
		} else {
			fmt.Fprintf(&b, "| **Total** | | **%s** |\n", models.FormatDuration(grandTotal))
		}

		// Project summary (only if any entries have a project).
		projTotals := map[string]time.Duration{}
		for _, e := range entries {
			if e.Project != "" {
				projTotals[e.Project] += e.ComputedDuration()
			}
		}
		if len(projTotals) > 0 {
			b.WriteString("\n## By Project\n\n")
			if rate > 0 {
				b.WriteString("| Project | Duration | Amount |\n")
				b.WriteString("|---------|----------|--------|\n")
			} else {
				b.WriteString("| Project | Duration |\n")
				b.WriteString("|---------|----------|\n")
			}

			projs := make([]string, 0, len(projTotals))
			for p := range projTotals {
				projs = append(projs, p)
			}
			sort.Slice(projs, func(i, j int) bool { return strings.ToLower(projs[i]) < strings.ToLower(projs[j]) })

			for _, p := range projs {
				d := projTotals[p]
				if rate > 0 {
					fmt.Fprintf(&b, "| %s | %s | $%.2f |\n", p, models.FormatDuration(d), d.Hours()*rate)
				} else {
					fmt.Fprintf(&b, "| %s | %s |\n", p, models.FormatDuration(d))
				}
			}
		}

		if invoiceOutput != "" {
			if err := os.WriteFile(invoiceOutput, []byte(b.String()), 0o644); err != nil {
				return fmt.Errorf("write %s: %w", invoiceOutput, err)
			}
			fmt.Fprintf(os.Stderr, "Wrote invoice → %s\n", invoiceOutput)
			return nil
		}
		fmt.Print(b.String())
		return nil
	},
}

func init() {
	invoiceCmd.Flags().StringVar(&invoiceMonth, "month", "", "Month to invoice (YYYY-MM, default: current month)")
	invoiceCmd.Flags().Float64Var(&invoiceRate, "rate", 0, "Hourly rate in $ (overrides TIMECTL_HOURLY_RATE)")
	invoiceCmd.Flags().StringVar(&invoiceClient, "client", "", "Client/company name to show in the invoice header")
	invoiceCmd.Flags().StringVarP(&invoiceOutput, "output", "o", "", "Output file (default: stdout)")
}
