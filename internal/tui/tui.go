package tui

import (
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/aeon022/missionctl-core/overlay"
	"github.com/aeon022/missionctl-core/theme"
	"github.com/aeon022/timectl/internal/models"
	"github.com/aeon022/timectl/internal/store"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ── Design tokens ────────────────────────────────────────────────────────────

var (
	// Shared across the suite via missionctl-core/theme.
	colorBlue   = theme.Blue
	colorCyan   = lipgloss.AdaptiveColor{Light: "30", Dark: "51"}
	colorGreen  = theme.Green
	colorRed    = theme.Red
	colorAmber  = theme.Amber
	colorMuted  = theme.Muted
	colorSubtle = theme.Subtle
	// selectedBg/selectedFg intentionally NOT shared — timectl's selected-row
	// color is a deliberately different shade from the suite default.
	selectedBg = lipgloss.AdaptiveColor{Light: "159", Dark: "23"}
	selectedFg = lipgloss.AdaptiveColor{Light: "16", Dark: "255"}
)

var (
	styleHeader = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorBlue).
			Padding(0, 1)

	styleRunning = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorCyan)

	styleFooter = lipgloss.NewStyle().
			Foreground(colorMuted)

	styleSelected = lipgloss.NewStyle().
			Background(selectedBg).
			Foreground(selectedFg).
			Padding(0, 1)

	styleNormal = lipgloss.NewStyle().
			Foreground(colorSubtle).
			Padding(0, 1)

	styleDivider = lipgloss.NewStyle().
			Foreground(colorMuted)

	styleAmber = lipgloss.NewStyle().
			Foreground(colorAmber)

	styleRed = lipgloss.NewStyle().
			Foreground(colorRed)

	styleBlue = lipgloss.NewStyle().
			Foreground(colorBlue)

	styleMuted = lipgloss.NewStyle().
			Foreground(colorMuted)

	styleGreen = lipgloss.NewStyle().
			Foreground(colorGreen)

	panelStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorBlue).
			Padding(0, 1)

	styleCyan = lipgloss.NewStyle().
			Foreground(colorCyan)
)

// ── Views ────────────────────────────────────────────────────────────────────

type viewKind int

const (
	viewMain viewKind = iota
	viewWeek
	viewStats
	viewTaskPick
	viewHelp
)

// ── Messages ─────────────────────────────────────────────────────────────────

type tickMsg time.Time
type refreshMsg struct{}
type dayEntriesMsg struct{ entries []models.Entry }
type errMsg struct{ err error }
type weekLoadedMsg struct{ summaries []models.DaySummary }
type statsLoadedMsg struct{ text string }
type heatLoadedMsg struct{ data []heatDay }
type taskPickMsg struct{ tasks []string }

// ── Heat data ─────────────────────────────────────────────────────────────────

type heatDay struct {
	date  time.Time
	total time.Duration
}

// ── Input mode ────────────────────────────────────────────────────────────────

type inputMode int

const (
	modeNone inputMode = iota
	modeNewTask
	modeConfirmDelete
	modeEditNotes
	modeFilter
)

// ── model ────────────────────────────────────────────────────────────────────

type model struct {
	store   *store.Store
	width   int
	height  int
	current viewKind

	entries    []models.Entry // filtered view of allEntries
	allEntries []models.Entry
	filterQ    string
	running    *models.Entry
	cursor     int
	imode      inputMode
	input      textinput.Model
	errMsg     string

	weekSummaries []models.DaySummary
	statsText     string

	heatData   []heatDay
	heatLoaded bool
	animStep   int

	browseDate time.Time // zero = today
	goalHours  float64
	hourlyRate float64

	taskList   []string
	taskCursor int

	// "?" transient help popup — reachable from viewMain, viewWeek, and
	// viewStats (not just the main view, unlike the other tools this
	// pattern was rolled out to first), so the view it was opened from
	// must be remembered to pick the right background and to return to
	// the right place on close instead of always dumping back to viewMain.
	helpReturnTo viewKind
	helpVP       viewport.Model
	helpPopW     int
	helpPopH     int
}

