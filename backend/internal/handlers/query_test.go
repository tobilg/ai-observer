package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/tobilg/ai-observer/internal/api"
	"github.com/tobilg/ai-observer/internal/storage"
	"github.com/tobilg/ai-observer/internal/websocket"
)

// setupTestHandlers creates handlers with an in-memory DuckDB for testing
func setupTestHandlers(t *testing.T) (*Handlers, func()) {
	t.Helper()
	store, err := storage.NewDuckDBStore(":memory:")
	if err != nil {
		t.Fatalf("failed to create test store: %v", err)
	}
	hub := websocket.NewHub()
	h := New(store, hub)
	cleanup := func() {
		store.Close()
	}
	return h, cleanup
}

// insertTestTrace inserts a test trace into the store
func insertTestTrace(t *testing.T, store *storage.DuckDBStore, traceID, spanID, serviceName, spanName string) {
	t.Helper()
	spans := []api.Span{{
		TraceID:     traceID,
		SpanID:      spanID,
		ServiceName: serviceName,
		SpanName:    spanName,
		Timestamp:   time.Now(),
		Duration:    100000000,
		StatusCode:  "OK",
	}}
	if err := store.InsertSpans(context.Background(), spans); err != nil {
		t.Fatalf("failed to insert test trace: %v", err)
	}
}

// insertTestLog inserts a test log into the store
func insertTestLog(t *testing.T, store *storage.DuckDBStore, serviceName, severity, body string) {
	t.Helper()
	logs := []api.LogRecord{{
		Timestamp:      time.Now(),
		ServiceName:    serviceName,
		SeverityText:   severity,
		SeverityNumber: 9,
		Body:           body,
	}}
	if err := store.InsertLogs(context.Background(), logs); err != nil {
		t.Fatalf("failed to insert test log: %v", err)
	}
}

func TestGetStats(t *testing.T) {
	h, cleanup := setupTestHandlers(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/stats", nil)
	rec := httptest.NewRecorder()

	h.GetStats(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}

	var stats api.StatsResponse
	if err := json.NewDecoder(rec.Body).Decode(&stats); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// Empty database should have zero counts
	if stats.TraceCount != 0 {
		t.Errorf("expected 0 traces, got %d", stats.TraceCount)
	}
}

func TestGetStatsIgnoresTimeRange(t *testing.T) {
	h, cleanup := setupTestHandlers(t)
	defer cleanup()

	ctx := context.Background()
	if err := h.store.InsertSpans(ctx, []api.Span{{
		TraceID:     "outside-request-range",
		SpanID:      "span-1",
		ServiceName: "svc",
		SpanName:    "root",
		Timestamp:   time.Now(),
		StatusCode:  "OK",
	}}); err != nil {
		t.Fatalf("failed to insert test span: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/stats?from=2000-01-01T00:00:00Z&to=2000-01-02T00:00:00Z", nil)
	rec := httptest.NewRecorder()

	h.GetStats(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}

	var stats api.StatsResponse
	if err := json.NewDecoder(rec.Body).Decode(&stats); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if stats.TraceCount != 1 {
		t.Errorf("expected all-time trace count 1, got %d", stats.TraceCount)
	}
}

func TestListServices(t *testing.T) {
	h, cleanup := setupTestHandlers(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/services", nil)
	rec := httptest.NewRecorder()

	h.ListServices(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}

	var resp api.ServicesResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// Empty database should have no services
	if len(resp.Services) != 0 {
		t.Errorf("expected 0 services, got %d", len(resp.Services))
	}
}

func TestQueryTraces(t *testing.T) {
	h, cleanup := setupTestHandlers(t)
	defer cleanup()

	tests := []struct {
		name       string
		query      string
		wantStatus int
	}{
		{"default params", "/api/traces", http.StatusOK},
		{"with limit", "/api/traces?limit=10", http.StatusOK},
		{"with offset", "/api/traces?offset=5", http.StatusOK},
		{"with service filter", "/api/traces?service=test-service", http.StatusOK},
		{"with search filter", "/api/traces?search=test", http.StatusOK},
		{"with time range", "/api/traces?from=2024-01-01T00:00:00Z&to=2024-12-31T23:59:59Z", http.StatusOK},
		{"limit capped at 1000", "/api/traces?limit=5000", http.StatusOK},
		{"invalid limit uses default", "/api/traces?limit=invalid", http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.query, nil)
			rec := httptest.NewRecorder()

			h.QueryTraces(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("expected status %d, got %d", tt.wantStatus, rec.Code)
			}
		})
	}
}

func TestGetTrace(t *testing.T) {
	h, cleanup := setupTestHandlers(t)
	defer cleanup()

	// Insert a test trace first
	insertTestTrace(t, h.store, "trace-123", "span-456", "test-service", "test-span")

	tests := []struct {
		name       string
		traceID    string
		wantStatus int
	}{
		{"existing trace", "trace-123", http.StatusOK},
		{"non-existent trace", "nonexistent", http.StatusNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/traces/"+tt.traceID+"?kind="+api.TraceKindOTelTrace, nil)
			// Set up chi URL params
			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("traceId", tt.traceID)
			req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

			rec := httptest.NewRecorder()
			h.GetTrace(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("expected status %d, got %d: %s", tt.wantStatus, rec.Code, rec.Body.String())
			}
		})
	}
}

