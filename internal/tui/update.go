package tui

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/aeon022/missionctl-core/palette"
	"github.com/aeon022/timectl/internal/models"
	"github.com/aeon022/timectl/internal/store"
	"github.com/sahilm/fuzzy"
)

// ── Input mode ────────────────────────────────────────────────────────────────

type inputMode int

const (
	modeNone inputMode = iota
	modeNewTask
	modeConfirmDelete
	modeEditNotes
	modeFilter
	modeCommand
)

// ── Init ─────────────────────────────────────────────────────────────────────

func (m model) Init() tea.Cmd {
	return tea.Batch(doRefresh(m.store), tick(), cmdLoadHeat(m.store), idleCheckTick())
}

func tick() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func doRefresh(s *store.Store) tea.Cmd {
	return func() tea.Msg { return refreshMsg{} }
}

// idleCheckMsg fires every idleCheckInterval — separate from the 1s tickMsg
// so a subprocess (ioreg) isn't spawned every second, only every 30s.
type idleCheckMsg struct{}

const idleCheckInterval = 30 * time.Second

func idleCheckTick() tea.Cmd {
	return tea.Tick(idleCheckInterval, func(t time.Time) tea.Msg {
		return idleCheckMsg{}
	})
}

// idleThreshold is how long with no keyboard/mouse input system-wide
// before a running timer auto-stops. Overridable via TIMECTL_IDLE_MINUTES,
// same env-var convention as TIMECTL_HOURLY_RATE/TIMECTL_GOAL_HOURS.
func idleThreshold() time.Duration {
	if v := os.Getenv("TIMECTL_IDLE_MINUTES"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 {
			return time.Duration(f * float64(time.Minute))
		}
	}
	return 10 * time.Minute
}

// systemIdleSeconds returns how long it's been since the last keyboard or
// mouse input anywhere on the system (not just this terminal) — the
// standard `ioreg` HIDIdleTime technique, since a terminal app only sees
// input when it's the focused window, not system-wide idleness.
func systemIdleSeconds() (float64, error) {
	out, err := exec.Command("ioreg", "-c", "IOHIDSystem").Output()
	if err != nil {
		return 0, err
	}
	for _, line := range strings.Split(string(out), "\n") {
		if !strings.Contains(line, "HIDIdleTime") {
			continue
		}
		parts := strings.Split(line, "=")
		if len(parts) != 2 {
			continue
		}
		ns, err := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
		if err != nil {
			return 0, err
		}
		return ns / 1e9, nil
	}
	return 0, fmt.Errorf("HIDIdleTime not found in ioreg output")
}

// cmdAutoStopIdle stops the running timer with a note, for when idle
// detection fires — same store call "s" (cmdStop) uses, just with a note
// explaining why it stopped without the user pressing anything.
func cmdAutoStopIdle(s *store.Store) tea.Cmd {
	return func() tea.Msg {
		_, err := s.Stop("auto-stopped (idle)")
		if err != nil {
			return errMsg{err}
		}
		return idleAutoStoppedMsg{}
	}
}

type idleAutoStoppedMsg struct{}

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
		// -1, not msg.Height: reserves one row of slack so View() output
		// never lands on exactly the terminal's real height with no
		// trailing newline — a long-standing bubbletea v1 quirk
		// (charmbracelet/bubbletea#304) that can fail to fully redraw.
		m.height = msg.Height - 1
		if m.height < 1 {
			m.height = 1
		}
		return m, nil

	case tickMsg:
		running, _ := m.store.Running()
		m.running = running
		m.animStep++
		return m, tick()

	case idleCheckMsg:
		if m.running == nil {
			return m, idleCheckTick()
		}
		idleSecs, err := systemIdleSeconds()
		if err != nil || time.Duration(idleSecs*float64(time.Second)) < idleThreshold() {
			return m, idleCheckTick()
		}
		return m, tea.Batch(cmdAutoStopIdle(m.store), idleCheckTick())

	case idleAutoStoppedMsg:
		m.statusMsg = fmt.Sprintf("Timer auto-stopped — idle %s", models.FormatDuration(idleThreshold()))
		m.statusTime = time.Now()
		return m, doRefresh(m.store)

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
			m.cursor = max(0, len(m.entries)-1)
		}
		return m, nil

	case dayEntriesMsg:
		m.allEntries = msg.entries
		m.entries = filterEntries(msg.entries, m.filterQ)
		if m.cursor >= len(m.entries) {
			m.cursor = max(0, len(m.entries)-1)
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

	case tea.MouseWheelMsg:
		switch msg.Button {
		case tea.MouseWheelUp:
			if m.current == viewMain && m.cursor > 0 {
				m.cursor--
			}
		case tea.MouseWheelDown:
			if m.current == viewMain && m.cursor < len(m.entries)-1 {
				m.cursor++
			}
		}
		return m, nil

	case tea.MouseClickMsg:
		if msg.Button != tea.MouseLeft || m.current != viewMain {
			return m, nil
		}
		if i := m.rowHitTest(msg.X, msg.Y); i >= 0 {
			m.cursor = i
		}
		return m, nil

	case tea.MouseMotionMsg:
		if m.current == viewMain {
			m.hoverRow = m.rowHitTest(msg.X, msg.Y)
		}
		return m, nil

	case tea.KeyPressMsg:
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
func (m model) handleNavKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	// The delete-undo toast gets the longer undoWindow instead of the
	// usual 3s — it's also the window "u" checks below, so the message
	// and the capability it describes expire together.
	clearAfter := 3 * time.Second
	if m.lastDeleted != nil {
		clearAfter = undoWindow
	}
	if time.Since(m.statusTime) > clearAfter {
		m.statusMsg = ""
		m.lastDeleted = nil
	}

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

	case "1", "2", "3", "4", "5", "6", "7", "8", "9":
		// jump to the nth entry — no scroll window to account for here,
		// today's entry list renders in full (see rowHitTest's comment).
		n := int(msg.String()[0] - '0')
		if n <= len(m.entries) {
			m.cursor = n - 1
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

	case "u":
		if m.lastDeleted != nil {
			e := m.lastDeleted
			m.lastDeleted = nil
			m.statusMsg = ""
			return m, m.cmdRestore(*e)
		}

	case "y":
		if m.current == viewMain && len(m.entries) > 0 {
			m.statusMsg = "Copied to clipboard"
			m.statusTime = time.Now()
			return m, copyToClipboardCmd(m.entries[m.cursor].Task)
		}

	case "g":
		if m.current == viewMain && len(m.entries) > 0 {
			id := m.entries[m.cursor].LinkedTaskID
			if id == "" {
				break
			}
			return m, tea.ExecProcess(exec.Command("taskctl", "--task", id), func(err error) tea.Msg {
				if err != nil {
					return errMsg{err}
				}
				return refreshMsg{}
			})
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

	case ":":
		if m.current == viewMain {
			m.imode = modeCommand
			m.paletteCursor = 0
			m.input.Placeholder = "command…"
			m.input.SetValue("")
			m.input.Focus()
		}

	case "?":
		m = m.openHelp()
	}

	return m, nil
}

// handleTaskPickKey handles keys in the task picker view.
func (m model) handleTaskPickKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
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
			return m, m.cmdStartLinked(task.Title, "", task.Title, task.ID)
		}
	}
	return m, nil
}

