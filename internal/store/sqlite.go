package store

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/aeon022/timectl/internal/models"
	_ "modernc.org/sqlite"
)

const timeLayout = time.RFC3339Nano

// Store wraps the SQLite database.
type Store struct {
	db *sql.DB
}

// DefaultPath returns the canonical path to the database file.
func DefaultPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share", "timectl", "time.db"), nil
}

// Open opens (or creates) the database at path.
func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	s := &Store{db: db}
	if err := s.init(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

// Close releases the database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) init() error {
	pragmas := []string{
		"PRAGMA journal_mode=WAL;",
		"PRAGMA foreign_keys=ON;",
		"PRAGMA synchronous=NORMAL;",
	}
	for _, p := range pragmas {
		if _, err := s.db.Exec(p); err != nil {
			return fmt.Errorf("pragma: %w", err)
		}
	}

	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS entries (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			task        TEXT NOT NULL,
			project     TEXT NOT NULL DEFAULT '',
			started_at  TEXT NOT NULL,
			stopped_at  TEXT,
			notes       TEXT NOT NULL DEFAULT '',
			created_at  TEXT NOT NULL DEFAULT (datetime('now'))
		);
	`)
	return err
}

// Start inserts a new running entry. Returns an error if one is already running.
func (s *Store) Start(task, project string) (models.Entry, error) {
	running, err := s.Running()
	if err != nil {
		return models.Entry{}, err
	}
	if running != nil {
		return models.Entry{}, fmt.Errorf("timer already running: %q (started %s)",
			running.Task, running.StartedAt.Format("15:04:05"))
	}

	now := time.Now()
	res, err := s.db.Exec(
		`INSERT INTO entries (task, project, started_at) VALUES (?, ?, ?)`,
		task, project, now.Format(timeLayout),
	)
	if err != nil {
		return models.Entry{}, fmt.Errorf("insert entry: %w", err)
	}

	id, _ := res.LastInsertId()
	return models.Entry{
		ID:        id,
		Task:      task,
		Project:   project,
		StartedAt: now,
	}, nil
}

// Stop sets stopped_at on the currently running entry.
func (s *Store) Stop(notes string) (models.Entry, error) {
	running, err := s.Running()
	if err != nil {
		return models.Entry{}, err
	}
	if running == nil {
		return models.Entry{}, errors.New("no timer is currently running")
	}

	now := time.Now()
	if notes != "" {
		_, err = s.db.Exec(
			`UPDATE entries SET stopped_at = ?, notes = ? WHERE id = ?`,
			now.Format(timeLayout), notes, running.ID,
		)
	} else {
		_, err = s.db.Exec(
			`UPDATE entries SET stopped_at = ? WHERE id = ?`,
			now.Format(timeLayout), running.ID,
		)
	}
	if err != nil {
		return models.Entry{}, fmt.Errorf("stop entry: %w", err)
	}

	running.StoppedAt = &now
	running.Duration = now.Sub(running.StartedAt)
	if notes != "" {
		running.Notes = notes
	}
	return *running, nil
}

// Running returns the currently active entry or nil.
func (s *Store) Running() (*models.Entry, error) {
	row := s.db.QueryRow(
		`SELECT id, task, project, started_at, notes FROM entries WHERE stopped_at IS NULL LIMIT 1`,
	)
	var e models.Entry
	var startedStr string
	err := row.Scan(&e.ID, &e.Task, &e.Project, &startedStr, &e.Notes)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	t, err := time.Parse(timeLayout, startedStr)
	if err != nil {
		return nil, fmt.Errorf("parse started_at: %w", err)
	}
	e.StartedAt = t
	return &e, nil
}

// Today returns entries started today (local time).
func (s *Store) Today() ([]models.Entry, error) {
	now := time.Now()
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	end := start.Add(24 * time.Hour)
	return s.Range(start, end)
}

// Week returns entries from Monday of the current week through Sunday.
func (s *Store) Week() ([]models.Entry, error) {
	now := time.Now()
	weekday := int(now.Weekday())
	if weekday == 0 {
		weekday = 7
	}
	monday := now.AddDate(0, 0, -(weekday - 1))
	start := time.Date(monday.Year(), monday.Month(), monday.Day(), 0, 0, 0, 0, now.Location())
	end := start.AddDate(0, 0, 7)
	return s.Range(start, end)
}

// Range returns entries whose started_at falls within [from, to).
func (s *Store) Range(from, to time.Time) ([]models.Entry, error) {
	rows, err := s.db.Query(
		`SELECT id, task, project, started_at, stopped_at, notes
		   FROM entries
		  WHERE started_at >= ? AND started_at < ?
		  ORDER BY started_at ASC`,
		from.Format(timeLayout),
		to.Format(timeLayout),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEntries(rows)
}

// Delete removes an entry by ID.
func (s *Store) Delete(id int64) error {
	res, err := s.db.Exec(`DELETE FROM entries WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("entry %d not found", id)
	}
	return nil
}

