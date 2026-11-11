package traceview

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/tobilg/ai-observer/internal/api"
	"github.com/tobilg/ai-observer/internal/storage"
)

var start = time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC)

func span(id, parent, turn string, second int) api.Span {
	s := api.Span{
		Timestamp: start.Add(time.Duration(second) * time.Second), TraceID: "session-trace", SpanID: id,
		ParentSpanID: parent, SpanName: "op-" + id, ServiceName: "codex_cli_rs",
	}
	if turn != "" {
		s.SpanAttributes = map[string]string{"turn_id": turn}
	}
	return s
}

func ids(spans []api.Span) []string {
	out := make([]string, len(spans))
	for i, s := range spans {
		out[i] = s.SpanID
	}
	return out
}

// Codex records a session as one trace: session -> turns -> tools
func codexSession() []api.Span {
	return []api.Span{
		span("session", "", "", 0),
		span("turn-1", "session", "t1", 1),
		span("tool-1a", "turn-1", "", 3),
		span("tool-1b", "turn-1", "", 2),
		// A span of turn 1 that hangs off the session, and its child
		span("stream-1", "session", "t1", 4),
		span("chunk-1", "stream-1", "", 5),
		span("turn-2", "session", "t2", 6),
		span("tool-2", "turn-2", "", 7),
		span("untagged", "session", "", 8),
		span("untagged-child", "untagged", "", 9),
	}
}

func TestCodexOperationGroupsATurn(t *testing.T) {
	spans := codexSession()
	got := ids(codexOperationSpans(spans[1], spans))
	want := []string{"turn-1", "tool-1b", "tool-1a", "stream-1", "chunk-1"}
	if len(got) != len(want) {
		t.Fatalf("spans = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("spans = %v, want %v (the turn's spans and descendants, oldest first)", got, want)
		}
	}
}

func TestCodexOperationWithoutTurnIsASubtree(t *testing.T) {
	spans := codexSession()
	got := ids(codexOperationSpans(spans[8], spans))
	if len(got) != 2 || got[0] != "untagged" || got[1] != "untagged-child" {
		t.Errorf("spans = %v, want the untagged span and its child", got)
	}
}

// Against the storage reads: kinds, missing roots and Copilot-style span IDs
func TestLoad(t *testing.T) {
	ctx := context.Background()
	store, err := storage.NewDuckDBStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	spans := codexSession()
	other := api.Span{Timestamp: start, TraceID: "otel-trace", SpanID: "root", SpanName: "chat", ServiceName: "copilot-chat"}
	if err := store.InsertSpans(ctx, append(spans, other)); err != nil {
		t.Fatal(err)
	}

	all := storage.Page{Limit: 1000}
	operation, err := Load(ctx, store, "turn-2", api.TraceKindCodexOperation, all)
	if err != nil {
		t.Fatal(err)
	}
	if got := ids(operation.Spans); len(got) != 2 || got[0] != "turn-2" || got[1] != "tool-2" {
		t.Errorf("codex operation = %v, want turn-2 and tool-2", got)
	}
	if o := operation.Trace; o == nil || o.ID != "turn-2" || o.Kind != api.TraceKindCodexOperation || o.RootSpan != "op-turn-2" ||
		o.TraceID != "session-trace" || o.SpanCount != 2 || o.GroupLevel != api.TraceGroupLevelCodexTurn ||
		!o.StartTime.Equal(start.Add(6*time.Second)) || o.Duration != int64(time.Second) || operation.Total != 2 {
		t.Errorf("codex operation overview = %+v, total %d", o, operation.Total)
	}

	// Operations are paged like traces
	first, err := Load(ctx, store, "turn-1", api.TraceKindCodexOperation, storage.Page{Limit: 3})
	if err != nil {
		t.Fatal(err)
	}
	rest, err := Load(ctx, store, "turn-1", api.TraceKindCodexOperation, storage.Page{Limit: 3, Cursor: first.NextCursor})
	if err != nil {
		t.Fatal(err)
	}
	if got := append(ids(first.Spans), ids(rest.Spans)...); len(got) != 5 || !first.HasMore || rest.HasMore || first.Total != 5 ||
		got[0] != "turn-1" || got[3] != "stream-1" || got[4] != "chunk-1" || first.Trace.SpanCount != 5 {
		t.Errorf("paged operation = %v (hasMore %v, %v; total %d)", got, first.HasMore, rest.HasMore, first.Total)
	}

	whole, err := Load(ctx, store, "session-trace", api.TraceKindOTelTrace, all)
	if err != nil {
		t.Fatal(err)
	}
	if len(whole.Spans) != len(spans) || whole.Trace.SpanCount != len(spans) || whole.Trace.RootSpan != "op-session" {
		t.Errorf("whole trace = %d spans, overview %+v; want %d", len(whole.Spans), whole.Trace, len(spans))
	}

	// A root span ID finds its trace
	bySpan, err := Load(ctx, store, "root", api.TraceKindOTelTrace, all)
	if err != nil {
		t.Fatal(err)
	}
	if len(bySpan.Spans) != 1 || bySpan.Spans[0].TraceID != "otel-trace" || bySpan.Trace.TraceID != "otel-trace" {
		t.Errorf("trace by span ID = %+v", bySpan)
	}

	// Codex operations are only Codex spans
	for _, id := range []string{"no-such-span", "root"} {
		missing, err := Load(ctx, store, id, api.TraceKindCodexOperation, all)
		if err != nil || missing != nil {
			t.Errorf("codex operation %q = %v, %v; want none", id, missing, err)
		}
	}
	if missing, err := Load(ctx, store, "no-such-trace", api.TraceKindOTelTrace, all); err != nil || missing != nil {
		t.Errorf("unknown trace = %v, %v; want none", missing, err)
	}

	if _, err := Load(ctx, store, "session-trace", "bogus", all); !errors.Is(err, ErrUnsupportedKind) {
		t.Errorf("unknown kind: error = %v, want ErrUnsupportedKind", err)
	}
}
