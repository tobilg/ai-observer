package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/tobilg/ai-observer/internal/api"
)

// getJSON runs handler on a GET of target and decodes a 200 response into v
func getJSON(t *testing.T, handler http.HandlerFunc, target string, params map[string]string, v any) int {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	if params != nil {
		rctx := chi.NewRouteContext()
		for k, val := range params {
			rctx.URLParams.Add(k, val)
		}
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	}
	rec := httptest.NewRecorder()
	handler(rec, req)
	if rec.Code == http.StatusOK && v != nil {
		if err := json.NewDecoder(rec.Body).Decode(v); err != nil {
			t.Fatal(err)
		}
	}
	return rec.Code
}

func TestQuerySessions_Search(t *testing.T) {
	h, cleanup := setupTestHandlers(t)
	defer cleanup()
	now := time.Now().UTC().Truncate(time.Second)
	var logs []api.LogRecord
	for i := range 3 {
		logs = append(logs, api.LogRecord{Timestamp: now.Add(-time.Duration(10-i) * time.Minute), ServiceName: "claude-code",
			LogAttributes: map[string]string{"session.id": "s" + strconv.Itoa(i), "event.name": "user_prompt",
				"prompt": "please fix the flaky test number " + strconv.Itoa(i)}})
	}
	logs = append(logs, api.LogRecord{Timestamp: now.Add(-time.Minute), ServiceName: "codex-app-server",
		LogAttributes: map[string]string{"conversation.id": "other", "event.name": "codex.user_prompt", "prompt": "unrelated"}})
	if err := h.db().InsertLogs(context.Background(), logs); err != nil {
		t.Fatal(err)
	}
	from := url.QueryEscape(now.Add(-time.Hour).Format(time.RFC3339))
	base := "/api/sessions?from=" + from + "&search=" + url.QueryEscape(`"flaky test" fix`)

	var first api.SessionsResponse
	if code := getJSON(t, h.QuerySessions, base+"&limit=2", nil, &first); code != http.StatusOK {
		t.Fatalf("status %d", code)
	}
	if first.Total != 3 || len(first.Sessions) != 2 || !first.HasMore || first.NextCursor == "" {
		t.Fatalf("first page = %+v", first)
	}
	s := first.Sessions[0]
	if s.SessionID != "s2" || s.Snippet != "please fix the flaky test number 2" || s.Matches != 1 || s.MatchedAt == nil ||
		s.Title != "please fix the flaky test number 2" || s.MessageCount != 1 {
		t.Errorf("first match = %+v", s)
	}
	var second api.SessionsResponse
	getJSON(t, h.QuerySessions, base+"&limit=2&cursor="+first.NextCursor, nil, &second)
	if len(second.Sessions) != 1 || second.Sessions[0].SessionID != "s0" || second.HasMore || second.Total != 3 {
		t.Errorf("second page = %+v", second)
	}

	// The service filter applies, and a tool name covers its services
	var codex api.SessionsResponse
	getJSON(t, h.QuerySessions, "/api/sessions?from="+from+"&service=codex&search=unrelated", nil, &codex)
	if len(codex.Sessions) != 1 || codex.Sessions[0].SessionID != "other" {
		t.Errorf("codex search = %+v", codex)
	}

	// Without search, rows have no snippet
	var plain api.SessionsResponse
	getJSON(t, h.QuerySessions, "/api/sessions?from="+from, nil, &plain)
	if plain.Total != 4 || plain.Sessions[0].Snippet != "" || plain.Sessions[0].MatchedAt != nil {
		t.Errorf("plain list = %+v", plain)
	}

	var words []string
	for i := range 11 {
		words = append(words, "w"+strconv.Itoa(i))
	}
	tooMany := url.QueryEscape(strings.Join(words, " "))
	for _, bad := range []string{"&search=" + tooMany, "&search=%22%22", "&search=x&cursor=bogus"} {
		if code := getJSON(t, h.QuerySessions, "/api/sessions?from="+from+bad, nil, nil); code != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400", bad, code)
		}
	}
}

