package tui

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/aeon022/missionctl-core/humanize"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/aeon022/missionctl-core/keymap"
	"github.com/aeon022/missionctl-core/overlay"
	"github.com/aeon022/missionctl-core/palette"
	"github.com/aeon022/missionctl-core/theme"
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
		return overlay.Center(m.backgroundView(), m.renderHelpPopup(), m.width, m.height, 0)
	default:
		return m.mainView()
	}
}

// heatmapPanelW is the heatmap (left) panel's fixed outer width — shared
// with rowHitTest so the entry list's screen X-offset can't drift from
// what mainView actually renders.
const heatmapPanelW = 30

// undoWindow is how long after a delete "u" still restores it — same
// duration taskctl uses for its own delete-undo.
const undoWindow = 5 * time.Second

func (m model) mainView() string {
	w, h := m.width, m.height
	if w < 40 {
		w = 80
	}
	if h < 20 {
		h = 24
	}

	heatW := heatmapPanelW
	rightW := w - heatW - 6
	if rightW < 20 {
		rightW = 20
	}

	panelH := h - 6
	if m.imode == modeCommand {
		// The palette's own footer block grows past the usual single input
		// line (up to 6 match rows) — shrink the fixed-height panels above
		// it by the same amount so the whole frame still fits the terminal
		// instead of pushing the header off the top.
		panelH -= 7
	}
	left := panelStyle.Width(heatW).Height(panelH).Render(m.renderHeatmap())
	right := panelStyle.Width(rightW).Height(panelH).Render(m.renderToday(rightW, panelH))
	panels := lipgloss.JoinHorizontal(lipgloss.Top, left, "  ", right)

	var footer string
	switch {
	case m.imode != modeNone:
		footer = m.renderInput()
	case m.errMsg != "":
		footer = styleRed.Render("Error: " + m.errMsg)
	case m.statusMsg != "":
		footer = styleGreen.Render("✓ " + m.statusMsg)
	case m.filterQ != "":
		footer = styleAmber.Render("filter: /"+m.filterQ) + styleFooter.Render("  esc:clear  ?:help")
	default:
		footer = styleFooter.Render("n:start  T:tasks  s:stop  e:notes  d:delete  u:undo  y:copy  g:open task  /:filter  ←/→/t:day  w:week  v:stats  ?:help  q:quit")
	}

	return lipgloss.JoinVertical(lipgloss.Left,
		styleHeader.Render("timectl — today"),
		panels,
		footer,
	)
}

// rowHitTest returns the m.entries index at screen position (x, y), or -1
// if the click missed. Entries have no scroll window (renderToday appends
// every entry unconditionally) and no section headers, so the mapping is
// just a fixed offset: header line(1) + today panel's top border(1) +
// renderToday's own 2-line preamble (idle/running or date-browse line,
// then a divider — always exactly 2 lines either way) = row 4. x must
// land inside the today (right) panel, past the heatmap panel + gap.
func (m model) rowHitTest(x, y int) int {
	if x < heatmapPanelW+2 {
		return -1
	}
	idx := y - 4
	if idx < 0 || idx >= len(m.entries) {
		return -1
	}
	return idx
}

func (m model) renderHeatmap() string {
	today := time.Now()
	totalMap := map[string]time.Duration{}
	for _, d := range m.heatData {
		totalMap[d.date.Format("2006-01-02")] = d.total
	}

	days := make([]time.Time, 30)
	for i := range days {
		days[29-i] = today.AddDate(0, 0, -i)
	}

	cells := make([]string, int(days[0].Weekday()))
	for i := range cells {
		cells[i] = "  "
	}
	for _, d := range days {
		cells = append(cells, heatCellHours(totalMap[d.Format("2006-01-02")]))
	}
	for len(cells)%7 != 0 {
		cells = append(cells, "  ")
	}

	var lines []string
	lines = append(lines, styleMuted.Render("last 30 days"))
	lines = append(lines, "")
	lines = append(lines, styleMuted.Render("S M T W T F S"))
	for i := 0; i < len(cells); i += 7 {
		lines = append(lines, strings.Join(cells[i:i+7], " "))
	}

	// Today total + streak.
	var todayTotal time.Duration
	for _, e := range m.entries {
		todayTotal += e.ComputedDuration()
	}
	lines = append(lines, "")
	lines = append(lines, styleMuted.Render("today"))
	if todayTotal > 0 {
		lines = append(lines, styleCyan.Render(models.FormatDuration(todayTotal)))
	} else {
		lines = append(lines, styleMuted.Render("nothing yet"))
	}

	return strings.Join(lines, "\n")
}

func heatCellHours(d time.Duration) string {
	b := "█"
	h := d.Hours()
	switch {
	case h == 0:
		return styleMuted.Render(b)
	case h < 2:
		return lipgloss.NewStyle().Foreground(adaptive("30", "23")).Render(b)
	case h < 4:
		return styleCyan.Render(b)
	default:
		return styleCyan.Bold(true).Render(b)
	}
}

