package models

import (
	"fmt"
	"strings"
	"time"
)

// Entry represents a single time-tracking record.
type Entry struct {
	ID           int64
	Task         string
	Project      string
	StartedAt    time.Time
	StoppedAt    *time.Time
	Duration     time.Duration
	Notes        string
	LinkedTask   string // taskctl task title, for display
	LinkedTaskID string // taskctl task id, for navigation ("g" jumps to it)
}

// IsRunning reports whether this entry has no stop time.
func (e Entry) IsRunning() bool {
	return e.StoppedAt == nil
}

// ComputedDuration returns the elapsed time; for running entries it uses now.
func (e Entry) ComputedDuration() time.Duration {
	if e.StoppedAt != nil {
		return e.StoppedAt.Sub(e.StartedAt)
	}
	return time.Since(e.StartedAt)
}

// DaySummary aggregates entries for a single calendar day.
type DaySummary struct {
	Date    time.Time
	Entries []Entry
	Total   time.Duration
	ByTask  map[string]time.Duration
}

// FormatDuration converts a duration to "Xh Ym Zs" style.
func FormatDuration(d time.Duration) string {
	d = d.Round(time.Second)
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	if h > 0 {
		return fmt.Sprintf("%dh %dm %ds", h, m, s)
	}
	if m > 0 {
		return fmt.Sprintf("%dm %ds", m, s)
	}
	return fmt.Sprintf("%ds", s)
}

// WeekBarRow is one rendered line of a week bar chart: a day label, a
// fixed-width fill bar, and its formatted duration.
type WeekBarRow struct {
	Label    string
	Bar      string
	Duration string
}

// WeekBarChart computes the per-day bar rows and week total for a week's
// worth of DaySummary, sharing the bar-width/fill math between the CLI
// (`week` command) and TUI week view — callers add their own styling.
func WeekBarChart(summaries []DaySummary) (rows []WeekBarRow, weekTotal time.Duration) {
	const barWidth = 24

	var maxH float64
	for _, ds := range summaries {
		if h := ds.Total.Hours(); h > maxH {
			maxH = h
		}
	}
	if maxH < 1 {
		maxH = 1
	}

	rows = make([]WeekBarRow, 0, len(summaries))
	for _, ds := range summaries {
		weekTotal += ds.Total
		hours := ds.Total.Hours()
		filled := int(hours / maxH * barWidth)
		if hours > 0 && filled == 0 {
			filled = 1
		}
		bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)
		rows = append(rows, WeekBarRow{
			Label:    ds.Date.Format("Mon 01/02"),
			Bar:      bar,
			Duration: FormatDuration(ds.Total),
		})
	}
	return rows, weekTotal
}

// ComputeStreak counts consecutive days (walking backward from today) that
// are present in daySet, keyed by "2006-01-02".
func ComputeStreak(daySet map[string]bool) int {
	streak := 0
	now := time.Now()
	for {
		day := now.Format("2006-01-02")
		if !daySet[day] {
			break
		}
		streak++
		now = now.AddDate(0, 0, -1)
	}
	return streak
}
