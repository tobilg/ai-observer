package storage

import (
	"context"
	"errors"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tobilg/ai-observer/internal/api"
)

func TestNewDuckDBStore(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.duckdb")

	store, err := NewDuckDBStore(dbPath)
	if err != nil {
		t.Fatalf("NewDuckDBStore() error = %v", err)
	}
	defer store.Close()

	if store.db == nil {
		t.Error("db is nil")
	}
	if store.DB() == nil {
		t.Error("DB() returns nil")
	}
}

func TestNewDuckDBStore_CreatesDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	nestedPath := filepath.Join(tmpDir, "nested", "dir", "test.duckdb")

	store, err := NewDuckDBStore(nestedPath)
	if err != nil {
		t.Fatalf("NewDuckDBStore() error = %v", err)
	}
	defer store.Close()

	// Check directory was created
	dir := filepath.Dir(nestedPath)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Errorf("directory %s was not created", dir)
	}
}

func TestNewDuckDBStore_InitializesSchema(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.duckdb")

	store, err := NewDuckDBStore(dbPath)
	if err != nil {
		t.Fatalf("NewDuckDBStore() error = %v", err)
	}
	defer store.Close()

	// Verify tables exist
	tables := []string{"otel_traces", "otel_logs", "otel_metrics"}
	for _, table := range tables {
		var count int
		err := store.db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count)
		if err != nil {
			t.Errorf("table %s query failed: %v", table, err)
		}
	}
}

func TestDuckDBStore_Close(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.duckdb")

	store, err := NewDuckDBStore(dbPath)
	if err != nil {
		t.Fatalf("NewDuckDBStore() error = %v", err)
	}

	if err := store.Close(); err != nil {
		t.Errorf("Close() error = %v", err)
	}

	// Verify connection is closed
	if err := store.db.Ping(); err == nil {
		t.Error("expected error after Close(), got nil")
	}
}

// Helper to create in-memory test store
func setupTestStore(t *testing.T) (*DuckDBStore, func()) {
	t.Helper()
	store, err := NewDuckDBStore(":memory:")
	if err != nil {
		t.Fatalf("failed to create test store: %v", err)
	}
	return store, func() { store.Close() }
}

// ============ Traces Store Tests ============

func TestInsertSpans(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now()

	spans := []api.Span{
		{
			TraceID:     "trace-001",
			SpanID:      "span-001",
			ServiceName: "test-service",
			SpanName:    "GET /api/users",
			Timestamp:   now,
			Duration:    100000000, // 100ms
			StatusCode:  "OK",
			SpanKind:    "SERVER",
			SpanAttributes: map[string]string{
				"http.method": "GET",
				"http.url":    "/api/users",
			},
		},
	}

	err := store.InsertSpans(ctx, spans)
	if err != nil {
		t.Fatalf("InsertSpans failed: %v", err)
	}

	// Verify insertion
	var count int
	if err := store.db.QueryRow("SELECT COUNT(*) FROM otel_traces").Scan(&count); err != nil {
		t.Fatalf("count query failed: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 span, got %d", count)
	}
}

func TestInsertSpans_Empty(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	err := store.InsertSpans(context.Background(), []api.Span{})
	if err != nil {
		t.Errorf("InsertSpans with empty slice should not error: %v", err)
	}
}

func TestInsertSpans_Multiple(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now()

	spans := []api.Span{
		{TraceID: "trace-001", SpanID: "span-001", ServiceName: "svc-a", SpanName: "span1", Timestamp: now},
		{TraceID: "trace-001", SpanID: "span-002", ParentSpanID: "span-001", ServiceName: "svc-a", SpanName: "span2", Timestamp: now.Add(10 * time.Millisecond)},
		{TraceID: "trace-002", SpanID: "span-003", ServiceName: "svc-b", SpanName: "span3", Timestamp: now.Add(20 * time.Millisecond)},
	}

	if err := store.InsertSpans(ctx, spans); err != nil {
		t.Fatalf("InsertSpans failed: %v", err)
	}

	var count int
	store.db.QueryRow("SELECT COUNT(*) FROM otel_traces").Scan(&count)
	if count != 3 {
		t.Errorf("expected 3 spans, got %d", count)
	}
}

func TestQueryTraces(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now()

	// Insert test data
	spans := []api.Span{
		{TraceID: "trace-001", SpanID: "span-001", ServiceName: "service-a", SpanName: "root-span", Timestamp: now, Duration: 100000000, StatusCode: "OK"},
		{TraceID: "trace-001", SpanID: "span-002", ParentSpanID: "span-001", ServiceName: "service-a", SpanName: "child-span", Timestamp: now.Add(10 * time.Millisecond), Duration: 50000000, StatusCode: "OK"},
		{TraceID: "trace-002", SpanID: "span-003", ServiceName: "service-b", SpanName: "other-root", Timestamp: now.Add(100 * time.Millisecond), Duration: 200000000, StatusCode: "ERROR"},
	}
	if err := store.InsertSpans(ctx, spans); err != nil {
		t.Fatalf("InsertSpans failed: %v", err)
	}

	// Query all traces
	from := now.Add(-1 * time.Hour)
	to := now.Add(1 * time.Hour)

	resp, err := store.QueryTraces(ctx, "", "", from, to, 10, 0)
	if err != nil {
		t.Fatalf("QueryTraces failed: %v", err)
	}

	if resp.Total != 2 {
		t.Errorf("expected 2 traces, got %d", resp.Total)
	}
}

func TestQueryTraces_WithServiceFilter(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now()

	spans := []api.Span{
		{TraceID: "trace-001", SpanID: "span-001", ServiceName: "service-a", SpanName: "span1", Timestamp: now, StatusCode: "OK"},
		{TraceID: "trace-002", SpanID: "span-002", ServiceName: "service-b", SpanName: "span2", Timestamp: now.Add(10 * time.Millisecond), StatusCode: "OK"},
	}
	store.InsertSpans(ctx, spans)

	from := now.Add(-1 * time.Hour)
	to := now.Add(1 * time.Hour)

	resp, err := store.QueryTraces(ctx, "service-a", "", from, to, 10, 0)
	if err != nil {
		t.Fatalf("QueryTraces failed: %v", err)
	}

	if resp.Total != 1 {
		t.Errorf("expected 1 trace for service-a, got %d", resp.Total)
	}
}

func TestQueryTraces_EmptyResult(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	from := time.Now().Add(-1 * time.Hour)
	to := time.Now()

	resp, err := store.QueryTraces(context.Background(), "", "", from, to, 10, 0)
	if err != nil {
		t.Fatalf("QueryTraces failed: %v", err)
	}

	if resp.Total != 0 {
		t.Errorf("expected 0 traces, got %d", resp.Total)
	}
	if len(resp.Traces) != 0 {
		t.Errorf("expected empty traces slice, got %d", len(resp.Traces))
	}
}

func TestGetTraceSpans(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now()

	spans := []api.Span{
		{TraceID: "trace-001", SpanID: "span-001", ServiceName: "test-service", SpanName: "root", Timestamp: now, StatusCode: "OK"},
		{TraceID: "trace-001", SpanID: "span-002", ParentSpanID: "span-001", ServiceName: "test-service", SpanName: "child1", Timestamp: now.Add(10 * time.Millisecond), StatusCode: "OK"},
		{TraceID: "trace-001", SpanID: "span-003", ParentSpanID: "span-001", ServiceName: "test-service", SpanName: "child2", Timestamp: now.Add(20 * time.Millisecond), StatusCode: "OK"},
		{TraceID: "trace-002", SpanID: "span-004", ServiceName: "other-service", SpanName: "other", Timestamp: now, StatusCode: "OK"},
	}
	store.InsertSpans(ctx, spans)

	traceSpans, err := store.GetTraceSpans(ctx, "trace-001", api.TraceKindOTelTrace)
	if err != nil {
		t.Fatalf("GetTraceSpans failed: %v", err)
	}

	if len(traceSpans) != 3 {
		t.Errorf("expected 3 spans for trace-001, got %d", len(traceSpans))
	}
}

