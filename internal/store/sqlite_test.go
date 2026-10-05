package store

import (
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/aeon022/timectl/internal/models"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "timectl.db"), false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestStartStop(t *testing.T) {
	s := testStore(t)

	e, err := s.Start("Fix auth bug", "diaryctl")
	if err != nil {
		t.Fatal(err)
	}
	if e.Task != "Fix auth bug" || e.Project != "diaryctl" {
		t.Fatalf("unexpected entry: %+v", e)
	}

	running, err := s.Running()
	if err != nil {
		t.Fatal(err)
	}
	if running == nil || running.ID != e.ID {
		t.Fatalf("running entry not found: %+v", running)
	}
	if !running.IsRunning() {
		t.Error("IsRunning must be true before stop")
	}

	stopped, err := s.Stop("done")
	if err != nil {
		t.Fatal(err)
	}
	if stopped.StoppedAt == nil {
		t.Fatal("StoppedAt not set")
	}
	if stopped.Notes != "done" {
		t.Errorf("unexpected notes: %q", stopped.Notes)
	}
	if stopped.Duration < 0 {
		t.Errorf("negative duration: %v", stopped.Duration)
	}

	running, _ = s.Running()
	if running != nil {
		t.Fatalf("timer still running after stop: %+v", running)
	}
}

func TestStartWhileRunning(t *testing.T) {
	s := testStore(t)
	if _, err := s.Start("first", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Start("second", ""); err == nil {
		t.Fatal("want error when a timer is already running")
	}
}

func TestStopWithoutRunning(t *testing.T) {
	s := testStore(t)
	if _, err := s.Stop(""); err == nil {
		t.Fatal("want error when no timer is running")
	}
}

func TestTodayAndSummary(t *testing.T) {
	s := testStore(t)
	if _, err := s.Start("task a", "proj"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Stop(""); err != nil {
		t.Fatal(err)
	}

	entries, err := s.Today()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("want 1 entry today, got %d", len(entries))
	}

	sum, err := s.DaySummary(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(sum.Entries) != 1 {
		t.Fatalf("want 1 summary entry, got %d", len(sum.Entries))
	}
	if _, ok := sum.ByTask["task a"]; !ok {
		t.Errorf("ByTask missing task: %+v", sum.ByTask)
	}
}

func TestDelete(t *testing.T) {
	s := testStore(t)
	e, err := s.Start("temp", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Stop(""); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(e.ID); err != nil {
		t.Fatal(err)
	}
	entries, _ := s.Today()
	if len(entries) != 0 {
		t.Fatalf("entry not deleted: %+v", entries)
	}
}

func TestFormatDuration(t *testing.T) {
	got := models.FormatDuration(90 * time.Minute)
	if got == "" {
		t.Fatal("FormatDuration(90m) is empty")
	}
	if !strings.Contains(got, "1h") || !strings.Contains(got, "30m") {
		t.Errorf("FormatDuration(90m) = %q, want it to mention 1h and 30m", got)
	}
}

// Stored timestamps are compared and sorted as TEXT. RFC3339Nano trims
// trailing zeros (".1Z" > ".12Z" as text although 0.1s is earlier), so new
// rows are written fixed-width; rows from before that must still load and
// sort correctly against new ones.
func TestRangeOrdersMixedOldAndNewTimestampFormats(t *testing.T) {
	path := filepath.Join(t.TempDir(), "timectl.db")
	s, err := Open(path, false)
	if err != nil {
		t.Fatal(err)
	}
	rows := []struct{ task, started string }{
		{"a", "2026-01-01T10:00:00.1Z"},         // old format: trimmed zeros, 0.100s
		{"b", "2026-01-01T10:00:00.120000000Z"}, // new format, 0.120s
		{"c", "2026-01-01T10:00:00.123Z"},       // old format, 0.123s
		{"d", "2026-01-01T10:00:01Z"},           // old format, whole second
	}
	for _, r := range rows {
		if _, err := s.db.Exec(`INSERT INTO entries (task, started_at) VALUES (?, ?)`, r.task, r.started); err != nil {
			t.Fatal(err)
		}
	}
	// A database from before the fixed-width layout: reopening must normalize it.
	if _, err := s.db.Exec(`PRAGMA user_version = 0`); err != nil {
		t.Fatal(err)
	}
	s.Close()
	if s, err = Open(path, false); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	got, err := s.Range(from, from.Add(24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	var order string
	for _, e := range got {
		order += e.Task
	}
	if order != "abcd" {
		t.Errorf("order = %s, want abcd (a 0.1s < b 0.12s < c 0.123s < d 1s)", order)
	}
}

func TestNewRowsAreWrittenFixedWidth(t *testing.T) {
	s := testStore(t)
	e, err := s.Start("x", "")
	if err != nil {
		t.Fatal(err)
	}
	var started string
	if err := s.db.QueryRow(`SELECT started_at FROM entries WHERE id = ?`, e.ID).Scan(&started); err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`\.\d{9}(Z|[+-]\d\d:\d\d)$`).MatchString(started) {
		t.Errorf("started_at %q: want exactly 9 fraction digits then the zone", started)
	}
}
