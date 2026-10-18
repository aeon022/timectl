package tui

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/aeon022/missionctl-core/tuitest"
	"github.com/aeon022/timectl/internal/models"
)

// TestRenderLandingMockups writes an ANSI screenshot of the real TUI (made-up demo data) for the landing
// page: TIMECTL_MOCKUP_DIR=/dir go test ./internal/tui -run TestRenderLandingMockups
func TestRenderLandingMockups(t *testing.T) {
	dir := os.Getenv("TIMECTL_MOCKUP_DIR")
	if dir == "" {
		t.Skip("set TIMECTL_MOCKUP_DIR to render landing mockups")
	}
	m := testModel(t, false)
	now := time.Now().Truncate(time.Minute)
	ago := func(h, mi int) time.Time { return now.Add(-time.Duration(h)*time.Hour - time.Duration(mi)*time.Minute) }
	stop := func(h, mi int) *time.Time { s := ago(h, mi); return &s }
	entries := []models.Entry{
		{ID: 5, Task: "Landing page", Project: "missionctl", StartedAt: ago(1, 42), Notes: "hero scene + theme tabs"},
		{ID: 4, Task: "Review pull requests", Project: "missionctl", StartedAt: ago(2, 55), StoppedAt: stop(1, 45), Notes: "core + taskctl"},
		{ID: 3, Task: "Invoice Nordlicht", Project: "client work", StartedAt: ago(3, 45), StoppedAt: stop(3, 5), LinkedTask: "Prepare invoice for Nordlicht"},
		{ID: 2, Task: "Standup", Project: "team", StartedAt: ago(4, 20), StoppedAt: stop(4, 5)},
		{ID: 1, Task: "Inbox zero", StartedAt: ago(5, 10), StoppedAt: stop(4, 40)},
	}
	var heat []heatDay
	for i := 29; i >= 0; i-- {
		d := now.AddDate(0, 0, -i)
		h := []time.Duration{0, 3, 6, 7, 5, 8, 4, 0, 2, 6, 7, 8, 6, 1}[i%14]
		if wd := d.Weekday(); wd == time.Saturday || wd == time.Sunday {
			h = h / 3
		}
		heat = append(heat, heatDay{date: d, total: h*time.Hour + time.Duration(i%4)*15*time.Minute})
	}
	m.running = &entries[0]
	mi, _ := tuitest.Send(m, tea.WindowSizeMsg{Width: 118, Height: 18}, dayEntriesMsg{entries: entries}, heatLoadedMsg{data: heat})
	out := mi.(model).View().Content
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "timectl.ansi"), []byte(out), 0o644); err != nil {
		t.Fatal(err)
	}
}
