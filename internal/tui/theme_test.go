package tui

import (
	"strings"
	"testing"

	"github.com/aeon022/missionctl-core/theme"
	"github.com/aeon022/missionctl-core/tuitest"
)

func TestThemeKeySwitchesLiveAndSaves(t *testing.T) {
	m := testModel(t, false)
	orig := theme.Current()
	t.Cleanup(func() { _ = theme.Apply(orig) })

	before := styleHeader.GetForeground()
	mm, _ := tuitest.Send(m, tuitest.Key("C"))

	if theme.Current() == orig {
		t.Fatalf("C must switch to the next theme, still %q", orig)
	}
	if styleHeader.GetForeground() == before {
		t.Error("cached styles must be rebuilt when the theme changes")
	}
	if got := mm.(model).statusMsg; got != "Theme: "+theme.Current() {
		t.Errorf("a short confirmation should name the theme, got %q", got)
	}
}

func TestEmptyDayShowsOnboarding(t *testing.T) {
	m := testModel(t, false)
	if out := tuitest.Text(m); !strings.Contains(out, "Welcome to timectl") || !strings.Contains(out, "n   start your first timer") {
		t.Errorf("empty day should show the checklist:\n%s", out)
	}
}
