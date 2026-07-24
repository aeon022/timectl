package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/aeon022/timectl/internal/models"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

func newTestModel() model {
	return model{width: 100, height: 30, input: textinput.New()}
}

func TestHelpOverlay_OpenScrollClose(t *testing.T) {
	m := newTestModel()

	mi, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	m = mi.(model)
	if m.current != viewHelp {
		t.Fatalf("expected viewHelp after '?', got %v", m.current)
	}
	if m.helpVP.TotalLineCount() == 0 {
		t.Fatal("expected help content to be populated")
	}

	mi, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = mi.(model)
	if m.current != viewMain {
		t.Errorf("expected esc to close help back to viewMain, got %v", m.current)
	}
}

func TestHelpOverlay_ReturnsToViewItWasOpenedFrom(t *testing.T) {
	// Regression test: "?" is reachable from viewMain, viewWeek, AND
	// viewStats here (unlike the other tools this pattern was rolled out
	// to, where help is only reachable from one list view). Closing help
	// used to unconditionally dump back to viewMain regardless of where it
	// was opened from — must return to the actual origin view instead.
	for _, origin := range []viewKind{viewMain, viewWeek, viewStats} {
		m := newTestModel()
		m.current = origin

		mi, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
		m = mi.(model)
		if m.current != viewHelp {
			t.Fatalf("origin %v: expected viewHelp after '?', got %v", origin, m.current)
		}
		if m.helpReturnTo != origin {
			t.Errorf("origin %v: expected helpReturnTo to remember it, got %v", origin, m.helpReturnTo)
		}

		mi, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
		m = mi.(model)
		if m.current != origin {
			t.Errorf("origin %v: expected esc to return to it, landed on %v instead", origin, m.current)
		}
	}
}

func TestHelpOverlay_FitsWithinBackgroundHeight(t *testing.T) {
	m := newTestModel()
	m = m.openHelp()
	bgLines := len(strings.Split(m.backgroundView(), "\n"))
	if m.helpPopH > bgLines {
		t.Errorf("popup height %d exceeds background height %d", m.helpPopH, bgLines)
	}
}

func TestFilterEntries_FuzzyMatchesTaskName(t *testing.T) {
	entries := []models.Entry{
		{Task: "budgetctl release"},
		{Task: "write docs"},
		{Task: "review PR"},
	}
	got := filterEntries(entries, "bgt")
	if len(got) != 1 || got[0].Task != "budgetctl release" {
		t.Errorf("expected fuzzy 'bgt' to match only 'budgetctl release', got %+v", got)
	}
}

func TestFilterEntries_FallsBackToProjectAndNotesSubstring(t *testing.T) {
	entries := []models.Entry{
		{Task: "write docs", Project: "budgetctl"},
		{Task: "unrelated", Notes: "about budgetctl imports"},
		{Task: "totally unrelated"},
	}
	got := filterEntries(entries, "budgetctl")
	if len(got) != 2 {
		t.Fatalf("expected 2 entries matched via project/notes fallback, got %d: %+v", len(got), got)
	}
}

func TestFilterEntries_EmptyQueryReturnsAllUnfiltered(t *testing.T) {
	entries := []models.Entry{{Task: "a"}, {Task: "b"}}
	got := filterEntries(entries, "")
	if len(got) != 2 {
		t.Errorf("expected empty query to return all entries, got %d", len(got))
	}
}

func TestHighlightMatches_ColorsOnlyMatchedRunes(t *testing.T) {
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer lipgloss.SetColorProfile(termenv.Ascii)

	idxs := fuzzyMatchIndexes("bgt", "budgetctl")
	if idxs == nil {
		t.Fatal("expected 'bgt' to fuzzy-match 'budgetctl'")
	}
	out := highlightMatches("budgetctl", idxs, styleMuted)
	// The highlighted variant must differ from a plain, unhighlighted render.
	if out == styleMuted.Render("budgetctl") {
		t.Error("expected highlightMatches to differ from plain styleMuted.Render for a real match")
	}
}

func TestHighlightMatches_NoMatchRendersPlain(t *testing.T) {
	out := highlightMatches("hello", nil, styleMuted)
	if out != styleMuted.Render("hello") {
		t.Errorf("expected nil idxs to render plain, got %q want %q", out, styleMuted.Render("hello"))
	}
}

func TestTruncate_DoesNotSplitMultiByteRunes(t *testing.T) {
	got := truncate("Überstunden Projekt", 6)
	if !strings.Contains(got, "Ü") {
		t.Errorf("expected the leading umlaut to survive truncation, got %q", got)
	}
	if r := []rune(got); len(r) != 6 {
		t.Errorf("expected exactly 6 runes (5 + ellipsis), got %d: %q", len(r), got)
	}
}

func TestRenderToday_SelectedRowNeverWraps(t *testing.T) {
	// Regression test: styleSelected carries Padding(0, 1) — 2 columns
	// reserved INSIDE its Width(contentW) budget. The row-building format
	// string's fixed-width budget must account for that padding, or the
	// selected (cursor) row overflows by exactly the padding width and
	// lipgloss word-wraps it onto an unwanted second line. Found while
	// adding fuzzy highlighting to this same row.
	//
	// Invariant: the total line count must be identical whether the task
	// name is short (well under the column budget) or long enough to fill
	// it exactly — if the long version wraps, it adds an extra line the
	// short version doesn't have.
	stop := time.Now()
	newModel := func(task string) model {
		return model{
			width: 100, height: 30,
			entries: []models.Entry{
				{Task: task, StartedAt: stop.Add(-time.Hour), StoppedAt: &stop},
			},
		}
	}

	shortLines := len(strings.Split(newModel("short task").renderToday(100, 30), "\n"))
	longLines := len(strings.Split(newModel(strings.Repeat("a very long task name ", 4)).renderToday(100, 30), "\n"))

	if longLines != shortLines {
		t.Errorf("expected the same line count regardless of task name length (no word-wrap), got %d lines for short vs %d for long", shortLines, longLines)
	}
}
