package tui

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/aeon022/missionctl-core/tuitest"
	"github.com/aeon022/timectl/internal/models"
	"github.com/aeon022/timectl/internal/store"
)

// tour visits every view and input mode and leaves each with esc. Commands
// returned by Update (DB writes, taskctl lookups, pbcopy) are never executed.
var tour = []string{
	"?", "esc", // help
	"w", "esc", // week
	"v", "esc", // stats
	"T", "esc", // task picker
	"n", "esc", // start: input mode
	"n", "x", "enter", "esc", // start with a name
	"e", "esc", // notes
	"d", "esc", // delete confirm
	"/", "x", "esc", // filter
	":", "esc", // palette
	"left", "right", "t", "j", "k", "down", "up",
}

func testModel(t *testing.T, withEntries bool) model {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("TIMECTL_DATA_DIR", "")
	s, err := store.Open(filepath.Join(t.TempDir(), "timectl.db"), false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	m := newModel(s)
	if withEntries {
		stop := time.Now()
		m.entries = []models.Entry{
			{ID: 1, Task: "Write tests", Project: "timectl", StartedAt: stop.Add(-2 * time.Hour), StoppedAt: &stop},
			{ID: 2, Task: "Review", StartedAt: stop.Add(-time.Hour), StoppedAt: &stop},
		}
		m.allEntries = m.entries
	}
	return m
}

func TestSmokeTour(t *testing.T) {
	for _, size := range [][2]int{{100, 30}, {60, 15}} {
		m := testModel(t, true)
		tuitest.SmokeSize(t, m, size[0], size[1], tour...)

		end, _ := tuitest.Send(m, tuitest.Resize(size[0], size[1]))
		end, _ = tuitest.Keys(end, tour...)
		if got := end.(model); got.current != viewMain || got.imode != modeNone {
			t.Errorf("%dx%d: tour ended in view %d / input mode %d, want main/none", size[0], size[1], got.current, got.imode)
		}
	}
}

func TestSmokeEmptyData(t *testing.T) {
	m := testModel(t, false)
	tuitest.SmokeSize(t, m, 100, 30, "?", "esc", "w", "esc", "v", "esc", "T", "esc", "j", "k", "left", "right", "d", "esc", "e", "esc")
	tuitest.SmokeSize(t, m, 60, 15, "?", "esc", "w", "esc")
}

func TestMainFooterNeverWiderThanTerminal(t *testing.T) {
	for _, w := range []int{60, 80, 100, 140} {
		m := testModel(t, true)
		m.width, m.height = w, 30
		lines := strings.Split(strings.TrimRight(tuitest.Text(m), "\n "), "\n")
		if got := lipgloss.Width(lines[len(lines)-1]); got > w {
			t.Errorf("width %d: footer is %d cells wide: %q", w, got, lines[len(lines)-1])
		}
	}
}
