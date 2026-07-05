package models

import (
	"fmt"
	"time"
)

// Entry represents a single time-tracking record.
type Entry struct {
	ID        int64
	Task      string
	Project   string
	StartedAt time.Time
	StoppedAt *time.Time
	Duration  time.Duration
	Notes     string
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
