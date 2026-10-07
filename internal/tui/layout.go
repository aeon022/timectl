package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/aeon022/missionctl-core/emptystate"
	"github.com/aeon022/missionctl-core/humanize"
	"github.com/aeon022/missionctl-core/statusbar"
	"github.com/aeon022/missionctl-core/theme"
	"github.com/aeon022/missionctl-core/ui"
	"github.com/charmbracelet/x/ansi"
)

// ── Layout ───────────────────────────────────────────────────────────────────
//
// One struct holds every number the main view's geometry depends on, so
// mainView (drawing) and rowHitTest (mouse) can never disagree about where a
// row is — the bug class the other tools' redesigns kept hitting.

const (
	heatPanelW = 30 // outer width of the "Last 30 days" panel
	weekPanelH = 5  // "This week" panel: 2 border rows + label/bar/duration rows
)

type mainLayout struct {
	w, h       int
	spacious   bool // terminal >= 30 rows: blank line under the header
	headRows   int
	footer     string
	footerRows int
	bodyH      int // rows between header and footer
	showHeat   bool
	topH       int // height of the panels row
	showWeek   bool
	leftW      int // heat panel outer width (0 when hidden)
	rightW     int // Today panel outer width
	todayX     int // screen column of the Today panel's left border
	preamble   int // lines inside the Today panel before the first entry
	entriesY   int // screen row of the first visible entry
	cap        int // entry rows that fit
	start      int // first visible entry index
}

// dims is the effective drawing size (the old defaults for an unsized model).
func (m model) dims() (w, h int) {
	w, h = m.width, m.height
	if w < 40 {
		w = 80
	}
	if h < 20 {
		h = 24
	}
	return w, h
}

func (m model) layout() mainLayout {
	w, h := m.dims()
	L := mainLayout{w: w, h: h}
	L.spacious = h+1 >= 30 // m.height already carries the 1-row redraw slack
	L.headRows = 2
	if L.spacious {
		L.headRows = 3
	}
	L.footer = m.footerString(w)
	L.footerRows = strings.Count(L.footer, "\n") + 1
	L.bodyH = max(h-L.headRows-L.footerRows, 4)
	L.showHeat = w >= 100
	minTop := 8 // panel rows needed for a usable Today panel; the heat panel needs 14
	if L.showHeat {
		minTop = 14
	}
	L.showWeek = L.spacious && w >= 70 && L.bodyH >= weekPanelH+minTop
	avail := L.bodyH
	if L.showWeek {
		avail -= weekPanelH
	}
	if !m.browseDate.IsZero() {
		L.preamble = 1
	}
	// Panels hug their content (no big empty boxes); leftover height stays blank
	// below. Entries need rows, the empty state needs ~4.
	need := 2 + L.preamble + m.todayBottomRows() + max(len(m.entries), 4)
	L.topH = min(avail, max(need, minTop))
	L.todayX, L.rightW = 1, w-2
	if L.showHeat {
		L.leftW = heatPanelW
		L.todayX = 1 + heatPanelW + 1
		L.rightW = w - 2 - heatPanelW - 1
	}
	L.cap = max(L.topH-2-L.preamble-m.todayBottomRows(), 1)
	L.start = windowStart(m.cursor, len(m.entries), L.cap)
	L.entriesY = L.headRows + 1 + L.preamble
	return L
}

// windowStart is the first visible index of a cap-row window over n rows that
// keeps cursor roughly centered (stateless, so draw and hit-test agree).
func windowStart(cursor, n, cap int) int {
	if n <= cap || cap <= 0 {
		return 0
	}
	return max(0, min(cursor-cap/2, n-cap))
}

// rowHitTest returns the m.entries index at screen position (x, y), or -1.
func (m model) rowHitTest(x, y int) int {
	L := m.layout()
	if x < L.todayX+1 || x >= L.todayX+L.rightW-1 {
		return -1
	}
	row := y - L.entriesY
	if row < 0 || row >= L.cap {
		return -1
	}
	if idx := L.start + row; idx < len(m.entries) {
		return idx
	}
	return -1
}

// ── Header / footer ──────────────────────────────────────────────────────────

// clockString renders a running timer's elapsed time as HH:MM:SS.
func clockString(d time.Duration) string {
	d = d.Round(time.Second)
	return fmt.Sprintf("%02d:%02d:%02d", int(d.Hours()), int(d.Minutes())%60, int(d.Seconds())%60)
}