// DaySummary aggregates entries for the given date.
func (s *Store) DaySummary(date time.Time) (models.DaySummary, error) {
	start := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, date.Location())
	end := start.Add(24 * time.Hour)

	entries, err := s.Range(start, end)
	if err != nil {
		return models.DaySummary{}, err
	}

	summary := models.DaySummary{
		Date:    start,
		Entries: entries,
		ByTask:  make(map[string]time.Duration),
	}
	for _, e := range entries {
		d := e.ComputedDuration()
		summary.Total += d
		summary.ByTask[e.Task] += d
	}
	return summary, nil
}

// WeekSummary returns a DaySummary for each day Mon–Sun of the current week.
func (s *Store) WeekSummary() ([]models.DaySummary, error) {
	now := time.Now()
	weekday := int(now.Weekday())
	if weekday == 0 {
		weekday = 7
	}
	monday := now.AddDate(0, 0, -(weekday - 1))
	start := time.Date(monday.Year(), monday.Month(), monday.Day(), 0, 0, 0, 0, now.Location())

	summaries := make([]models.DaySummary, 7)
	for i := range summaries {
		day := start.AddDate(0, 0, i)
		ds, err := s.DaySummary(day)
		if err != nil {
			return nil, err
		}
		summaries[i] = ds
	}
	return summaries, nil
}

// UpdateNotes sets the notes field of an entry.
func (s *Store) UpdateNotes(id int64, notes string) error {
	_, err := s.db.Exec(`UPDATE entries SET notes = ? WHERE id = ?`, notes, id)
	return err
}

// RecentDays returns entries from the last n days.
func (s *Store) RecentDays(n int) ([]models.Entry, error) {
	now := time.Now()
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	start = start.AddDate(0, 0, -n+1)
	end := time.Now().Add(time.Minute)
	return s.Range(start, end)
}

// FilteredRange returns entries in [from,to) optionally filtered by project.
func (s *Store) FilteredRange(from, to time.Time, project string) ([]models.Entry, error) {
	if project == "" {
		return s.Range(from, to)
	}
	rows, err := s.db.Query(
		`SELECT id, task, project, started_at, stopped_at, notes
		   FROM entries
		  WHERE started_at >= ? AND started_at < ? AND project = ?
		  ORDER BY started_at ASC`,
		from.Format(timeLayout),
		to.Format(timeLayout),
		project,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEntries(rows)
}

func scanEntries(rows *sql.Rows) ([]models.Entry, error) {
	var entries []models.Entry
	for rows.Next() {
		var e models.Entry
		var startedStr string
		var stoppedStr sql.NullString
		if err := rows.Scan(&e.ID, &e.Task, &e.Project, &startedStr, &stoppedStr, &e.Notes); err != nil {
			return nil, err
		}
		t, err := time.Parse(timeLayout, startedStr)
		if err != nil {
			return nil, fmt.Errorf("parse started_at: %w", err)
		}
		e.StartedAt = t
		if stoppedStr.Valid {
			st, err := time.Parse(timeLayout, stoppedStr.String)
			if err != nil {
				return nil, fmt.Errorf("parse stopped_at: %w", err)
			}
			e.StoppedAt = &st
			e.Duration = st.Sub(t)
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}
