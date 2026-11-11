package storage

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/tobilg/ai-observer/internal/api"
)

// A transcript needs all spans of traces with GenAI spans, whose parents
// decide what belongs to the session, but of other traces (Codex: up to
// 100,000 spans a turn) only the spans of the session
func TestSessionSpansReadsWholeTracesOnlyWithGenAISpans(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	ctx := context.Background()
	ts := time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC)
	span := func(service, trace, id, parent string, second int, attrs map[string]string) api.Span {
		return api.Span{Timestamp: ts.Add(time.Duration(second) * time.Second), TraceID: trace, SpanID: id, ParentSpanID: parent,
			ServiceName: service, SpanName: "span-" + id, SpanAttributes: attrs}
	}
	session := map[string]string{"session.id": "s1"}

	if err := store.InsertSpans(ctx, []api.Span{
		// A Codex turn: no GenAI spans
		span("codex-app-server", "turn", "turn-root", "", 0, nil),
		span("codex-app-server", "turn", "turn-tool", "turn-root", 1, session),
		span("codex-app-server", "turn", "turn-child", "turn-tool", 2, nil),
		// A Copilot trace: the chat span has no session ID of its own
		span("copilot-chat", "chat", "agent", "", 3, map[string]string{"session.id": "s1", "gen_ai.operation.name": "invoke_agent"}),
		span("copilot-chat", "chat", "step", "agent", 4, nil),
		span("copilot-chat", "chat", "reply", "step", 5, map[string]string{"gen_ai.operation.name": "chat"}),
		// Another session
		span("copilot-chat", "other", "other", "", 6, map[string]string{"session.id": "s2", "gen_ai.operation.name": "chat"}),
	}); err != nil {
		t.Fatal(err)
	}

	spans, err := store.SessionSpans(ctx, "s1")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, s := range spans {
		got = append(got, s.SpanID)
	}
	if want := []string{"turn-tool", "agent", "step", "reply"}; !slices.Equal(got, want) {
		t.Errorf("session spans = %v, want %v", got, want)
	}
	if spans[0].SpanAttributes["session.id"] != "s1" {
		t.Errorf("attributes of the session's span = %v", spans[0].SpanAttributes)
	}
}