// headerTimer is the header's middle: the live timer, or a dimmed idle note.
func (m model) headerTimer() string {
	if m.running == nil {
		return styleMuted.Render("no timer running")
	}
	dot := "●"
	if m.animStep/2%2 == 1 {
		dot = "○" // slow pulse, driven by the 1 s tick
	}
	label := m.running.Task
	if m.running.Project != "" {
		label += " (" + m.running.Project + ")"
	}
	return lipgloss.NewStyle().Foreground(theme.GreenV2).Render(dot) + " " +
		lipgloss.NewStyle().Bold(true).Render(label) + "  " +
		lipgloss.NewStyle().Foreground(theme.BlueV2).Render(clockString(m.running.ComputedDuration()))
}

// headerBlock is the shared top of every view: title · live timer · date, a
// rule, and (spacious terminals) a blank line.
func (m model) headerBlock(title string, spacious bool) string {
	w, _ := m.dims()
	left := lipgloss.NewStyle().Bold(true).Foreground(theme.BlueV2).Render("timectl") + styleMuted.Render(" · "+title)
	right := styleMuted.Render(time.Now().Format("Mon 02 Jan"))
	out := " " + ui.Header(w-2, left, m.headerTimer(), right) + "\n " + ui.Divider(w-2, "")
	if spacious {
		out += "\n"
	}
	return out
}

// footerString is the main view's bottom: an input line / palette block, a
// message, or the one-line contextual hints with the entry counter on the right.
func (m model) footerString(w int) string {
	switch {
	case m.imode != modeNone:
		return lipgloss.NewStyle().MaxWidth(w).Render(m.renderInput()) // fixed-width inputs must not overflow narrow terminals
	case m.errMsg != "":
		return "  " + styleRed.Render("Error: "+m.errMsg)
	case m.statusMsg != "":
		return "  " + styleGreen.Render("✓ "+m.statusMsg)
	case m.filterQ != "":
		return "  " + styleAmber.Render("filter: /"+m.filterQ) + styleFooter.Render("  esc:clear  ?:help")
	}
	right := ""
	if n := len(m.entries); n > 0 {
		right = styleMuted.Render(fmt.Sprintf("%d/%d", m.cursor+1, n))
	}
	first, second := [2]string{"n", "start"}, [2]string{"s", "stop"}
	if m.running != nil {
		first, second = second, first // a running timer: stopping comes first
	}
	// Priority order: statusbar drops the LAST hints first when narrow.
	hints := statusbar.Hints(max(w-4-lipgloss.Width(right), 10),
		first, second, [2]string{"?", "help"}, [2]string{"q", "quit"},
		[2]string{"T", "tasks"}, [2]string{"e", "notes"}, [2]string{"d", "delete"}, [2]string{"u", "undo"},
		[2]string{"/", "filter"}, [2]string{"←/→/t", "day"}, [2]string{"w", "week"}, [2]string{"v", "stats"},
		[2]string{"y", "copy"}, [2]string{"g", "open task"},
	)
	return "  " + statusbar.Line(w-2, hints, right)
}

// ── Main view ────────────────────────────────────────────────────────────────

func (m model) mainView() string {
	L := m.layout()
	title := "Today"
	if !m.browseDate.IsZero() {
		title = m.browseDate.Format("Mon 02 Jan")
	}
	top := ui.Panel(L.rightW, L.topH, title, m.renderToday(L.rightW-4, L.topH-2), true)
	if L.showHeat {
		left := ui.Panel(L.leftW, L.topH, "Last 30 days", m.renderHeat(L.leftW-3), false)
		top = lipgloss.JoinHorizontal(lipgloss.Top, left, " ", top)
	}
	body := indent1(top)
	if L.showWeek {
		days, totals, todayIdx := m.weekTotals()
		var week time.Duration
		for _, d := range totals {
			week += d
		}
		body += "\n" + indent1(ui.Panel(L.w-2, weekPanelH, "This week · "+ui.Duration(week),
			weekBand(L.w-5, days, totals, todayIdx, m.goalHours), false))
	}
	return ui.Frame(L.h, m.headerBlock("today", L.spacious), body, L.footer)
}

