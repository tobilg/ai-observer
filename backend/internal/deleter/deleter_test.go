package deleter

import (
	"context"
	"testing"
	"time"

	"github.com/tobilg/ai-observer/internal/api"
	"github.com/tobilg/ai-observer/internal/storage"
)

// setupTestStore creates an in-memory DuckDB store for testing
func setupTestStore(t *testing.T) (*storage.DuckDBStore, func()) {
	t.Helper()
	store, err := storage.NewDuckDBStore(":memory:")
	if err != nil {
		t.Fatalf("failed to create test store: %v", err)
	}
	return store, func() { store.Close() }
}

// setupTestData inserts sample data into the test store
func setupTestData(t *testing.T, store *storage.DuckDBStore) {
	t.Helper()
	ctx := context.Background()
	now := time.Now()

	// Insert logs
	logs := []api.LogRecord{
		{Timestamp: now.Add(-1 * time.Hour), ServiceName: "claude_code", SeverityText: "INFO", Body: "log 1"},
		{Timestamp: now.Add(-30 * time.Minute), ServiceName: "claude_code", SeverityText: "INFO", Body: "log 2"},
		{Timestamp: now, ServiceName: "codex_cli_rs", SeverityText: "WARN", Body: "log 3"},
	}
	if err := store.InsertLogs(ctx, logs); err != nil {
		t.Fatalf("failed to insert logs: %v", err)
	}

	// Insert metrics
	metrics := []api.MetricDataPoint{
		{Timestamp: now.Add(-1 * time.Hour), ServiceName: "claude_code", MetricName: "token.usage", MetricType: "gauge", Value: ptrFloat64(100.0)},
		{Timestamp: now, ServiceName: "claude_code", MetricName: "cost.usage", MetricType: "gauge", Value: ptrFloat64(0.05)},
		{Timestamp: now, ServiceName: "gemini_cli", MetricName: "session.count", MetricType: "gauge", Value: ptrFloat64(1.0)},
	}
	if err := store.InsertMetrics(ctx, metrics); err != nil {
		t.Fatalf("failed to insert metrics: %v", err)
	}

	// Insert traces
	spans := []api.Span{
		{Timestamp: now, TraceID: "trace1", SpanID: "span1", SpanName: "test_span", ServiceName: "claude_code"},
		{Timestamp: now, TraceID: "trace1", SpanID: "span2", SpanName: "child_span", ServiceName: "claude_code", ParentSpanID: "span1"},
	}
	if err := store.InsertSpans(ctx, spans); err != nil {
		t.Fatalf("failed to insert spans: %v", err)
	}
}

func ptrFloat64(v float64) *float64 {
	return &v
}

