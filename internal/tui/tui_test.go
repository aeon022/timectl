package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
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