func TestGetSessionTranscript_TotalsAndSearch(t *testing.T) {
	h, cleanup := setupTestHandlers(t)
	defer cleanup()
	start := time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC)
	var logs []api.LogRecord
	for i := range 6 {
		prompt := "step " + strconv.Itoa(i)
		if i%2 == 1 {
			prompt += " retry the Deploy"
		}
		logs = append(logs,
			api.LogRecord{Timestamp: start.Add(time.Duration(2*i) * time.Minute), ServiceName: "claude-code",
				LogAttributes: map[string]string{"session.id": "s1", "event.name": "user_prompt", "prompt": prompt}},
			api.LogRecord{Timestamp: start.Add(time.Duration(2*i+1) * time.Minute), ServiceName: "claude-code",
				LogAttributes: map[string]string{"session.id": "s1", "event.name": "api_request", "cost_usd": "0.1", "input_tokens": "10", "output_tokens": "1"}})
	}
	if err := h.db().InsertLogs(context.Background(), logs); err != nil {
		t.Fatal(err)
	}
	params := map[string]string{"sessionId": "s1"}

	var page api.TranscriptResponse
	if code := getJSON(t, h.GetSessionTranscript, "/api/sessions/s1/transcript?limit=2&search="+url.QueryEscape("deploy RETRY"), params, &page); code != http.StatusOK {
		t.Fatalf("status %d", code)
	}
	if len(page.Messages) != 2 || page.Total != 12 || page.Title != "step 0" {
		t.Errorf("page = %d messages of %d, title %q", len(page.Messages), page.Total, page.Title)
	}
	if page.Totals == nil || page.Totals.UserMessages != 6 || page.Totals.AssistantMessages != 6 ||
		page.Totals.InputTokens != 60 || page.Totals.OutputTokens != 6 || page.Totals.CostUSD < 0.599 || page.Totals.CostUSD > 0.601 {
		t.Errorf("totals = %+v, want all 12 messages'", page.Totals)
	}
	// Positions of all matches, also those on pages not loaded
	if page.Search == nil || !reflect.DeepEqual(page.Search.Terms, []string{"deploy", "RETRY"}) || !reflect.DeepEqual(page.Search.Matches, []int{2, 6, 10}) {
		t.Errorf("search = %+v, want messages 2, 6 and 10", page.Search)
	}
	var none api.TranscriptResponse
	getJSON(t, h.GetSessionTranscript, "/api/sessions/s1/transcript?search=nowhere", params, &none)
	if none.Search == nil || none.Search.Matches == nil || len(none.Search.Matches) != 0 {
		t.Errorf("no matches = %+v, want an empty list", none.Search)
	}
	var plain api.TranscriptResponse
	getJSON(t, h.GetSessionTranscript, "/api/sessions/s1/transcript", params, &plain)
	if plain.Search != nil || plain.Totals == nil {
		t.Errorf("without search: search %+v, totals %+v", plain.Search, plain.Totals)
	}
	if code := getJSON(t, h.GetSessionTranscript, "/api/sessions/s1/transcript?search=%22%22", params, nil); code != http.StatusBadRequest {
		t.Errorf("empty search: status %d, want 400", code)
	}
}

// Log and span rows name their session, so the dashboard can link to it
func TestRowsCarrySessionID(t *testing.T) {
	h, cleanup := setupTestHandlers(t)
	defer cleanup()
	now := time.Now().UTC().Truncate(time.Second)
	if err := h.store.InsertTelemetry(context.Background(),
		[]api.LogRecord{{Timestamp: now.Add(-time.Minute), ServiceName: "claude-code", Body: "b", LogAttributes: map[string]string{"session.id": "s1"}}},
		nil,
		[]api.Span{{Timestamp: now.Add(-time.Minute), TraceID: "t1", SpanID: "a", SpanName: "op", ServiceName: "claude-code",
			SpanAttributes: map[string]string{"session.id": "s1"}}},
	); err != nil {
		t.Fatal(err)
	}
	var logs api.LogsResponse
	getJSON(t, h.QueryLogs, "/api/logs", nil, &logs)
	if len(logs.Logs) != 1 || logs.Logs[0].SessionID != "s1" {
		t.Errorf("logs = %+v", logs.Logs)
	}
	var traces api.TracesResponse
	getJSON(t, h.QueryTraces, "/api/traces", nil, &traces)
	if len(traces.Traces) != 1 || traces.Traces[0].SessionID != "s1" {
		t.Errorf("traces = %+v", traces.Traces)
	}
	var spans api.SpansResponse
	getJSON(t, h.GetTrace, "/api/traces/t1/spans?kind=otel_trace", map[string]string{"traceId": "t1"}, &spans)
	if len(spans.Spans) != 1 || spans.Spans[0].SessionID != "s1" || spans.Trace == nil || spans.Trace.SessionID != "s1" {
		t.Errorf("spans = %+v", spans)
	}
}