func TestParseScope(t *testing.T) {
	tests := []struct {
		input    string
		expected Scope
		wantErr  bool
	}{
		{"logs", ScopeLogs, false},
		{"metrics", ScopeMetrics, false},
		{"traces", ScopeTraces, false},
		{"all", ScopeAll, false},
		{"LOGS", ScopeLogs, false},   // case insensitive
		{"Traces", ScopeTraces, false}, // case insensitive
		{"invalid", "", true},
		{"", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result, err := ParseScope(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error for input %q, got nil", tt.input)
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error for input %q: %v", tt.input, err)
				return
			}
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestSummaryIsEmpty(t *testing.T) {
	tests := []struct {
		name     string
		summary  Summary
		expected bool
	}{
		{"empty", Summary{}, true},
		{"has logs", Summary{LogCount: 1}, false},
		{"has metrics", Summary{MetricCount: 1}, false},
		{"has traces", Summary{TraceCount: 1}, false},
		{"has spans", Summary{SpanCount: 1}, false},
		{"has all", Summary{LogCount: 1, MetricCount: 2, TraceCount: 3, SpanCount: 4}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.summary.IsEmpty()
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestPreviewLogs(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	setupTestData(t, store)

	ctx := context.Background()
	now := time.Now()

	opts := Options{
		Scope: ScopeLogs,
		From:  now.Add(-2 * time.Hour),
		To:    now.Add(1 * time.Hour),
	}

	summary, err := Preview(ctx, store, opts)
	if err != nil {
		t.Fatalf("Preview failed: %v", err)
	}

	if summary.LogCount != 3 {
		t.Errorf("expected 3 logs, got %d", summary.LogCount)
	}
	if summary.MetricCount != 0 {
		t.Errorf("expected 0 metrics, got %d", summary.MetricCount)
	}
}

func TestPreviewMetrics(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	setupTestData(t, store)

	ctx := context.Background()
	now := time.Now()

	opts := Options{
		Scope: ScopeMetrics,
		From:  now.Add(-2 * time.Hour),
		To:    now.Add(1 * time.Hour),
	}

	summary, err := Preview(ctx, store, opts)
	if err != nil {
		t.Fatalf("Preview failed: %v", err)
	}

	if summary.MetricCount != 3 {
		t.Errorf("expected 3 metrics, got %d", summary.MetricCount)
	}
	if summary.LogCount != 0 {
		t.Errorf("expected 0 logs, got %d", summary.LogCount)
	}
}

func TestPreviewTraces(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	setupTestData(t, store)

	ctx := context.Background()
	now := time.Now()

	opts := Options{
		Scope: ScopeTraces,
		From:  now.Add(-2 * time.Hour),
		To:    now.Add(1 * time.Hour),
	}

	summary, err := Preview(ctx, store, opts)
	if err != nil {
		t.Fatalf("Preview failed: %v", err)
	}

	if summary.SpanCount != 2 {
		t.Errorf("expected 2 spans, got %d", summary.SpanCount)
	}
}

func TestPreviewAll(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	setupTestData(t, store)

	ctx := context.Background()
	now := time.Now()

	opts := Options{
		Scope: ScopeAll,
		From:  now.Add(-2 * time.Hour),
		To:    now.Add(1 * time.Hour),
	}

	summary, err := Preview(ctx, store, opts)
	if err != nil {
		t.Fatalf("Preview failed: %v", err)
	}

	if summary.LogCount != 3 {
		t.Errorf("expected 3 logs, got %d", summary.LogCount)
	}
	if summary.MetricCount != 3 {
		t.Errorf("expected 3 metrics, got %d", summary.MetricCount)
	}
	if summary.SpanCount != 2 {
		t.Errorf("expected 2 spans, got %d", summary.SpanCount)
	}
}

func TestPreviewWithServiceFilter(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	setupTestData(t, store)

	ctx := context.Background()
	now := time.Now()

	opts := Options{
		Scope:   ScopeAll,
		From:    now.Add(-2 * time.Hour),
		To:      now.Add(1 * time.Hour),
		Service: "claude_code",
	}

	summary, err := Preview(ctx, store, opts)
	if err != nil {
		t.Fatalf("Preview failed: %v", err)
	}

	if summary.LogCount != 2 {
		t.Errorf("expected 2 logs for claude_code, got %d", summary.LogCount)
	}
	if summary.MetricCount != 2 {
		t.Errorf("expected 2 metrics for claude_code, got %d", summary.MetricCount)
	}
	if summary.SpanCount != 2 {
		t.Errorf("expected 2 spans for claude_code, got %d", summary.SpanCount)
	}
}

func TestPreviewUnknownScope(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now()

	opts := Options{
		Scope: Scope("unknown"),
		From:  now.Add(-2 * time.Hour),
		To:    now.Add(1 * time.Hour),
	}

	_, err := Preview(ctx, store, opts)
	if err == nil {
		t.Error("expected error for unknown scope")
	}
}

func TestExecuteLogs(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	setupTestData(t, store)

	ctx := context.Background()
	now := time.Now()

	opts := Options{
		Scope: ScopeLogs,
		From:  now.Add(-2 * time.Hour),
		To:    now.Add(1 * time.Hour),
	}

	summary, err := Execute(ctx, store, opts)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if summary.LogCount != 3 {
		t.Errorf("expected 3 logs deleted, got %d", summary.LogCount)
	}

	// Verify logs are actually deleted
	previewSummary, err := Preview(ctx, store, opts)
	if err != nil {
		t.Fatalf("Preview after delete failed: %v", err)
	}
	if previewSummary.LogCount != 0 {
		t.Errorf("expected 0 logs after delete, got %d", previewSummary.LogCount)
	}
}

func TestExecuteMetrics(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	setupTestData(t, store)

	ctx := context.Background()
	now := time.Now()

	opts := Options{
		Scope: ScopeMetrics,
		From:  now.Add(-2 * time.Hour),
		To:    now.Add(1 * time.Hour),
	}

	summary, err := Execute(ctx, store, opts)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if summary.MetricCount != 3 {
		t.Errorf("expected 3 metrics deleted, got %d", summary.MetricCount)
	}

	// Verify metrics are actually deleted
	previewSummary, err := Preview(ctx, store, opts)
	if err != nil {
		t.Fatalf("Preview after delete failed: %v", err)
	}
	if previewSummary.MetricCount != 0 {
		t.Errorf("expected 0 metrics after delete, got %d", previewSummary.MetricCount)
	}
}

func TestExecuteTraces(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	setupTestData(t, store)

	ctx := context.Background()
	now := time.Now()

	opts := Options{
		Scope: ScopeTraces,
		From:  now.Add(-2 * time.Hour),
		To:    now.Add(1 * time.Hour),
	}

	summary, err := Execute(ctx, store, opts)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if summary.SpanCount != 2 {
		t.Errorf("expected 2 spans deleted, got %d", summary.SpanCount)
	}
}

func TestExecuteAll(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	setupTestData(t, store)

	ctx := context.Background()
	now := time.Now()

	opts := Options{
		Scope: ScopeAll,
		From:  now.Add(-2 * time.Hour),
		To:    now.Add(1 * time.Hour),
	}

	summary, err := Execute(ctx, store, opts)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if summary.LogCount != 3 {
		t.Errorf("expected 3 logs deleted, got %d", summary.LogCount)
	}
	if summary.MetricCount != 3 {
		t.Errorf("expected 3 metrics deleted, got %d", summary.MetricCount)
	}
	if summary.SpanCount != 2 {
		t.Errorf("expected 2 spans deleted, got %d", summary.SpanCount)
	}
}

func TestExecuteWithServiceFilter(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	setupTestData(t, store)

	ctx := context.Background()
	now := time.Now()

	opts := Options{
		Scope:   ScopeLogs,
		From:    now.Add(-2 * time.Hour),
		To:      now.Add(1 * time.Hour),
		Service: "claude_code",
	}

	summary, err := Execute(ctx, store, opts)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	// Should only delete 2 logs (claude_code), not the codex one
	if summary.LogCount != 2 {
		t.Errorf("expected 2 logs deleted for claude_code, got %d", summary.LogCount)
	}

	// Verify the codex log still exists
	allOpts := Options{
		Scope: ScopeLogs,
		From:  now.Add(-2 * time.Hour),
		To:    now.Add(1 * time.Hour),
	}
	previewSummary, err := Preview(ctx, store, allOpts)
	if err != nil {
		t.Fatalf("Preview after delete failed: %v", err)
	}
	if previewSummary.LogCount != 1 {
		t.Errorf("expected 1 log remaining (codex), got %d", previewSummary.LogCount)
	}
}

func TestExecuteUnknownScope(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now()

	opts := Options{
		Scope: Scope("unknown"),
		From:  now.Add(-2 * time.Hour),
		To:    now.Add(1 * time.Hour),
	}

	_, err := Execute(ctx, store, opts)
	if err == nil {
		t.Error("expected error for unknown scope")
	}
}

func TestPrintSummary(t *testing.T) {
	summary := &Summary{
		LogCount:    100,
		MetricCount: 200,
		TraceCount:  50,
		SpanCount:   300,
	}

	now := time.Now()
	t.Run("logs scope", func(t *testing.T) {
		opts := Options{
			Scope: ScopeLogs,
			From:  now.Add(-24 * time.Hour),
			To:    now,
		}
		// Just verify it doesn't panic
		PrintSummary(summary, opts)
	})

	t.Run("metrics scope", func(t *testing.T) {
		opts := Options{
			Scope: ScopeMetrics,
			From:  now.Add(-24 * time.Hour),
			To:    now,
		}
		PrintSummary(summary, opts)
	})

	t.Run("traces scope", func(t *testing.T) {
		opts := Options{
			Scope: ScopeTraces,
			From:  now.Add(-24 * time.Hour),
			To:    now,
		}
		PrintSummary(summary, opts)
	})

	t.Run("all scope", func(t *testing.T) {
		opts := Options{
			Scope: ScopeAll,
			From:  now.Add(-24 * time.Hour),
			To:    now,
		}
		PrintSummary(summary, opts)
	})

	t.Run("with service", func(t *testing.T) {
		opts := Options{
			Scope:   ScopeAll,
			From:    now.Add(-24 * time.Hour),
			To:      now,
			Service: "claude_code",
		}
		PrintSummary(summary, opts)
	})
}

func TestPrintResult(t *testing.T) {
	summary := &Summary{
		LogCount:    100,
		MetricCount: 200,
		SpanCount:   300,
	}

	now := time.Now()
	t.Run("logs scope", func(t *testing.T) {
		opts := Options{
			Scope: ScopeLogs,
			From:  now.Add(-24 * time.Hour),
			To:    now,
		}
		PrintResult(summary, opts)
	})

	t.Run("metrics scope", func(t *testing.T) {
		opts := Options{
			Scope: ScopeMetrics,
			From:  now.Add(-24 * time.Hour),
			To:    now,
		}
		PrintResult(summary, opts)
	})

	t.Run("traces scope", func(t *testing.T) {
		opts := Options{
			Scope: ScopeTraces,
			From:  now.Add(-24 * time.Hour),
			To:    now,
		}
		PrintResult(summary, opts)
	})

	t.Run("all scope", func(t *testing.T) {
		opts := Options{
			Scope: ScopeAll,
			From:  now.Add(-24 * time.Hour),
			To:    now,
		}
		PrintResult(summary, opts)
	})
}

func TestRunWithSkipConfirm(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	setupTestData(t, store)

	ctx := context.Background()
	now := time.Now()

	opts := Options{
		Scope:       ScopeLogs,
		From:        now.Add(-2 * time.Hour),
		To:          now.Add(1 * time.Hour),
		SkipConfirm: true,
	}

	err := Run(ctx, store, opts)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	// Verify logs are deleted
	summary, err := Preview(ctx, store, opts)
	if err != nil {
		t.Fatalf("Preview after Run failed: %v", err)
	}
	if summary.LogCount != 0 {
		t.Errorf("expected 0 logs after Run, got %d", summary.LogCount)
	}
}

func TestRunEmptyDatabase(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	// Don't insert any data

	ctx := context.Background()
	now := time.Now()

	opts := Options{
		Scope:       ScopeAll,
		From:        now.Add(-2 * time.Hour),
		To:          now.Add(1 * time.Hour),
		SkipConfirm: true,
	}

	// Should not error on empty database
	err := Run(ctx, store, opts)
	if err != nil {
		t.Fatalf("Run failed on empty database: %v", err)
	}
}

func TestRunAllScopes(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	setupTestData(t, store)

	ctx := context.Background()
	now := time.Now()

	opts := Options{
		Scope:       ScopeAll,
		From:        now.Add(-2 * time.Hour),
		To:          now.Add(1 * time.Hour),
		SkipConfirm: true,
	}

	err := Run(ctx, store, opts)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	// Verify all data is deleted
	summary, err := Preview(ctx, store, opts)
	if err != nil {
		t.Fatalf("Preview after Run failed: %v", err)
	}
	if !summary.IsEmpty() {
		t.Errorf("expected empty database after deleting all, got logs=%d, metrics=%d, spans=%d",
			summary.LogCount, summary.MetricCount, summary.SpanCount)
	}
}

func TestConfirmDelete(t *testing.T) {
	// ConfirmDelete reads from stdin, which is tricky to test without mocking
	// For now, we just ensure the function exists and can be called
	// In real usage, it prompts for user input
	// Testing this would require mocking os.Stdin
	t.Skip("ConfirmDelete requires stdin mocking")
}

func TestRunWithUnknownScope(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now()

	opts := Options{
		Scope:       Scope("invalid"),
		From:        now.Add(-2 * time.Hour),
		To:          now.Add(1 * time.Hour),
		SkipConfirm: true,
	}

	err := Run(ctx, store, opts)
	if err == nil {
		t.Error("expected error for unknown scope in Run")
	}
}