// handleInputKey handles keys while in an input prompt.
func (m model) handleInputKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.imode == modeCommand {
		closePalette := func(mm model) model {
			mm.imode = modeNone
			mm.input.Blur()
			mm.input.SetValue("")
			mm.paletteCursor = 0
			return mm
		}
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "esc":
			return closePalette(m), nil
		case "up", "ctrl+p":
			if m.paletteCursor > 0 {
				m.paletteCursor--
			}
			return m, nil
		case "down", "ctrl+n":
			matches := palette.Match(paletteCommands, m.input.Value())
			if m.paletteCursor < len(matches)-1 {
				m.paletteCursor++
			}
			return m, nil
		case "enter":
			matches := palette.Match(paletteCommands, m.input.Value())
			if len(matches) == 0 {
				return closePalette(m), nil
			}
			if m.paletteCursor >= len(matches) {
				m.paletteCursor = len(matches) - 1
			}
			chosen := matches[m.paletteCursor]
			m = closePalette(m)
			replay := tea.KeyPressMsg{Text: chosen.Key, Code: []rune(chosen.Key)[0]}
			return m.handleNavKey(replay)
		}
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		m.paletteCursor = 0
		return m, cmd
	}

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
				e := m.entries[m.cursor]
				m.lastDeleted = &e
				m.statusMsg = fmt.Sprintf("Deleted %q — press u to undo", e.Task)
				m.statusTime = time.Now()
				return m, m.cmdDelete(e.ID)
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

// filterEntries fuzzy-matches q against entry task names (ranked
// best-match-first, like fzf/k9s), falling back to a plain substring match
// on project/notes for entries the task fuzzy-match missed — someone might
// search by project or a note fragment rather than the task name itself.
func filterEntries(entries []models.Entry, q string) []models.Entry {
	q = strings.TrimSpace(q)
	if q == "" {
		return entries
	}

	tasks := make([]string, len(entries))
	for i, e := range entries {
		tasks[i] = e.Task
	}
	matches := fuzzy.Find(q, tasks)

	out := make([]models.Entry, 0, len(matches))
	matched := make(map[int]bool, len(matches))
	for _, mt := range matches {
		out = append(out, entries[mt.Index])
		matched[mt.Index] = true
	}

	ql := strings.ToLower(q)
	for i, e := range entries {
		if matched[i] {
			continue
		}
		if strings.Contains(strings.ToLower(e.Project), ql) || strings.Contains(strings.ToLower(e.Notes), ql) {
			out = append(out, e)
		}
	}
	return out
}

// fuzzyMatchIndexes returns the rune indexes within s that q fuzzy-matched,
// or nil if q is empty or doesn't match at all.
func fuzzyMatchIndexes(q, s string) []int {
	if q == "" {
		return nil
	}
	matches := fuzzy.Find(q, []string{s})
	if len(matches) == 0 {
		return nil
	}
	return matches[0].MatchedIndexes
}

// highlightMatches renders s with the rune positions in idxs (from
// fuzzyMatchIndexes) styled via a warm, underlined variant of base, and
// every other character via base itself — fzf-style match highlighting.
//
// Renders one character at a time rather than nesting a highlighted span
// inside a single outer Render() call: lipgloss's Render() ends every
// string with a full SGR reset, so an inner Render() call's reset would
// wipe out the outer style for everything after the first highlighted
// character. Per-character rendering keeps every segment self-contained.
//
// idxs are indexes into s BEFORE any truncation — callers must resolve
// indexes against the same, untruncated string used to compute them.
func highlightMatches(s string, idxs []int, base lipgloss.Style) string {
	if len(idxs) == 0 {
		return base.Render(s)
	}
	hi := base.Foreground(colorAmber).Underline(true)
	matchSet := make(map[int]bool, len(idxs))
	for _, i := range idxs {
		matchSet[i] = true
	}
	var b strings.Builder
	for i, r := range []rune(s) {
		if matchSet[i] {
			b.WriteString(hi.Render(string(r)))
		} else {
			b.WriteString(base.Render(string(r)))
		}
	}
	return b.String()
}
