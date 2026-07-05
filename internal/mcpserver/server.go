package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/aeon022/timectl/internal/models"
	"github.com/aeon022/timectl/internal/store"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// Serve starts an MCP server on stdio.
func Serve(s *store.Store) error {
	srv := server.NewMCPServer(
		"timectl",
		"1.0.0",
		server.WithToolCapabilities(true),
	)

	addStartTimer(srv, s)
	addStopTimer(srv, s)
	addGetTimeLog(srv, s)
	addGetTimeStats(srv, s)

	return server.ServeStdio(srv)
}

// ── start_timer ───────────────────────────────────────────────────────────────

func addStartTimer(srv *server.MCPServer, s *store.Store) {
	tool := mcp.NewTool("start_timer",
		mcp.WithDescription("Start a new time-tracking timer. Errors if a timer is already running."),
		mcp.WithString("task", mcp.Required(), mcp.Description("Name of the task to track")),
		mcp.WithString("project", mcp.Description("Optional project / tag")),
		mcp.WithString("notes", mcp.Description("Optional notes")),
	)

	srv.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		task, err := req.RequireString("task")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		project := req.GetString("project", "")

		entry, err := s.Start(task, project)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		out := map[string]any{
			"id":         entry.ID,
			"task":       entry.Task,
			"project":    entry.Project,
			"started_at": entry.StartedAt.Format(time.RFC3339),
		}
		return jsonResult(out)
	})
}

// ── stop_timer ────────────────────────────────────────────────────────────────

func addStopTimer(srv *server.MCPServer, s *store.Store) {
	tool := mcp.NewTool("stop_timer",
		mcp.WithDescription("Stop the currently running timer."),
		mcp.WithString("notes", mcp.Description("Optional notes to attach")),
	)

	srv.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		notes := req.GetString("notes", "")

		entry, err := s.Stop(notes)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		d := entry.ComputedDuration()
		out := map[string]any{
			"id":               entry.ID,
			"task":             entry.Task,
			"project":          entry.Project,
			"started_at":       entry.StartedAt.Format(time.RFC3339),
			"stopped_at":       entry.StoppedAt.Format(time.RFC3339),
			"duration_seconds": int(d.Seconds()),
			"duration_human":   models.FormatDuration(d),
			"notes":            entry.Notes,
		}
		return jsonResult(out)
	})
}

// ── get_time_log ──────────────────────────────────────────────────────────────

func addGetTimeLog(srv *server.MCPServer, s *store.Store) {
	tool := mcp.NewTool("get_time_log",
		mcp.WithDescription("Get time log entries for a specific date, optionally filtered by project."),
		mcp.WithString("date", mcp.Description("Date in YYYY-MM-DD format (default: today)")),
		mcp.WithString("project", mcp.Description("Optional project filter")),
	)

	srv.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		dateStr := req.GetString("date", "")
		project := req.GetString("project", "")

		var date time.Time
		if dateStr == "" {
			date = time.Now()
		} else {
			var err error
			date, err = time.Parse("2006-01-02", dateStr)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("invalid date %q: %v", dateStr, err)), nil
			}
		}

		start := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, date.Location())
		end := start.Add(24 * time.Hour)

		entries, err := s.FilteredRange(start, end, project)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		var totalSec int64
		entryList := make([]map[string]any, 0, len(entries))
		for _, e := range entries {
			d := e.ComputedDuration()
			totalSec += int64(d.Seconds())
			item := map[string]any{
				"id":               e.ID,
				"task":             e.Task,
				"project":          e.Project,
				"started_at":       e.StartedAt.Format(time.RFC3339),
				"duration_seconds": int(d.Seconds()),
				"duration_human":   models.FormatDuration(d),
				"notes":            e.Notes,
				"running":          e.IsRunning(),
			}
			if e.StoppedAt != nil {
				item["stopped_at"] = e.StoppedAt.Format(time.RFC3339)
			}
			entryList = append(entryList, item)
		}

		out := map[string]any{
			"date":          start.Format("2006-01-02"),
			"entries":       entryList,
			"total_seconds": totalSec,
			"total_human":   models.FormatDuration(time.Duration(totalSec) * time.Second),
		}
		return jsonResult(out)
	})
}

// ── get_time_stats ────────────────────────────────────────────────────────────

func addGetTimeStats(srv *server.MCPServer, s *store.Store) {
	tool := mcp.NewTool("get_time_stats",
		mcp.WithDescription("Get aggregated time statistics over the last N days."),
		mcp.WithNumber("days", mcp.Description("Number of days to look back (default: 7)")),
	)

	srv.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		days := int(req.GetFloat("days", 7))
		if days <= 0 {
			days = 7
		}

		entries, err := s.RecentDays(days)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		byTask := map[string]int64{}
		byProject := map[string]int64{}
		dayTotals := map[string]time.Duration{}
		daySet := map[string]bool{}

		for _, e := range entries {
			d := e.ComputedDuration()
			byTask[e.Task] += int64(d.Seconds())
			if e.Project != "" {
				byProject[e.Project] += int64(d.Seconds())
			}
			day := e.StartedAt.Format("2006-01-02")
			dayTotals[day] += d
			daySet[day] = true
		}

		var totalSec int64
		for _, v := range byTask {
			totalSec += v
		}

		var avgSec int64
		if len(dayTotals) > 0 {
			var tot time.Duration
			for _, v := range dayTotals {
				tot += v
			}
			avgSec = int64(tot.Seconds()) / int64(len(dayTotals))
		}

		streak := computeStreak(daySet)

		out := map[string]any{
			"by_task":               byTask,
			"by_project":            byProject,
			"total_seconds":         totalSec,
			"daily_average_seconds": avgSec,
			"streak_days":           streak,
			"days_with_entries":     len(dayTotals),
		}
		return jsonResult(out)
	})
}

// ── helpers ───────────────────────────────────────────────────────────────────

func jsonResult(v any) (*mcp.CallToolResult, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(string(b)), nil
}

func computeStreak(daySet map[string]bool) int {
	streak := 0
	now := time.Now()
	for {
		day := now.Format("2006-01-02")
		if !daySet[day] {
			break
		}
		streak++
		now = now.AddDate(0, 0, -1)
	}
	return streak
}
