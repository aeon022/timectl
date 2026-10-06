package store

import (
	"testing"
	"time"

	"github.com/aeon022/missionctl-core/activity"
)

func TestStartStopAreLogged(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MISSIONCTL_DATA_DIR", t.TempDir())
	t.Setenv("MISSIONCTL_ACTIVITY", "")
	s := testStore(t)

	if _, err := s.Start("Angebot schreiben", "kunde-x"); err != nil {
		t.Fatal(err)
	}
	// a refused second start (timer already running) is not an action
	if _, err := s.Start("Anderes", ""); err == nil {
		t.Fatal("second start should fail")
	}
	if _, err := s.Stop("notes must never reach the log"); err != nil {
		t.Fatal(err)
	}
	// stopping with nothing running fails and logs nothing
	if _, err := s.Stop(""); err == nil {
		t.Fatal("stop without a running timer should fail")
	}

	evs, err := activity.Read(activity.Day(time.Now()))
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 2 {
		t.Fatalf("want started + stopped, got %+v", evs)
	}
	if evs[0].Tool != "timectl" || evs[0].Action != "started" || evs[0].Title != "Angebot schreiben" {
		t.Errorf("start = %+v", evs[0])
	}
	if evs[1].Action != "stopped" || evs[1].Title != "Angebot schreiben" {
		t.Errorf("stop = %+v", evs[1])
	}
}

func TestStartStopWorkWhenActivityIsOff(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MISSIONCTL_DATA_DIR", t.TempDir())
	t.Setenv("MISSIONCTL_ACTIVITY", "off")
	s := testStore(t)
	if _, err := s.Start("X", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Stop(""); err != nil {
		t.Fatal(err)
	}
	if evs, _ := activity.Read(activity.Day(time.Now())); len(evs) != 0 {
		t.Errorf("logging off, got %+v", evs)
	}
}
