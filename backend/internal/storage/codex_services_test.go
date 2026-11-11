package storage

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/tobilg/ai-observer/internal/api"
)

// A tool name as service filter covers all of the tool's services: Codex
// sends codex_cli_rs, codex-app-server and codex-exec-server. A service name
// covers only itself.
func TestToolFilterCoversAllServices(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	from, to := now.Add(-time.Hour), now.Add(time.Hour)

	logs := []api.LogRecord{
		{Timestamp: now, ServiceName: "codex_cli_rs", Body: "tui", LogAttributes: map[string]string{"conversation.id": "c-old"}},
		{Timestamp: now, ServiceName: "codex-app-server", Body: "turn", LogAttributes: map[string]string{"conversation.id": "c-new"}},
		{Timestamp: now, ServiceName: "claude-code", Body: "claude", LogAttributes: map[string]string{"session.id": "s1"}},
	}
	value := func(v float64) *float64 { return &v }
	metrics := []api.MetricDataPoint{
		{Timestamp: now, ServiceName: "codex_cli_rs", MetricName: "codex_cli_rs.token.usage", MetricType: "sum", Value: value(10), Attributes: map[string]string{"type": "input", "model": "gpt-6"}},
		{Timestamp: now, ServiceName: "codex-app-server", MetricName: "codex_cli_rs.token.usage", MetricType: "sum", Value: value(20), Attributes: map[string]string{"type": "input", "model": "gpt-6"}},
	}
	if err := store.InsertTelemetry(ctx, logs, metrics, nil); err != nil {
		t.Fatal(err)
	}

	for filter, want := range map[string]int{"codex": 2, "codex-app-server": 1, "codex_cli_rs": 1, "": 3} {
		resp, err := store.QueryLogs(ctx, filter, "", "", "", from, to, Page{Limit: 10})
		if err != nil {
			t.Fatal(err)
		}
		if resp.Total != want {
			t.Errorf("logs of %q = %d, want %d", filter, resp.Total, want)
		}
		sessions, err := store.QuerySessions(ctx, filter, from, to, Page{Limit: 10})
		if err != nil {
			t.Fatal(err)
		}
		if sessions.Total != want {
			t.Errorf("sessions of %q = %d, want %d", filter, sessions.Total, want)
		}
	}

	total := func(filter string) float64 {
		t.Helper()
		series, err := store.QueryMetricTotals(ctx, "codex_cli_rs.token.usage", filter, from, to, "")
		if err != nil {
			t.Fatal(err)
		}
		var sum float64
		for _, s := range series.Series {
			for _, dp := range s.DataPoints {
				sum += dp[1]
			}
		}
		return sum
	}
	if got := total("codex"); got != 30 {
		t.Errorf("Codex tokens = %v, want 30", got)
	}
	if got := total("codex-app-server"); got != 20 {
		t.Errorf("app server tokens = %v, want 20", got)
	}
	// Dashboard widgets query through the batch endpoint
	batch := store.QueryBatchMetricSeries(ctx, []api.MetricQuery{
		{ID: "all", Name: "codex_cli_rs.token.usage", Service: "codex", Aggregate: true},
		{ID: "app", Name: "codex_cli_rs.token.usage", Service: "codex-app-server", Aggregate: true},
	}, from, to, 60)
	for i, want := range []float64{30, 20} {
		r := batch.Results[i]
		var sum float64
		for _, s := range r.Series {
			for _, dp := range s.DataPoints {
				sum += dp[1]
			}
		}
		if !r.Success || sum != want {
			t.Errorf("batch query %s = %v (%+v), want %v", r.ID, sum, r, want)
		}
	}
	if names, err := store.GetMetricNames(ctx, "codex"); err != nil || !slices.Equal(names, []string{"codex_cli_rs.token.usage"}) {
		t.Errorf("Codex metric names = %v (%v)", names, err)
	}

	summary, err := store.DeleteAllInRange(ctx, from, to, "codex")
	if err != nil {
		t.Fatal(err)
	}
	if summary.LogCount != 2 || summary.MetricCount != 2 {
		t.Errorf("deleted %+v, want Codex's 2 logs and 2 metric points", summary)
	}
	if resp, _ := store.QueryLogs(ctx, "", "", "", "", from, to, Page{Limit: 10}); resp.Total != 1 || resp.Logs[0].ServiceName != "claude-code" {
		t.Errorf("logs left = %+v, want Claude Code's", resp.Logs)
	}
}