func newModel(s *store.Store) model {
	ti := textinput.New()
	ti.CharLimit = 200
	ti.Width = 50
	goal := 8.0
	if v := os.Getenv("TIMECTL_GOAL_HOURS"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 {
			goal = f
		}
	}
	var hourlyRate float64
	if v := os.Getenv("TIMECTL_HOURLY_RATE"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 {
			hourlyRate = f
		}
	}
	return model{
		store:      s,
		input:      ti,
		goalHours:  goal,
		hourlyRate: hourlyRate,
	}
}

// ── Init ─────────────────────────────────────────────────────────────────────

func (m model) Init() tea.Cmd {
	return tea.Batch(doRefresh(m.store), tick(), cmdLoadHeat(m.store))
}

func tick() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func doRefresh(s *store.Store) tea.Cmd {
	return func() tea.Msg { return refreshMsg{} }
}

func cmdLoadHeat(s *store.Store) tea.Cmd {
	return func() tea.Msg {
		entries, err := s.RecentDays(30)
		if err != nil {
			return heatLoadedMsg{}
		}
		dayMap := map[string]time.Duration{}
		for _, e := range entries {
			dayMap[e.StartedAt.Format("2006-01-02")] += e.ComputedDuration()
		}
		today := time.Now()
		data := make([]heatDay, 30)
		for i := range data {
			d := today.AddDate(0, 0, -(29 - i))
			data[i] = heatDay{date: d, total: dayMap[d.Format("2006-01-02")]}
		}
		return heatLoadedMsg{data: data}
	}
}

// ── Update ───────────────────────────────────────────────────────────────────

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tickMsg:
		running, _ := m.store.Running()
		m.running = running
		m.animStep++
		return m, tick()

	case refreshMsg:
		var entries []models.Entry
		var err error
		if m.browseDate.IsZero() {
			entries, err = m.store.Today()
		} else {
			from := m.browseDate.Truncate(24 * time.Hour)
			to := from.Add(24 * time.Hour)
			entries, err = m.store.Range(from, to)
		}
		if err != nil {
			m.errMsg = err.Error()
		} else {
			m.allEntries = entries
			m.entries = filterEntries(entries, m.filterQ)
			m.errMsg = ""
		}
		running, _ := m.store.Running()
		m.running = running
		if m.cursor >= len(m.entries) {
			m.cursor = maxInt(0, len(m.entries)-1)
		}
		return m, nil

	case dayEntriesMsg:
		m.allEntries = msg.entries
		m.entries = filterEntries(msg.entries, m.filterQ)
		if m.cursor >= len(m.entries) {
			m.cursor = maxInt(0, len(m.entries)-1)
		}
		return m, nil

	case heatLoadedMsg:
		m.heatData = msg.data
		m.heatLoaded = true
		return m, nil

	case weekLoadedMsg:
		m.weekSummaries = msg.summaries
		return m, nil

	case statsLoadedMsg:
		m.statsText = msg.text
		return m, nil

	case taskPickMsg:
		m.taskList = msg.tasks
		m.taskCursor = 0
		return m, nil

	case errMsg:
		m.errMsg = msg.err.Error()
		return m, nil

	case tea.KeyMsg:
		if m.imode != modeNone {
			return m.handleInputKey(msg)
		}
		return m.handleNavKey(msg)
	}

	// Pass other messages (e.g. textinput blinking) through.
	if m.imode != modeNone {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
	return m, nil
}

