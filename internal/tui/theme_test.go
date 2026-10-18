package tui

import (
	"strings"
	"testing"

	"github.com/aeon022/missionctl-core/theme"
	"github.com/aeon022/missionctl-core/tuitest"
)

func TestThemeKeyOpensPickerWithLivePreview(t *testing.T) {
	m := testModel(t, false)
	orig := theme.Current()
	t.Cleanup(func() { _ = theme.Apply(orig) })

	before := styleHeader.GetForeground()
	mm, _ := tuitest.Send(m, tuitest.Key("C"))
	if !strings.Contains(tuitest.Text(mm), "Colour theme") {
		t.Fatalf("C opens the theme popup:\n%s", tuitest.Text(mm))
	}
	mm, _ = tuitest.Send(mm, tuitest.Key("j"))
	if theme.Current() == orig || styleHeader.GetForeground() == before {
		t.Error("moving in the popup previews the theme live and rebuilds the cached styles")
	}
	mm, _ = tuitest.Send(mm, tuitest.Key("enter"))
	if got := mm.(model).statusMsg; got != "Theme: "+theme.Current() {
		t.Errorf("enter confirms the theme, got %q", got)
	}
}

func TestEmptyDayShowsOnboarding(t *testing.T) {
	m := testModel(t, false)
	if out := tuitest.Text(m); !strings.Contains(out, "Welcome to timectl") || !strings.Contains(out, "n   start your first timer") {
		t.Errorf("empty day should show the checklist:\n%s", out)
	}
}