func (m model) renderToday(width, height int) string {
	var lines []string

	// Date header when browsing past days.
	if !m.browseDate.IsZero() {
		label := styleAmber.Render(m.browseDate.Format("Mon Jan 02"))
		lines = append(lines, " "+label+styleMuted.Render("  ← prev · → next · t today")+" ")
	}

	// Header: running timer or idle (only relevant for today).
	if m.running != nil && m.browseDate.IsZero() {
		spins := [4]string{"⠋", "⠙", "⠹", "⠸"}
		spin := spins[m.animStep%4]
		elapsed := models.FormatDuration(m.running.ComputedDuration())
		proj := ""
		if m.running.Project != "" {
			proj = " (" + m.running.Project + ")"
		}
		lines = append(lines,
			styleRunning.Render(fmt.Sprintf("▶ %s%s", m.running.Task, proj))+
				"  "+styleMuted.Render(elapsed)+"  "+styleAmber.Render(spin))
	} else if m.browseDate.IsZero() {
		lines = append(lines, styleMuted.Render("No timer running"))
	}
	lines = append(lines, styleDivider.Render(strings.Repeat("─", width-4)))

	if len(m.entries) == 0 {
		lines = append(lines, "", styleMuted.Render("  No entries yet — press n to start a timer."))
		return strings.Join(lines, "\n")
	}

	// Compute max duration for bar scaling.
	var maxDur time.Duration
	for _, e := range m.entries {
		if d := e.ComputedDuration(); d > maxDur {
			maxDur = d
		}
	}
	if maxDur == 0 {
		maxDur = time.Second
	}

	// Row layout (manual 1-space padding on each side). rowPlain's width
	// must land on contentW-2, not contentW: it gets fed through
	// styleSelected.Width(contentW).Render(...) for the cursor row, and
	// styleSelected also carries Padding(0, 1) — 2 columns reserved INSIDE
	// that Width() budget for its own padding. Undercounting either the
	// literal separators in the "%-2s%s  %-*s  [%s]  %-9s" format string
	// below, or this padding, makes rowPlain wider than the space actually
	// left for content, and lipgloss word-wraps the overflow onto a second
	// line. Found while adding fuzzy-search highlighting to this same row,
	// verified with a forced-ANSI render (not visible in plain-text output).
	// contentW = width - 2
	// fixed = indicator(2) + time(5) + sep(2) + "  ["(3) + bar(12) + "]  "(3) + dur(9) + padding(2) = 38
	contentW := width - 2
	barW := 12
	fixed := 2 /*indicator*/ + 5 /*time*/ + 2 /*sep*/ + 3 /*"  ["*/ + barW + 3 /*"]  "*/ + 9 /*dur*/ + 2 /*styleSelected Padding(0,1)*/
	taskW := contentW - fixed
	if taskW < 6 {
		taskW = 6
	}

	var total time.Duration
	for i, e := range m.entries {
		d := e.ComputedDuration()
		total += d

		// Indicator.
		indicator := "  "
		if e.IsRunning() {
			indicator = styleGreen.Render("▶") + " "
		}

		// Start time.
		startStr := e.StartedAt.Format("15:04")

		// Task name — truncate and pad; append linked task indicator if present.
		taskDisplay := e.Task
		if e.LinkedTask != "" {
			taskDisplay = e.Task + " → " + e.LinkedTask
		}
		matchIdx := fuzzyMatchIndexes(m.filterQ, taskDisplay)
		task := humanize.Truncate(taskDisplay, taskW)

		// Duration bar.
		filled := int(float64(d) / float64(maxDur) * float64(barW))
		if d > 0 && filled == 0 {
			filled = 1
		}
		barPlain := strings.Repeat("█", filled) + strings.Repeat("░", barW-filled)
		barStyled := styleCyan.Render(barPlain)

		durStr := models.FormatDuration(d)
		if len(durStr) > 9 {
			durStr = durStr[:9]
		}

		switch {
		case i == m.cursor, i == m.hoverRow:
			// Selected/hovered: plain text row so styleSelected/theme.HoverV2
			// fills correctly (see the comment further down about not
			// nesting already-styled text inside a wrapping Render call).
			rowPlain := fmt.Sprintf("%-2s%s  %-*s  [%s]  %-9s",
				func() string {
					if e.IsRunning() {
						return "▶ "
					}
					return "  "
				}(),
				startStr, taskW, task, barPlain, durStr)
			if i == m.cursor {
				lines = append(lines, styleSelected.Width(contentW).Render(rowPlain))
			} else {
				lines = append(lines, theme.HoverV2.Width(contentW).Render(rowPlain))
			}
		default:
			// Styled: build with concatenation to avoid styleNormal wrapping ANSI.
			// Highlight first (per-character, self-contained ANSI), THEN pad
			// via a plain (colorless) Width() — padding the already-styled
			// string with fmt's "%-*s" would count escape bytes as width and
			// misalign the column.
			taskCol := lipgloss.NewStyle().Width(taskW).Render(highlightMatches(task, matchIdx, styleMuted))
			row := " " + indicator + startStr + "  " +
				taskCol +
				"  [" + barStyled + "]  " +
				styleMuted.Render(durStr) + " "
			lines = append(lines, row)
		}
	}

	lines = append(lines, styleDivider.Render(strings.Repeat("─", width-4)))
	lines = append(lines, " "+styleBlue.Render("Total: "+models.FormatDuration(total))+" ")

	if m.goalHours > 0 && m.browseDate.IsZero() {
		const goalBarW = 20
		pct := math.Min(1.0, total.Hours()/m.goalHours)
		filled := int(pct * goalBarW)
		bar := strings.Repeat("█", filled) + strings.Repeat("░", goalBarW-filled)
		goalTotal := time.Duration(m.goalHours * float64(time.Hour))
		goalLine := " " + styleMuted.Render("goal  [") + styleCyan.Render(bar) +
			styleMuted.Render("]  ") + styleCyan.Render(models.FormatDuration(total)) +
			styleMuted.Render(" / "+models.FormatDuration(goalTotal)) + " "
		lines = append(lines, goalLine)
	}

	return strings.Join(lines, "\n")
}

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