// handleNavKey handles keys when not in input mode.
func (m model) handleNavKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Task picker gets its own key handling.
	if m.current == viewTaskPick {
		return m.handleTaskPickKey(msg)
	}

	// Help overlay: close keys return to whichever view it was opened from.
	if m.current == viewHelp {
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "q", "esc", "?":
			m.current = m.helpReturnTo
			return m, nil
		}
		var cmd tea.Cmd
		m.helpVP, cmd = m.helpVP.Update(msg)
		return m, cmd
	}

	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit

	case "q":
		if m.current != viewMain {
			m.current = viewMain
			return m, nil
		}
		return m, tea.Quit

	case "esc":
		if m.current != viewMain {
			m.current = viewMain
		} else if m.filterQ != "" {
			m.filterQ = ""
			m.entries = filterEntries(m.allEntries, "")
			m.cursor = 0
		}

	case "j", "down":
		if m.cursor < len(m.entries)-1 {
			m.cursor++
		}

	case "k", "up":
		if m.cursor > 0 {
			m.cursor--
		}

	case "w":
		m.current = viewWeek
		return m, m.cmdLoadWeek()

	case "v":
		m.current = viewStats
		return m, m.cmdLoadStats()

	case "left":
		if m.current == viewMain {
			if m.browseDate.IsZero() {
				m.browseDate = time.Now().Truncate(24 * time.Hour)
			}
			m.browseDate = m.browseDate.AddDate(0, 0, -1)
			return m, doRefresh(m.store)
		}

	case "t":
		if m.current == viewMain && !m.browseDate.IsZero() {
			m.browseDate = time.Time{}
			return m, doRefresh(m.store)
		}

	case "right":
		if m.current == viewMain && !m.browseDate.IsZero() {
			next := m.browseDate.AddDate(0, 0, 1)
			today := time.Now().Truncate(24 * time.Hour)
			if next.Before(today) {
				m.browseDate = next
			} else {
				m.browseDate = time.Time{} // back to today
			}
			return m, doRefresh(m.store)
		}

	case "c":
		if m.current == viewMain && len(m.entries) > 0 {
			e := m.entries[m.cursor]
			val := e.Task
			if e.Project != "" {
				val = e.Task + "@" + e.Project
			}
			m.imode = modeNewTask
			m.input.Placeholder = "Task  (or task@project)"
			m.input.SetValue(val)
			m.input.CursorEnd()
			m.input.Focus()
		}

	case "r":
		if m.current == viewMain && len(m.entries) > 0 {
			e := m.entries[m.cursor]
			return m, m.cmdStart(e.Task, e.Project)
		}

	case "n":
		if m.current == viewMain {
			m.imode = modeNewTask
			m.input.Placeholder = "Task  (or task@project)"
			m.input.SetValue("")
			m.input.Focus()
		}

	case "s":
		return m, m.cmdStop()

	case "d":
		if m.current == viewMain && len(m.entries) > 0 {
			m.imode = modeConfirmDelete
			m.input.Placeholder = "y to confirm, n to cancel"
			m.input.SetValue("")
			m.input.Focus()
		}

	case "e":
		if m.current == viewMain && len(m.entries) > 0 {
			m.imode = modeEditNotes
			m.input.Placeholder = "Notes..."
			m.input.SetValue(m.entries[m.cursor].Notes)
			m.input.Focus()
		}

	case "T":
		m.current = viewTaskPick
		m.taskList = nil
		m.taskCursor = 0
		return m, m.cmdLoadTasks()

	case "/":
		if m.current == viewMain {
			m.imode = modeFilter
			m.input.Placeholder = "filter by task, project, notes…"
			m.input.SetValue(m.filterQ)
			m.input.CursorEnd()
			m.input.Focus()
		}

	case "?":
		m = m.openHelp()
	}

	return m, nil
}

// handleTaskPickKey handles keys in the task picker view.
func (m model) handleTaskPickKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc", "q":
		m.current = viewMain
		return m, nil
	case "j", "down":
		if m.taskCursor < len(m.taskList)-1 {
			m.taskCursor++
		}
	case "k", "up":
		if m.taskCursor > 0 {
			m.taskCursor--
		}
	case "enter":
		if len(m.taskList) > 0 {
			task := m.taskList[m.taskCursor]
			m.current = viewMain
			return m, m.cmdStartLinked(task, "", task)
		}
	}
	return m, nil
}

