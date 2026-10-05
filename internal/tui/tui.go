package tui

import (
	"os"
	"strconv"
	"time"

	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	"github.com/aeon022/timectl/internal/models"
	"github.com/aeon022/timectl/internal/store"
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
type taskPickMsg struct{ tasks []store.OpenTask }

// ── model ────────────────────────────────────────────────────────────────────

type model struct {
	store   *store.Store
	width   int
	height  int
	current viewKind

	entries       []models.Entry // filtered view of allEntries
	allEntries    []models.Entry
	lastLoad      time.Time // when refreshMsg last reloaded the entries (focus reload staleness)
	filterQ       string
	running       *models.Entry
	cursor        int
	hoverRow      int // m.entries index under the mouse cursor, -1 when none
	imode         inputMode
	input         textinput.Model
	paletteCursor int // index into the filtered palette command matches, valid while imode == modeCommand
	errMsg        string
	statusMsg     string // confirmation text (e.g. "Copied to clipboard"), cleared 3s after statusTime on the next keypress — same lazy pattern budgetctl/mailctl/notectl/calctl use
	statusTime    time.Time

	// undo: "u" within undoWindow of a delete restores the deleted entry —
	// same pattern and window taskctl uses for its own delete-undo.
	// statusTime doubles as its expiry clock (see handleNavKey).
	lastDeleted *models.Entry

	weekSummaries []models.DaySummary
	statsText     string

	heatData   []heatDay
	heatLoaded bool
	animStep   int

	browseDate time.Time // zero = today
	goalHours  float64
	hourlyRate float64

	taskList   []store.OpenTask
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
	ti.SetWidth(50)
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
		hoverRow:   -1,
	}
}
