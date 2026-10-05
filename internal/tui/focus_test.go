package tui

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

func focusOn(m model) (model, tea.Cmd) {
	next, cmd := m.Update(tea.FocusMsg{})
	return next.(model), cmd
}

func TestFocusReloadsStaleMainView(t *testing.T) {
	m := newTestModel()
	m.lastLoad = time.Now().Add(-time.Minute)
	if _, cmd := focusOn(m); cmd == nil {
		t.Error("stale main view must reload on focus")
	}
}

func TestFocusSkipsFreshOrBusy(t *testing.T) {
	cases := map[string]func(m *model){
		"fresh":       func(m *model) { m.lastLoad = time.Now() },
		"palette":     func(m *model) { m.imode = modeCommand },
		"filter":      func(m *model) { m.imode = modeFilter },
		"new task":    func(m *model) { m.imode = modeNewTask },
		"confirm del": func(m *model) { m.imode = modeConfirmDelete },
		"edit notes":  func(m *model) { m.imode = modeEditNotes },
		"week view":   func(m *model) { m.current = viewWeek },
		"stats view":  func(m *model) { m.current = viewStats },
		"help popup":  func(m *model) { m.current = viewHelp },
		"task picker": func(m *model) { m.current = viewTaskPick },
	}
	for name, set := range cases {
		t.Run(name, func(t *testing.T) {
			m := newTestModel()
			m.lastLoad = time.Now().Add(-time.Minute)
			set(&m)
			if _, cmd := focusOn(m); cmd != nil {
				t.Errorf("%s: focus must not reload", name)
			}
		})
	}
}

func TestViewReportsFocus(t *testing.T) {
	if !newTestModel().View().ReportFocus {
		t.Error("View must set ReportFocus or FocusMsg never arrives")
	}
}

func TestCopyIsBatchOfOSC52AndPbcopy(t *testing.T) {
	b, ok := copyToClipboardCmd("task")().(tea.BatchMsg)
	if !ok || len(b) != 2 {
		t.Errorf("want a 2-command batch (SetClipboard + pbcopy), got %v", b)
	}
}
