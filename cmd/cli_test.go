package cmd

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/aeon022/timectl/internal/models"
)

// run executes the real cobra tree against a throwaway data dir and returns stdout.
func run(t *testing.T, args ...string) (string, error) {
	t.Helper()
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	rootCmd.SetArgs(args)
	rootCmd.SetOut(io.Discard)
	rootCmd.SetErr(io.Discard)
	err := rootCmd.Execute()
	w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	io.Copy(&buf, r)
	return buf.String(), err
}

func isolate(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("TIMECTL_DATA_DIR", t.TempDir())
	t.Setenv("MISSIONCTL_DATA_DIR", t.TempDir())
	t.Setenv("TIMECTL_HOURLY_RATE", "")
	// cobra keeps flag values between Execute calls
	todayJSON, invoiceMonth, invoiceRate, invoiceClient, invoiceOutput = false, "", 0, "", ""
}

func seed(t *testing.T, entries ...models.Entry) {
	t.Helper()
	s, err := openStore()
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, e := range entries {
		if err := s.Restore(e); err != nil {
			t.Fatal(err)
		}
	}
}

func entry(id int64, task, project, from string, minutes int) models.Entry {
	start, _ := time.ParseInLocation("2006-01-02 15:04", from, time.Local)
	stop := start.Add(time.Duration(minutes) * time.Minute)
	return models.Entry{ID: id, Task: task, Project: project, StartedAt: start, StoppedAt: &stop}
}

func TestStartStopTodayAndJSON(t *testing.T) {
	isolate(t)
	if o, _ := run(t, "today"); !strings.Contains(o, "No entries today") {
		t.Fatalf("empty today: %q", o)
	}
	if _, err := run(t, "start", "Write tests"); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, "stop"); err != nil {
		t.Fatal(err)
	}
	o, err := run(t, "today")
	if err != nil || !strings.Contains(o, "Write tests") || !strings.Contains(o, "Total:") {
		t.Fatalf("today table: %q %v", o, err)
	}
	todayJSON = true
	o, err = run(t, "today", "--json")
	var doc struct {
		Entries []map[string]any `json:"entries"`
		Total   string           `json:"total_human"`
	}
	if err != nil || json.Unmarshal([]byte(o), &doc) != nil || len(doc.Entries) != 1 || doc.Entries[0]["task"] != "Write tests" || doc.Total == "" {
		t.Fatalf("today json: %q %v", o, err)
	}
}

func TestInvoiceTotalsAndRate(t *testing.T) {
	isolate(t)
	seed(t,
		entry(1, "Design", "Acme", "2026-09-02 09:00", 90),
		entry(2, "Review", "Acme", "2026-09-03 10:00", 30),
		entry(3, "Other month", "Acme", "2026-08-31 10:00", 600), // must not be billed
	)
	o, err := run(t, "invoice", "--month", "2026-09", "--rate", "100", "--client", "Globex")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Globex", "Design", "Review", "$200.00", "2h"} {
		if !strings.Contains(o, want) {
			t.Errorf("invoice misses %q:\n%s", want, o)
		}
	}
	if strings.Contains(o, "Other month") {
		t.Errorf("an entry from another month was billed:\n%s", o)
	}
	if _, err := run(t, "invoice", "--month", "2026/09"); err == nil {
		t.Error("an invalid month must be rejected")
	}
}
