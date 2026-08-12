package mcpserver

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
)

// newTestServer builds an MCPServer wired to a temporary store, using the
// same addXxx(srv) registration functions Serve() uses — without calling
// Serve() itself, which blocks forever on stdio. Each handler opens its own
// store per call via openStore(), which resolves TIMECTL_DATA_DIR — set here
// to an isolated temp dir so tests never touch the real database.
func newTestServer(t *testing.T) *mcpserver.MCPServer {
	t.Helper()
	t.Setenv("TIMECTL_DATA_DIR", t.TempDir())

	srv := mcpserver.NewMCPServer("timectl", "test", mcpserver.WithToolCapabilities(true))
	addStartTimer(srv)
	addStopTimer(srv)
	addGetTimeLog(srv)
	addGetTimeStats(srv)
	return srv
}

// callTool dispatches a tools/call JSON-RPC request through the server, the
// same path a real MCP client goes through — this works across mcp-go
// versions, unlike the newer GetTool() helper.
func callTool(t *testing.T, srv *mcpserver.MCPServer, name string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	req := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  string(mcp.MethodToolsCall),
		"params": map[string]any{
			"name":      name,
			"arguments": args,
		},
	}
	raw, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	msg := srv.HandleMessage(context.Background(), raw)
	resp, ok := msg.(mcp.JSONRPCResponse)
	if !ok {
		t.Fatalf("expected a JSON-RPC response for %q, got %T: %+v", name, msg, msg)
	}
	res, ok := resp.Result.(mcp.CallToolResult)
	if !ok {
		t.Fatalf("expected mcp.CallToolResult for %q, got %T", name, resp.Result)
	}
	if res.IsError {
		t.Fatalf("handler for %q returned an error result: %+v", name, res.Content)
	}
	return &res
}

func resultText(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	if len(res.Content) == 0 {
		t.Fatal("result has no content")
	}
	tc, ok := res.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", res.Content[0])
	}
	return tc.Text
}

func TestStartStopAndGetTimeLog(t *testing.T) {
	srv := newTestServer(t)

	startRes := callTool(t, srv, "start_timer", map[string]any{"task": "Write tests", "project": "missionctl"})
	if !strings.Contains(resultText(t, startRes), "Write tests") {
		t.Errorf("expected task name in start_timer result, got:\n%s", resultText(t, startRes))
	}

	stopRes := callTool(t, srv, "stop_timer", map[string]any{"notes": "done for now"})
	if !strings.Contains(resultText(t, stopRes), "Write tests") {
		t.Errorf("expected task name in stop_timer result, got:\n%s", resultText(t, stopRes))
	}

	logRes := callTool(t, srv, "get_time_log", nil)
	if !strings.Contains(resultText(t, logRes), "Write tests") {
		t.Errorf("expected entry in get_time_log, got:\n%s", resultText(t, logRes))
	}
}

func TestStartTimerRequiresTask(t *testing.T) {
	srv := newTestServer(t)

	req := map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": string(mcp.MethodToolsCall),
		"params": map[string]any{"name": "start_timer", "arguments": map[string]any{}},
	}
	raw, _ := json.Marshal(req)
	msg := srv.HandleMessage(context.Background(), raw)
	resp, ok := msg.(mcp.JSONRPCResponse)
	if !ok {
		t.Fatalf("expected a JSON-RPC response, got %T", msg)
	}
	res, ok := resp.Result.(mcp.CallToolResult)
	if !ok {
		t.Fatalf("expected *mcp.CallToolResult, got %T", resp.Result)
	}
	if !res.IsError {
		t.Fatal("expected an error result when task is missing")
	}
}

func TestGetTimeStats(t *testing.T) {
	srv := newTestServer(t)

	callTool(t, srv, "start_timer", map[string]any{"task": "Deep work"})
	callTool(t, srv, "stop_timer", nil)

	res := callTool(t, srv, "get_time_stats", map[string]any{"days": 7})
	if !strings.Contains(resultText(t, res), "Deep work") {
		t.Errorf("expected task in get_time_stats, got:\n%s", resultText(t, res))
	}
}
