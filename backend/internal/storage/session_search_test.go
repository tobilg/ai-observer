package storage

import (
	"context"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/tobilg/ai-observer/internal/api"
	"github.com/tobilg/ai-observer/internal/redact"
)

func TestSearchSessions(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	long := strings.Repeat("background ", 40)
	logs := []api.LogRecord{
		{Timestamp: now.Add(-48 * time.Hour), ServiceName: "claude-code", LogAttributes: map[string]string{"session.id": "s1", "prompt": "set up the project"}},
		{Timestamp: now.Add(-2 * time.Hour), ServiceName: "claude-code", LogAttributes: map[string]string{"session.id": "s1", "prompt": long + "fix the Flaky Test in parser_test.go " + long}},
		{Timestamp: now.Add(-time.Hour), ServiceName: "codex_cli_rs", Body: "the flaky test is gone", LogAttributes: map[string]string{"conversation.id": "s2"}},
		{Timestamp: now.Add(-time.Hour), ServiceName: "codex_cli_rs", Body: "unrelated", LogAttributes: map[string]string{"conversation.id": "s3"}},
	}
	spans := []api.Span{
		{Timestamp: now.Add(-50 * time.Minute), TraceID: "t", SpanID: "root", SpanName: "invoke_agent", ServiceName: "copilot-chat",
			Duration: int64(25 * time.Minute), SpanAttributes: map[string]string{"gen_ai.conversation.id": "s4", "gen_ai.request.model": "gpt-6"}},
		{Timestamp: now.Add(-30 * time.Minute), TraceID: "t", SpanID: "a", ParentSpanID: "root", SpanName: "chat", ServiceName: "copilot-chat",
			SpanAttributes: map[string]string{"gen_ai.conversation.id": "s4", "gen_ai.input.messages": `[{"role":"user","content":"why is this test flaky?"}]`}},
	}
	if err := store.InsertTelemetry(ctx, logs, nil, spans); err != nil {
		t.Fatal(err)
	}

	matches, err := store.SearchSessions(ctx, []string{"FLAKY test"}, "", now.Add(-24*time.Hour), now, 10)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, m := range matches {
		ids = append(ids, m.SessionID)
	}
	if strings.Join(ids, ",") != "s2,s1" {
		t.Fatalf("matches = %v, want s2 then s1 (newest match first)", ids)
	}
	s1 := matches[1]
	// The whole session, also its first log before the range
	if s1.MessageCount != 2 || !s1.StartTime.Equal(now.Add(-48*time.Hour)) || s1.Matches != 1 {
		t.Errorf("s1 = %+v", s1)
	}
	if !strings.Contains(s1.Snippet, "fix the Flaky Test in parser_test.go") || !strings.HasPrefix(s1.Snippet, "…") || len(s1.Snippet) > 3*snippetRadius {
		t.Errorf("s1 snippet = %q", s1.Snippet)
	}
	if matches[0].Snippet != "the flaky test is gone" {
		t.Errorf("s2 snippet = %q", matches[0].Snippet)
	}

	// The phrase "test flaky" only appears in the Copilot span
	spanMatches, err := store.SearchSessions(ctx, []string{"test flaky"}, "", now.Add(-24*time.Hour), now, 10)
	if err != nil || len(spanMatches) != 1 || spanMatches[0].SessionID != "s4" || !strings.Contains(spanMatches[0].Snippet, "test flaky") {
		t.Fatalf("span matches = %+v, %v", spanMatches, err)
	}
	// A session of spans only: its extent and count come from the spans
	if s4 := spanMatches[0]; !s4.StartTime.Equal(now.Add(-50*time.Minute)) || !s4.LastTime.Equal(now.Add(-25*time.Minute)) ||
		s4.MessageCount != 2 || s4.Model != "gpt-6" {
		t.Errorf("span-only session = %+v", s4.Session)
	}

	only, err := store.SearchSessions(ctx, []string{"flaky"}, "codex_cli_rs", now.Add(-24*time.Hour), now, 10)
	if err != nil || len(only) != 1 || only[0].SessionID != "s2" {
		t.Errorf("codex matches = %+v, %v", only, err)
	}
	if limited, _ := store.SearchSessions(ctx, []string{"flaky"}, "", now.Add(-24*time.Hour), now, 1); len(limited) != 1 {
		t.Errorf("limit 1 returned %d", len(limited))
	}
	for _, terms := range [][]string{nil, {"  "}} {
		if _, err := store.SearchSessions(ctx, terms, "", now.Add(-time.Hour), now, 10); err == nil {
			t.Errorf("the search %q was accepted", terms)
		}
	}
}

