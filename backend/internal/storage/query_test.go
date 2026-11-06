package storage

import (
	"context"
	"testing"
	"time"

	"github.com/tobilg/ai-observer/internal/api"
)

// A session shows the model of its latest record, not the alphabetically
// largest one
func TestQuerySessionsShowsLatestModel(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	logs := []api.LogRecord{
		{Timestamp: now.Add(-3 * time.Minute), ServiceName: "claude-code", LogAttributes: map[string]string{"session.id": "s1", "model": "b-model"}},
		{Timestamp: now.Add(-2 * time.Minute), ServiceName: "claude-code", LogAttributes: map[string]string{"session.id": "s1", "model": "a-model"}},
		{Timestamp: now.Add(-1 * time.Minute), ServiceName: "claude-code", LogAttributes: map[string]string{"session.id": "s1"}},
	}
	if err := store.InsertLogs(ctx, logs); err != nil {
		t.Fatal(err)
	}

	resp, err := store.QuerySessions(ctx, "", now.Add(-time.Hour), now, Page{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Sessions) != 1 {
		t.Fatalf("sessions = %d, want 1", len(resp.Sessions))
	}
	if got := resp.Sessions[0].Model; got != "a-model" {
		t.Errorf("model = %q, want a-model (the latest)", got)
	}
}

// % and _ in a search match themselves, not any characters
func TestSearchTreatsWildcardsLiterally(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	logs := []api.LogRecord{
		{Timestamp: now.Add(-time.Minute), ServiceName: "svc", Body: "coverage 100% reached"},
		{Timestamp: now.Add(-time.Minute), ServiceName: "svc", Body: "coverage 1000 reached"},
		{Timestamp: now.Add(-time.Minute), ServiceName: "svc", Body: `path C:\tmp\file_name`},
		{Timestamp: now.Add(-time.Minute), ServiceName: "svc", Body: "file-name"},
	}
	if err := store.InsertLogs(ctx, logs); err != nil {
		t.Fatal(err)
	}
	spans := []api.Span{
		{Timestamp: now.Add(-time.Minute), TraceID: "t1", SpanID: "s1", SpanName: "load_config", ServiceName: "svc"},
		{Timestamp: now.Add(-time.Minute), TraceID: "t2", SpanID: "s2", SpanName: "loadXconfig", ServiceName: "svc"},
	}
	if err := store.InsertSpans(ctx, spans); err != nil {
		t.Fatal(err)
	}

	for search, want := range map[string]int{"100%": 1, "file_name": 1, `tmp\file`: 1, "coverage": 2} {
		resp, err := store.QueryLogs(ctx, "", "", "", search, now.Add(-time.Hour), now, Page{Limit: 10})
		if err != nil {
			t.Fatal(err)
		}
		if len(resp.Logs) != want {
			t.Errorf("logs matching %q = %d, want %d", search, len(resp.Logs), want)
		}
	}

	resp, err := store.QueryTraces(ctx, "", "load_config", now.Add(-time.Hour), now, Page{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Traces) != 1 || resp.Traces[0].TraceID != "t1" {
		t.Errorf("traces matching load_config = %+v, want only t1", resp.Traces)
	}
}

func TestGetLogLevelsAndStatsInRange(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	logs := []api.LogRecord{
		{Timestamp: now.Add(-48 * time.Hour), ServiceName: "svc", SeverityText: "ERROR", Body: "old"},
		{Timestamp: now.Add(-time.Minute), ServiceName: "svc", SeverityText: "INFO", Body: "new"},
	}
	if err := store.InsertLogs(ctx, logs); err != nil {
		t.Fatal(err)
	}
	recent := TimeRange{From: now.Add(-time.Hour), To: now}

	levels, err := store.GetLogLevelsInRange(ctx, recent)
	if err != nil {
		t.Fatal(err)
	}
	if levels["INFO"] != 1 || levels["ERROR"] != 0 {
		t.Errorf("levels in the last hour = %v, want only INFO 1", levels)
	}

	stats, err := store.GetStatsInRange(ctx, recent)
	if err != nil {
		t.Fatal(err)
	}
	if stats.LogCount != 1 {
		t.Errorf("log count in the last hour = %d, want 1", stats.LogCount)
	}
	if all, _ := store.GetStats(ctx); all.LogCount != 2 {
		t.Errorf("all-time log count = %d, want 2", all.LogCount)
	}
}

// The session list pages through the sessions active in the range, but shows
// each one's whole extent: a session that started before the range keeps its
// start, all of its messages and its latest model
func TestQuerySessionsShowsWholeSession(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	attrs := func(model string) map[string]string {
		a := map[string]string{"session.id": "s1"}
		if model != "" {
			a["model"] = model
		}
		return a
	}
	logs := []api.LogRecord{
		{Timestamp: now.Add(-50 * time.Hour), ServiceName: "claude-code", LogAttributes: attrs("early-model")},
		{Timestamp: now.Add(-49 * time.Hour), ServiceName: "claude-code", LogAttributes: attrs("latest-model")},
		{Timestamp: now.Add(-10 * time.Minute), ServiceName: "claude-code", LogAttributes: attrs("")},
		// The same session ID from another service is another session
		{Timestamp: now.Add(-5 * time.Minute), ServiceName: "gemini_cli", LogAttributes: attrs("gemini-model")},
		// Not active in the range
		{Timestamp: now.Add(-30 * time.Hour), ServiceName: "claude-code", LogAttributes: map[string]string{"session.id": "s2"}},
	}
	if err := store.InsertLogs(ctx, logs); err != nil {
		t.Fatal(err)
	}

	resp, err := store.QuerySessions(ctx, "", now.Add(-time.Hour), now, Page{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Total != 2 || len(resp.Sessions) != 2 {
		t.Fatalf("sessions = %+v (total %d), want s1 of gemini_cli and claude-code", resp.Sessions, resp.Total)
	}
	gemini, claude := resp.Sessions[0], resp.Sessions[1]
	if gemini.ServiceName != "gemini_cli" || claude.ServiceName != "claude-code" {
		t.Fatalf("order = %s, %s; want the most recently active first", gemini.ServiceName, claude.ServiceName)
	}
	if !claude.StartTime.Equal(now.Add(-50*time.Hour)) || !claude.LastTime.Equal(now.Add(-10*time.Minute)) {
		t.Errorf("claude-code extent = %s..%s, want the whole session", claude.StartTime, claude.LastTime)
	}
	if claude.MessageCount != 3 || claude.Model != "latest-model" {
		t.Errorf("claude-code messages = %d, model = %q; want 3 and latest-model", claude.MessageCount, claude.Model)
	}
	if gemini.MessageCount != 1 || gemini.Model != "gemini-model" {
		t.Errorf("gemini_cli messages = %d, model = %q; want 1 and gemini-model", gemini.MessageCount, gemini.Model)
	}

	filtered, err := store.QuerySessions(ctx, "claude-code", now.Add(-time.Hour), now, Page{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered.Sessions) != 1 || filtered.Sessions[0].MessageCount != 3 {
		t.Errorf("claude-code sessions = %+v, want s1 with 3 messages", filtered.Sessions)
	}
}
