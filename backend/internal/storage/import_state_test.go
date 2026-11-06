package storage

import (
	"context"
	"testing"
	"time"

	"github.com/tobilg/ai-observer/internal/api"
)

func importedFileFixture() ([]api.LogRecord, []api.Span, *ImportState) {
	now := time.Now()
	logs := []api.LogRecord{{
		Timestamp:     now,
		ServiceName:   "claude-code",
		Body:          "hello",
		LogAttributes: map[string]string{"session.id": "session-1"},
	}}
	spans := []api.Span{{
		Timestamp:   now,
		TraceID:     "trace-1",
		SpanID:      "span-1",
		SpanName:    "turn",
		ServiceName: "claude-code",
	}}
	state := &ImportState{
		Source:      "claude-code",
		FilePath:    "/tmp/session.jsonl",
		FileHash:    "abc",
		ImportedAt:  now,
		RecordCount: 1,
	}
	return logs, spans, state
}

func TestInsertImportedFileInsertsTelemetryAndState(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	ctx := context.Background()

	logs, spans, state := importedFileFixture()
	if err := store.InsertImportedFile(ctx, logs, nil, spans, state, nil); err != nil {
		t.Fatalf("InsertImportedFile failed: %v", err)
	}

	var logCount, spanCount int
	if err := store.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM otel_logs").Scan(&logCount); err != nil {
		t.Fatalf("counting logs: %v", err)
	}
	if err := store.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM otel_traces").Scan(&spanCount); err != nil {
		t.Fatalf("counting spans: %v", err)
	}
	if logCount != 1 || spanCount != 1 {
		t.Errorf("expected 1 log and 1 span, got %d logs and %d spans", logCount, spanCount)
	}

	got, err := store.GetImportState(ctx, state.Source, state.FilePath)
	if err != nil || got == nil {
		t.Fatalf("expected import state to be recorded, got %v (err %v)", got, err)
	}
	if got.FileHash != state.FileHash || got.RecordCount != state.RecordCount {
		t.Errorf("unexpected import state: %+v", got)
	}
}

func TestInsertImportedFileRollsBackOnFailure(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	ctx := context.Background()

	// Make the span insert fail after logs have been written in the transaction.
	if _, err := store.db.ExecContext(ctx, "DROP TABLE otel_traces"); err != nil {
		t.Fatalf("dropping traces table: %v", err)
	}

	logs, spans, state := importedFileFixture()
	if err := store.InsertImportedFile(ctx, logs, nil, spans, state, nil); err == nil {
		t.Fatal("expected InsertImportedFile to fail when spans cannot be stored")
	}

	var logCount int
	if err := store.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM otel_logs").Scan(&logCount); err != nil {
		t.Fatalf("counting logs: %v", err)
	}
	if logCount != 0 {
		t.Errorf("expected logs to be rolled back, found %d", logCount)
	}

	got, err := store.GetImportState(ctx, state.Source, state.FilePath)
	if err != nil {
		t.Fatalf("GetImportState failed: %v", err)
	}
	if got != nil {
		t.Errorf("expected no import state after a failed import, got %+v", got)
	}
}