func TestSearchSessionsWithSeveralTerms(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	logs := []api.LogRecord{
		{Timestamp: now.Add(-3 * time.Hour), ServiceName: "claude-code", LogAttributes: map[string]string{"session.id": "s1", "prompt": "the parser test fails"}},
		{Timestamp: now.Add(-2 * time.Hour), ServiceName: "claude-code", LogAttributes: map[string]string{"session.id": "s1", "prompt": "it is flaky on CI"}},
		{Timestamp: now.Add(-time.Hour), ServiceName: "codex_cli_rs", Body: "Retrying with exponential backoff",
			LogAttributes: map[string]string{"conversation.id": "s2", "tool": "parser"}},
		{Timestamp: now.Add(-time.Hour), ServiceName: "gemini_cli", LogAttributes: map[string]string{"session.id": "s3", "prompt": "backoff for the parser"}},
		{Timestamp: now.Add(-time.Hour), ServiceName: "gemini_cli", LogAttributes: map[string]string{"session.id": "s5", "output": `panic in <main> at C:\Users\dev\app.go`}},
	}
	if err := store.InsertTelemetry(ctx, logs, nil, nil); err != nil {
		t.Fatal(err)
	}
	search := func(terms ...string) []SessionMatch {
		t.Helper()
		matches, err := store.SearchSessions(ctx, terms, "", now.Add(-24*time.Hour), now, 10)
		if err != nil {
			t.Fatal(err)
		}
		return matches
	}
	ids := func(matches []SessionMatch) string {
		var ids []string
		for _, m := range matches {
			ids = append(ids, m.SessionID)
		}
		sort.Strings(ids)
		return strings.Join(ids, ",")
	}

	// All terms, in any order and case, also spread over the body and the
	// attributes of a record
	if got := ids(search("PARSER", "backoff")); got != "s2,s3" {
		t.Errorf("parser backoff = %s, want s2,s3", got)
	}
	if got := ids(search("backoff", "parser", "retr")); got != "s2" {
		t.Errorf("backoff parser retr = %s, want s2", got)
	}
	// Terms in different records of a session do not match
	if got := ids(search("parser", "flaky")); got != "" {
		t.Errorf("parser flaky = %s, want no match", got)
	}
	// Attribute keys are not searched: every record has a session ID key
	if got := ids(search("session")); got != "" {
		t.Errorf("session = %s, want no match", got)
	}
	// Values are matched as stored, also with characters JSON escapes
	if got := ids(search(`C:\Users\dev`, "<main>")); got != "s5" {
		t.Errorf("escaped characters = %s, want s5", got)
	}

	// The snippet comes from the text with the most terms
	if m := search("parser", "backoff", "exponential"); len(m) != 1 || m[0].Snippet != "Retrying with exponential backoff" {
		t.Errorf("snippet = %+v", m)
	}
}