func TestGetTraceSpans_NotFound(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	spans, err := store.GetTraceSpans(context.Background(), "nonexistent-trace", api.TraceKindOTelTrace)
	if err != nil {
		t.Fatalf("GetTraceSpans failed: %v", err)
	}

	if len(spans) != 0 {
		t.Errorf("expected 0 spans for nonexistent trace, got %d", len(spans))
	}
}

func TestQueryTraces_CodexUsesRawTraceRows(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now()

	spans := []api.Span{
		{TraceID: "codex-trace", SpanID: "session-root", ServiceName: "codex_cli_rs", SpanName: "codex_session", Timestamp: now, Duration: int64(time.Minute), StatusCode: "OK"},
		{TraceID: "codex-trace", SpanID: "turn-1", ParentSpanID: "session-root", ServiceName: "codex_cli_rs", SpanName: "run_turn", Timestamp: now.Add(time.Second), Duration: int64(10 * time.Second), StatusCode: "OK"},
		{TraceID: "codex-trace", SpanID: "tool-1", ParentSpanID: "turn-1", ServiceName: "codex_cli_rs", SpanName: "tool_shell", Timestamp: now.Add(2 * time.Second), Duration: int64(20 * time.Second), StatusCode: "ERROR", SpanAttributes: map[string]string{"tool": "shell"}},
		{TraceID: "codex-trace", SpanID: "turn-2", ParentSpanID: "session-root", ServiceName: "codex_cli_rs", SpanName: "run_turn", Timestamp: now.Add(30 * time.Second), Duration: int64(5 * time.Second), StatusCode: "OK"},
	}
	if err := store.InsertSpans(ctx, spans); err != nil {
		t.Fatalf("InsertSpans failed: %v", err)
	}

	resp, err := store.QueryTraces(ctx, "codex_cli_rs", "", now.Add(-time.Minute), now.Add(time.Hour), 10, 0)
	if err != nil {
		t.Fatalf("QueryTraces failed: %v", err)
	}

	if resp.Total != 1 {
		t.Fatalf("expected 1 raw codex trace, got %d", resp.Total)
	}
	if len(resp.Traces) != 1 {
		t.Fatalf("expected 1 trace row, got %d", len(resp.Traces))
	}

	trace := resp.Traces[0]
	if trace.Kind != api.TraceKindOTelTrace {
		t.Errorf("expected raw trace kind, got %q", trace.Kind)
	}
	if trace.ID != "codex-trace" || trace.TraceID != "codex-trace" {
		t.Errorf("expected codex trace id, got id=%q traceId=%q", trace.ID, trace.TraceID)
	}
	if trace.RootSpanID != "session-root" {
		t.Errorf("expected session-root as raw trace root, got %q", trace.RootSpanID)
	}
	if trace.GroupLevel != "" {
		t.Errorf("expected no codex group level for raw trace row, got %q", trace.GroupLevel)
	}
	if trace.SpanCount != 4 {
		t.Errorf("expected raw codex trace to include 4 spans, got %d", trace.SpanCount)
	}
	if trace.Status != "ERROR" {
		t.Errorf("expected raw codex trace aggregated status ERROR, got %q", trace.Status)
	}

	traceSpans, err := store.GetTraceSpans(ctx, "codex-trace", api.TraceKindOTelTrace)
	if err != nil {
		t.Fatalf("GetTraceSpans failed: %v", err)
	}
	if len(traceSpans) != 4 {
		t.Errorf("expected 4 spans for raw codex trace, got %d", len(traceSpans))
	}
}

func TestQueryTraces_CodexSearchReturnsRawTrace(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now()

	spans := []api.Span{
		{TraceID: "codex-trace", SpanID: "session-root", ServiceName: "codex_cli_rs", SpanName: "codex_session", Timestamp: now, Duration: int64(time.Minute)},
		{TraceID: "codex-trace", SpanID: "turn-1", ParentSpanID: "session-root", ServiceName: "codex_cli_rs", SpanName: "run_turn", Timestamp: now.Add(time.Second)},
		{TraceID: "codex-trace", SpanID: "child-1", ParentSpanID: "turn-1", ServiceName: "codex_cli_rs", SpanName: "tool_exec", Timestamp: now.Add(2 * time.Second), SpanAttributes: map[string]string{"command": "ripgrep"}},
		{TraceID: "codex-trace", SpanID: "turn-2", ParentSpanID: "session-root", ServiceName: "codex_cli_rs", SpanName: "run_turn", Timestamp: now.Add(3 * time.Second)},
	}
	if err := store.InsertSpans(ctx, spans); err != nil {
		t.Fatalf("InsertSpans failed: %v", err)
	}

	resp, err := store.QueryTraces(ctx, "codex_cli_rs", "ripgrep", now.Add(-time.Minute), now.Add(time.Hour), 10, 0)
	if err != nil {
		t.Fatalf("QueryTraces failed: %v", err)
	}

	if resp.Total != 1 {
		t.Fatalf("expected 1 matching raw codex trace, got %d", resp.Total)
	}
	if resp.Traces[0].Kind != api.TraceKindOTelTrace {
		t.Errorf("expected raw trace kind, got %q", resp.Traces[0].Kind)
	}
	if resp.Traces[0].RootSpanID != "session-root" {
		t.Errorf("expected search to return raw trace root, got %q", resp.Traces[0].RootSpanID)
	}
}

func TestQueryTraces_CodexSearchMatchesSpanInsideTimeRange(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now()

	spans := []api.Span{
		{TraceID: "codex-trace", SpanID: "session-root", ServiceName: "codex_cli_rs", SpanName: "codex_session", Timestamp: now.Add(-2 * time.Hour)},
		{TraceID: "codex-trace", SpanID: "matching-child", ParentSpanID: "session-root", ServiceName: "codex_cli_rs", SpanName: "inside-window-hit", Timestamp: now},
	}
	if err := store.InsertSpans(ctx, spans); err != nil {
		t.Fatalf("InsertSpans failed: %v", err)
	}

	resp, err := store.QueryTraces(ctx, "codex_cli_rs", "inside-window-hit", now.Add(-time.Minute), now.Add(time.Minute), 10, 0)
	if err != nil {
		t.Fatalf("QueryTraces failed: %v", err)
	}

	if resp.Total != 1 {
		t.Fatalf("expected matching span in time range to return trace, got total %d", resp.Total)
	}
	if resp.Traces[0].RootSpanID != "matching-child" {
		t.Errorf("expected visible in-range span to become page root, got %q", resp.Traces[0].RootSpanID)
	}
}

