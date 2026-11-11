package deleter

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tobilg/ai-observer/internal/api"
	"github.com/tobilg/ai-observer/internal/storage"
)

// A chunked deletion deletes what one transaction would, also at the
// chunks' boundaries
func TestExecuteChunkedMatchesExecute(t *testing.T) {
	ctx := context.Background()
	start := time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC)
	fill := func(store *storage.DuckDBStore) {
		var logs []api.LogRecord
		var spans []api.Span
		for h := 0; h <= 72; h++ {
			at := start.Add(time.Duration(h) * time.Hour)
			logs = append(logs, api.LogRecord{Timestamp: at, ServiceName: "claude-code", Body: "x"})
			spans = append(spans, api.Span{Timestamp: at, TraceID: "t", SpanID: at.String(), SpanName: "s", ServiceName: "codex-app-server"})
		}
		if err := store.InsertLogs(ctx, logs); err != nil {
			t.Fatal(err)
		}
		if err := store.InsertSpans(ctx, spans); err != nil {
			t.Fatal(err)
		}
	}
	for _, opts := range []Options{
		{Scope: ScopeAll, From: start.Add(5 * time.Hour), To: start.Add(48 * time.Hour)},
		{Scope: ScopeLogs, From: start, To: start.Add(72 * time.Hour)},
		{Scope: ScopeTraces, From: start, To: start.Add(30 * time.Hour), Service: "codex"},
		{Scope: ScopeAll, From: start.Add(time.Hour), To: start.Add(time.Hour)},
	} {
		whole, cleanup := setupTestStore(t)
		chunked, cleanup2 := setupTestStore(t)
		fill(whole)
		fill(chunked)
		want, err := Execute(ctx, whole, opts)
		if err != nil {
			t.Fatal(err)
		}
		got, err := ExecuteChunked(ctx, chunked, opts, 24*time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		if *got != *want {
			t.Errorf("%+v: chunked %+v, whole %+v", opts, got, want)
		}
		cleanup()
		cleanup2()
	}
}

// After deleting, the command checkpoints and reports the free space
// inside the database file
func TestRunReportsFreeSpace(t *testing.T) {
	store, err := storage.NewDuckDBStore(filepath.Join(t.TempDir(), "db.duckdb"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	setupTestData(t, store)
	var out bytes.Buffer
	now := time.Now()
	if err := Run(context.Background(), store, Options{Scope: ScopeAll, From: now.Add(-2 * time.Hour), To: now.Add(time.Hour), SkipConfirm: true, Out: &out}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "free blocks that new data reuses") {
		t.Errorf("output does not report the free space:\n%s", out.String())
	}
	if size, err := store.DatabaseSize(context.Background()); err != nil || size.WALBytes != 0 {
		t.Errorf("no checkpoint after the deletion: %+v, %v", size, err)
	}
}
