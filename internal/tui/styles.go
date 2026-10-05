package tui

import (
	"image/color"
	"os"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/aeon022/missionctl-core/palette"
	"github.com/aeon022/missionctl-core/theme"
)

// ── Design tokens ────────────────────────────────────────────────────────────

// adaptive resolves a light/dark ANSI color pair once, at startup — v2 dropped
// AdaptiveColor, and these package-level styles are built once, not per render.
var adaptive = func() func(light, dark string) color.Color {
	pick := lipgloss.LightDark(lipgloss.HasDarkBackground(os.Stdin, os.Stdout))
	return func(light, dark string) color.Color { return pick(lipgloss.Color(light), lipgloss.Color(dark)) }
}()

var (
	// Shared across the suite via missionctl-core/theme.
	colorBlue   = theme.BlueV2
	colorCyan   = adaptive("30", "51")
	colorGreen  = theme.GreenV2
	colorRed    = theme.RedV2
	colorAmber  = theme.AmberV2
	colorMuted  = theme.MutedV2
	colorSubtle = theme.SubtleV2
	// selectedBg/selectedFg intentionally NOT shared — timectl's selected-row
	// color is a deliberately different shade from the suite default.
	selectedBg = adaptive("159", "23")
	selectedFg = adaptive("16", "255")
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

// ── Heat data ─────────────────────────────────────────────────────────────────

type heatDay struct {
	date  time.Time
	total time.Duration
}

// ── command palette (":") ────────────────────────────────────────────────────
//
// Types out full words instead of memorizing single-key shortcuts. Reuses
// the exact same key handling every shortcut already goes through
// (handleNavKey) by replaying the mapped keypress, so behavior is
// guaranteed identical to typing the key directly. Matching logic lives in
// missionctl-core/palette (shared across the suite); this list is
// timectl-specific.
var paletteCommands = []palette.Command{
	{Name: "new", Desc: "Start new timer (task@project)", Key: "n"},
	{Name: "taskpick", Desc: "Start timer from open taskctl task", Key: "T"},
	{Name: "stop", Desc: "Stop running timer", Key: "s"},
	{Name: "restart", Desc: "Restart selected entry's task", Key: "r"},
	{Name: "copy", Desc: "Copy selected entry into new-task input", Key: "c"},
	{Name: "edit", Desc: "Edit notes", Key: "e"},
	{Name: "delete", Desc: "Delete entry (asks to confirm)", Key: "d"},
	{Name: "undo", Desc: "Undo last delete", Key: "u"},
	{Name: "clipboard", Desc: "Copy task name to clipboard", Key: "y"},
	{Name: "goto", Desc: "Open linked taskctl task", Key: "g"},
	{Name: "today", Desc: "Back to today", Key: "t"},
	{Name: "week", Desc: "Week breakdown", Key: "w"},
	{Name: "stats", Desc: "Stats (top tasks, streak, earnings)", Key: "v"},
	{Name: "search", Desc: "Filter by task, project, notes", Key: "/"},
	{Name: "help", Desc: "Show help", Key: "?"},
	{Name: "quit", Desc: "Quit timectl", Key: "q"},
}
