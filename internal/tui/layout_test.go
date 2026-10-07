package tui

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/aeon022/missionctl-core/tuitest"
	"github.com/aeon022/timectl/internal/models"
)

// layoutModel builds a sized model with n finished entries (titles task-00…),
// optionally a running timer "Planning" started 42 minutes ago.
func layoutModel(t *testing.T, n int, running bool, w, h int) model {
	t.Helper()
	m := testModel(t, false)
	now := time.Now()
	for i := 0; i < n; i++ {
		start := now.Add(-time.Duration(n-i+1) * time.Hour)
		stop := start.Add(time.Duration(30+i*15) * time.Minute)
		m.entries = append(m.entries, models.Entry{ID: int64(i + 1), Task: fmt.Sprintf("task-%02d", i), Project: "proj", StartedAt: start, StoppedAt: &stop})
	}
	if running {
		m.entries = append(m.entries, models.Entry{ID: 99, Task: "Planning", StartedAt: now.Add(-42 * time.Minute)})
		m.running = &m.entries[len(m.entries)-1]
	}
	m.allEntries = m.entries
	for i := 0; i < 30; i++ {
		m.heatData = append(m.heatData, heatDay{date: now.AddDate(0, 0, -(29 - i)), total: time.Duration(i%5) * 40 * time.Minute})
	}
	mm, _ := tuitest.Send(m, tuitest.Resize(w, h))
	return mm.(model)
}

func textLines(m model) []string { return strings.Split(tuitest.Text(m), "\n") }

func TestNothingWiderThanTerminalAndHeightIsConstant(t *testing.T) {
	for _, w := range []int{40, 60, 80, 100, 140, 170} {
		for _, h := range []int{24, 30, 36} {
			states := map[string]func(model) model{
				"running": func(m model) model { return m },
				"idle":    func(m model) model { m.running = nil; return m },
				"empty":   func(m model) model { m.entries, m.allEntries = nil, nil; m.running = nil; return m },
				"browse":  func(m model) model { m.browseDate = time.Now().AddDate(0, 0, -1); return m },
				"palette": func(m model) model { m.imode = modeCommand; return m },
				"filter":  func(m model) model { m.filterQ = "task"; return m },
				"week": func(m model) model {
					m.current = viewWeek
					now := time.Now()
					for i := 0; i < 7; i++ {
						m.weekSummaries = append(m.weekSummaries, models.DaySummary{Date: now.AddDate(0, 0, i-3), Total: time.Duration(i) * time.Hour})
					}
					return m
				},
				"stats": func(m model) model {
					m.current = viewStats
					m.statsText = "  1. a long task name here   2h\n  2. b   1h 05m"
					return m
				},
				"taskpick": func(m model) model {
					m.current = viewTaskPick
					m.taskList = nil
					return m
				},
			}
			for name, mod := range states {
				m := mod(layoutModel(t, 5, true, w, h))
				lines := textLines(m)
				if want := m.height; len(lines) != want {
					t.Errorf("%s %dx%d: %d lines, want %d", name, w, h, len(lines), want)
				}
				for i, l := range lines {
					if lw := lipgloss.Width(l); lw > w {
						t.Errorf("%s %dx%d: line %d is %d wide: %q", name, w, h, i, lw, l)
						break
					}
				}
			}
		}
	}
}

func TestHeaderShowsLiveTimerOrIdle(t *testing.T) {
	m := layoutModel(t, 2, true, 110, 30)
	head := textLines(m)[0]
	if !strings.Contains(head, "timectl · today") || !strings.Contains(head, "Planning") ||
		!regexp.MustCompile(`00:4[12]:\d\d`).MatchString(head) {
		t.Errorf("header must carry the live timer (task + HH:MM:SS): %q", head)
	}
	if want := time.Now().Format("Mon 02 Jan"); !strings.Contains(head, want) {
		t.Errorf("header missing the date %q: %q", want, head)
	}
	m.running = nil
	if head := textLines(m)[0]; !strings.Contains(head, "no timer running") {
		t.Errorf("idle header: %q", head)
	}
	// the clock follows the entry, not a snapshot: a later render shows a later time
	m2 := layoutModel(t, 0, true, 110, 30)
	m2.running.StartedAt = time.Now().Add(-2*time.Hour - 5*time.Second)
	if head := textLines(m2)[0]; !strings.Contains(head, "02:00:0") {
		t.Errorf("clock = elapsed since StartedAt: %q", head)
	}
}