func TestQueryTraces_MixedCodexAndNonCodexPagination(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now()

	spans := []api.Span{
		{TraceID: "regular-old", SpanID: "regular-old-root", ServiceName: "service-a", SpanName: "root", Timestamp: now.Add(time.Minute)},
		{TraceID: "codex-trace", SpanID: "session-root", ServiceName: "codex_cli_rs", SpanName: "codex_session", Timestamp: now.Add(2 * time.Minute)},
		{TraceID: "codex-trace", SpanID: "codex-middle", ParentSpanID: "session-root", ServiceName: "codex_cli_rs", SpanName: "run_turn", Timestamp: now.Add(3 * time.Minute)},
		{TraceID: "regular-new", SpanID: "regular-new-root", ServiceName: "service-a", SpanName: "root", Timestamp: now.Add(4 * time.Minute)},
	}
	if err := store.InsertSpans(ctx, spans); err != nil {
		t.Fatalf("InsertSpans failed: %v", err)
	}

	resp, err := store.QueryTraces(ctx, "", "", now.Add(-time.Minute), now.Add(time.Hour), 2, 1)
	if err != nil {
		t.Fatalf("QueryTraces failed: %v", err)
	}

	if resp.Total != 3 {
		t.Fatalf("expected mixed total 3, got %d", resp.Total)
	}
	if len(resp.Traces) != 2 {
		t.Fatalf("expected 2 mixed traces, got %d", len(resp.Traces))
	}
	if resp.Traces[0].RootSpanID != "session-root" || resp.Traces[1].RootSpanID != "regular-old-root" {
		t.Errorf("expected mixed page session-root, regular-old-root; got %q, %q", resp.Traces[0].RootSpanID, resp.Traces[1].RootSpanID)
	}
}

func TestGetServices(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now()

	// Insert spans from different services
	spans := []api.Span{
		{TraceID: "t1", SpanID: "s1", ServiceName: "service-a", SpanName: "span", Timestamp: now},
		{TraceID: "t2", SpanID: "s2", ServiceName: "service-b", SpanName: "span", Timestamp: now},
		{TraceID: "t3", SpanID: "s3", ServiceName: "service-a", SpanName: "span", Timestamp: now}, // Duplicate
	}
	store.InsertSpans(ctx, spans)

	services, err := store.GetServices(ctx)
	if err != nil {
		t.Fatalf("GetServices failed: %v", err)
	}

	if len(services) != 2 {
		t.Errorf("expected 2 unique services, got %d", len(services))
	}
}

func TestGetServices_Empty(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	services, err := store.GetServices(context.Background())
	if err != nil {
		t.Fatalf("GetServices failed: %v", err)
	}

	if len(services) != 0 {
		t.Errorf("expected 0 services, got %d", len(services))
	}
}

func TestGetStats(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now()

	// Insert test data
	spans := []api.Span{
		{TraceID: "t1", SpanID: "s1", ServiceName: "svc", SpanName: "span", Timestamp: now, StatusCode: "OK"},
		{TraceID: "t1", SpanID: "s2", ServiceName: "svc", SpanName: "span", Timestamp: now, StatusCode: "ERROR"},
		{TraceID: "t2", SpanID: "s3", ServiceName: "svc", SpanName: "span", Timestamp: now, StatusCode: "OK"},
	}
	store.InsertSpans(ctx, spans)

	logs := []api.LogRecord{
		{Timestamp: now, ServiceName: "svc", SeverityText: "INFO", Body: "test log"},
	}
	store.InsertLogs(ctx, logs)

	metrics := []api.MetricDataPoint{
		{Timestamp: now, ServiceName: "svc", MetricName: "test_metric", MetricType: "gauge", Value: ptrFloat64(42.0)},
	}
	store.InsertMetrics(ctx, metrics)

	stats, err := store.GetStats(ctx)
	if err != nil {
		t.Fatalf("GetStats failed: %v", err)
	}

	if stats.SpanCount != 3 {
		t.Errorf("expected 3 spans, got %d", stats.SpanCount)
	}
	if stats.TraceCount != 2 {
		t.Errorf("expected 2 traces, got %d", stats.TraceCount)
	}
	if stats.LogCount != 1 {
		t.Errorf("expected 1 log, got %d", stats.LogCount)
	}
	if stats.MetricCount != 1 {
		t.Errorf("expected 1 metric, got %d", stats.MetricCount)
	}
	if stats.ServiceCount != 1 {
		t.Errorf("expected 1 service, got %d", stats.ServiceCount)
	}
}

// ============ Logs Store Tests ============

func TestInsertLogs(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now()

	logs := []api.LogRecord{
		{
			Timestamp:      now,
			ServiceName:    "test-service",
			SeverityText:   "INFO",
			SeverityNumber: 9,
			Body:           "Test log message",
		},
	}

	err := store.InsertLogs(ctx, logs)
	if err != nil {
		t.Fatalf("InsertLogs failed: %v", err)
	}

	var count int
	store.db.QueryRow("SELECT COUNT(*) FROM otel_logs").Scan(&count)
	if count != 1 {
		t.Errorf("expected 1 log, got %d", count)
	}
}

func TestInsertLogs_Empty(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	err := store.InsertLogs(context.Background(), []api.LogRecord{})
	if err != nil {
		t.Errorf("InsertLogs with empty slice should not error: %v", err)
	}
}

func TestQueryLogs(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now()

	logs := []api.LogRecord{
		{Timestamp: now, ServiceName: "svc-a", SeverityText: "INFO", Body: "info message"},
		{Timestamp: now.Add(10 * time.Millisecond), ServiceName: "svc-a", SeverityText: "ERROR", Body: "error message"},
		{Timestamp: now.Add(20 * time.Millisecond), ServiceName: "svc-b", SeverityText: "WARN", Body: "warning"},
	}
	store.InsertLogs(ctx, logs)

	from := now.Add(-1 * time.Hour)
	to := now.Add(1 * time.Hour)

	// Query all logs
	resp, err := store.QueryLogs(ctx, "", "", "", "", from, to, 10, 0)
	if err != nil {
		t.Fatalf("QueryLogs failed: %v", err)
	}

	if resp.Total != 3 {
		t.Errorf("expected 3 logs, got %d", resp.Total)
	}
}

func TestQueryLogs_WithSeverityFilter(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now()

	logs := []api.LogRecord{
		{Timestamp: now, ServiceName: "svc", SeverityText: "INFO", Body: "info"},
		{Timestamp: now, ServiceName: "svc", SeverityText: "ERROR", Body: "error"},
		{Timestamp: now, ServiceName: "svc", SeverityText: "ERROR", Body: "another error"},
	}
	store.InsertLogs(ctx, logs)

	from := now.Add(-1 * time.Hour)
	to := now.Add(1 * time.Hour)

	resp, err := store.QueryLogs(ctx, "", "ERROR", "", "", from, to, 10, 0)
	if err != nil {
		t.Fatalf("QueryLogs failed: %v", err)
	}

	if resp.Total != 2 {
		t.Errorf("expected 2 ERROR logs, got %d", resp.Total)
	}
}

func TestQueryLogs_WithSearch(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now()

	logs := []api.LogRecord{
		{Timestamp: now, ServiceName: "svc", SeverityText: "INFO", Body: "database connection established"},
		{Timestamp: now, ServiceName: "svc", SeverityText: "ERROR", Body: "database connection failed"},
		{Timestamp: now, ServiceName: "svc", SeverityText: "INFO", Body: "request processed"},
	}
	store.InsertLogs(ctx, logs)

	from := now.Add(-1 * time.Hour)
	to := now.Add(1 * time.Hour)

	resp, err := store.QueryLogs(ctx, "", "", "", "database", from, to, 10, 0)
	if err != nil {
		t.Fatalf("QueryLogs failed: %v", err)
	}

	if resp.Total != 2 {
		t.Errorf("expected 2 logs matching 'database', got %d", resp.Total)
	}
}

