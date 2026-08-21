package store

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	coreconfig "github.com/aeon022/missionctl-core/config"
	"github.com/aeon022/missionctl-core/syncdir"
	"github.com/aeon022/timectl/internal/models"
	_ "modernc.org/sqlite"
)

const timeLayout = time.RFC3339Nano

// Store wraps the SQLite database.
type Store struct {
	db   *sql.DB
	path string
}

// DefaultPath returns the canonical path to the database file.
func DefaultPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share", "timectl", "time.db"), nil
}

// ResolveDBPath returns the database file path, and whether it's a
// user-configured (possibly folder-synced) directory rather than the
// private default — set via the TIMECTL_DATA_DIR env var, e.g. pointing
// inside iCloud Drive or Dropbox. timectl has no config-file layer for
// this one setting, so an env var is the only override, deliberately
// minimal rather than introducing a whole new config file for it.
func ResolveDBPath() (path string, shared bool, err error) {
	if dir := os.Getenv("TIMECTL_DATA_DIR"); dir != "" {
		resolved, sh := coreconfig.ResolveDir("timectl", dir)
		return filepath.Join(resolved, "time.db"), sh, nil
	}
	p, err := DefaultPath()
	return p, false, err
}

// OpenDefault resolves the default (or TIMECTL_DATA_DIR-overridden) DB path
// and opens it — the shared open-path used by both the CLI and the MCP
// server.
func OpenDefault() (*Store, error) {
	path, shared, err := ResolveDBPath()
	if err != nil {
		return nil, fmt.Errorf("resolve db path: %w", err)
	}
	return Open(path, shared)
}

// timectl opens a fresh *Store per operation rather than holding one open
// for the process's lifetime, and flock(2) isn't reentrant within a
// process — locks reference-counts the real OS-level lock per path so the
// same process's own concurrent/sequential opens don't conflict with
// themselves; only the first open of a path acquires it for real, and only
// the last matching Close() releases it. A conflict is reported only when
// a genuinely different process holds it.
var (
	lockMu sync.Mutex
	locks  = map[string]*lockEntry{}
)

type lockEntry struct {
	lock  *syncdir.Lock
	count int
}

func acquireLock(path string) error {
	lockMu.Lock()
	defer lockMu.Unlock()
	e, ok := locks[path]
	if !ok {
		l, err := syncdir.Acquire(path)
		if err != nil {
			return err
		}
		e = &lockEntry{lock: l}
		locks[path] = e
	}
	e.count++
	return nil
}

func releaseLock(path string) {
	lockMu.Lock()
	defer lockMu.Unlock()
	e, ok := locks[path]
	if !ok {
		return
	}
	e.count--
	if e.count == 0 {
		e.lock.Release()
		delete(locks, path)
	}
}

// Open opens (or creates) the database at path. shared must reflect
// whether path is a user-configured (possibly folder-synced) directory
// rather than the private default — see ResolveDBPath.
func Open(path string, shared bool) (*Store, error) {
	if isPlaceholder, placeholder := syncdir.ICloudPlaceholder(path); isPlaceholder {
		return nil, fmt.Errorf("%s hasn't finished downloading from iCloud yet (found %s) — open Finder and download it, or disable \"Optimize Mac Storage\" for this folder", path, placeholder)
	}

	if err := acquireLock(path); err != nil {
		if errors.Is(err, syncdir.ErrLocked) {
			return nil, fmt.Errorf("timectl is already running elsewhere, or a previous session crashed — remove %s.lock if you're sure nothing else is using it", path)
		}
		return nil, err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		releaseLock(path)
		return nil, fmt.Errorf("create data dir: %w", err)
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		releaseLock(path)
		return nil, fmt.Errorf("open db: %w", err)
	}

	s := &Store{db: db, path: path}
	if err := s.init(shared); err != nil {
		_ = db.Close()
		releaseLock(path)
		return nil, err
	}
	return s, nil
}

// Close releases the database connection.
func (s *Store) Close() error {
	err := s.db.Close()
	releaseLock(s.path)
	return err
}