func TestDurationsUseCompactFormat(t *testing.T) {
	m := layoutModel(t, 0, false, 110, 30)
	stop := time.Now()
	m.entries = []models.Entry{
		{ID: 1, Task: "exactly two hours", StartedAt: stop.Add(-2 * time.Hour), StoppedAt: &stop},
		{ID: 2, Task: "one oh five", StartedAt: stop.Add(-65 * time.Minute), StoppedAt: &stop},
	}
	m.allEntries = m.entries
	text := tuitest.Text(m)
	for _, want := range []string{"2h", "1h 05m", "Total  3h 05m", "/ 8h"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in:\n%s", want, text)
		}
	}
	if regexp.MustCompile(`\d+m \d+s|\d+h \d+m \d+s`).MatchString(text) {
		t.Errorf("old 'Xh Ym Zs' format leaked into the chrome:\n%s", text)
	}
}

func TestFooterPutsStopFirstWhileRunning(t *testing.T) {
	foot := func(m model) string { l := textLines(m); return l[len(l)-1] }
	running, idle := foot(layoutModel(t, 2, true, 140, 30)), foot(layoutModel(t, 2, false, 140, 30))
	if strings.Index(running, "s stop") > strings.Index(running, "n start") {
		t.Errorf("running: stop must come first: %q", running)
	}
	if strings.Index(idle, "n start") > strings.Index(idle, "s stop") {
		t.Errorf("idle: start must come first: %q", idle)
	}
	if !strings.Contains(running, "? help") || !strings.Contains(running, "q quit") {
		t.Errorf("? and q must survive: %q", running)
	}
	if narrow := foot(layoutModel(t, 2, true, 60, 30)); !strings.Contains(narrow, "? help") || !strings.Contains(narrow, "q quit") {
		t.Errorf("? and q are dropped last at 60 cols: %q", narrow)
	}
}

func TestResponsivePanelsAndTiers(t *testing.T) {
	wide := tuitest.Text(layoutModel(t, 3, true, 110, 30))
	if !strings.Contains(wide, "Last 30 days") || !strings.Contains(wide, "This week") || !strings.Contains(wide, "Today") {
		t.Errorf("110x30 must show heat, today and week panels:\n%s", wide)
	}
	if narrow := tuitest.Text(layoutModel(t, 3, true, 90, 30)); strings.Contains(narrow, "Last 30 days") {
		t.Errorf("< 100 cols hides the heat panel:\n%s", narrow)
	}
	if compact := tuitest.Text(layoutModel(t, 3, true, 110, 24)); strings.Contains(compact, "This week") {
		t.Errorf("a short terminal hides the week band:\n%s", compact)
	}
	// blank line under the header only in the spacious tier
	if l := textLines(layoutModel(t, 3, true, 110, 30)); strings.TrimSpace(l[2]) != "" {
		t.Errorf("spacious: line 2 should be blank, got %q", l[2])
	}
	if l := textLines(layoutModel(t, 3, true, 110, 24)); strings.TrimSpace(l[2]) == "" {
		t.Errorf("compact: no blank line under the header, got %q", l[2])
	}
	if !strings.Contains(tuitest.Text(layoutModel(t, 3, true, 110, 30)), "▌") {
		t.Error("the selected entry carries the accent bar")
	}
}

