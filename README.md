# timectl

Terminal-first time tracking for developers. Part of the **missionctl** suite.

---

## Quick Start

```bash
# Build & install
./setup.sh

# Start your first timer
timectl start "Fix auth bug" --project diaryctl

# Check status
timectl status

# Stop and log it
timectl stop

# Review today
timectl today

# Open the interactive TUI
timectl
```

### MCP configuration (Claude Desktop / Claude Code)

```json
{
  "mcpServers": {
    "timectl": {
      "command": "timectl",
      "args": ["mcp"]
    }
  }
}
```

---

## Cheatsheet

### CLI

| Command | Description |
|---|---|
| `timectl` | Open TUI |
| `timectl start TASK [-p PROJECT] [-n NOTES]` | Start a timer |
| `timectl stop [-n NOTES]` | Stop running timer |
| `timectl status` | Show running timer |
| `timectl today [--json]` | Today's entries |
| `timectl week [--json]` | This week's breakdown |
| `timectl log [-d DAYS] [-p PROJECT] [--json]` | Recent entries |
| `timectl delete ID` | Delete an entry |
| `timectl mcp` | Start MCP server (stdio) |

### TUI keys

| Key | Action |
|---|---|
| `n` | Start new timer (prompts for task) |
| `s` | Stop running timer |
| `j` / `k` | Navigate entries |
| `d` | Delete selected entry (confirm y/n) |
| `e` | Edit notes on selected entry |
| `w` | Week view (bar chart) |
| `v` | Stats view |
| `esc` | Back to main view |
| `q` | Quit |

---

## CLI Reference

### `timectl start TASK`

Start a timer. Errors if one is already running.

```
$ timectl start "Fix auth bug" --project diaryctl
▶ Started: Fix auth bug (diaryctl)
  09:14:32 — running
```

Flags: `--project / -p`, `--notes / -n`

### `timectl stop`

Stop the running timer.

```
$ timectl stop
■ Stopped: Fix auth bug (diaryctl)
  09:14:32 → 11:03:17 — 1h 48m 45s
```

Flags: `--notes / -n`

### `timectl status`

```
$ timectl status
▶ Fix auth bug (diaryctl) — running for 1h 12m 03s
```

### `timectl today [--json]`

```
$ timectl today
  09:00 – 10:30  1h 30m  Plan sprint       [work]
  10:45 – 12:00  1h 15m  Fix auth bug      [diaryctl]
  14:00 – 15:45  1h 45m  Write tests       [diaryctl]
  ────────────────────────────────────────────────────
  Total: 4h 30m
```

### `timectl week [--json]`

Weekly breakdown by day with bar chart and per-task totals.

### `timectl log [--days N] [--project NAME] [--json]`

Recent entries grouped by day. Default: last 7 days.

### `timectl delete ID`

Prompts for confirmation before deleting.

---

## TUI Guide

Run `timectl` with no arguments to open the TUI.

**Main view** shows today's entries and the active timer in the header (updated every second). Use `n` to start a new timer, `s` to stop, `j`/`k` to navigate, `d` to delete with confirmation, `e` to edit notes.

**Week view** (`w`) renders a proportional bar chart for Mon–Sun of the current week.

**Stats view** (`v`) shows top tasks by time, top projects, average day length (last 14 days), and current consecutive-day streak.

Data is stored at `~/.local/share/timectl/time.db` (SQLite, WAL mode).

---

## MCP Tools

| Tool | Description | Params |
|---|---|---|
| `start_timer` | Start a timer | `task` (req), `project`, `notes` |
| `stop_timer` | Stop running timer | `notes` |
| `get_time_log` | Entries for a date | `date` (YYYY-MM-DD), `project` |
| `get_time_stats` | Aggregated stats | `days` (default 7) |

### AI workflow examples

**"How much time did I spend on diaryctl this week?"**

```
get_time_stats with days=7
→ by_project: { "diaryctl": 14400 }  # 4h total
```

**"Start a timer for writing documentation"**

```
start_timer with task="Write documentation", project="timectl"
→ { "id": 42, "started_at": "2026-07-01T09:00:00Z", ... }
```

**"Show me my productivity patterns for this month"**

```
get_time_stats with days=30
→ daily_average_seconds, streak_days, by_task breakdown
```

---

## Integration

**diaryctl** — time data can appear inline in diary entries. Reference `timectl get_time_log` from a diary MCP workflow to annotate what you were working on.

**taskctl** — link timers to tasks by using the task name or ID as the `task` argument. Query both MCPs together to see planned vs. actual time.

---

## Architecture

```
timectl/
├── main.go                      # Entry point
├── cmd/                         # Cobra CLI commands
│   ├── root.go                  # Default: opens TUI
│   ├── start.go / stop.go       # Timer control
│   ├── status.go                # Running timer
│   ├── today.go / week.go       # Reporting
│   ├── log.go / delete.go       # Log & delete
│   └── mcp.go                   # MCP server
├── internal/
│   ├── models/models.go         # Entry, DaySummary, FormatDuration
│   ├── store/sqlite.go          # SQLite store (WAL, no CGo)
│   ├── tui/tui.go               # Bubbletea TUI (3 views)
│   └── mcpserver/server.go      # MCP server (4 tools)
└── setup.sh                     # Build & install script
```

**Data flow:** CLI commands → `store.Store` → SQLite at `~/.local/share/timectl/time.db`. TUI polls via `tea.Tick` every second for the running timer. MCP server exposes the same store methods over stdio JSON-RPC.
