package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/aeon022/timectl/internal/models"
	"github.com/aeon022/timectl/internal/store"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ── Design tokens ────────────────────────────────────────────────────────────

var (
	colorBlue   = lipgloss.AdaptiveColor{Light: "25", Dark: "33"}
	colorGreen  = lipgloss.AdaptiveColor{Light: "28", Dark: "42"}
	colorRed    = lipgloss.AdaptiveColor{Light: "160", Dark: "203"}
	colorAmber  = lipgloss.AdaptiveColor{Light: "214", Dark: "220"}
	colorMuted  = lipgloss.AdaptiveColor{Light: "243", Dark: "246"}
	colorSubtle = lipgloss.AdaptiveColor{Light: "250", Dark: "239"}
	selectedBg  = lipgloss.AdaptiveColor{Light: "189", Dark: "17"}
	selectedFg  = lipgloss.AdaptiveColor{Light: "16", Dark: "255"}
)

var (
	styleHeader = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorBlue).
			Padding(0, 1)

	styleRunning = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorGreen)

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
			BorderForeground(colorMuted).
			Padding(0, 1)
)

// ── Views ────────────────────────────────────────────────────────────────────

type viewKind int

const (
	viewMain viewKind = iota
	viewWeek
	viewStats
)

// ── Messages ─────────────────────────────────────────────────────────────────

type tickMsg time.Time
type refreshMsg struct{}
type errMsg struct{ err error }
type weekLoadedMsg struct{ summaries []models.DaySummary }
type statsLoadedMsg struct{ text string }
type heatLoadedMsg struct{ data []heatDay }

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
)

// ── model ────────────────────────────────────────────────────────────────────

type model struct {
	store   *store.Store
	width   int
	height  int
	current viewKind

	entries  []models.Entry
	running  *models.Entry
	cursor   int
	imode    inputMode
	input    textinput.Model
	errMsg   string

	weekSummaries []models.DaySummary
	statsText     string

	heatData   []heatDay
	heatLoaded bool
	animStep   int
}

func newModel(s *store.Store) model {
	ti := textinput.New()
	ti.CharLimit = 200
	ti.Width = 50
	return model{
		store: s,
		input: ti,
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
		entries, err := m.store.Today()
		if err != nil {
			m.errMsg = err.Error()
		} else {
			m.entries = entries
			m.errMsg = ""
		}
		running, _ := m.store.Running()
		m.running = running
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

	case "n":
		if m.current == viewMain {
			m.imode = modeNewTask
			m.input.Placeholder = "Task name..."
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
	}

	return m, nil
}

// handleInputKey handles keys while in an input prompt.
func (m model) handleInputKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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
				return m, m.cmdStart(val, "")
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
	return func() tea.Msg {
		text, err := buildStatsText(s)
		if err != nil {
			return errMsg{err}
		}
		return statsLoadedMsg{text}
	}
}

// ── View ─────────────────────────────────────────────────────────────────────

func (m model) View() string {
	switch m.current {
	case viewWeek:
		return m.weekView()
	case viewStats:
		return m.statsView()
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
	default:
		footer = styleFooter.Render("n start · s stop · e notes · d delete · j/k · w week · v stats · q quit")
	}

	return lipgloss.JoinVertical(lipgloss.Left,
		styleHeader.Render("timectl"),
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
		lines = append(lines, styleGreen.Render(models.FormatDuration(todayTotal)))
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
		return lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "71", Dark: "22"}).Render(b)
	case h < 4:
		return styleGreen.Render(b)
	default:
		return styleGreen.Bold(true).Render(b)
	}
}

func (m model) renderToday(width, height int) string {
	var lines []string

	// Header: running timer or idle.
	if m.running != nil {
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
	} else {
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

		// Task name — truncate and pad.
		task := e.Task
		if len(task) > taskW {
			task = task[:taskW-1] + "…"
		}

		// Duration bar.
		filled := int(float64(d) / float64(maxDur) * float64(barW))
		if d > 0 && filled == 0 {
			filled = 1
		}
		barPlain := strings.Repeat("█", filled) + strings.Repeat("░", barW-filled)
		barStyled := styleGreen.Render(barPlain)

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
				lipgloss.NewStyle().Foreground(colorGreen).Render(bar),
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

// ── Stats builder ─────────────────────────────────────────────────────────────

func buildStatsText(s *store.Store) (string, error) {
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