func TestClickMappingMatchesTheRenderedRows(t *testing.T) {
	for _, tc := range []struct {
		name string
		n    int
		w, h int
		mod  func(model) model
	}{
		{"wide", 4, 150, 36, nil},
		{"spacious", 4, 110, 30, nil},
		{"compact", 4, 110, 24, nil},
		{"narrow-no-heat", 4, 80, 30, nil},
		{"browse-day", 4, 110, 30, func(m model) model { m.browseDate = time.Now().AddDate(0, 0, -1); return m }},
		{"palette-open", 4, 110, 30, func(m model) model { m.imode = modeCommand; return m }},
		{"scrolled", 40, 110, 24, func(m model) model { m.cursor = 30; return m }},
		{"scrolled-cursor-end", 40, 110, 30, func(m model) model { m.cursor = 39; return m }},
	} {
		m := layoutModel(t, tc.n, false, tc.w, tc.h)
		if tc.mod != nil {
			m = tc.mod(m)
		}
		lines := textLines(m)
		L := m.layout()
		x := L.todayX + 6
		seen := 0
		for y, line := range lines {
			for i, e := range m.entries {
				if !strings.Contains(line, e.Task) {
					continue
				}
				seen++
				if got := m.rowHitTest(x, y); got != i {
					t.Errorf("%s: row %q is at y=%d but rowHitTest = %d, want %d", tc.name, e.Task, y, got, i)
				}
			}
		}
		if seen == 0 || seen != L.cap && tc.n > L.cap {
			t.Errorf("%s: saw %d rendered entry rows, layout cap %d", tc.name, seen, L.cap)
		}
		// a click on the heat panel, above the entries, or below them hits nothing
		if L.showHeat && m.rowHitTest(3, L.entriesY) != -1 {
			t.Errorf("%s: heat panel click must miss", tc.name)
		}
		if m.rowHitTest(x, L.entriesY-1) != -1 || m.rowHitTest(x, L.entriesY+L.cap) != -1 {
			t.Errorf("%s: clicks above/below the entry rows must miss", tc.name)
		}
	}
}

func TestMouseClickSelectsRowInNewGeometry(t *testing.T) {
	m := layoutModel(t, 4, false, 110, 30)
	L := m.layout()
	mm, _ := tuitest.Send(m, tuitest.Click(L.todayX+6, L.entriesY+2))
	if got := mm.(model).cursor; got != 2 {
		t.Errorf("click on the third entry row selected %d", got)
	}
}

func TestWindowStart(t *testing.T) {
	for _, c := range []struct{ cur, n, cap, want int }{
		{0, 3, 5, 0}, {0, 10, 5, 0}, {2, 10, 5, 0}, {5, 10, 5, 3}, {9, 10, 5, 5}, {4, 10, 0, 0},
	} {
		if got := windowStart(c.cur, c.n, c.cap); got != c.want {
			t.Errorf("windowStart(%d,%d,%d) = %d, want %d", c.cur, c.n, c.cap, got, c.want)
		}
		if c.cap > 0 && c.n > c.cap {
			s := windowStart(c.cur, c.n, c.cap)
			if c.cur < s || c.cur >= s+c.cap {
				t.Errorf("cursor %d not inside window [%d,%d)", c.cur, s, s+c.cap)
			}
		}
	}
}

func TestWeekTotalsMondayFirstTodayLiveAndEmptyWeek(t *testing.T) {
	wed := time.Date(2026, 10, 7, 15, 0, 0, 0, time.Local) // a Wednesday
	heat := []heatDay{
		{time.Date(2026, 10, 5, 0, 0, 0, 0, time.Local), 4 * time.Hour},  // Mon
		{time.Date(2026, 10, 6, 0, 0, 0, 0, time.Local), time.Hour},      // Tue
		{time.Date(2026, 10, 7, 0, 0, 0, 0, time.Local), 10 * time.Hour}, // Wed: stale, replaced by the live total
		{time.Date(2026, 10, 4, 0, 0, 0, 0, time.Local), 9 * time.Hour},  // previous Sunday: not in this week
	}
	days, totals, idx := weekTotals(heat, 2*time.Hour, wed)
	if idx != 2 || days[0].Weekday() != time.Monday || days[0].Day() != 5 || days[6].Weekday() != time.Sunday || days[6].Day() != 11 {
		t.Fatalf("week = %v … %v, today index %d", days[0], days[6], idx)
	}
	want := [7]time.Duration{4 * time.Hour, time.Hour, 2 * time.Hour, 0, 0, 0, 0}
	if totals != want {
		t.Errorf("totals = %v, want %v", totals, want)
	}
	sun := time.Date(2026, 10, 11, 9, 0, 0, 0, time.Local)
	if _, _, idx := weekTotals(nil, 0, sun); idx != 6 {
		t.Errorf("Sunday is index 6, got %d", idx)
	}
	_, empty, _ := weekTotals(nil, 0, wed)
	for _, d := range empty {
		if d != 0 {
			t.Errorf("empty week must be all zero: %v", empty)
		}
	}
}

