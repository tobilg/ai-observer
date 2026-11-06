package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tobilg/ai-observer/internal/api"
	"github.com/tobilg/ai-observer/internal/storage"
)

// freePort returns a TCP port that was free a moment ago
func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// setupRunEnv points the commands at a temporary database, empty session
// directories and free ports, and returns the database path
func setupRunEnv(t *testing.T) (dbPath string, apiPort, otlpPort int) {
	t.Helper()
	dir := t.TempDir()
	dbPath = filepath.Join(dir, "test.duckdb")
	apiPort, otlpPort = freePort(t), freePort(t)
	t.Setenv("AI_OBSERVER_DATABASE_PATH", dbPath)
	t.Setenv("AI_OBSERVER_API_PORT", fmt.Sprint(apiPort))
	t.Setenv("AI_OBSERVER_OTLP_PORT", fmt.Sprint(otlpPort))
	t.Setenv("AI_OBSERVER_LOG_LEVEL", "ERROR")
	t.Setenv("AI_OBSERVER_RETENTION_DAYS", "")
	t.Setenv("AI_OBSERVER_AUTH_TOKEN", "")
	for _, env := range []string{"AI_OBSERVER_CLAUDE_PATH", "AI_OBSERVER_CODEX_PATH", "AI_OBSERVER_GEMINI_PATH"} {
		sessions := filepath.Join(dir, env)
		if err := os.MkdirAll(sessions, 0o755); err != nil {
			t.Fatal(err)
		}
		t.Setenv(env, sessions)
	}
	return dbPath, apiPort, otlpPort
}

func waitHealthy(t *testing.T, port int, done <-chan error) {
	t.Helper()
	url := fmt.Sprintf("http://127.0.0.1:%d/health", port)
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case err := <-done:
			t.Fatalf("stopped before serving: %v", err)
		default:
		}
		resp, err := http.Get(url)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("%s did not become healthy", url)
}

func waitStopped(t *testing.T, done <-chan error) {
	t.Helper()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("shutdown: %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("did not shut down after the context was cancelled")
	}
}

func TestRunServerServesUntilCancelled(t *testing.T) {
	dbPath, apiPort, otlpPort := setupRunEnv(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- runServer(ctx, nil) }()
	waitHealthy(t, apiPort, done)
	waitHealthy(t, otlpPort, done)

	cancel()
	waitStopped(t, done)
	if _, err := os.Stat(dbPath); err != nil {
		t.Errorf("database was not created: %v", err)
	}
	// The shutdown closed the database, so another process can open it
	store, err := storage.NewDuckDBStore(dbPath)
	if err != nil {
		t.Fatalf("reopening the database: %v", err)
	}
	store.Close()
}

func TestRunServerReportsBusyPort(t *testing.T) {
	_, apiPort, _ := setupRunEnv(t)
	l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", apiPort))
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()

	if err := runServer(context.Background(), nil); err == nil || !strings.Contains(err.Error(), "address already in use") {
		t.Errorf("error = %v, want the port in use", err)
	}
}

func TestRunWatchServesAPIUntilCancelled(t *testing.T) {
	_, apiPort, otlpPort := setupRunEnv(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- runWatch(ctx, []string{"all", "--backfill"}) }()
	waitHealthy(t, apiPort, done)

	// Watch mode serves no OTLP
	if resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/health", otlpPort)); err == nil {
		resp.Body.Close()
		t.Error("the OTLP port answered in watch mode")
	}

	cancel()
	waitStopped(t, done)
}

func TestRunWatchRejectsUnknownTool(t *testing.T) {
	setupRunEnv(t)
	if err := runWatch(context.Background(), []string{"copilot"}); err == nil || !strings.Contains(err.Error(), "invalid tool") {
		t.Errorf("error = %v, want an invalid tool", err)
	}
}

