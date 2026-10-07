package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/aeon022/missionctl-core/keymap"
	"github.com/aeon022/missionctl-core/overlay"
	"github.com/aeon022/missionctl-core/palette"
	"github.com/aeon022/missionctl-core/ui"
	"github.com/aeon022/timectl/internal/models"
	"github.com/aeon022/timectl/internal/store"
)

// ── View ─────────────────────────────────────────────────────────────────────

func (m model) View() tea.View {
	v := tea.NewView(m.viewContent())
	// v1's tea.WithAltScreen()/WithMouseAllMotion() Program options are gone
	// in v2 — they are per-View fields now.
	v.AltScreen = true
	v.MouseMode = tea.MouseModeAllMotion
	v.ReportFocus = true // FocusMsg → reload stale data when the window regains focus
	return v
}

func (m model) viewContent() string {
	switch m.current {
	case viewWeek:
		return m.weekView()
	case viewStats:
		return m.statsView()
	case viewTaskPick:
		return m.taskPickView()
	case viewHelp:
		// inset 0: no enclosing border around the background views here.
		return overlay.CenterDim(m.backgroundView(), m.renderHelpPopup(), m.width, m.height, 0)
	default:
		return m.mainView()
	}
}

// undoWindow is how long after a delete "u" still restores it — same
// duration taskctl uses for its own delete-undo.
const undoWindow = 5 * time.Second

func (m model) renderInput() string {
	if m.imode == modeCommand {
		return m.renderPaletteBlock()
	}
	var prompt string
	switch m.imode {
	case modeNewTask:
		prompt = "New task: "
	case modeConfirmDelete:
		if len(m.entries) > 0 {
			prompt = fmt.Sprintf("Delete %q? (y/n): ", m.entries[m.cursor].Task)
		}
	case modeEditNotes:
		prompt = "Notes: "
	case modeFilter:
		prompt = "/ "
	}
	return "  " + styleAmber.Render(prompt) + m.input.View()
}

// renderPaletteBlock renders the ":" command palette's input line + up to 6
// live-filtered matches, as a single multi-line string joined for
// lipgloss.JoinVertical (mainView's footer slot).
func (m model) renderPaletteBlock() string {
	lines := []string{"  " + styleAmber.Render(": ") + m.input.View()}
	matches := palette.Match(paletteCommands, m.input.Value())
	if len(matches) > 6 {
		matches = matches[:6]
	}
	if len(matches) == 0 {
		lines = append(lines, "    "+styleMuted.Render("no matching command"))
	}
	for i, c := range matches {
		row := fmt.Sprintf("%-9s %s", c.Name, c.Desc)
		if i == m.paletteCursor {
			lines = append(lines, "    "+styleGreen.Render("▶ "+row))
		} else {
			lines = append(lines, "      "+styleMuted.Render(row))
		}
	}
	return strings.Join(lines, "\n")
}

func (m model) helpContent() string {
	return keymap.New("timectl", "time tracking from the terminal").
		Section("Timer").
		Row("n", "start new timer (task@project)").
		Row("T", "task picker — start timer from open taskctl task").
		Row("s", "stop running timer").
		Row("r", "restart selected entry's task").
		Row("c", "copy selected entry into new-task input").
		Section("Entries").
		Row("j / k", "move selection").
		Row("e", "edit notes").
		Row("d", "delete entry (asks to confirm)").
		Row("u", "undo last delete").
		Row("y", "copy task name to clipboard").
		Row("g", "open linked taskctl task (if entry has one)").
		Row("/", "filter by task, project, notes (esc clears)").
		Row(":", "command palette — type an action by name").
		Section("Views").
		Row("← / →", "browse previous / next day").
		Row("t", "back to today").
		Row("w", "week breakdown").
		Row("v", "stats (top tasks, streak, earnings)").
		Section("Other").
		Row("?", "toggle this help").
		Row("q", "quit / back").
		String()
}

// backgroundView renders whichever view help was opened from (help is
// reachable from more than just the main view here), so the overlay's
// background matches what was actually on screen.
func (m model) backgroundView() string {
	switch m.helpReturnTo {
	case viewWeek:
		return m.weekView()
	case viewStats:
		return m.statsView()
	default:
		return m.mainView()
	}
}