func TestWeekBandWidthsLabelsAndTodayHighlight(t *testing.T) {
	wed := time.Date(2026, 10, 7, 15, 0, 0, 0, time.Local)
	days, totals, idx := weekTotals(nil, 3*time.Hour, wed)
	for _, w := range []int{30, 50, 80, 140} {
		band := weekBand(w, days, totals, idx, 8)
		lines := strings.Split(band, "\n")
		if len(lines) != 3 {
			t.Fatalf("band has %d lines", len(lines))
		}
		for _, l := range lines {
			if lipgloss.Width(l) > w+6 { // columns have a floor width; very small bands may overshoot slightly
				t.Errorf("width %d: line is %d wide: %q", w, lipgloss.Width(l), l)
			}
		}
	}
	text := tuitest.Text(layoutModel(t, 0, false, 110, 30))
	if !strings.Contains(text, "Wed 07"[:0]+time.Now().Format("Mon 02")) {
		t.Errorf("today's label missing from the band:\n%s", text)
	}
	plain := stripANSI(weekBand(100, days, totals, idx, 8))
	if !strings.Contains(plain, "Mon 05") || !strings.Contains(plain, "Sun 11") || !strings.Contains(plain, "3h") || !strings.Contains(plain, "–") {
		t.Errorf("band:\n%s", plain)
	}
	// bars are scaled to the daily goal: 3h of an 8h goal is 37.5% → not full, not empty
	bars := strings.Split(plain, "\n")[1]
	if !strings.Contains(bars, "█") || !strings.Contains(bars, "░") {
		t.Errorf("bars:\n%s", bars)
	}
}

func TestHeatLevelBoundaries(t *testing.T) {
	for in, want := range map[time.Duration]int{
		0: 0, time.Minute: 1, 59 * time.Minute: 1, time.Hour: 2, 2*time.Hour + 59*time.Minute: 2,
		3 * time.Hour: 3, 5*time.Hour + 59*time.Minute: 3, 6 * time.Hour: 4, 12 * time.Hour: 4,
	} {
		if got := heatLevel(in); got != want {
			t.Errorf("heatLevel(%v) = %d, want %d", in, got, want)
		}
	}
}

func TestOtherViewsGetHeaderAndCompactDurations(t *testing.T) {
	m := layoutModel(t, 2, true, 110, 30)
	now := time.Now()
	m.current = viewWeek
	for i := 0; i < 7; i++ {
		m.weekSummaries = append(m.weekSummaries, models.DaySummary{Date: now.AddDate(0, 0, i-3), Total: 2 * time.Hour})
	}
	text := tuitest.Text(m)
	if !strings.Contains(text, "timectl · this week") || !strings.Contains(text, "Total  14h") || regexp.MustCompile(`\d+m \d+s`).MatchString(text) {
		t.Errorf("week view:\n%s", text)
	}
	m.current = viewStats
	m.statsText = ""
	if !strings.Contains(tuitest.Text(m), "timectl · stats") || !strings.Contains(tuitest.Text(m), "Loading") {
		t.Errorf("stats view:\n%s", tuitest.Text(m))
	}
	m.current = viewTaskPick
	m.taskList = nil
	if !strings.Contains(tuitest.Text(m), "timectl · open tasks") {
		t.Errorf("task pick view:\n%s", tuitest.Text(m))
	}
}

