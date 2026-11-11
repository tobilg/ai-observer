package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/tobilg/ai-observer/internal/api"
)

// TestParameterValidation checks that the list and detail endpoints answer
// invalid parameters with a 400 naming the parameter, and keep accepting
// valid ones (limits above the maximum are clamped)
func TestParameterValidation(t *testing.T) {
	h, cleanup := setupTestHandlers(t)
	defer cleanup()
	now := time.Now().UTC()
	if err := h.db().InsertLogs(context.Background(), []api.LogRecord{{
		Timestamp: now.Add(-time.Minute), ServiceName: "claude-code", SeverityText: "INFO", Body: "claude_code.user_prompt",
		LogAttributes: map[string]string{"event.name": "user_prompt", "session.id": "s1", "prompt": "hello"},
	}}); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		handler http.HandlerFunc
		target  string
		params  map[string]string
		status  int
		message string // part of the error message
	}{
		{"traces: limit", h.QueryTraces, "/api/traces?limit=abc", nil, 400, `invalid limit "abc"`},
		{"traces: offset over max", h.QueryTraces, "/api/traces?offset=10001", nil, 400, "offset exceeds 10000"},
		{"traces: background", h.QueryTraces, "/api/traces?background=1", nil, 400, `invalid background "1": expected true or false`},
		{"traces: two errors", h.QueryTraces, "/api/traces?limit=-1&from=x", nil, 400, `invalid from "x": expected an RFC 3339 time; invalid limit "-1": expected a positive integer (values above 1000 are clamped to it)`},
		{"traces: large limit", h.QueryTraces, "/api/traces?limit=100000", nil, 200, ""},
		{"recent traces: limit", h.QueryRecentTraces, "/api/traces/recent?limit=0", nil, 400, `invalid limit "0"`},
		{"trace: kind missing", h.GetTrace, "/api/traces/t1", map[string]string{"traceId": "t1"}, 400, "kind is required"},
		{"trace: kind", h.GetTrace, "/api/traces/t1?kind=x", map[string]string{"traceId": "t1"}, 400, `invalid kind "x": expected one of otel_trace, codex_operation`},
		{"trace: limit", h.GetTrace, "/api/traces/t1?kind=otel_trace&limit=x", map[string]string{"traceId": "t1"}, 400, `invalid limit "x"`},
		{"trace: unknown", h.GetTrace, "/api/traces/t1?kind=otel_trace&limit=5000", map[string]string{"traceId": "t1"}, 404, ""},
		{"logs: limit", h.QueryLogs, "/api/logs?limit=1.5", nil, 400, `invalid limit "1.5"`},
		{"logs: to", h.QueryLogs, "/api/logs?to=2025", nil, 400, `invalid to "2025"`},
		{"log levels: from after to", h.GetLogLevels, "/api/logs/levels?from=2025-01-02T00:00:00Z&to=2025-01-01T00:00:00Z", nil, 400, "from must not be after to"},
		{"sessions: limit", h.QuerySessions, "/api/sessions?limit=none", nil, 400, `invalid limit "none"`},
		{"sessions: search", h.QuerySessions, "/api/sessions?search=%22%22", nil, 400, "invalid search"},
		{"sessions: ok", h.QuerySessions, "/api/sessions?limit=5000&unknown=1", nil, 200, ""},
		{"transcript: limit", h.GetSessionTranscript, "/api/sessions/s1/transcript?limit=0", map[string]string{"sessionId": "s1"}, 400, `invalid limit "0"`},
		{"transcript: large limit", h.GetSessionTranscript, "/api/sessions/s1/transcript?limit=5000", map[string]string{"sessionId": "s1"}, 200, ""},
		{"transcript: cursor", h.GetSessionTranscript, "/api/sessions/s1/transcript?cursor=x", map[string]string{"sessionId": "s1"}, 400, "invalid cursor"},
		{"stats: to", h.GetStats, "/api/stats?to=x", nil, 400, `invalid to "x"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.target, nil)
			if tt.params != nil {
				rctx := chi.NewRouteContext()
				for k, v := range tt.params {
					rctx.URLParams.Add(k, v)
				}
				req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
			}
			rec := httptest.NewRecorder()
			tt.handler(rec, req)
			if rec.Code != tt.status {
				t.Fatalf("status = %d, want %d: %s", rec.Code, tt.status, rec.Body.String())
			}
			if tt.message == "" {
				return
			}
			var body api.ErrorResponse
			if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(body.Message, tt.message) {
				t.Errorf("message %q lacks %q", body.Message, tt.message)
			}
		})
	}
}