func indent1(s string) string { return " " + strings.ReplaceAll(s, "\n", "\n ") }

// todayBottomRows is how many rows the Today panel reserves under the entries:
// divider + total, plus the goal bar when it applies.
func (m model) todayBottomRows() int {
	if m.goalHours > 0 && m.browseDate.IsZero() {
		return 3
	}
	return 2
}

// padLeft/padRight pad by display width (the strings may carry ANSI).
func padLeft(s string, w int) string  { return strings.Repeat(" ", max(w-lipgloss.Width(s), 0)) + s }
func padRight(s string, w int) string { return s + strings.Repeat(" ", max(w-lipgloss.Width(s), 0)) }

// renderToday is the Today panel's content: entry rows (windowed around the
// cursor), then a pinned divider, total and goal bar. width is the content
// width, height the number of content rows.
func (m model) renderToday(width, height int) string {
	var lines []string
	if !m.browseDate.IsZero() {
		lines = append(lines, styleMuted.Render("← prev · → next · t today"))
	}
	pre := len(lines)
	if len(m.entries) == 0 {
		lines = append(lines, "", emptystate.Render(0, 0, "", "No entries yet", "press n to start a timer"))
		return strings.Join(lines, "\n")
	}
	var maxDur, total time.Duration
	for _, e := range m.entries {
		d := e.ComputedDuration()
		total += d
		maxDur = max(maxDur, d)
	}
	maxDur = max(maxDur, time.Second)

	capRows := max(height-pre-m.todayBottomRows(), 1)
	start := windowStart(m.cursor, len(m.entries), capRows)
	for i := start; i < min(start+capRows, len(m.entries)); i++ {
		lines = append(lines, m.entryRow(i, width, maxDur))
	}
	for len(lines) < pre+capRows { // pin the total to the panel's bottom
		lines = append(lines, "")
	}
	lines = append(lines, ui.Divider(width, ""))
	lines = append(lines, styleMuted.Render("Total  ")+lipgloss.NewStyle().Bold(true).Render(ui.Duration(total)))
	if m.goalHours > 0 && m.browseDate.IsZero() {
		lines = append(lines, goalLine(width, total, m.goalHours))
	}
	return strings.Join(lines, "\n")
}

// goalLine: "Goal   ███░░░  3h / 8h  37%".
func goalLine(width int, total time.Duration, goalHours float64) string {
	goal := time.Duration(goalHours * float64(time.Hour))
	ratio := total.Hours() / goalHours
	tail := fmt.Sprintf("  %s / %s  %d%%", ui.Duration(total), ui.Duration(goal), int(ratio*100))
	barW := min(max(width-7-lipgloss.Width(tail), 0), 24)
	bar := ""
	if barW >= 6 {
		bar = ui.Bar(barW, ratio, false)
	}
	return styleMuted.Render("Goal   ") + bar + styleMuted.Render(tail)
}

// entryRow renders entry i as one row of width cw: running marker, start time,
// task (+ dimmed project), slim bar relative to the longest entry, duration.
func (m model) entryRow(i, cw int, maxDur time.Duration) string {
	e := m.entries[i]
	d := e.ComputedDuration()
	tw := cw - 2 // ui.Row's accent/indent lead
	const durW = 7
	showBar := tw >= 46
	fixed := 2 + 5 + 2 + 2 + durW // marker, start, gaps, duration
	if showBar {
		fixed += 8 + 2
	}
	taskW := max(tw-fixed, 6)

	taskDisplay := e.Task
	if e.LinkedTask != "" {
		taskDisplay += " → " + e.LinkedTask
	}
	cell := ""
	if pw := min(lipgloss.Width(e.Project), 14); e.Project != "" && taskW >= 24 {
		cell = padRight(highlightMatches(humanize.Truncate(taskDisplay, taskW-pw-2), fuzzyMatchIndexes(m.filterQ, humanize.Truncate(taskDisplay, taskW-pw-2)), lipgloss.NewStyle()), taskW-pw-2) +
			"  " + lipgloss.NewStyle().Foreground(theme.SubtleV2).Render(ui.MidEllipsis(e.Project, pw))
	} else {
		t := humanize.Truncate(taskDisplay, taskW)
		cell = highlightMatches(t, fuzzyMatchIndexes(m.filterQ, t), lipgloss.NewStyle())
	}
	marker := "  "
	if e.IsRunning() {
		marker = lipgloss.NewStyle().Foreground(theme.GreenV2).Render("▶") + " "
	}
	text := marker + lipgloss.NewStyle().Foreground(theme.SubtleV2).Render(e.StartedAt.Format("15:04")) + "  " + padRight(cell, taskW)
	if showBar {
		text += "  " + ui.Bar(8, float64(d)/float64(maxDur), false)
	}
	text += "  " + padLeft(ui.Duration(d), durW)

	switch {
	case i == m.cursor:
		return ui.Row(cw, true, text)
	case i == m.hoverRow:
		plain := "  " + ansi.Strip(text)
		return theme.HoverV2.Render(padRight(ansi.Truncate(plain, cw, "…"), cw))
	}
	return ui.Row(cw, false, text)
}