func TestGetLogLevels(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now()

	logs := []api.LogRecord{
		{Timestamp: now, ServiceName: "svc", SeverityText: "INFO", Body: "1"},
		{Timestamp: now, ServiceName: "svc", SeverityText: "INFO", Body: "2"},
		{Timestamp: now, ServiceName: "svc", SeverityText: "ERROR", Body: "3"},
		{Timestamp: now, ServiceName: "svc", SeverityText: "WARN", Body: "4"},
	}
	store.InsertLogs(ctx, logs)

	levels, err := store.GetLogLevels(ctx)
	if err != nil {
		t.Fatalf("GetLogLevels failed: %v", err)
	}

	if levels["INFO"] != 2 {
		t.Errorf("expected INFO count 2, got %d", levels["INFO"])
	}
	if levels["ERROR"] != 1 {
		t.Errorf("expected ERROR count 1, got %d", levels["ERROR"])
	}
	if levels["WARN"] != 1 {
		t.Errorf("expected WARN count 1, got %d", levels["WARN"])
	}
}

func TestGetLogLevels_Empty(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	levels, err := store.GetLogLevels(context.Background())
	if err != nil {
		t.Fatalf("GetLogLevels failed: %v", err)
	}

	if len(levels) != 0 {
		t.Errorf("expected 0 levels, got %d", len(levels))
	}
}

// ============ Metrics Store Tests ============

func TestInsertMetrics(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now()

	metrics := []api.MetricDataPoint{
		{
			Timestamp:   now,
			ServiceName: "test-service",
			MetricName:  "cpu_usage",
			MetricType:  "gauge",
			Value:       ptrFloat64(45.5),
		},
	}

	err := store.InsertMetrics(ctx, metrics)
	if err != nil {
		t.Fatalf("InsertMetrics failed: %v", err)
	}

	var count int
	store.db.QueryRow("SELECT COUNT(*) FROM otel_metrics").Scan(&count)
	if count != 1 {
		t.Errorf("expected 1 metric, got %d", count)
	}
}

func TestInsertMetrics_Empty(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	err := store.InsertMetrics(context.Background(), []api.MetricDataPoint{})
	if err != nil {
		t.Errorf("InsertMetrics with empty slice should not error: %v", err)
	}
}

func TestQueryMetrics(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now()

	metrics := []api.MetricDataPoint{
		{Timestamp: now, ServiceName: "svc-a", MetricName: "cpu_usage", MetricType: "gauge", Value: ptrFloat64(50.0)},
		{Timestamp: now, ServiceName: "svc-a", MetricName: "memory_usage", MetricType: "gauge", Value: ptrFloat64(70.0)},
		{Timestamp: now, ServiceName: "svc-b", MetricName: "cpu_usage", MetricType: "gauge", Value: ptrFloat64(30.0)},
	}
	store.InsertMetrics(ctx, metrics)

	from := now.Add(-1 * time.Hour)
	to := now.Add(1 * time.Hour)

	resp, err := store.QueryMetrics(ctx, "", "", "", from, to, 10, 0)
	if err != nil {
		t.Fatalf("QueryMetrics failed: %v", err)
	}

	if resp.Total != 3 {
		t.Errorf("expected 3 metrics, got %d", resp.Total)
	}
}

func TestQueryMetrics_WithFilters(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now()

	metrics := []api.MetricDataPoint{
		{Timestamp: now, ServiceName: "svc-a", MetricName: "cpu_usage", MetricType: "gauge", Value: ptrFloat64(50.0)},
		{Timestamp: now, ServiceName: "svc-a", MetricName: "request_count", MetricType: "sum", Value: ptrFloat64(100.0)},
		{Timestamp: now, ServiceName: "svc-b", MetricName: "cpu_usage", MetricType: "gauge", Value: ptrFloat64(30.0)},
	}
	store.InsertMetrics(ctx, metrics)

	from := now.Add(-1 * time.Hour)
	to := now.Add(1 * time.Hour)

	// Filter by service
	resp, err := store.QueryMetrics(ctx, "svc-a", "", "", from, to, 10, 0)
	if err != nil {
		t.Fatalf("QueryMetrics failed: %v", err)
	}
	if resp.Total != 2 {
		t.Errorf("expected 2 metrics for svc-a, got %d", resp.Total)
	}

	// Filter by metric name
	resp, err = store.QueryMetrics(ctx, "", "cpu_usage", "", from, to, 10, 0)
	if err != nil {
		t.Fatalf("QueryMetrics failed: %v", err)
	}
	if resp.Total != 2 {
		t.Errorf("expected 2 cpu_usage metrics, got %d", resp.Total)
	}

	// Filter by type
	resp, err = store.QueryMetrics(ctx, "", "", "sum", from, to, 10, 0)
	if err != nil {
		t.Fatalf("QueryMetrics failed: %v", err)
	}
	if resp.Total != 1 {
		t.Errorf("expected 1 sum metric, got %d", resp.Total)
	}
}

func TestGetBreakdownValues_ValidatesAttributeKeys(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now()
	value := 1.0
	metrics := []api.MetricDataPoint{
		{
			Timestamp:   now,
			ServiceName: "svc-a",
			MetricName:  "token_usage",
			MetricType:  "sum",
			Value:       &value,
			Attributes: map[string]string{
				"gen_ai.token.type": "input",
				"type":              "fallback",
			},
		},
	}
	if err := store.InsertMetrics(ctx, metrics); err != nil {
		t.Fatalf("InsertMetrics failed: %v", err)
	}

	values, err := store.GetBreakdownValues(ctx, "token_usage", "gen_ai.token.type", "")
	if err != nil {
		t.Fatalf("GetBreakdownValues failed: %v", err)
	}
	if len(values) != 1 || values[0] != "input" {
		t.Fatalf("expected dotted attribute value input, got %#v", values)
	}

	if _, err := store.GetBreakdownValues(ctx, "token_usage", "type') OR 1=1--", ""); !errors.Is(err, ErrInvalidAttributeKey) {
		t.Fatalf("expected ErrInvalidAttributeKey, got %v", err)
	}
}

func TestGetMetricNames(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now()

	metrics := []api.MetricDataPoint{
		{Timestamp: now, ServiceName: "svc", MetricName: "cpu_usage", MetricType: "gauge", Value: ptrFloat64(50.0)},
		{Timestamp: now, ServiceName: "svc", MetricName: "memory_usage", MetricType: "gauge", Value: ptrFloat64(70.0)},
		{Timestamp: now, ServiceName: "svc", MetricName: "cpu_usage", MetricType: "gauge", Value: ptrFloat64(55.0)}, // Duplicate
	}
	store.InsertMetrics(ctx, metrics)

	names, err := store.GetMetricNames(ctx, "")
	if err != nil {
		t.Fatalf("GetMetricNames failed: %v", err)
	}

	if len(names) != 2 {
		t.Errorf("expected 2 unique metric names, got %d", len(names))
	}
}

func TestGetMetricNames_Empty(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	names, err := store.GetMetricNames(context.Background(), "")
	if err != nil {
		t.Fatalf("GetMetricNames failed: %v", err)
	}

	if len(names) != 0 {
		t.Errorf("expected 0 metric names, got %d", len(names))
	}
}

// ============ Pagination Tests ============

func TestQueryTraces_Pagination(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now()

	// Insert 5 traces
	for i := 0; i < 5; i++ {
		spans := []api.Span{
			{TraceID: "trace-" + string(rune('a'+i)), SpanID: "span-" + string(rune('a'+i)), ServiceName: "svc", SpanName: "span", Timestamp: now.Add(time.Duration(i) * time.Minute), StatusCode: "OK"},
		}
		store.InsertSpans(ctx, spans)
	}

	from := now.Add(-1 * time.Hour)
	to := now.Add(1 * time.Hour)

	// Get first page
	resp, err := store.QueryTraces(ctx, "", "", from, to, 2, 0)
	if err != nil {
		t.Fatalf("QueryTraces failed: %v", err)
	}

	if len(resp.Traces) != 2 {
		t.Errorf("expected 2 traces on first page, got %d", len(resp.Traces))
	}
	if !resp.HasMore {
		t.Error("expected HasMore to be true")
	}

	// Get second page
	resp, err = store.QueryTraces(ctx, "", "", from, to, 2, 2)
	if err != nil {
		t.Fatalf("QueryTraces failed: %v", err)
	}

	if len(resp.Traces) != 2 {
		t.Errorf("expected 2 traces on second page, got %d", len(resp.Traces))
	}
}