func TestGetTrace_MissingTraceID(t *testing.T) {
	h, cleanup := setupTestHandlers(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/traces/", nil)
	// Empty route context (no traceId)
	rctx := chi.NewRouteContext()
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rec := httptest.NewRecorder()
	h.GetTrace(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", rec.Code)
	}
}

func TestGetTrace_MissingKind(t *testing.T) {
	h, cleanup := setupTestHandlers(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/traces/trace-123", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("traceId", "trace-123")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rec := httptest.NewRecorder()
	h.GetTrace(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", rec.Code)
	}
}

func TestQueryLogs(t *testing.T) {
	h, cleanup := setupTestHandlers(t)
	defer cleanup()

	// Insert test logs
	insertTestLog(t, h.store, "test-service", "INFO", "Test log message")
	insertTestLog(t, h.store, "test-service", "ERROR", "Error log message")

	tests := []struct {
		name       string
		query      string
		wantStatus int
	}{
		{"default params", "/api/logs", http.StatusOK},
		{"with severity filter", "/api/logs?severity=ERROR", http.StatusOK},
		{"with service filter", "/api/logs?service=test-service", http.StatusOK},
		{"with search", "/api/logs?search=error", http.StatusOK},
		{"with pagination", "/api/logs?limit=10&offset=0", http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.query, nil)
			rec := httptest.NewRecorder()

			h.QueryLogs(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("expected status %d, got %d", tt.wantStatus, rec.Code)
			}
		})
	}
}

func TestGetLogLevels(t *testing.T) {
	h, cleanup := setupTestHandlers(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/logs/levels", nil)
	rec := httptest.NewRecorder()

	h.GetLogLevels(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
}

func TestQueryMetrics(t *testing.T) {
	h, cleanup := setupTestHandlers(t)
	defer cleanup()

	tests := []struct {
		name       string
		query      string
		wantStatus int
	}{
		{"default params", "/api/metrics", http.StatusOK},
		{"with name filter", "/api/metrics?name=cpu_usage", http.StatusOK},
		{"with type filter", "/api/metrics?type=gauge", http.StatusOK},
		{"with service filter", "/api/metrics?service=test-service", http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.query, nil)
			rec := httptest.NewRecorder()

			h.QueryMetrics(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("expected status %d, got %d", tt.wantStatus, rec.Code)
			}
		})
	}
}

func TestListMetricNames(t *testing.T) {
	h, cleanup := setupTestHandlers(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/metrics/names", nil)
	rec := httptest.NewRecorder()

	h.ListMetricNames(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
}

func TestQueryMetricSeries(t *testing.T) {
	h, cleanup := setupTestHandlers(t)
	defer cleanup()

	tests := []struct {
		name       string
		query      string
		wantStatus int
	}{
		{"missing name parameter", "/api/metrics/series", http.StatusBadRequest},
		{"with name parameter", "/api/metrics/series?name=cpu_usage", http.StatusOK},
		{"with all params", "/api/metrics/series?name=cpu_usage&service=test&interval=60&aggregate=true", http.StatusOK},
		{"with time range", "/api/metrics/series?name=cpu_usage&from=2024-01-01T00:00:00Z&to=2024-12-31T23:59:59Z", http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.query, nil)
			rec := httptest.NewRecorder()

			h.QueryMetricSeries(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("expected status %d, got %d: %s", tt.wantStatus, rec.Code, rec.Body.String())
			}
		})
	}
}