// Snippets are cut from redacted text: telemetry stored before redaction was
// turned on does not leak a secret whose start or end the cut leaves out
func TestSearchSnippetIsRedactedBeforeCut(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	key := "-----BEGIN RSA " + "PRIVATE KEY-----\n" + strings.Repeat("MIIEowIBAAKCAQEAx9", 30) + "\n-----END RSA " + "PRIVATE KEY-----"
	logs := []api.LogRecord{{Timestamp: now.Add(-time.Hour), ServiceName: "claude-code",
		LogAttributes: map[string]string{"session.id": "s1", "tool_output": "cat id_rsa\n" + key}}}
	if err := store.InsertLogs(ctx, logs); err != nil { // stored without a redactor
		t.Fatal(err)
	}
	store.SetRedactor(redact.New(redact.Secrets))

	matches, err := store.SearchSessions(ctx, []string{"id_rsa"}, "", now.Add(-24*time.Hour), now, 10)
	if err != nil || len(matches) != 1 {
		t.Fatalf("matches = %+v, %v", matches, err)
	}
	if snippet := matches[0].Snippet; strings.Contains(snippet, "MIIEow") || !strings.Contains(snippet, "[REDACTED:private_key]") {
		t.Errorf("snippet = %q", snippet)
	}
}

func TestSnippetAround(t *testing.T) {
	if got, ok := snippetAround("short text", []string{"TEXT"}); !ok || got != "short text" {
		t.Errorf("short = %q, %v", got, ok)
	}
	text := strings.Repeat("ä", 200) + "needle" + strings.Repeat("ö", 200)
	got, ok := snippetAround(text, []string{"needle"})
	if !ok || !strings.Contains(got, "needle") || !strings.HasPrefix(got, "…") || !strings.HasSuffix(got, "…") {
		t.Errorf("multi-byte = %q", got)
	}
	for _, r := range got {
		if r == '�' {
			t.Fatal("the snippet cut a character in half")
		}
	}
	if _, ok := snippetAround("nothing here", []string{"needle"}); ok {
		t.Error("a missing term matched")
	}

	// Nearby terms are shown together, from the first occurrence on
	pad := strings.Repeat("x ", 150)
	got, _ = snippetAround(pad+"hay and the needle "+pad, []string{"needle", "hay"})
	if !strings.Contains(got, "hay and the needle") || !strings.HasPrefix(got, "…") || !strings.HasSuffix(got, "…") {
		t.Errorf("nearby terms = %q", got)
	}
	// Lowercasing that changes byte lengths (İ) keeps the match in view
	got, ok = snippetAround(strings.Repeat("İ", 300)+" the needle "+strings.Repeat("x", 300), []string{"NEEDLE"})
	if !ok || !strings.Contains(got, "the needle") {
		t.Errorf("after İ = %q", got)
	}
	// Terms far apart: around the first one only
	got, _ = snippetAround("needle "+pad+pad+"hay", []string{"hay", "needle"})
	if !strings.HasPrefix(got, "needle") || strings.Contains(got, "hay") || len(got) > 2*snippetRadius {
		t.Errorf("distant terms = %q", got)
	}
}

func TestQueryMetricTotalsByAttribute(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	point := func(v float64, repo string) api.MetricDataPoint {
		return api.MetricDataPoint{Timestamp: now.Add(-time.Minute), ServiceName: "claude-code", MetricName: "claude_code.cost.usage",
			MetricType: "sum", Value: &v, Attributes: map[string]string{"model": "m", "repository": repo}}
	}
	if err := store.InsertMetrics(ctx, []api.MetricDataPoint{point(1, "acme/api"), point(2, "acme/api"), point(4, "acme/web")}); err != nil {
		t.Fatal(err)
	}
	resp, err := store.QueryMetricTotals(ctx, "claude_code.cost.usage", "", now.Add(-time.Hour), now, "repository")
	if err != nil {
		t.Fatal(err)
	}
	totals := map[string]float64{}
	for _, s := range resp.Series {
		totals[s.Labels["repository"]] += s.DataPoints[0][1]
	}
	if totals["acme/api"] != 3 || totals["acme/web"] != 4 {
		t.Errorf("totals = %v", totals)
	}
	if _, err := store.QueryMetricTotals(ctx, "claude_code.cost.usage", "", now.Add(-time.Hour), now, "bad'key"); err == nil {
		t.Error("an invalid attribute was accepted")
	}
}
