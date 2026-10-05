package tui

import (
	"os/exec"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/aeon022/timectl/internal/store"
)

// ── Run ───────────────────────────────────────────────────────────────────────

// Run starts the TUI.
func Run(s *store.Store) error {
	m := newModel(s)
	// WithFPS(30) + motionThrottleFilter: all-motion mouse mode re-renders on every
	// pixel of movement, which at the default 60fps can overwhelm the terminal
	// (duplicate-content corruption seen in notectl/mailctl).
	p := tea.NewProgram(m, tea.WithFilter(motionThrottleFilter()), tea.WithFPS(30))
	_, err := p.Run()
	return err
}

// copyToClipboardCmd copies via OSC 52 (works over SSH/tmux) and also shells
// out to pbcopy — Terminal.app ignores OSC 52 — same approach taskctl/mailctl/
// notectl/calctl/habctl use for their own "y" copy shortcuts, no clipboard
// library needed.
func copyToClipboardCmd(text string) tea.Cmd {
	return tea.Batch(tea.SetClipboard(text), func() tea.Msg {
		cmd := exec.Command("pbcopy")
		cmd.Stdin = strings.NewReader(text)
		_ = cmd.Run()
		return nil
	})
}