func (m model) weekView() string {
	var b strings.Builder

	b.WriteString(styleHeader.Render("timectl — this week") + "\n\n")

	if len(m.weekSummaries) == 0 {
		b.WriteString(styleMuted.Render("  No data yet.") + "\n")
	} else {
		rows, weekTotal := models.WeekBarChart(m.weekSummaries)

		for _, r := range rows {
			line := fmt.Sprintf("  %s  %s  %s",
				r.Label,
				styleCyan.Render(r.Bar),
				styleBlue.Render(r.Duration),
			)
			b.WriteString(line + "\n")
		}

		b.WriteString(styleDivider.Render("  "+strings.Repeat("─", 55)) + "\n")
		b.WriteString(styleBlue.Render(fmt.Sprintf("  Total: %s", models.FormatDuration(weekTotal))) + "\n")
	}

	m.padToFooter(&b)
	b.WriteString(styleFooter.Render("  esc/q back") + "\n")
	return b.String()
}

func (m model) statsView() string {
	var b strings.Builder
	b.WriteString(styleHeader.Render("timectl — stats") + "\n\n")
	if m.statsText == "" {
		b.WriteString(styleMuted.Render("  Loading...") + "\n")
	} else {
		b.WriteString(m.statsText)
	}
	m.padToFooter(&b)
	b.WriteString(styleFooter.Render("  esc/q back") + "\n")
	return b.String()
}

func (m model) taskPickView() string {
	var b strings.Builder
	b.WriteString(styleHeader.Render("timectl — open tasks") + "\n\n")

	if m.taskList == nil {
		b.WriteString(styleMuted.Render("  Loading...") + "\n")
	} else if len(m.taskList) == 0 {
		b.WriteString(styleMuted.Render("  No open tasks found in taskctl.") + "\n")
	} else {
		for i, t := range m.taskList {
			if i == m.taskCursor {
				b.WriteString(styleSelected.Render("  "+t.Title) + "\n")
			} else {
				b.WriteString("  " + styleMuted.Render(t.Title) + "\n")
			}
		}
	}

	m.padToFooter(&b)
	b.WriteString(styleFooter.Render("  j/k navigate · enter start timer · esc/q back") + "\n")
	return b.String()
}

// padToFooter pins the trailing footer line to the bottom of the terminal
// instead of letting it glue itself right under a short body — pads with
// blank lines up to m.height first, same pattern mainView gets for free
// from lipgloss's panelStyle.Height().
func (m model) padToFooter(b *strings.Builder) {
	if m.height <= 0 {
		return
	}
	for lines := strings.Count(b.String(), "\n"); lines < m.height-1; lines++ {
		b.WriteString("\n")
	}
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
		sb.WriteString(fmt.Sprintf("  %d. %-28s %s\n", i+1, kv.k, models.FormatDuration(kv.v)))
	}

	sb.WriteString("\n" + styleAmber.Render("  Top projects:") + "\n")
	topProjs := topN(projTotals, 3)
	if len(topProjs) == 0 {
		sb.WriteString(styleMuted.Render("  (none tagged)") + "\n")
	}
	for i, kv := range topProjs {
		sb.WriteString(fmt.Sprintf("  %d. %-28s %s\n", i+1, kv.k, models.FormatDuration(kv.v)))
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
	sb.WriteString(fmt.Sprintf("  %s\n", models.FormatDuration(avg)))

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
