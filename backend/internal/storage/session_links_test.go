package storage

import (
	"context"
	"testing"
	"time"

	"github.com/tobilg/ai-observer/internal/api"
)

// Logs, spans and traces name their session, so lists can link to it
func TestRecordsCarrySessionID(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	logs := []api.LogRecord{
		{Timestamp: now.Add(-3 * time.Minute), ServiceName: "codex-app-server", Body: "with session",
			LogAttributes: map[string]string{"conversation.id": "conv-1"}},
		{Timestamp: now.Add(-2 * time.Minute), ServiceName: "codex-app-server", Body: "without session"},
	}
	span := func(trace, id, parent string, attrs map[string]string) api.Span {
		return api.Span{Timestamp: now.Add(-time.Minute), TraceID: trace, SpanID: id, ParentSpanID: parent,
			SpanName: "op", ServiceName: "claude-code", SpanAttributes: attrs}
	}
	spans := []api.Span{
		// One session, on some of the trace's spans
		span("shared", "root", "", map[string]string{"session.id": "s1"}),
		span("shared", "child", "root", nil),
		span("shared", "child2", "root", map[string]string{"session.id": "s1"}),
		// Two sessions: the trace names none
		span("mixed", "m1", "", map[string]string{"session.id": "s1"}),
		span("mixed", "m2", "m1", map[string]string{"session.id": "s2"}),
		// No session
		span("none", "n1", "", nil),
	}
	if err := store.InsertTelemetry(ctx, logs, nil, spans); err != nil {
		t.Fatal(err)
	}

	logResp, err := store.QueryLogs(ctx, "", "", "", "", now.Add(-time.Hour), now, Page{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	for _, log := range logResp.Logs {
		want := map[string]string{"with session": "conv-1", "without session": ""}[log.Body]
		if log.SessionID != want {
			t.Errorf("log %q: session %q, want %q", log.Body, log.SessionID, want)
		}
	}

	traces, err := store.QueryTraces(ctx, TraceFilter{}, now.Add(-time.Hour), now, Page{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"shared": "s1", "mixed": "", "none": ""}
	if len(traces.Traces) != len(want) {
		t.Fatalf("traces = %+v", traces.Traces)
	}
	for _, trace := range traces.Traces {
		if trace.SessionID != want[trace.TraceID] {
			t.Errorf("trace %s: session %q, want %q", trace.TraceID, trace.SessionID, want[trace.TraceID])
		}
	}
	recent, err := store.GetRecentTraces(ctx, 10, now.Add(-time.Hour), now)
	if err != nil || len(recent.Traces) != 3 {
		t.Fatalf("recent traces = %+v, %v", recent, err)
	}

	page, err := store.TraceSpansPage(ctx, "shared", Page{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if page.Trace.SessionID != "s1" {
		t.Errorf("trace overview session = %q, want s1", page.Trace.SessionID)
	}
	spanSessions := map[string]string{}
	for _, s := range page.Spans {
		spanSessions[s.SpanID] = s.SessionID
	}
	if spanSessions["root"] != "s1" || spanSessions["child"] != "" || spanSessions["child2"] != "s1" {
		t.Errorf("span sessions = %v", spanSessions)
	}
	if mixed, err := store.TraceSpansPage(ctx, "mixed", Page{Limit: 10}); err != nil || mixed.Trace.SessionID != "" {
		t.Errorf("mixed trace overview = %+v, %v; want no session", mixed, err)
	}

	sessionSpans, err := store.SessionSpans(ctx, "s2")
	if err != nil || len(sessionSpans) != 1 || sessionSpans[0].SessionID != "s2" {
		t.Errorf("session spans = %+v, %v", sessionSpans, err)
	}
}
