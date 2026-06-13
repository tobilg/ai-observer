package storage

import (
	"context"
	"testing"
	"time"

	"github.com/tobilg/ai-observer/internal/api"
)

func TestInsertWatcherBatchInsertsTelemetryAndState(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()
	state := &ImportState{
		Source:       "codex",
		FilePath:     "/tmp/session.jsonl",
		ByteOffset:   100,
		MessageCount: 1,
		ParserState:  `{"messageIndex":1}`,
	}
	logs := []api.LogRecord{{
		Timestamp:   time.Now(),
		ServiceName: "codex_cli_rs",
		Body:        "hello",
		LogAttributes: map[string]string{
			"session.id": "session-1",
		},
	}}

	if err := store.InsertWatcherBatch(ctx, logs, nil, nil, state); err != nil {
		t.Fatalf("InsertWatcherBatch failed: %v", err)
	}

	var logCount int
	if err := store.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM otel_logs").Scan(&logCount); err != nil {
		t.Fatalf("counting logs: %v", err)
	}
	if logCount != 1 {
		t.Fatalf("log count = %d, want 1", logCount)
	}

	got, err := store.GetImportState(ctx, "codex", "/tmp/session.jsonl")
	if err != nil {
		t.Fatalf("GetImportState failed: %v", err)
	}
	if got == nil {
		t.Fatal("expected import state")
	}
	if got.ByteOffset != 100 || got.MessageCount != 1 || got.ParserState != `{"messageIndex":1}` {
		t.Fatalf("import state = %+v, want offset 100/message count 1/parser state", got)
	}
}