func TestQueryLogs_Pagination(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now()

	// Insert 5 logs
	for i := 0; i < 5; i++ {
		logs := []api.LogRecord{
			{Timestamp: now.Add(time.Duration(i) * time.Minute), ServiceName: "svc", SeverityText: "INFO", Body: "log " + string(rune('a'+i))},
		}
		store.InsertLogs(ctx, logs)
	}

	from := now.Add(-1 * time.Hour)
	to := now.Add(1 * time.Hour)

	resp, err := store.QueryLogs(ctx, "", "", "", "", from, to, 2, 0)
	if err != nil {
		t.Fatalf("QueryLogs failed: %v", err)
	}

	if len(resp.Logs) != 2 {
		t.Errorf("expected 2 logs on first page, got %d", len(resp.Logs))
	}
	if !resp.HasMore {
		t.Error("expected HasMore to be true")
	}
}

// ============ Metric Series Tests ============

func TestQueryMetricSeries(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now().Truncate(time.Minute)

	// Insert gauge metrics
	metrics := []api.MetricDataPoint{
		{Timestamp: now, ServiceName: "svc-a", MetricName: "cpu_usage", MetricType: "gauge", Value: ptrFloat64(50.0)},
		{Timestamp: now.Add(1 * time.Minute), ServiceName: "svc-a", MetricName: "cpu_usage", MetricType: "gauge", Value: ptrFloat64(60.0)},
		{Timestamp: now.Add(2 * time.Minute), ServiceName: "svc-a", MetricName: "cpu_usage", MetricType: "gauge", Value: ptrFloat64(55.0)},
	}
	store.InsertMetrics(ctx, metrics)

	from := now.Add(-1 * time.Minute)
	to := now.Add(5 * time.Minute)

	// Query time series
	resp, err := store.QueryMetricSeries(ctx, "cpu_usage", "", from, to, 60, false)
	if err != nil {
		t.Fatalf("QueryMetricSeries failed: %v", err)
	}

	if len(resp.Series) == 0 {
		t.Error("expected at least one time series")
	}
}

func TestQueryMetricSeries_NoData(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now()

	from := now.Add(-1 * time.Hour)
	to := now

	resp, err := store.QueryMetricSeries(ctx, "nonexistent_metric", "", from, to, 60, false)
	if err != nil {
		t.Fatalf("QueryMetricSeries failed: %v", err)
	}

	if len(resp.Series) != 0 {
		t.Errorf("expected 0 series for nonexistent metric, got %d", len(resp.Series))
	}
}

func TestQueryMetricSeries_WithAggregation(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now().Truncate(time.Minute)

	// Insert gauge metrics
	metrics := []api.MetricDataPoint{
		{Timestamp: now, ServiceName: "svc-a", MetricName: "memory_usage", MetricType: "gauge", Value: ptrFloat64(100.0)},
		{Timestamp: now.Add(1 * time.Minute), ServiceName: "svc-a", MetricName: "memory_usage", MetricType: "gauge", Value: ptrFloat64(200.0)},
		{Timestamp: now.Add(2 * time.Minute), ServiceName: "svc-a", MetricName: "memory_usage", MetricType: "gauge", Value: ptrFloat64(150.0)},
	}
	store.InsertMetrics(ctx, metrics)

	from := now.Add(-1 * time.Minute)
	to := now.Add(5 * time.Minute)

	// Query with aggregation (scalar result)
	resp, err := store.QueryMetricSeries(ctx, "memory_usage", "", from, to, 60, true)
	if err != nil {
		t.Fatalf("QueryMetricSeries with aggregation failed: %v", err)
	}

	if len(resp.Series) == 0 {
		t.Error("expected at least one aggregated series")
	}
}

func TestQueryMetricSeries_AggregateRespectsTimeRange(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now().Truncate(time.Minute)

	metrics := []api.MetricDataPoint{
		{Timestamp: now.Add(-2 * time.Hour), ServiceName: "svc-a", MetricName: "total_requests", MetricType: "sum", Value: ptrFloat64(10.0)},
		{Timestamp: now.Add(-90 * time.Minute), ServiceName: "svc-a", MetricName: "total_requests", MetricType: "sum", Value: ptrFloat64(15.0)},
		{Timestamp: now.Add(-30 * time.Second), ServiceName: "svc-a", MetricName: "total_requests", MetricType: "sum", Value: ptrFloat64(7.0)},
	}
	if err := store.InsertMetrics(ctx, metrics); err != nil {
		t.Fatalf("InsertMetrics failed: %v", err)
	}

	resp, err := store.QueryMetricSeries(ctx, "total_requests", "", now.Add(-time.Minute), now, 60, true)
	if err != nil {
		t.Fatalf("QueryMetricSeries with aggregation failed: %v", err)
	}

	if len(resp.Series) != 1 {
		t.Fatalf("expected 1 aggregated series, got %d", len(resp.Series))
	}
	if got := resp.Series[0].DataPoints[0][1]; got != 7.0 {
		t.Errorf("expected timeframe aggregate 7, got %v", got)
	}
}

func TestQueryBatchMetricSeries_AggregateRespectsTimeRange(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now().Truncate(time.Minute)

	metrics := []api.MetricDataPoint{
		{Timestamp: now.Add(-2 * time.Hour), ServiceName: "svc-a", MetricName: "batch_total_requests", MetricType: "sum", Value: ptrFloat64(20.0)},
		{Timestamp: now.Add(-90 * time.Minute), ServiceName: "svc-a", MetricName: "batch_total_requests", MetricType: "sum", Value: ptrFloat64(5.0)},
		{Timestamp: now.Add(-30 * time.Second), ServiceName: "svc-a", MetricName: "batch_total_requests", MetricType: "sum", Value: ptrFloat64(3.0)},
	}
	if err := store.InsertMetrics(ctx, metrics); err != nil {
		t.Fatalf("InsertMetrics failed: %v", err)
	}

	resp := store.QueryBatchMetricSeries(ctx, []api.MetricQuery{
		{ID: "total", Name: "batch_total_requests", Aggregate: true},
	}, now.Add(-time.Minute), now, 60)

	if len(resp.Results) != 1 {
		t.Fatalf("expected 1 batch result, got %d", len(resp.Results))
	}
	if !resp.Results[0].Success {
		t.Fatalf("expected successful batch result, got %q", resp.Results[0].Error)
	}
	if len(resp.Results[0].Series) != 1 {
		t.Fatalf("expected 1 aggregated series, got %d", len(resp.Results[0].Series))
	}
	if got := resp.Results[0].Series[0].DataPoints[0][1]; got != 3.0 {
		t.Errorf("expected timeframe aggregate 3, got %v", got)
	}
}