// ── Heat panel + week band ───────────────────────────────────────────────────

func heatLevel(d time.Duration) int {
	h := d.Hours()
	switch {
	case h <= 0:
		return 0
	case h < 1:
		return 1
	case h < 3:
		return 2
	case h < 6:
		return 3
	}
	return 4
}

// todayTotal is today's tracked time, live from the loaded entries (so the
// running timer counts) when browsing today.
func (m model) todayTotal() time.Duration {
	var t time.Duration
	for _, e := range m.allEntries {
		t += e.ComputedDuration()
	}
	return t
}

// renderHeat is the "Last 30 days" panel content: Monday-first week rows of
// heat cells, a legend and today's total.
func (m model) renderHeat(width int) string {
	now := time.Now()
	byDay := map[string]time.Duration{}
	for _, d := range m.heatData {
		byDay[d.date.Format("2006-01-02")] = d.total
	}
	if m.browseDate.IsZero() {
		byDay[now.Format("2006-01-02")] = m.todayTotal()
	}
	first := now.AddDate(0, 0, -29)
	cells := make([]string, (int(first.Weekday())+6)%7) // Monday-first lead padding
	for i := range cells {
		cells[i] = " "
	}
	for i := 0; i < 30; i++ {
		cells = append(cells, ui.Heat(heatLevel(byDay[first.AddDate(0, 0, i).Format("2006-01-02")]), 4))
	}
	for len(cells)%7 != 0 {
		cells = append(cells, " ")
	}
	lines := []string{styleMuted.Render("M T W T F S S")}
	for i := 0; i < len(cells); i += 7 {
		lines = append(lines, strings.Join(cells[i:i+7], " "))
	}
	lines = append(lines, "", styleMuted.Render("less ")+ui.Heat(1, 4)+" "+ui.Heat(2, 4)+" "+ui.Heat(4, 4)+styleMuted.Render(" more"), "")
	t := m.todayTotal()
	lines = append(lines, styleMuted.Render("today"))
	if t > 0 {
		lines = append(lines, lipgloss.NewStyle().Bold(true).Foreground(theme.BlueV2).Render(ui.Duration(t)))
	} else {
		lines = append(lines, styleMuted.Render("nothing yet"))
	}
	return strings.Join(lines, "\n")
}

// weekTotals returns Monday..Sunday of the current week, each day's tracked
// time (today live from the entries) and today's index.
func (m model) weekTotals() (days [7]time.Time, totals [7]time.Duration, todayIdx int) {
	return weekTotals(m.heatData, m.todayTotal(), time.Now())
}

func weekTotals(heat []heatDay, todayTotal time.Duration, now time.Time) (days [7]time.Time, totals [7]time.Duration, todayIdx int) {
	byDay := map[string]time.Duration{}
	for _, d := range heat {
		byDay[d.date.Format("2006-01-02")] = d.total
	}
	todayIdx = (int(now.Weekday()) + 6) % 7
	monday := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).AddDate(0, 0, -todayIdx)
	for i := range days {
		days[i] = monday.AddDate(0, 0, i)
		totals[i] = byDay[days[i].Format("2006-01-02")]
	}
	totals[todayIdx] = todayTotal
	return days, totals, todayIdx
}