func TestRunDeleteAndExportDatabase(t *testing.T) {
	dbPath, _, _ := setupRunEnv(t)
	day := time.Date(2025, 3, 10, 12, 0, 0, 0, time.Local)
	store, err := storage.NewDuckDBStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	logs := []api.LogRecord{
		{Timestamp: day, ServiceName: "claude-code", Body: "keep"},
		{Timestamp: day, ServiceName: "codex_cli_rs", Body: "delete"},
	}
	if err := store.InsertLogs(context.Background(), logs); err != nil {
		t.Fatal(err)
	}
	store.Close()

	if err := runDelete([]string{"logs", "--from", "2025-03-10", "--to", "2025-03-10", "--service", "codex", "--yes"}); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := runDelete([]string{"logs", "--from", "2025-03-10", "--to", "2025-03-10", "--service", "nope", "--yes"}); err == nil {
		t.Error("delete with an unknown service succeeded")
	}

	out := t.TempDir()
	if err := runExport([]string{"all", "--output", out, "--from", "2025-03-10", "--to", "2025-03-10", "--yes"}); err != nil {
		t.Fatalf("export: %v", err)
	}
	files, err := filepath.Glob(filepath.Join(out, "*.parquet"))
	if err != nil || len(files) == 0 {
		t.Fatalf("exported files = %v (%v), want parquet files", files, err)
	}

	store, err = storage.NewDuckDBStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	resp, err := store.QueryLogs(context.Background(), "", "", "", "", day.Add(-time.Hour), day.Add(time.Hour), storage.Page{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Logs) != 1 || resp.Logs[0].Body != "keep" {
		t.Errorf("logs after delete = %+v, want only the claude-code log", resp.Logs)
	}
}

func TestLoadPricingOverrides(t *testing.T) {
	if err := loadPricingOverrides(""); err != nil {
		t.Errorf("without a directory: %v", err)
	}
	// Missing files are skipped
	if err := loadPricingOverrides(t.TempDir()); err != nil {
		t.Errorf("empty directory: %v", err)
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "codex.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := loadPricingOverrides(dir); err == nil || !strings.Contains(err.Error(), "AI_OBSERVER_PRICING_DIR") {
		t.Errorf("broken override file: error = %v, want one naming AI_OBSERVER_PRICING_DIR", err)
	}
}

// mcpInitialize posts an MCP initialize request and returns the status
func mcpInitialize(t *testing.T, port int) int {
	t.Helper()
	body := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}`
	req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("http://127.0.0.1:%d/mcp", port), strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var reply struct {
		Result struct {
			ServerInfo struct {
				Name string `json:"name"`
			} `json:"serverInfo"`
		} `json:"result"`
	}
	if resp.StatusCode == http.StatusOK {
		if err := json.NewDecoder(resp.Body).Decode(&reply); err != nil || reply.Result.ServerInfo.Name != "ai-observer" {
			t.Errorf("initialize reply: %+v, %v", reply, err)
		}
	}
	return resp.StatusCode
}

// The MCP endpoint is served with --mcp (or AI_OBSERVER_MCP=true) only
func TestRunServerMCPFlag(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		_, apiPort, _ := setupRunEnv(t)
		t.Setenv("AI_OBSERVER_MCP", "")
		ctx, cancel := context.WithCancel(context.Background())
		var args []string
		if enabled {
			args = []string{"--mcp"}
		}
		done := make(chan error, 1)
		go func() { done <- runServer(ctx, args) }()
		waitHealthy(t, apiPort, done)

		status := mcpInitialize(t, apiPort)
		if enabled && status != http.StatusOK {
			t.Errorf("with --mcp: initialize status %d, want 200", status)
		}
		if !enabled && status != http.StatusNotFound {
			t.Errorf("without --mcp: status %d, want 404", status)
		}
		cancel()
		waitStopped(t, done)
	}
}

func TestParseServeAndWatchMCPFlags(t *testing.T) {
	if mcp, err := parseServeArgs([]string{"--mcp"}); err != nil || !mcp {
		t.Errorf("serve --mcp = %v, %v", mcp, err)
	}
	if mcp, err := parseServeArgs(nil); err != nil || mcp {
		t.Errorf("serve = %v, %v", mcp, err)
	}
	if _, err := parseServeArgs([]string{"extra"}); err == nil {
		t.Error("serve accepted an argument")
	}
	args, err := parseWatchArgs([]string{"codex", "--mcp"})
	if err != nil || !args.MCP || args.ToolArg != "codex" {
		t.Errorf("watch codex --mcp = %+v, %v", args, err)
	}
}