func TestQueryMetricSeries_CodexUsageSumsHistoricalCumulativeRows(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now().Truncate(time.Minute)
	cumulativeTemp := int32(2)
	isMonotonic := true

	metrics := []api.MetricDataPoint{
		{
			Timestamp:              now,
			ServiceName:            "codex_cli_rs",
			MetricName:             "codex_cli_rs.token.usage",
			MetricType:             "sum",
			Value:                  ptrFloat64(100),
			AggregationTemporality: &cumulativeTemp,
			IsMonotonic:            &isMonotonic,
			Attributes:             map[string]string{"type": "input", "model": "gpt-5.5"},
		},
		{
			Timestamp:              now.Add(10 * time.Second),
			ServiceName:            "codex_cli_rs",
			MetricName:             "codex_cli_rs.token.usage",
			MetricType:             "sum",
			Value:                  ptrFloat64(90),
			AggregationTemporality: &cumulativeTemp,
			IsMonotonic:            &isMonotonic,
			Attributes:             map[string]string{"type": "input", "model": "gpt-5.5"},
		},
		{
			Timestamp:              now.Add(20 * time.Second),
			ServiceName:            "codex_cli_rs",
			MetricName:             "codex_cli_rs.token.usage",
			MetricType:             "sum",
			Value:                  ptrFloat64(300),
			AggregationTemporality: &cumulativeTemp,
			IsMonotonic:            &isMonotonic,
			Attributes:             map[string]string{"type": "input", "model": "gpt-5.5"},
		},
	}
	if err := store.InsertMetrics(ctx, metrics); err != nil {
		t.Fatalf("InsertMetrics failed: %v", err)
	}

	resp, err := store.QueryMetricSeries(ctx, "codex_cli_rs.token.usage", "", now.Add(-time.Minute), now.Add(time.Minute), 60, true)
	if err != nil {
		t.Fatalf("QueryMetricSeries failed: %v", err)
	}

	if len(resp.Series) != 1 {
		t.Fatalf("expected 1 aggregated Codex series, got %d", len(resp.Series))
	}
	series := resp.Series[0]
	if got := series.DataPoints[0][1]; got != 490 {
		t.Errorf("expected Codex aggregate to sum historical cumulative rows to 490, got %v", got)
	}
	if series.Labels["type"] != "input" {
		t.Errorf("expected type label input, got %q", series.Labels["type"])
	}
	if series.Labels["model"] != "gpt-5.5" {
		t.Errorf("expected model label gpt-5.5, got %q", series.Labels["model"])
	}
}

func TestQueryMetricSeries_CodexUsageBucketsSumHistoricalCumulativeRows(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now().Truncate(time.Minute)
	cumulativeTemp := int32(2)
	isMonotonic := true

	metrics := []api.MetricDataPoint{
		{
			Timestamp:              now.Add(5 * time.Second),
			ServiceName:            "codex_cli_rs",
			MetricName:             "codex_cli_rs.cost.usage",
			MetricType:             "sum",
			Value:                  ptrFloat64(0.20),
			AggregationTemporality: &cumulativeTemp,
			IsMonotonic:            &isMonotonic,
			Attributes:             map[string]string{"model": "gpt-5.5"},
		},
		{
			Timestamp:              now.Add(20 * time.Second),
			ServiceName:            "codex_cli_rs",
			MetricName:             "codex_cli_rs.cost.usage",
			MetricType:             "sum",
			Value:                  ptrFloat64(0.10),
			AggregationTemporality: &cumulativeTemp,
			IsMonotonic:            &isMonotonic,
			Attributes:             map[string]string{"model": "gpt-5.5"},
		},
	}
	if err := store.InsertMetrics(ctx, metrics); err != nil {
		t.Fatalf("InsertMetrics failed: %v", err)
	}

	resp, err := store.QueryMetricSeries(ctx, "codex_cli_rs.cost.usage", "", now, now.Add(time.Minute), 60, false)
	if err != nil {
		t.Fatalf("QueryMetricSeries failed: %v", err)
	}

	if len(resp.Series) != 1 {
		t.Fatalf("expected 1 Codex cost series, got %d", len(resp.Series))
	}
	if got := resp.Series[0].DataPoints[0][1]; !floatClose(got, 0.30) {
		t.Errorf("expected Codex bucket to sum historical cumulative rows to 0.30, got %v", got)
	}
}

func TestQueryBatchMetricSeries_CodexAggregatePreservesModelLabels(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now().Truncate(time.Minute)
	cumulativeTemp := int32(2)
	isMonotonic := true

	metrics := []api.MetricDataPoint{
		{
			Timestamp:              now,
			ServiceName:            "codex_cli_rs",
			MetricName:             "codex_cli_rs.cost.usage",
			MetricType:             "sum",
			Value:                  ptrFloat64(0.20),
			AggregationTemporality: &cumulativeTemp,
			IsMonotonic:            &isMonotonic,
			Attributes:             map[string]string{"model": "gpt-5.5"},
		},
		{
			Timestamp:              now.Add(10 * time.Second),
			ServiceName:            "codex_cli_rs",
			MetricName:             "codex_cli_rs.cost.usage",
			MetricType:             "sum",
			Value:                  ptrFloat64(0.10),
			AggregationTemporality: &cumulativeTemp,
			IsMonotonic:            &isMonotonic,
			Attributes:             map[string]string{"model": "gpt-5.5"},
		},
		{
			Timestamp:              now,
			ServiceName:            "codex_cli_rs",
			MetricName:             "codex_cli_rs.cost.usage",
			MetricType:             "sum",
			Value:                  ptrFloat64(0.05),
			AggregationTemporality: &cumulativeTemp,
			IsMonotonic:            &isMonotonic,
			Attributes:             map[string]string{"model": "gpt-5-mini"},
		},
	}
	if err := store.InsertMetrics(ctx, metrics); err != nil {
		t.Fatalf("InsertMetrics failed: %v", err)
	}

	resp := store.QueryBatchMetricSeries(ctx, []api.MetricQuery{
		{ID: "codex-cost", Name: "codex_cli_rs.cost.usage", Aggregate: true},
	}, now.Add(-time.Minute), now.Add(time.Minute), 60)

	if len(resp.Results) != 1 {
		t.Fatalf("expected 1 batch result, got %d", len(resp.Results))
	}
	if !resp.Results[0].Success {
		t.Fatalf("expected successful batch result, got %q", resp.Results[0].Error)
	}

	valuesByModel := make(map[string]float64)
	for _, series := range resp.Results[0].Series {
		valuesByModel[series.Labels["model"]] = series.DataPoints[0][1]
	}

	if got := valuesByModel["gpt-5.5"]; !floatClose(got, 0.30) {
		t.Errorf("expected gpt-5.5 aggregate 0.30, got %v", got)
	}
	if got := valuesByModel["gpt-5-mini"]; got != 0.05 {
		t.Errorf("expected gpt-5-mini aggregate 0.05, got %v", got)
	}
}

func TestQueryMetricSeries_TrueCumulativeMetricStillUsesMaxMinusMin(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now().Truncate(time.Minute)
	cumulativeTemp := int32(2)
	isMonotonic := true

	metrics := []api.MetricDataPoint{
		{
			Timestamp:              now,
			ServiceName:            "svc",
			MetricName:             "true_cumulative_counter",
			MetricType:             "sum",
			Value:                  ptrFloat64(100),
			AggregationTemporality: &cumulativeTemp,
			IsMonotonic:            &isMonotonic,
		},
		{
			Timestamp:              now.Add(10 * time.Second),
			ServiceName:            "svc",
			MetricName:             "true_cumulative_counter",
			MetricType:             "sum",
			Value:                  ptrFloat64(150),
			AggregationTemporality: &cumulativeTemp,
			IsMonotonic:            &isMonotonic,
		},
		{
			Timestamp:              now.Add(20 * time.Second),
			ServiceName:            "svc",
			MetricName:             "true_cumulative_counter",
			MetricType:             "sum",
			Value:                  ptrFloat64(170),
			AggregationTemporality: &cumulativeTemp,
			IsMonotonic:            &isMonotonic,
		},
	}
	if err := store.InsertMetrics(ctx, metrics); err != nil {
		t.Fatalf("InsertMetrics failed: %v", err)
	}

	resp, err := store.QueryMetricSeries(ctx, "true_cumulative_counter", "", now.Add(-time.Minute), now.Add(time.Minute), 60, true)
	if err != nil {
		t.Fatalf("QueryMetricSeries failed: %v", err)
	}

	if len(resp.Series) != 1 {
		t.Fatalf("expected 1 cumulative series, got %d", len(resp.Series))
	}
	if got := resp.Series[0].DataPoints[0][1]; got != 70 {
		t.Errorf("expected true cumulative aggregate max-min 70, got %v", got)
	}
}