func (s *Store) init(shared bool) error {
	pragmas := []string{
		"PRAGMA journal_mode=" + syncdir.JournalMode(shared) + ";",
		"PRAGMA foreign_keys=ON;",
		"PRAGMA synchronous=NORMAL;",
		"PRAGMA busy_timeout=5000;",
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
	if err != nil {
		return err
	}

	// Migration: add linked_task/linked_task_id columns if they don't exist
	// yet. SQLite doesn't support IF NOT EXISTS for ALTER TABLE, so we
	// ignore the error.
	_, _ = s.db.Exec(`ALTER TABLE entries ADD COLUMN linked_task TEXT NOT NULL DEFAULT ''`)
	_, _ = s.db.Exec(`ALTER TABLE entries ADD COLUMN linked_task_id TEXT NOT NULL DEFAULT ''`)

	return nil
}

// Start inserts a new running entry. Returns an error if one is already running.
func (s *Store) Start(task, project string) (models.Entry, error) {
	return s.StartLinked(task, project, "", "")
}

// StartLinked inserts a new running entry optionally linked to a taskctl
// task — linkedTaskID is its stable taskctl id (empty if the entry isn't
// tied to a task, or the task picker's title-only path is used).
func (s *Store) StartLinked(task, project, linkedTask, linkedTaskID string) (models.Entry, error) {
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
		`INSERT INTO entries (task, project, started_at, linked_task, linked_task_id) VALUES (?, ?, ?, ?, ?)`,
		task, project, now.Format(timeLayout), linkedTask, linkedTaskID,
	)
	if err != nil {
		return models.Entry{}, fmt.Errorf("insert entry: %w", err)
	}

	id, _ := res.LastInsertId()
	return models.Entry{
		ID:           id,
		Task:         task,
		Project:      project,
		StartedAt:    now,
		LinkedTask:   linkedTask,
		LinkedTaskID: linkedTaskID,
	}, nil
}

// OpenTask is one open taskctl task, as offered by the "T" task-picker.
type OpenTask struct {
	ID    string
	Title string
}

// OpenTasks reads the taskctl database and returns open (needsAction)
// tasks, sorted by priority DESC, due_date ASC. Returns an empty slice (no
// error) if taskctl's DB isn't found, so timectl works standalone too.
func (s *Store) OpenTasks() ([]OpenTask, error) {
	// Matches taskctl's own config.DBPath(): the private default
	// (~/Library/Application Support/taskctl/taskctl.db) unless taskctl's
	// own TASKCTL_DATA_DIR env var is set, in which case its data moved
	// there instead. This mirrors taskctl's resolution rather than
	// importing taskctl's config package directly (separate Go modules),
	// so it only tracks a move made via TASKCTL_DATA_DIR — a move made
	// purely through taskctl's data_dir config-file key, with no env var
	// set, won't be picked up here.
	var dbPath string
	if dir := os.Getenv("TASKCTL_DATA_DIR"); dir != "" {
		resolved, _ := coreconfig.ResolveDir("taskctl", dir)
		dbPath = filepath.Join(resolved, "taskctl.db")
	} else {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, nil
		}
		dbPath = filepath.Join(home, "Library", "Application Support", "taskctl", "taskctl.db")
	}
	if _, err := os.Stat(dbPath); err != nil {
		return nil, nil
	}

	tdb, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, nil
	}
	defer tdb.Close()

	rows, err := tdb.Query(`
		SELECT id, title FROM tasks
		WHERE status = 'needsAction'
		ORDER BY priority DESC, due_date ASC
	`)
	if err != nil {
		return nil, nil
	}
	defer rows.Close()

	var tasks []OpenTask
	for rows.Next() {
		var t OpenTask
		if err := rows.Scan(&t.ID, &t.Title); err != nil {
			continue
		}
		tasks = append(tasks, t)
	}
	return tasks, nil
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
		`SELECT id, task, project, started_at, notes, linked_task, linked_task_id FROM entries WHERE stopped_at IS NULL LIMIT 1`,
	)
	var e models.Entry
	var startedStr string
	err := row.Scan(&e.ID, &e.Task, &e.Project, &startedStr, &e.Notes, &e.LinkedTask, &e.LinkedTaskID)
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

// Range returns entries whose started_at falls within [from, to).
func (s *Store) Range(from, to time.Time) ([]models.Entry, error) {
	rows, err := s.db.Query(
		`SELECT id, task, project, started_at, stopped_at, notes, linked_task, linked_task_id
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

// Restore re-inserts a previously-deleted entry with its exact original
// ID and fields — used by the TUI's delete-undo ("u" within undoWindow of
// a delete). SQLite allows an explicit value in an AUTOINCREMENT column,
// so the restored row keeps the same ID any other reference to it had.
func (s *Store) Restore(e models.Entry) error {
	var stoppedAt any
	if e.StoppedAt != nil {
		stoppedAt = e.StoppedAt.Format(timeLayout)
	}
	_, err := s.db.Exec(
		`INSERT INTO entries (id, task, project, started_at, stopped_at, notes, linked_task, linked_task_id) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		e.ID, e.Task, e.Project, e.StartedAt.Format(timeLayout), stoppedAt, e.Notes, e.LinkedTask, e.LinkedTaskID,
	)
	return err
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
		`SELECT id, task, project, started_at, stopped_at, notes, linked_task, linked_task_id
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
		if err := rows.Scan(&e.ID, &e.Task, &e.Project, &startedStr, &stoppedStr, &e.Notes, &e.LinkedTask, &e.LinkedTaskID); err != nil {
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