// handleInputKey handles keys while in an input prompt.
func (m model) handleInputKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Filter mode filters live while typing.
	if m.imode == modeFilter {
		switch msg.String() {
		case "esc":
			m.imode = modeNone
			m.input.Blur()
			m.filterQ = ""
			m.entries = filterEntries(m.allEntries, "")
			m.cursor = 0
			return m, nil
		case "enter":
			m.imode = modeNone
			m.input.Blur()
			return m, nil
		}
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		m.filterQ = strings.TrimSpace(m.input.Value())
		m.entries = filterEntries(m.allEntries, m.filterQ)
		m.cursor = 0
		return m, cmd
	}

	switch msg.String() {
	case "esc":
		m.imode = modeNone
		m.input.Blur()
		return m, nil

	case "enter":
		val := strings.TrimSpace(m.input.Value())
		mode := m.imode
		m.imode = modeNone
		m.input.Blur()

		switch mode {
		case modeNewTask:
			if val != "" {
				task, project := parseTaskInput(val)
				return m, m.cmdStart(task, project)
			}
		case modeConfirmDelete:
			if val == "y" && len(m.entries) > 0 {
				id := m.entries[m.cursor].ID
				return m, m.cmdDelete(id)
			}
		case modeEditNotes:
			if len(m.entries) > 0 {
				id := m.entries[m.cursor].ID
				return m, m.cmdSaveNotes(id, val)
			}
		}
		return m, nil
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// parseTaskInput splits "task@project" into task and project components.
func parseTaskInput(s string) (task, project string) {
	if idx := strings.LastIndex(s, "@"); idx > 0 {
		return strings.TrimSpace(s[:idx]), strings.TrimSpace(s[idx+1:])
	}
	return s, ""
}

// filterEntries returns entries matching q in task, project, or notes.
func filterEntries(entries []models.Entry, q string) []models.Entry {
	q = strings.ToLower(strings.TrimSpace(q))
	if q == "" {
		return entries
	}
	var out []models.Entry
	for _, e := range entries {
		if strings.Contains(strings.ToLower(e.Task), q) ||
			strings.Contains(strings.ToLower(e.Project), q) ||
			strings.Contains(strings.ToLower(e.Notes), q) {
			out = append(out, e)
		}
	}
	return out
}

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

func (m model) cmdStartLinked(task, project, linkedTask string) tea.Cmd {
	s := m.store
	return func() tea.Msg {
		_, err := s.StartLinked(task, project, linkedTask)
		if err != nil {
			return errMsg{err}
		}
		return refreshMsg{}
	}
}

// ── View ─────────────────────────────────────────────────────────────────────

func (m model) View() string {
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

func (m model) mainView() string {
	w, h := m.width, m.height
	if w < 40 {
		w = 80
	}
	if h < 20 {
		h = 24
	}

	heatW := 30
	rightW := w - heatW - 6
	if rightW < 20 {
		rightW = 20
	}

	left := panelStyle.Width(heatW).Height(h - 6).Render(m.renderHeatmap())
	right := panelStyle.Width(rightW).Height(h - 6).Render(m.renderToday(rightW, h-6))
	panels := lipgloss.JoinHorizontal(lipgloss.Top, left, "  ", right)

	var footer string
	switch {
	case m.imode != modeNone:
		footer = m.renderInput()
	case m.errMsg != "":
		footer = styleRed.Render("Error: " + m.errMsg)
	case m.filterQ != "":
		footer = styleAmber.Render("filter: /"+m.filterQ) + styleFooter.Render("  ·  esc clear · ? help")
	default:
		footer = styleFooter.Render("n start · T tasks · s stop · e notes · d delete · / filter · ←/→/t day · w week · v stats · ? help · q")
	}

	return lipgloss.JoinVertical(lipgloss.Left,
		styleHeader.Render("timectl — today"),
		panels,
		footer,
	)
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
		return lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "30", Dark: "23"}).Render(b)
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
		lines = append(lines, "", styleMuted.Render("  Press n to start a timer."))
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

	// Row layout (manual 1-space padding on each side):
	// contentW = width - 2
	// fixed = indicator(2) + time(5) + sep(2) + [bar](14) + sep(2) + dur(9) = 34
	// taskW = contentW - 34
	contentW := width - 2
	barW := 12
	fixed := 2 + 5 + 2 + barW + 2 + 2 + 9
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
		if len(taskDisplay) > taskW {
			taskDisplay = taskDisplay[:taskW-1] + "…"
		}
		task := taskDisplay

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

		if i == m.cursor {
			// Selected: plain text row so styleSelected fills correctly.
			rowPlain := fmt.Sprintf("%-2s%s  %-*s  [%s]  %-9s",
				func() string {
					if e.IsRunning() {
						return "▶ "
					}
					return "  "
				}(),
				startStr, taskW, task, barPlain, durStr)
			lines = append(lines, styleSelected.Width(contentW).Render(rowPlain))
		} else {
			// Styled: build with concatenation to avoid styleNormal wrapping ANSI.
			row := " " + indicator + startStr + "  " +
				styleMuted.Render(fmt.Sprintf("%-*s", taskW, task)) +
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

func (m model) weekView() string {
	var b strings.Builder

	b.WriteString(styleHeader.Render("timectl — this week") + "\n\n")

	if len(m.weekSummaries) == 0 {
		b.WriteString(styleMuted.Render("  No data yet.") + "\n")
	} else {
		var maxH float64
		for _, ds := range m.weekSummaries {
			h := ds.Total.Hours()
			if h > maxH {
				maxH = h
			}
		}
		if maxH < 1 {
			maxH = 1
		}

		var weekTotal time.Duration
		const barWidth = 24

		for _, ds := range m.weekSummaries {
			weekTotal += ds.Total
			label := ds.Date.Format("Mon 01/02")
			hours := ds.Total.Hours()
			filled := int(hours / maxH * barWidth)
			if hours > 0 && filled == 0 {
				filled = 1
			}
			bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)
			dur := models.FormatDuration(ds.Total)

			line := fmt.Sprintf("  %s  %s  %s",
				label,
				styleCyan.Render(bar),
				styleBlue.Render(dur),
			)
			b.WriteString(line + "\n")
		}

		b.WriteString(styleDivider.Render("  "+strings.Repeat("─", 55)) + "\n")
		b.WriteString(styleBlue.Render(fmt.Sprintf("  Total: %s", models.FormatDuration(weekTotal))) + "\n")
	}

	b.WriteString("\n" + styleFooter.Render("  esc/q back") + "\n")
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
	b.WriteString("\n" + styleFooter.Render("  esc/q back") + "\n")
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
		for i, title := range m.taskList {
			if i == m.taskCursor {
				b.WriteString(styleSelected.Render("  "+title) + "\n")
			} else {
				b.WriteString("  " + styleMuted.Render(title) + "\n")
			}
		}
	}

	b.WriteString("\n" + styleFooter.Render("  j/k navigate · enter start timer · esc/q back") + "\n")
	return b.String()
}