// Traces of Codex's app and exec servers in which no span belongs to a
// session are internal requests, left out of trace lists unless asked for,
// as are its approval reviews, which run in sessions of their own
func TestBackgroundTracesAreLeftOut(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	from, to := now.Add(-time.Hour), now.Add(time.Hour)

	spans := []api.Span{
		{TraceID: "fs-read", SpanID: "a1", ServiceName: "codex-app-server", SpanName: "fs.read_file", Timestamp: now, Duration: 1000},
		{TraceID: "turn", SpanID: "b1", ServiceName: "codex-app-server", SpanName: "turn/start", Timestamp: now.Add(time.Second), Duration: 1000},
		{TraceID: "turn", SpanID: "b2", ParentSpanID: "b1", ServiceName: "codex-app-server", SpanName: "run_turn", Timestamp: now.Add(time.Second),
			Duration: 1000, SpanAttributes: map[string]string{"conversation.id": "c1"}},
		{TraceID: "exec", SpanID: "c1", ServiceName: "codex-exec-server", SpanName: "fs.get_metadata", Timestamp: now.Add(2 * time.Second), Duration: 1000},
		{TraceID: "claude", SpanID: "d1", ServiceName: "claude-code", SpanName: "claude_code.interaction", Timestamp: now.Add(3 * time.Second), Duration: 1000},
		{TraceID: "review", SpanID: "e1", ServiceName: "codex-app-server", SpanName: "decide_request", Timestamp: now.Add(-time.Second), Duration: 1000},
		{TraceID: "review", SpanID: "e2", ParentSpanID: "e1", ServiceName: "codex-app-server", SpanName: "run_turn", Timestamp: now.Add(-time.Second),
			Duration: 1000, SpanAttributes: map[string]string{"conversation.id": "c2"}},
		{TraceID: "cli-start", SpanID: "g1", ServiceName: "codex_cli_rs", SpanName: "auth", Timestamp: now.Add(-time.Second), Duration: 1000},
		// An earlier Codex version's session trace: its spans of these
		// names are not roots
		{TraceID: "cli-session", SpanID: "h1", ServiceName: "codex_cli_rs", SpanName: "session", Timestamp: now.Add(-3 * time.Second), Duration: 1000},
		{TraceID: "cli-session", SpanID: "h2", ParentSpanID: "h1", ServiceName: "codex_cli_rs", SpanName: "auth", Timestamp: now.Add(-3 * time.Second), Duration: 1000},
		// Another tool's span of that name is not Codex's review
		{TraceID: "other", SpanID: "f1", ServiceName: "claude-code", SpanName: "decide_request", Timestamp: now.Add(-2 * time.Second), Duration: 1000},
	}
	if err := store.InsertSpans(ctx, spans); err != nil {
		t.Fatal(err)
	}

	ids := func(filter TraceFilter) ([]string, int) {
		t.Helper()
		resp, err := store.QueryTraces(ctx, filter, from, to, Page{Limit: 10})
		if err != nil {
			t.Fatal(err)
		}
		var ids []string
		for _, tr := range resp.Traces {
			ids = append(ids, tr.ID)
		}
		return ids, resp.Total
	}
	for _, tc := range []struct {
		filter TraceFilter
		want   []string
	}{
		{TraceFilter{}, []string{"claude", "turn", "other", "cli-session"}},
		{TraceFilter{Background: true}, []string{"claude", "exec", "turn", "fs-read", "review", "cli-start", "other", "cli-session"}},
		{TraceFilter{Service: "codex"}, []string{"turn", "cli-session"}},
		{TraceFilter{Service: "codex", Background: true}, []string{"exec", "turn", "fs-read", "review", "cli-start", "cli-session"}},
		{TraceFilter{Search: "run_turn"}, []string{"turn"}},
		{TraceFilter{Search: "fs."}, nil},
		{TraceFilter{Search: "fs.", Background: true}, []string{"exec", "fs-read"}},
	} {
		got, total := ids(tc.filter)
		if !slices.Equal(got, tc.want) || total != len(tc.want) {
			t.Errorf("%+v: traces %v (total %d), want %v", tc.filter, got, total, tc.want)
		}
	}

	// The turn keeps all of its spans
	if resp, _ := store.QueryTraces(ctx, TraceFilter{Service: "codex"}, from, to, Page{Limit: 10}); len(resp.Traces) != 2 || resp.Traces[0].ID != "turn" || resp.Traces[0].SpanCount != 2 {
		t.Errorf("turn = %+v, want both spans", resp.Traces)
	}
	recent, err := store.GetRecentTraces(ctx, 10, from, to)
	if err != nil {
		t.Fatal(err)
	}
	if len(recent.Traces) != 4 {
		t.Errorf("recent traces = %+v, want the turn, Claude's two and the CLI session", recent.Traces)
	}
}