func TestQueryBatchMetricSeries(t *testing.T) {
	h, cleanup := setupTestHandlers(t)
	defer cleanup()

	tests := []struct {
		name       string
		body       interface{}
		wantStatus int
	}{
		{
			name:       "empty queries",
			body:       map[string]interface{}{"queries": []interface{}{}},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "valid request",
			body: map[string]interface{}{
				"from":     "2024-01-01T00:00:00Z",
				"to":       "2024-12-31T23:59:59Z",
				"interval": 60,
				"queries": []map[string]interface{}{
					{"id": "q1", "name": "cpu_usage"},
				},
			},
			wantStatus: http.StatusOK,
		},
		{
			name: "query missing id",
			body: map[string]interface{}{
				"queries": []map[string]interface{}{
					{"name": "cpu_usage"},
				},
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "query missing name",
			body: map[string]interface{}{
				"queries": []map[string]interface{}{
					{"id": "q1"},
				},
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "invalid json",
			body:       "invalid",
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var body []byte
			var err error
			if s, ok := tt.body.(string); ok {
				body = []byte(s)
			} else {
				body, err = json.Marshal(tt.body)
				if err != nil {
					t.Fatalf("failed to marshal body: %v", err)
				}
			}

			req := httptest.NewRequest(http.MethodPost, "/api/metrics/batch-series", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()

			h.QueryBatchMetricSeries(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("expected status %d, got %d: %s", tt.wantStatus, rec.Code, rec.Body.String())
			}
		})
	}
}

func TestQueryBatchMetricSeries_TooManyQueries(t *testing.T) {
	h, cleanup := setupTestHandlers(t)
	defer cleanup()

	// Create more than 50 queries
	queries := make([]map[string]interface{}, 51)
	for i := range queries {
		queries[i] = map[string]interface{}{"id": "q" + string(rune(i)), "name": "metric"}
	}

	body, _ := json.Marshal(map[string]interface{}{"queries": queries})
	req := httptest.NewRequest(http.MethodPost, "/api/metrics/batch-series", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.QueryBatchMetricSeries(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status 400 for too many queries, got %d", rec.Code)
	}
}

func TestHealth(t *testing.T) {
	h, cleanup := setupTestHandlers(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	h.Health(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}

	var resp map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp["status"] != "ok" {
		t.Errorf("expected status 'ok', got '%s'", resp["status"])
	}
}

func TestQueryRecentTraces(t *testing.T) {
	h, cleanup := setupTestHandlers(t)
	defer cleanup()

	tests := []struct {
		name       string
		query      string
		wantStatus int
	}{
		{"default", "/api/traces/recent", http.StatusOK},
		{"with limit", "/api/traces/recent?limit=5", http.StatusOK},
		{"limit over 100 capped", "/api/traces/recent?limit=200", http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.query, nil)
			rec := httptest.NewRecorder()

			h.QueryRecentTraces(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("expected status %d, got %d", tt.wantStatus, rec.Code)
			}
		})
	}
}

func TestParsePagination(t *testing.T) {
	tests := []struct {
		name       string
		query      string
		wantLimit  int
		wantOffset int
	}{
		{"default values", "", 50, 0},
		{"custom limit", "limit=25", 25, 0},
		{"custom offset", "offset=10", 50, 10},
		{"both custom", "limit=20&offset=5", 20, 5},
		{"limit capped at 1000", "limit=2000", 1000, 0},
		{"invalid limit uses default", "limit=abc", 50, 0},
		{"negative offset uses 0", "offset=-5", 50, 0},
		{"zero limit uses default", "limit=0", 50, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/?"+tt.query, nil)
			limit, offset := parsePagination(req)

			if limit != tt.wantLimit {
				t.Errorf("limit: expected %d, got %d", tt.wantLimit, limit)
			}
			if offset != tt.wantOffset {
				t.Errorf("offset: expected %d, got %d", tt.wantOffset, offset)
			}
		})
	}
}

func TestParseTimeRange(t *testing.T) {
	// Use future dates to ensure from < to even with defaults
	futureDate := time.Now().Add(48 * time.Hour).Format(time.RFC3339)

	tests := []struct {
		name      string
		query     string
		checkFrom bool
		checkTo   bool
	}{
		{"default (no params)", "", false, false},
		{"with from", "from=2024-01-01T00:00:00Z", true, false},
		{"with to", "to=" + futureDate, false, true},
		{"with both", "from=2024-01-01T00:00:00Z&to=2024-12-31T23:59:59Z", true, true},
		{"invalid from uses default", "from=invalid", false, false},
		{"invalid to uses default", "to=invalid", false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/?"+tt.query, nil)
			from, to := parseTimeRange(req)

			// Both from and to should be valid times
			if from.IsZero() {
				t.Error("from time should not be zero")
			}
			if to.IsZero() {
				t.Error("to time should not be zero")
			}
		})
	}
}

func TestGetBreakdownValues(t *testing.T) {
	h, cleanup := setupTestHandlers(t)
	defer cleanup()

	tests := []struct {
		name       string
		query      string
		wantStatus int
	}{
		{"missing name parameter", "/api/metrics/breakdown-values?attribute=type", http.StatusBadRequest},
		{"missing attribute parameter", "/api/metrics/breakdown-values?name=test_metric", http.StatusBadRequest},
		{"valid parameters", "/api/metrics/breakdown-values?name=test_metric&attribute=type", http.StatusOK},
		{"valid dotted attribute", "/api/metrics/breakdown-values?name=test_metric&attribute=gen_ai.token.type", http.StatusOK},
		{"with optional service", "/api/metrics/breakdown-values?name=test_metric&attribute=type&service=test-service", http.StatusOK},
		{"invalid attribute", "/api/metrics/breakdown-values?name=test_metric&attribute=type%27)%20OR%201%3D1--", http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.query, nil)
			rec := httptest.NewRecorder()

			h.GetBreakdownValues(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("expected status %d, got %d: %s", tt.wantStatus, rec.Code, rec.Body.String())
			}
		})
	}
}
