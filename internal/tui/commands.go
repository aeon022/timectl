package tui

import (
	tea "charm.land/bubbletea/v2"
	"github.com/aeon022/timectl/internal/models"
)

// ── Commands ─────────────────────────────────────────────────────────────────

func (m model) cmdStart(task, project string) tea.Cmd {
	s := m.store
	return func() tea.Msg {
		_, err := s.Start(task, project)
		if err != nil {
			return errMsg{err}
		}
		return refreshMsg{}
	}
}

func (m model) cmdStop() tea.Cmd {
	s := m.store
	return func() tea.Msg {
		_, err := s.Stop("")
		if err != nil {
			return errMsg{err}
		}
		return refreshMsg{}
	}
}

func (m model) cmdDelete(id int64) tea.Cmd {
	s := m.store
	return func() tea.Msg {
		if err := s.Delete(id); err != nil {
			return errMsg{err}
		}
		return refreshMsg{}
	}
}

// cmdRestore re-inserts a deleted entry with its original ID/fields — used
// by "u" within undoWindow of a delete.
func (m model) cmdRestore(e models.Entry) tea.Cmd {
	s := m.store
	return func() tea.Msg {
		if err := s.Restore(e); err != nil {
			return errMsg{err}
		}
		return refreshMsg{}
	}
}

func (m model) cmdSaveNotes(id int64, notes string) tea.Cmd {
	s := m.store
	return func() tea.Msg {
		if err := s.UpdateNotes(id, notes); err != nil {
			return errMsg{err}
		}
		return refreshMsg{}
	}
}

func (m model) cmdLoadWeek() tea.Cmd {
	s := m.store
	return func() tea.Msg {
		summaries, err := s.WeekSummary()
		if err != nil {
			return errMsg{err}
		}
		return weekLoadedMsg{summaries}
	}
}

func (m model) cmdLoadStats() tea.Cmd {
	s := m.store
	rate := m.hourlyRate
	return func() tea.Msg {
		text, err := buildStatsText(s, rate)
		if err != nil {
			return errMsg{err}
		}
		return statsLoadedMsg{text}
	}
}

func (m model) cmdLoadTasks() tea.Cmd {
	s := m.store
	return func() tea.Msg {
		tasks, _ := s.OpenTasks()
		return taskPickMsg{tasks: tasks}
	}
}

func (m model) cmdStartLinked(task, project, linkedTask, linkedTaskID string) tea.Cmd {
	s := m.store
	return func() tea.Msg {
		_, err := s.StartLinked(task, project, linkedTask, linkedTaskID)
		if err != nil {
			return errMsg{err}
		}
		return refreshMsg{}
	}
}