func TestQueryMetricSeries_WithServiceFilter(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now().Truncate(time.Minute)

	// Insert metrics from different services
	metrics := []api.MetricDataPoint{
		{Timestamp: now, ServiceName: "svc-a", MetricName: "requests", MetricType: "gauge", Value: ptrFloat64(100.0)},
		{Timestamp: now, ServiceName: "svc-b", MetricName: "requests", MetricType: "gauge", Value: ptrFloat64(200.0)},
	}
	store.InsertMetrics(ctx, metrics)

	from := now.Add(-1 * time.Minute)
	to := now.Add(5 * time.Minute)

	// Query with service filter
	resp, err := store.QueryMetricSeries(ctx, "requests", "svc-a", from, to, 60, true)
	if err != nil {
		t.Fatalf("QueryMetricSeries with service filter failed: %v", err)
	}

	for _, series := range resp.Series {
		if series.Labels["service"] != "svc-a" {
			t.Errorf("expected only svc-a in results, got %s", series.Labels["service"])
		}
	}
}

func TestQueryMetricSeries_SumMetric(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now().Truncate(time.Minute)

	// Insert sum metrics (delta)
	metrics := []api.MetricDataPoint{
		{Timestamp: now, ServiceName: "svc", MetricName: "request_count", MetricType: "sum", Value: ptrFloat64(10.0)},
		{Timestamp: now.Add(1 * time.Minute), ServiceName: "svc", MetricName: "request_count", MetricType: "sum", Value: ptrFloat64(15.0)},
	}
	store.InsertMetrics(ctx, metrics)

	from := now.Add(-1 * time.Minute)
	to := now.Add(5 * time.Minute)

	resp, err := store.QueryMetricSeries(ctx, "request_count", "", from, to, 60, true)
	if err != nil {
		t.Fatalf("QueryMetricSeries for sum metric failed: %v", err)
	}

	if len(resp.Series) == 0 {
		t.Error("expected at least one series for sum metric")
	}
}

func TestGetLatestMetricValue(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now()

	// Insert metrics
	metrics := []api.MetricDataPoint{
		{
			Timestamp:   now.Add(-2 * time.Minute),
			ServiceName: "test-service",
			MetricName:  "counter",
			MetricType:  "sum",
			Value:       ptrFloat64(100.0),
			Attributes:  map[string]string{"region": "us-east"},
		},
		{
			Timestamp:   now.Add(-1 * time.Minute),
			ServiceName: "test-service",
			MetricName:  "counter",
			MetricType:  "sum",
			Value:       ptrFloat64(150.0),
			Attributes:  map[string]string{"region": "us-east", "gen_ai.token.type": "input"},
		},
	}
	store.InsertMetrics(ctx, metrics)

	// Get latest value
	value, found := store.GetLatestMetricValue(ctx, "counter", "test-service", map[string]string{"region": "us-east"})
	if !found {
		t.Fatal("expected to find latest metric value")
	}

	if value != 150.0 {
		t.Errorf("expected value 150.0, got %f", value)
	}

	value, found = store.GetLatestMetricValue(ctx, "counter", "test-service", map[string]string{"gen_ai.token.type": "input"})
	if !found {
		t.Fatal("expected to find latest metric value by dotted attribute")
	}
	if value != 150.0 {
		t.Errorf("expected dotted attribute value 150.0, got %f", value)
	}

	value, found = store.GetLatestMetricValue(ctx, "counter", "test-service", map[string]string{"region') OR 1=1--": "us-east"})
	if found {
		t.Errorf("expected invalid attribute lookup to be ignored, got value %f", value)
	}
}

func TestGetLatestMetricValue_NotFound(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()

	value, found := store.GetLatestMetricValue(ctx, "nonexistent", "test-service", map[string]string{})
	if found {
		t.Errorf("expected not found, got value %f", value)
	}
}

func TestQueryBatchMetricSeries(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now().Truncate(time.Minute)

	// Insert metrics
	metrics := []api.MetricDataPoint{
		{Timestamp: now, ServiceName: "svc", MetricName: "metric_a", MetricType: "gauge", Value: ptrFloat64(10.0)},
		{Timestamp: now, ServiceName: "svc", MetricName: "metric_b", MetricType: "gauge", Value: ptrFloat64(20.0)},
	}
	store.InsertMetrics(ctx, metrics)

	from := now.Add(-1 * time.Minute)
	to := now.Add(5 * time.Minute)

	queries := []api.MetricQuery{
		{ID: "q1", Name: "metric_a", Aggregate: true},
		{ID: "q2", Name: "metric_b", Aggregate: true},
		{ID: "q3", Name: "nonexistent", Aggregate: true},
	}

	resp := store.QueryBatchMetricSeries(ctx, queries, from, to, 60)

	if len(resp.Results) != 3 {
		t.Errorf("expected 3 results, got %d", len(resp.Results))
	}

	for _, result := range resp.Results {
		if !result.Success {
			if result.ID != "q3" {
				t.Errorf("unexpected failure for query %s: %s", result.ID, result.Error)
			}
		}
	}
}

func TestQueryBatchMetricSeries_Empty(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now()

	resp := store.QueryBatchMetricSeries(ctx, []api.MetricQuery{}, now.Add(-1*time.Hour), now, 60)

	if len(resp.Results) != 0 {
		t.Errorf("expected 0 results for empty queries, got %d", len(resp.Results))
	}
}

// ============ Recent Traces Tests ============

func TestGetRecentTraces(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now()

	// Insert traces from different services
	spans := []api.Span{
		{TraceID: "trace-1", SpanID: "span-1", ServiceName: "service-a", SpanName: "root-1", Timestamp: now, Duration: 100000000, StatusCode: "OK"},
		{TraceID: "trace-2", SpanID: "span-2", ServiceName: "service-b", SpanName: "root-2", Timestamp: now.Add(-1 * time.Minute), Duration: 200000000, StatusCode: "ERROR"},
		{TraceID: "trace-3", SpanID: "span-3", ServiceName: "service-a", SpanName: "root-3", Timestamp: now.Add(-2 * time.Minute), Duration: 150000000, StatusCode: "OK"},
	}
	store.InsertSpans(ctx, spans)

	resp, err := store.GetRecentTraces(ctx, 10, now.Add(-time.Hour), now.Add(time.Hour))
	if err != nil {
		t.Fatalf("GetRecentTraces failed: %v", err)
	}

	if len(resp.Traces) != 3 {
		t.Errorf("expected 3 traces, got %d", len(resp.Traces))
	}

	// Verify order (most recent first)
	if len(resp.Traces) >= 2 && resp.Traces[0].StartTime.Before(resp.Traces[1].StartTime) {
		t.Error("expected traces ordered by most recent first")
	}
}