// weekBand draws the week as 7 columns — label, bar, duration — each bar scaled
// to the daily goal (or the longest day when that is larger). width is the
// content width available.
func weekBand(width int, days [7]time.Time, totals [7]time.Duration, todayIdx int, goalHours float64) string {
	colW := max((width-6)/7, 4)
	scale := max(goalHours, 0.1)
	for _, t := range totals {
		scale = max(scale, t.Hours())
	}
	var labels, bars, durs []string
	for i := range days {
		label := days[i].Format("Mon 02")
		if colW < 6 {
			label = days[i].Format("Mon")
		}
		dur := "–"
		if totals[i] > 0 {
			dur = ui.Duration(totals[i])
		}
		switch {
		case i == todayIdx:
			label = lipgloss.NewStyle().Bold(true).Foreground(theme.BlueV2).Render(label)
			dur = lipgloss.NewStyle().Bold(true).Render(dur)
		case i > todayIdx:
			label, dur = styleMuted.Render(label), styleMuted.Render(dur)
		default:
			label, dur = styleMuted.Render(label), lipgloss.NewStyle().Render(dur)
		}
		labels = append(labels, padRight(ansi.Truncate(label, colW, "…"), colW))
		bars = append(bars, ui.Bar(colW, totals[i].Hours()/scale, false))
		durs = append(durs, padRight(ansi.Truncate(dur, colW, "…"), colW))
	}
	return strings.Join(labels, " ") + "\n" + strings.Join(bars, " ") + "\n" + strings.Join(durs, " ")
}

// ── Week / stats / task-pick views ───────────────────────────────────────────

// framed wraps a titled panel body with the shared header and a one-line footer.
func (m model) framed(title, panelTitle, content string, hints ...[2]string) string {
	w, h := m.dims()
	spacious := h+1 >= 30
	head := m.headerBlock(title, spacious)
	headRows := 2
	if spacious {
		headRows = 3
	}
	body := indent1(ui.Panel(w-2, max(h-headRows-1, 4), panelTitle, content, true))
	footer := "  " + statusbar.Line(w-2, statusbar.Hints(w-4, hints...), "")
	return ui.Frame(h, head, body, footer)
}

func (m model) weekView() string {
	w, _ := m.dims()
	var lines []string
	if len(m.weekSummaries) == 0 {
		lines = []string{"", styleMuted.Render("No data yet.")}
	} else {
		var maxD, total time.Duration
		for _, ds := range m.weekSummaries {
			maxD = max(maxD, ds.Total)
			total += ds.Total
		}
		maxD = max(maxD, time.Minute)
		barW := min(max(w-30, 8), 40)
		today := time.Now().Format("2006-01-02")
		for _, ds := range m.weekSummaries {
			label := ds.Date.Format("Mon 02")
			if ds.Date.Format("2006-01-02") == today {
				label = lipgloss.NewStyle().Bold(true).Foreground(theme.BlueV2).Render(label)
			} else {
				label = styleMuted.Render(label)
			}
			lines = append(lines, label+"  "+ui.Bar(barW, float64(ds.Total)/float64(maxD), false)+"  "+padLeft(ui.Duration(ds.Total), 7))
		}
		lines = append(lines, ui.Divider(min(w-6, barW+22), ""),
			styleMuted.Render("Total  ")+lipgloss.NewStyle().Bold(true).Render(ui.Duration(total)))
	}
	return m.framed("this week", "This week", strings.Join(lines, "\n"),
		[2]string{"esc", "back"}, [2]string{"?", "help"}, [2]string{"q", "quit"})
}

func (m model) statsView() string {
	content := m.statsText
	if content == "" {
		content = emptystate.Loading(0, 0, "", "Loading…")
	}
	return m.framed("stats", "Stats · last 14 days", content,
		[2]string{"esc", "back"}, [2]string{"?", "help"}, [2]string{"q", "quit"})
}

func (m model) taskPickView() string {
	w, _ := m.dims()
	var lines []string
	switch {
	case m.taskList == nil:
		lines = []string{emptystate.Loading(0, 0, "", "Loading…")}
	case len(m.taskList) == 0:
		lines = []string{styleMuted.Render("No open tasks found in taskctl.")}
	default:
		for i, t := range m.taskList {
			lines = append(lines, ui.Row(w-6, i == m.taskCursor, t.Title))
		}
	}
	return m.framed("open tasks", "Open tasks · taskctl", strings.Join(lines, "\n"),
		[2]string{"enter", "start timer"}, [2]string{"j/k", "navigate"}, [2]string{"esc", "back"})
}