// openHelp sizes and populates the transient help popup (see
// renderHelpPopup/overlay.Center) from the ACTUAL rendered background
// height, not the terminal size.
func (m model) openHelp() model {
	m.helpReturnTo = m.current
	bgLines := strings.Split(m.backgroundView(), "\n")

	safeH := max(6, len(bgLines))
	popH := min(safeH, 22)
	popW := min(70, m.width)
	if popW < 40 {
		popW = 40
	}

	vp := viewport.New(viewport.WithWidth(popW-6), viewport.WithHeight(popH-5)) // border 1+1, padding(1,2) → 2 rows/4 cols; -1 row for footer
	vp.SetContent(m.helpContent())

	m.helpVP = vp
	m.helpPopW = popW
	m.helpPopH = popH
	m.current = viewHelp
	return m
}

// renderHelpPopup renders the help viewport in a bordered box, meant to be
// composited over the background view via overlay.Center rather than
// replacing the whole screen.
func (m model) renderHelpPopup() string {
	footer := "esc / ?  close"
	if m.helpVP.TotalLineCount() > m.helpVP.Height() {
		footer = fmt.Sprintf("j/k scroll (%d%%)  ·  %s", int(m.helpVP.ScrollPercent()*100), footer)
	}
	body := m.helpVP.View() + "\n" + styleMuted.Render(footer)
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorBlue).
		Padding(1, 2).
		Width(m.helpPopW).
		Render(body)
}

// ── Stats builder ─────────────────────────────────────────────────────────────

func buildStatsText(s *store.Store, hourlyRate float64) (string, error) {
	entries, err := s.RecentDays(14)
	if err != nil {
		return "", err
	}

	taskTotals := map[string]time.Duration{}
	projTotals := map[string]time.Duration{}
	dayTotals := map[string]time.Duration{}
	daySet := map[string]bool{}

	for _, e := range entries {
		d := e.ComputedDuration()
		taskTotals[e.Task] += d
		if e.Project != "" {
			projTotals[e.Project] += d
		}
		day := e.StartedAt.Format("2006-01-02")
		dayTotals[day] += d
		daySet[day] = true
	}

	var sb strings.Builder

	sb.WriteString(styleAmber.Render("  Top tasks (last 14 days):") + "\n")
	for i, kv := range topN(taskTotals, 5) {
		sb.WriteString(fmt.Sprintf("  %d. %-28s %s\n", i+1, kv.k, ui.Duration(kv.v)))
	}

	sb.WriteString("\n" + styleAmber.Render("  Top projects:") + "\n")
	topProjs := topN(projTotals, 3)
	if len(topProjs) == 0 {
		sb.WriteString(styleMuted.Render("  (none tagged)") + "\n")
	}
	for i, kv := range topProjs {
		sb.WriteString(fmt.Sprintf("  %d. %-28s %s\n", i+1, kv.k, ui.Duration(kv.v)))
	}

	var totalDur time.Duration
	for _, d := range dayTotals {
		totalDur += d
	}
	var avg time.Duration
	if len(dayTotals) > 0 {
		avg = totalDur / time.Duration(len(dayTotals))
	}
	sb.WriteString("\n" + styleAmber.Render("  Average day (last 14 days):") + "\n")
	sb.WriteString(fmt.Sprintf("  %s\n", ui.Duration(avg)))

	streak := models.ComputeStreak(daySet)
	sb.WriteString("\n" + styleAmber.Render("  Current streak:") + "\n")
	sb.WriteString(fmt.Sprintf("  %d day(s)\n", streak))

	if hourlyRate > 0 {
		earnings := totalDur.Hours() * hourlyRate
		sb.WriteString("\n" + styleAmber.Render("  Earnings (last 14 days):") + "\n")
		sb.WriteString(fmt.Sprintf("  at $%.0f/h: $%.2f\n", hourlyRate, earnings))
	}

	return sb.String(), nil
}

type kvPair struct {
	k string
	v time.Duration
}

func topN(m map[string]time.Duration, n int) []kvPair {
	pairs := make([]kvPair, 0, len(m))
	for k, v := range m {
		pairs = append(pairs, kvPair{k, v})
	}
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].v > pairs[j].v })
	if n > len(pairs) {
		n = len(pairs)
	}
	return pairs[:n]
}

// motionThrottleFilter drops MouseMotionMsg messages arriving <16ms apart.
func motionThrottleFilter() func(tea.Model, tea.Msg) tea.Msg {
	var lastMotion time.Time
	return func(_ tea.Model, msg tea.Msg) tea.Msg {
		if _, ok := msg.(tea.MouseMotionMsg); !ok {
			return msg
		}
		now := time.Now()
		if now.Sub(lastMotion) < 16*time.Millisecond {
			return nil
		}
		lastMotion = now
		return msg
	}
}