func (m model) helpContent() string {
	row := func(k, desc string) string {
		return "  " + styleBlue.Render(fmt.Sprintf("%-10s", k)) + styleMuted.Render(desc) + "\n"
	}
	section := func(t string) string { return "\n  " + styleAmber.Render(t) + "\n" }

	var b strings.Builder
	b.WriteString(section("Timer"))
	b.WriteString(row("n", "start new timer (task@project)"))
	b.WriteString(row("T", "task picker — start timer from open taskctl task"))
	b.WriteString(row("s", "stop running timer"))
	b.WriteString(row("r", "restart selected entry's task"))
	b.WriteString(row("c", "copy selected entry into new-task input"))
	b.WriteString(section("Entries"))
	b.WriteString(row("j / k", "move selection"))
	b.WriteString(row("e", "edit notes"))
	b.WriteString(row("d", "delete entry (asks to confirm)"))
	b.WriteString(row("/", "filter by task, project, notes (esc clears)"))
	b.WriteString(section("Views"))
	b.WriteString(row("← / →", "browse previous / next day"))
	b.WriteString(row("t", "back to today"))
	b.WriteString(row("w", "week breakdown"))
	b.WriteString(row("v", "stats (top tasks, streak, earnings)"))
	b.WriteString(section("Other"))
	b.WriteString(row("?", "toggle this help"))
	b.WriteString(row("q", "quit / back"))
	return b.String()
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

	vp := viewport.New(popW-6, popH-5) // border 1+1, padding(1,2) → 2 rows/4 cols; -1 row for footer
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
	if m.helpVP.TotalLineCount() > m.helpVP.Height {
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

	streak := computeStreak(daySet)
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
	for i := range pairs {
		for j := i + 1; j < len(pairs); j++ {
			if pairs[j].v > pairs[i].v {
				pairs[i], pairs[j] = pairs[j], pairs[i]
			}
		}
	}
	if n > len(pairs) {
		n = len(pairs)
	}
	return pairs[:n]
}

func computeStreak(daySet map[string]bool) int {
	streak := 0
	now := time.Now()
	for {
		day := now.Format("2006-01-02")
		if !daySet[day] {
			break
		}
		streak++
		now = now.AddDate(0, 0, -1)
	}
	return streak
}

// ── Run ───────────────────────────────────────────────────────────────────────

// Run starts the TUI.
func Run(s *store.Store) error {
	m := newModel(s)
	p := tea.NewProgram(
		m,
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)
	_, err := p.Run()
	return err
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