func TestStatsTextUsesCompactDurations(t *testing.T) {
	m := testModel(t, false)
	if _, err := m.store.Start("stats task", "p"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.store.Stop(""); err != nil {
		t.Fatal(err)
	}
	text, err := buildStatsText(m.store, 0)
	if err != nil {
		t.Fatal(err)
	}
	if regexp.MustCompile(`\d+m \d+s`).MatchString(text) {
		t.Errorf("stats text still uses Xm Ys:\n%s", text)
	}
}

func stripANSI(s string) string { return regexp.MustCompile("\x1b\\[[0-9;]*m").ReplaceAllString(s, "") }

// secondaryViews builds every non-main view, so one table drives the chrome
// tests: header, constant height, nothing wider than the terminal, and a
// one-line footer with an "esc" hint.
func secondaryViews(m model) map[string]model {
	week := m
	week.current = viewWeek
	now := time.Now()
	for i := 0; i < 7; i++ {
		week.weekSummaries = append(week.weekSummaries, models.DaySummary{Date: now.AddDate(0, 0, i-3), Total: time.Duration(i) * time.Hour})
	}
	stats := m
	stats.current = viewStats
	stats.statsText = "  Top tasks:\n  1. a long task name here   2h\n  2. b   1h 05m"
	pick := m
	pick.current = viewTaskPick
	pick.taskList = nil
	help := m.openHelp()
	helpFromWeek := week.openHelp()
	return map[string]model{"week": week, "stats": stats, "taskpick": pick, "help": help, "help-over-week": helpFromWeek}
}

func TestSecondaryViewsShareTheSameChrome(t *testing.T) {
	for _, w := range []int{40, 60, 80, 100, 140, 170} {
		for _, h := range []int{24, 30, 36} {
			base := layoutModel(t, 5, true, w, h)
			for name, m := range secondaryViews(base) {
				lines := textLines(m)
				if len(lines) != m.height {
					t.Errorf("%s %dx%d: %d lines, want constant %d", name, w, h, len(lines), m.height)
				}
				for i, l := range lines {
					if lw := lipgloss.Width(l); lw > w {
						t.Errorf("%s %dx%d: line %d is %d wide: %q", name, w, h, i, lw, l)
						break
					}
				}
				text := tuitest.Text(m)
				if strings.HasPrefix(name, "help") {
					// a modal popup over the previous view: it carries its own close hint
					if !strings.Contains(text, "esc / ?  close") && !strings.Contains(text, "scroll") {
						t.Errorf("%s %dx%d: help popup has no close hint:\n%s", name, w, h, text)
					}
					continue
				}
				if !strings.Contains(lines[0], "timectl") {
					t.Errorf("%s %dx%d: no header line: %q", name, w, h, lines[0])
				}
				// the footer is exactly one line: the last non-empty line holds an esc hint
				last := ""
				for i := len(lines) - 1; i >= 0; i-- {
					if strings.TrimSpace(lines[i]) != "" {
						last = lines[i]
						break
					}
				}
				if !strings.Contains(strings.ToLower(last), "esc") {
					t.Errorf("%s %dx%d: footer has no esc hint: %q\n%s", name, w, h, last, text)
				}
			}
		}
	}
}

func TestHelpPopupIsAPanelAndScrolls(t *testing.T) {
	m := layoutModel(t, 3, false, 110, 30).openHelp()
	text := tuitest.Text(m)
	if !strings.Contains(text, "╭─ Help") {
		t.Errorf("help should be a titled ui.Panel:\n%s", text)
	}
	if !strings.Contains(text, "esc / ?  close") && !strings.Contains(text, "scroll") {
		t.Errorf("help footer missing:\n%s", text)
	}
	m2, _ := tuitest.Keys(m, "j", "j")
	if tuitest.Text(m2) == "" {
		t.Error("scrolling must keep rendering")
	}
}
