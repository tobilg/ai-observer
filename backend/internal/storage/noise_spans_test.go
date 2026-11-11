package storage

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/tobilg/ai-observer/internal/api"
)

func noDerivedMetrics([]api.Span) []api.MetricDataPoint { return nil }

// codexSpan is a span of Codex's app server in trace "t1"
func codexSpan(id, parent, name string) api.Span {
	return api.Span{
		Timestamp: time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC), TraceID: "t1", SpanID: id, ParentSpanID: parent,
		ServiceName: "codex-app-server", SpanName: name,
	}
}

func storedSpanIDs(t *testing.T, store *DuckDBStore) []string {
	t.Helper()
	rows, err := store.DB().Query("SELECT SpanId FROM otel_traces ORDER BY SpanId")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	return ids
}

// Codex's app server sends a span per streamed response event; they are
// stored only where a stored span hangs below them
func TestInsertTracesDropsNoiseSpans(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	ctx := context.Background()

	// Children end, and so are listed, before their parents
	stored, _, err := store.InsertTraces(ctx, []api.Span{
		codexSpan("recv-1", "resp-1", "receiving"),
		codexSpan("resp-1", "stream", "handle_responses"),
		codexSpan("recv-2", "resp-2", "receiving"),
		codexSpan("tool", "done", "build_tool_call"),
		codexSpan("done", "resp-2", "handle_output_item_done"),
		codexSpan("resp-2", "stream", "handle_responses"),
		codexSpan("append-inner", "append-outer", "append_items"),
		codexSpan("append-outer", "persist", "append_items"),
		codexSpan("persist", "stream", "persist_rollout_items"),
		codexSpan("stream", "turn", "receiving_stream"),
		codexSpan("turn", "", "turn/start"),
		// The same span names of another tool are not noise
		{Timestamp: time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC), TraceID: "t2", SpanID: "other", ServiceName: "copilot-chat", SpanName: "receiving"},
	}, noDerivedMetrics)
	if err != nil {
		t.Fatal(err)
	}

	// resp-2 stays as the parent of the tool call
	want := []string{"done", "other", "resp-2", "stream", "tool", "turn"}
	if got := storedSpanIDs(t, store); !slices.Equal(got, want) {
		t.Errorf("stored spans = %v, want %v", got, want)
	}
	if len(stored) != len(want) {
		t.Errorf("InsertTraces returned %d spans, want the %d stored ones", len(stored), len(want))
	}
}

// A noise span whose child was stored by an earlier export is kept, and so
// are its noise ancestors in the same export
func TestNoiseSpanWithStoredChildIsKept(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	ctx := context.Background()

	if _, _, err := store.InsertTraces(ctx, []api.Span{codexSpan("done", "resp", "handle_output_item_done")}, noDerivedMetrics); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.InsertTraces(ctx, []api.Span{
		codexSpan("outer", "turn", "persist_rollout_items"),
		codexSpan("resp", "outer", "handle_responses"),
		codexSpan("recv", "resp", "receiving"),
		// Same span ID in another trace: no parent of a stored span
		{Timestamp: time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC), TraceID: "t2", SpanID: "resp", ServiceName: "codex-app-server", SpanName: "handle_responses"},
	}, noDerivedMetrics); err != nil {
		t.Fatal(err)
	}

	rows := countRows(t, store, "SELECT COUNT(*) FROM otel_traces")
	if got := storedSpanIDs(t, store); !slices.Equal(got, []string{"done", "outer", "resp"}) || rows != 3 {
		t.Errorf("stored spans = %v (%d rows), want done, outer and resp of trace t1", got, rows)
	}
}

func TestFailedNoiseSpansAreKept(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	failed := codexSpan("failed", "turn", "fs.get_metadata")
	failed.StatusCode = "ERROR"
	if _, _, err := store.InsertTraces(context.Background(), []api.Span{failed, codexSpan("ok", "turn", "fs.get_metadata")}, noDerivedMetrics); err != nil {
		t.Fatal(err)
	}
	if got := storedSpanIDs(t, store); !slices.Equal(got, []string{"failed"}) {
		t.Errorf("stored spans = %v, want only the failed one", got)
	}
}

// AI_OBSERVER_STORE_ALL_SPANS
func TestStoreAllSpansKeepsNoiseSpans(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	store.SetStoreAllSpans(true)

	if _, _, err := store.InsertTraces(context.Background(), []api.Span{
		codexSpan("recv", "resp", "receiving"),
		codexSpan("resp", "stream", "handle_responses"),
	}, noDerivedMetrics); err != nil {
		t.Fatal(err)
	}
	if got := storedSpanIDs(t, store); len(got) != 2 {
		t.Errorf("stored spans = %v, want both", got)
	}
}