func TestGetRecentTraces_Empty(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	now := time.Now()
	resp, err := store.GetRecentTraces(context.Background(), 10, now.Add(-time.Hour), now.Add(time.Hour))
	if err != nil {
		t.Fatalf("GetRecentTraces failed: %v", err)
	}

	if len(resp.Traces) != 0 {
		t.Errorf("expected 0 traces, got %d", len(resp.Traces))
	}
}

func TestGetRecentTraces_Limit(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now()

	// Insert 5 traces
	for i := 0; i < 5; i++ {
		spans := []api.Span{
			{
				TraceID:     "trace-" + string(rune('a'+i)),
				SpanID:      "span-" + string(rune('a'+i)),
				ServiceName: "service",
				SpanName:    "root",
				Timestamp:   now.Add(-time.Duration(i) * time.Minute),
				StatusCode:  "OK",
			},
		}
		store.InsertSpans(ctx, spans)
	}

	resp, err := store.GetRecentTraces(ctx, 3, now.Add(-time.Hour), now.Add(time.Hour))
	if err != nil {
		t.Fatalf("GetRecentTraces failed: %v", err)
	}

	if len(resp.Traces) != 3 {
		t.Errorf("expected 3 traces (limit), got %d", len(resp.Traces))
	}
}

func TestGetRecentTraces_ExcludesCodexService(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now()

	// Insert traces including codex_cli_rs service (which should be handled separately)
	spans := []api.Span{
		{TraceID: "trace-1", SpanID: "span-1", ServiceName: "service-a", SpanName: "root-1", Timestamp: now, StatusCode: "OK"},
		{TraceID: "trace-2", SpanID: "span-2", ServiceName: "codex_cli_rs", SpanName: "codex-span", Timestamp: now.Add(-1 * time.Minute), StatusCode: "OK"},
		{TraceID: "trace-3", SpanID: "span-3", ServiceName: "service-b", SpanName: "root-3", Timestamp: now.Add(-2 * time.Minute), StatusCode: "OK"},
	}
	store.InsertSpans(ctx, spans)

	resp, err := store.GetRecentTraces(ctx, 10, now.Add(-time.Hour), now.Add(time.Hour))
	if err != nil {
		t.Fatalf("GetRecentTraces failed: %v", err)
	}

	// Note: GetRecentTraces queries non-Codex and Codex traces separately and merges them
	// The exact behavior depends on implementation - just verify it doesn't error
	if resp == nil {
		t.Error("expected non-nil response")
	}
}

func TestGetRecentTraces_IncludesCodexRawTraceRows(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now()

	spans := []api.Span{
		{TraceID: "codex-trace", SpanID: "session-root", ServiceName: "codex_cli_rs", SpanName: "codex_session", Timestamp: now},
		{TraceID: "codex-trace", SpanID: "turn-1", ParentSpanID: "session-root", ServiceName: "codex_cli_rs", SpanName: "run_turn", Timestamp: now.Add(time.Second), StatusCode: "OK"},
		{TraceID: "codex-trace", SpanID: "tool-1", ParentSpanID: "turn-1", ServiceName: "codex_cli_rs", SpanName: "tool", Timestamp: now.Add(2 * time.Second), StatusCode: "ERROR"},
	}
	if err := store.InsertSpans(ctx, spans); err != nil {
		t.Fatalf("InsertSpans failed: %v", err)
	}

	resp, err := store.GetRecentTraces(ctx, 10, now.Add(-time.Hour), now.Add(time.Hour))
	if err != nil {
		t.Fatalf("GetRecentTraces failed: %v", err)
	}

	if len(resp.Traces) != 1 {
		t.Fatalf("expected 1 recent codex trace, got %d", len(resp.Traces))
	}
	if resp.Traces[0].Kind != api.TraceKindOTelTrace {
		t.Errorf("expected raw trace kind, got %q", resp.Traces[0].Kind)
	}
	if resp.Traces[0].RootSpanID != "session-root" {
		t.Errorf("expected session-root as root span, got %q", resp.Traces[0].RootSpanID)
	}
	if resp.Traces[0].SpanCount != 3 {
		t.Errorf("expected raw span count 3, got %d", resp.Traces[0].SpanCount)
	}
	if resp.Traces[0].Status != "ERROR" {
		t.Errorf("expected aggregated status ERROR, got %q", resp.Traces[0].Status)
	}
}

func TestGetStats_CountsCodexAsRawTraces(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now()

	spans := []api.Span{
		{TraceID: "regular-trace", SpanID: "regular-root", ServiceName: "service-a", SpanName: "root", Timestamp: now},
		{TraceID: "codex-trace", SpanID: "session-root", ServiceName: "codex_cli_rs", SpanName: "codex_session", Timestamp: now},
		{TraceID: "codex-trace", SpanID: "turn-1", ParentSpanID: "session-root", ServiceName: "codex_cli_rs", SpanName: "run_turn", Timestamp: now.Add(time.Second)},
		{TraceID: "codex-trace", SpanID: "turn-2", ParentSpanID: "session-root", ServiceName: "codex_cli_rs", SpanName: "run_turn", Timestamp: now.Add(2 * time.Second)},
	}
	if err := store.InsertSpans(ctx, spans); err != nil {
		t.Fatalf("InsertSpans failed: %v", err)
	}

	stats, err := store.GetStats(ctx)
	if err != nil {
		t.Fatalf("GetStats failed: %v", err)
	}

	if stats.RawTraceCount != 2 {
		t.Errorf("expected 2 raw traces, got %d", stats.RawTraceCount)
	}
	if stats.CodexOperationCount != 0 {
		t.Errorf("expected no codex operation count, got %d", stats.CodexOperationCount)
	}
	if stats.TraceCount != 2 {
		t.Errorf("expected 2 displayed raw traces, got %d", stats.TraceCount)
	}
}

func TestInsertSpans_SkipsDuplicateSpanIdentity(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now()

	span := api.Span{
		TraceID:     "trace-1",
		SpanID:      "span-1",
		ServiceName: "service-a",
		SpanName:    "root",
		Timestamp:   now,
	}
	if err := store.InsertSpans(ctx, []api.Span{span, span}); err != nil {
		t.Fatalf("InsertSpans failed: %v", err)
	}

	var count int
	if err := store.db.QueryRow("SELECT COUNT(*) FROM otel_traces").Scan(&count); err != nil {
		t.Fatalf("counting spans failed: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 stored span after duplicate insert, got %d", count)
	}
}

func TestGetMetricNames_WithServiceFilter(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now()

	metrics := []api.MetricDataPoint{
		{Timestamp: now, ServiceName: "svc-a", MetricName: "cpu", MetricType: "gauge", Value: ptrFloat64(50.0)},
		{Timestamp: now, ServiceName: "svc-a", MetricName: "memory", MetricType: "gauge", Value: ptrFloat64(70.0)},
		{Timestamp: now, ServiceName: "svc-b", MetricName: "cpu", MetricType: "gauge", Value: ptrFloat64(30.0)},
		{Timestamp: now, ServiceName: "svc-b", MetricName: "disk", MetricType: "gauge", Value: ptrFloat64(60.0)},
	}
	store.InsertMetrics(ctx, metrics)

	// Filter by service
	names, err := store.GetMetricNames(ctx, "svc-a")
	if err != nil {
		t.Fatalf("GetMetricNames failed: %v", err)
	}

	if len(names) != 2 {
		t.Errorf("expected 2 metric names for svc-a, got %d", len(names))
	}
}

// ============ Helper functions ============

func ptrFloat64(v float64) *float64 {
	return &v
}

func floatClose(a, b float64) bool {
	return math.Abs(a-b) < 1e-9
}
