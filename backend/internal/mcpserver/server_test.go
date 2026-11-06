package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/tobilg/ai-observer/internal/api"
	"github.com/tobilg/ai-observer/internal/redact"
	"github.com/tobilg/ai-observer/internal/storage"
)

// fakeToken is credential-shaped test data built at runtime
var fakeToken = "gh" + "p_" + strings.Repeat("Ab3x", 9)

// testSession stores a few sessions, a trace and usage metrics, and returns
// an MCP client session connected to New over in-memory transports
func testSession(t *testing.T) (*mcp.ClientSession, *storage.DuckDBStore, time.Time) {
	t.Helper()
	store, err := storage.NewDuckDBStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	claude := func(at time.Duration, event string, attrs map[string]string) api.LogRecord {
		a := map[string]string{"session.id": "s-claude", "event.name": event}
		for k, v := range attrs {
			a[k] = v
		}
		return api.LogRecord{Timestamp: now.Add(-at), ServiceName: "claude-code", Body: "claude_code." + event, LogAttributes: a}
	}
	logs := []api.LogRecord{
		claude(3*time.Hour, "user_prompt", map[string]string{"prompt": "Fix the flaky test in parser_test.go"}),
		claude(3*time.Hour-time.Minute, "api_request", map[string]string{"model": "claude-sonnet-5-5", "input_tokens": "1200", "output_tokens": "300"}),
		claude(3*time.Hour-2*time.Minute, "user_prompt", map[string]string{"prompt": strings.Repeat("long prompt ", 400)}),
		// Stored before redaction was turned on
		claude(3*time.Hour-3*time.Minute, "user_prompt", map[string]string{"prompt": "push with " + fakeToken}),
		{Timestamp: now.Add(-time.Hour), ServiceName: "codex_cli_rs", Body: "codex.user_prompt", SeverityText: "INFO",
			LogAttributes: map[string]string{"conversation.id": "s-codex", "event.name": "codex.user_prompt", "prompt": "add a retry"}},
		{Timestamp: now.Add(-30 * time.Minute), ServiceName: "codex_cli_rs", Body: "rate limited", SeverityText: "ERROR",
			LogAttributes: map[string]string{"conversation.id": "s-codex"}},
	}
	spans := []api.Span{
		{Timestamp: now.Add(-2 * time.Hour), TraceID: "trace-1", SpanID: "root", SpanName: "agent turn", ServiceName: "codex_cli_rs", Duration: int64(3 * time.Second), StatusCode: "OK"},
		{Timestamp: now.Add(-2*time.Hour + time.Second), TraceID: "trace-1", SpanID: "tool", ParentSpanID: "root", SpanName: "tool call", ServiceName: "codex_cli_rs",
			Duration: int64(time.Second), SpanAttributes: map[string]string{"command": strings.Repeat("x", 1000)}},
		{Timestamp: now.Add(-2*time.Hour + 2*time.Second), TraceID: "trace-1", SpanID: "nested", ParentSpanID: "tool", SpanName: "exec", ServiceName: "codex_cli_rs"},
	}
	value := func(v float64) *float64 { return &v }
	metric := func(name string, v float64, attrs map[string]string) api.MetricDataPoint {
		return api.MetricDataPoint{Timestamp: now.Add(-2 * time.Hour), ServiceName: "claude-code", MetricName: name, MetricType: "sum", Value: value(v), Attributes: attrs}
	}
	metrics := []api.MetricDataPoint{
		metric("claude_code.cost.usage", 1.25, map[string]string{"model": "claude-sonnet-5-5", "repository": "acme/api"}),
		metric("claude_code.cost.usage", 0.75, map[string]string{"model": "claude-opus-5-5", "repository": "acme/web"}),
		metric("claude_code.token.usage", 1000, map[string]string{"model": "claude-sonnet-5-5", "type": "input", "repository": "acme/api"}),
		metric("claude_code.token.usage", 200, map[string]string{"model": "claude-sonnet-5-5", "type": "output", "repository": "acme/api"}),
	}
	if err := store.InsertTelemetry(ctx, logs, metrics, spans); err != nil {
		t.Fatal(err)
	}
	store.SetRedactor(redact.New(redact.Secrets))

	server := New(store, "test")
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	if _, err := server.Connect(ctx, serverTransport, nil); err != nil {
		t.Fatal(err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	cs, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs, store, now
}

// call runs a tool and decodes its structured result into out; it returns
// the error text of a failed call
func call(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any, out any) string {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	if res.IsError {
		var texts []string
		for _, c := range res.Content {
			if tc, ok := c.(*mcp.TextContent); ok {
				texts = append(texts, tc.Text)
			}
		}
		return strings.Join(texts, " ")
	}
	raw, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	// Fields a result leaves out (omitempty) must not keep earlier values
	v := reflect.ValueOf(out).Elem()
	v.Set(reflect.Zero(v.Type()))
	if err := json.Unmarshal(raw, out); err != nil {
		t.Fatalf("%s: decoding %s: %v", name, raw, err)
	}
	return ""
}

func TestToolsAreListedReadOnly(t *testing.T) {
	cs, _, _ := testSession(t)
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"search_conversations": true, "list_sessions": true, "get_transcript": true, "usage_summary": true,
		"search_logs": true, "list_traces": true, "get_trace": true, "list_metric_names": true, "query_metrics": true}
	for _, tool := range res.Tools {
		if !want[tool.Name] {
			t.Errorf("unexpected tool %s", tool.Name)
		}
		delete(want, tool.Name)
		if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint || tool.Description == "" || tool.OutputSchema == nil {
			t.Errorf("%s: annotations %+v, description %q", tool.Name, tool.Annotations, tool.Description)
		}
	}
	if len(want) != 0 {
		t.Errorf("missing tools: %v", want)
	}
}

func TestSearchConversations(t *testing.T) {
	cs, _, _ := testSession(t)
	var out searchConversationsOut
	if errText := call(t, cs, "search_conversations", map[string]any{"query": "FLAKY test"}, &out); errText != "" {
		t.Fatal(errText)
	}
	if len(out.Sessions) != 1 {
		t.Fatalf("sessions = %+v", out.Sessions)
	}
	s := out.Sessions[0]
	if s.SessionID != "s-claude" || s.Tool != "claude-code" || s.Model != "claude-sonnet-5-5" || s.Messages != 4 ||
		!strings.Contains(s.Snippet, "Fix the flaky test") {
		t.Errorf("session = %+v", s)
	}

	// Stored text is redacted in results, also text stored before
	// redaction was turned on
	if call(t, cs, "search_conversations", map[string]any{"query": "push with"}, &out); len(out.Sessions) != 1 ||
		strings.Contains(out.Sessions[0].Snippet, fakeToken) || !strings.Contains(out.Sessions[0].Snippet, "[REDACTED:github_token]") {
		t.Errorf("snippet = %+v", out.Sessions)
	}

	if errText := call(t, cs, "search_conversations", map[string]any{"query": "retry", "tool": "nope"}, &out); !strings.Contains(errText, "unknown tool") {
		t.Errorf("unknown tool error = %q", errText)
	}
	if errText := call(t, cs, "search_conversations", map[string]any{"query": "retry", "from": "yesterday"}, &out); !strings.Contains(errText, "invalid time") {
		t.Errorf("invalid time error = %q", errText)
	}
	if errText := call(t, cs, "search_conversations", map[string]any{}, &out); errText == "" {
		t.Error("a search without query succeeded")
	}

	// Several words match in any order; a quoted phrase only as written
	if errText := call(t, cs, "search_conversations", map[string]any{"query": "parser_test FIX"}, &out); errText != "" ||
		len(out.Sessions) != 1 || out.Sessions[0].SessionID != "s-claude" || strings.Join(out.Terms, "|") != "parser_test|FIX" {
		t.Errorf("parser_test FIX = %+v, %s", out, errText)
	}
	if call(t, cs, "search_conversations", map[string]any{"query": `"test flaky"`}, &out); len(out.Sessions) != 0 || strings.Join(out.Terms, "|") != "test flaky" {
		t.Errorf(`"test flaky" = %+v`, out)
	}
}

func TestSearchTerms(t *testing.T) {
	cases := map[string]string{
		"flaky parser test":            "flaky|parser|test",
		"  retry\tbackoff  ":           "retry|backoff",
		`"not found" timeout`:          "not found|timeout",
		`timeout "  context deadline `: "timeout|context deadline",
		"Retry retry RETRY":            "Retry",
		`a""b`:                         "a|b",
	}
	for query, want := range cases {
		if terms, err := searchTerms(query); err != nil || strings.Join(terms, "|") != want {
			t.Errorf("searchTerms(%q) = %q, %v; want %q", query, terms, err, want)
		}
	}
	var words []string
	for i := range maxSearchTerms {
		words = append(words, fmt.Sprintf("w%d", i))
	}
	if terms, err := searchTerms(strings.Join(words, " ")); err != nil || len(terms) != maxSearchTerms {
		t.Errorf("%d terms = %v, %v", maxSearchTerms, terms, err)
	}
	tooMany := strings.Join(words, " ") + " more"
	for _, bad := range []string{"", "   ", `""`, tooMany} {
		if _, err := searchTerms(bad); err == nil {
			t.Errorf("searchTerms(%q) succeeded", bad)
		}
	}
}

func TestListSessionsAndTranscript(t *testing.T) {
	cs, _, _ := testSession(t)
	var sessions listSessionsOut
	if errText := call(t, cs, "list_sessions", map[string]any{"limit": 1}, &sessions); errText != "" {
		t.Fatal(errText)
	}
	if sessions.Total != 2 || len(sessions.Sessions) != 1 || sessions.Sessions[0].SessionID != "s-codex" || sessions.NextCursor == "" {
		t.Fatalf("page 1 = %+v", sessions)
	}
	if call(t, cs, "list_sessions", map[string]any{"limit": 1, "cursor": sessions.NextCursor}, &sessions); len(sessions.Sessions) != 1 ||
		sessions.Sessions[0].SessionID != "s-claude" || sessions.NextCursor != "" {
		t.Errorf("page 2 = %+v", sessions)
	}
	if call(t, cs, "list_sessions", map[string]any{"tool": "codex"}, &sessions); sessions.Total != 1 {
		t.Errorf("codex sessions = %+v", sessions)
	}
	if errText := call(t, cs, "list_sessions", map[string]any{"cursor": "bogus"}, &sessions); !strings.Contains(errText, "invalid cursor") {
		t.Errorf("bad cursor error = %q", errText)
	}

	var tr getTranscriptOut
	if errText := call(t, cs, "get_transcript", map[string]any{"session_id": "s-claude", "roles": []string{"user"}, "limit": 2, "max_chars": 100}, &tr); errText != "" {
		t.Fatal(errText)
	}
	if tr.Tool != "claude-code" || tr.Total != 3 || len(tr.Messages) != 2 || tr.NextCursor != "2" {
		t.Fatalf("transcript = %+v", tr)
	}
	if !strings.Contains(tr.Messages[0].Content, "Fix the flaky test") || !tr.Messages[1].Truncated ||
		!strings.Contains(tr.Messages[1].Content, "more characters]") {
		t.Errorf("messages = %+v", tr.Messages)
	}
	if call(t, cs, "get_transcript", map[string]any{"session_id": "s-claude", "roles": []string{"user"}, "cursor": tr.NextCursor}, &tr); len(tr.Messages) != 1 ||
		strings.Contains(tr.Messages[0].Content, fakeToken) {
		t.Errorf("page 2 = %+v", tr.Messages)
	}
	if errText := call(t, cs, "get_transcript", map[string]any{"session_id": "missing"}, &tr); !strings.Contains(errText, "no session") {
		t.Errorf("missing session error = %q", errText)
	}
}

func TestUsageSummary(t *testing.T) {
	cs, store, now := testSession(t)
	var out usageSummaryOut
	if errText := call(t, cs, "usage_summary", map[string]any{}, &out); errText != "" {
		t.Fatal(errText)
	}
	if out.GroupBy != "tool" || len(out.Rows) != 1 || out.TotalCostUSD != 2 {
		t.Fatalf("by tool = %+v", out)
	}
	if r := out.Rows[0]; r.Tool != "claude-code" || r.Tokens["input"] != 1000 || r.Tokens["output"] != 200 || r.TotalTokens != 1200 {
		t.Errorf("claude row = %+v", r)
	}

	call(t, cs, "usage_summary", map[string]any{"group_by": "model"}, &out)
	if len(out.Rows) != 2 || out.Rows[0].Model != "claude-sonnet-5-5" || out.Rows[0].CostUSD != 1.25 || out.Rows[1].Model != "claude-opus-5-5" {
		t.Errorf("by model = %+v", out.Rows)
	}
	call(t, cs, "usage_summary", map[string]any{"group_by": "repository"}, &out)
	if len(out.Rows) != 2 || out.Rows[0].Repository != "acme/api" || out.Rows[0].TotalTokens != 1200 {
		t.Errorf("by repository = %+v", out.Rows)
	}
	call(t, cs, "usage_summary", map[string]any{"group_by": "day"}, &out)
	if len(out.Rows) == 0 || out.Rows[0].Day == "" || out.TotalCostUSD != 2 {
		t.Errorf("by day = %+v", out)
	}
	call(t, cs, "usage_summary", map[string]any{"tool": "codex"}, &out)
	if len(out.Rows) != 0 || out.TotalCostUSD != 0 {
		t.Errorf("codex usage = %+v", out)
	}
	if errText := call(t, cs, "usage_summary", map[string]any{"group_by": "week"}, &out); !strings.Contains(errText, "invalid group_by") {
		t.Errorf("group_by error = %q", errText)
	}

	// Codex input includes the cached tokens and output the reasoning ones:
	// the total counts each token once
	var codex []api.MetricDataPoint
	for tokenType, v := range map[string]float64{"input": 10000, "cacheRead": 8000, "output": 2000, "reasoning": 1000} {
		value := v
		codex = append(codex, api.MetricDataPoint{Timestamp: now.Add(-time.Hour), ServiceName: "codex_cli_rs", MetricName: "codex_cli_rs.token.usage",
			MetricType: "sum", Value: &value, Attributes: map[string]string{"model": "gpt-6", "type": tokenType}})
	}
	if err := store.InsertTelemetry(context.Background(), nil, codex, nil); err != nil {
		t.Fatal(err)
	}
	call(t, cs, "usage_summary", map[string]any{"tool": "codex"}, &out)
	if len(out.Rows) != 1 || out.Rows[0].TotalTokens != 12000 || out.Rows[0].Tokens["cacheRead"] != 8000 {
		t.Errorf("codex usage = %+v", out.Rows)
	}
}

func TestLogsTracesAndMetrics(t *testing.T) {
	cs, _, _ := testSession(t)
	var logs searchLogsOut
	if errText := call(t, cs, "search_logs", map[string]any{"severity": "ERROR"}, &logs); errText != "" {
		t.Fatal(errText)
	}
	if logs.Total != 1 || logs.Logs[0].Body != "rate limited" || logs.Logs[0].Tool != "codex" {
		t.Errorf("error logs = %+v", logs)
	}

	var traces listTracesOut
	if errText := call(t, cs, "list_traces", map[string]any{"tool": "codex_cli_rs"}, &traces); errText != "" {
		t.Fatal(errText)
	}
	if len(traces.Traces) != 1 || traces.Traces[0].ID != "trace-1" || traces.Traces[0].Spans != 3 || traces.Traces[0].DurationMs < 3000 {
		t.Fatalf("traces = %+v", traces)
	}
	var trace getTraceOut
	if errText := call(t, cs, "get_trace", map[string]any{"id": "trace-1", "max_chars": 50}, &trace); errText != "" {
		t.Fatal(errText)
	}
	var shape []string
	for _, s := range trace.Spans {
		shape = append(shape, strings.Repeat(">", s.Depth)+s.Name)
	}
	if strings.Join(shape, ",") != "agent turn,>tool call,>>exec" || !strings.Contains(trace.Spans[1].Attributes["command"], "more characters]") {
		t.Errorf("trace = %v, %+v", shape, trace.Spans[1].Attributes)
	}
	if call(t, cs, "get_trace", map[string]any{"id": "trace-1", "max_spans": 2}, &trace); len(trace.Spans) != 2 || !trace.Truncated {
		t.Errorf("limited trace = %+v", trace)
	}
	if errText := call(t, cs, "get_trace", map[string]any{"id": "nope"}, &trace); !strings.Contains(errText, "no trace") {
		t.Errorf("missing trace error = %q", errText)
	}
	if errText := call(t, cs, "get_trace", map[string]any{"id": "trace-1", "kind": "bogus"}, &trace); !strings.Contains(errText, "unknown kind") {
		t.Errorf("bad kind error = %q", errText)
	}

	var names listMetricNamesOut
	if call(t, cs, "list_metric_names", map[string]any{"tool": "claude"}, &names); len(names.Names) != 2 {
		t.Errorf("metric names = %v", names.Names)
	}
	var metrics queryMetricsOut
	if errText := call(t, cs, "query_metrics", map[string]any{"name": "claude_code.cost.usage", "aggregate": true}, &metrics); errText != "" {
		t.Fatal(errText)
	}
	total := 0.0
	for _, s := range metrics.Series {
		for _, p := range s.Points {
			total += p.Value
		}
	}
	if total != 2 {
		t.Errorf("aggregate cost = %v from %+v", total, metrics.Series)
	}
	if call(t, cs, "query_metrics", map[string]any{"name": "claude_code.cost.usage", "interval": "1h", "from": "6h"}, &metrics); metrics.Interval != "1h0m0s" ||
		len(metrics.Series) == 0 || len(metrics.Series[0].Points) < 6 || metrics.Series[0].Points[0].Time == "" {
		t.Errorf("bucketed metrics = %+v", metrics)
	}
	if errText := call(t, cs, "query_metrics", map[string]any{"name": "no.such.metric"}, &metrics); !strings.Contains(errText, "no metric") {
		t.Errorf("unknown metric error = %q", errText)
	}
	if errText := call(t, cs, "query_metrics", map[string]any{"name": "claude_code.cost.usage", "interval": "1h", "from": "30d"}, &metrics); errText != "" {
		t.Errorf("30 days of hourly buckets: %q", errText)
	}
}

func TestTranscriptResource(t *testing.T) {
	cs, _, _ := testSession(t)
	res, err := cs.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: "ai-observer://sessions/s-claude/transcript"})
	if err != nil {
		t.Fatal(err)
	}
	text := res.Contents[0].Text
	if res.Contents[0].MIMEType != "text/markdown" || !strings.HasPrefix(text, "# Session s-claude (claude-code)") ||
		!strings.Contains(text, "## user,") || !strings.Contains(text, "Fix the flaky test") || strings.Contains(text, fakeToken) {
		t.Errorf("transcript resource:\n%s", text)
	}
	if _, err := cs.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: "ai-observer://sessions/missing/transcript"}); err == nil {
		t.Error("a missing session was read")
	}
}

func TestParseTime(t *testing.T) {
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	cases := map[string]time.Time{
		"2026-11-01T10:00:00Z": time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC),
		"2026-11-01":           time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC),
		"30m":                  now.Add(-30 * time.Minute),
		"24h":                  now.Add(-24 * time.Hour),
		"7d":                   now.Add(-7 * 24 * time.Hour),
		"2w":                   now.Add(-14 * 24 * time.Hour),
		"now":                  now,
	}
	for in, want := range cases {
		if got, err := parseTime(in, now, false); err != nil || !got.Equal(want) {
			t.Errorf("parseTime(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	if end, _ := parseTime("2026-11-01", now, true); !end.Equal(time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC).Add(-time.Nanosecond)) {
		t.Errorf("end of day = %v", end)
	}
	for _, bad := range []string{"yesterday", "7x", "-3d", ""} {
		if _, err := parseTime(bad, now, false); err == nil {
			t.Errorf("parseTime(%q) succeeded", bad)
		}
	}
	if _, _, err := timeRange("1d", "2d", now, time.Hour); err == nil {
		t.Error("from after to was accepted")
	}
}
