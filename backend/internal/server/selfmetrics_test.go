package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tobilg/ai-observer/internal/api"
	"github.com/tobilg/ai-observer/internal/selfmetrics"
)

func TestSelfMetricsEndpoints(t *testing.T) {
	server := newAuthServer(t)
	authed := func(req *http.Request) *http.Request {
		req.Header.Set("Authorization", "Bearer s3cret")
		return req
	}

	exports := selfmetrics.OTLPRequests.With("http", "logs", "ok").Value()
	unauthorized := selfmetrics.OTLPRequests.With("http", "logs", "unauthorized").Value()
	stored := selfmetrics.RecordsStored.With("logs").Value()
	body := `{"resourceLogs":[{"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"svc"}}]},"scopeLogs":[{"logRecords":[{"timeUnixNano":"1700000000000000000","body":{"stringValue":"hi"}}]}]}]}`
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/logs", strings.NewReader(body)))
	req.Header.Set("Content-Type", "application/json")
	if rec := serve(server.otlpRouter, req); rec.Code != http.StatusOK {
		t.Fatalf("export: %d %s", rec.Code, rec.Body.String())
	}
	serve(server.otlpRouter, httptest.NewRequest(http.MethodPost, "/v1/logs", strings.NewReader(body)))
	if got := selfmetrics.OTLPRequests.With("http", "logs", "ok").Value(); got != exports+1 {
		t.Errorf("ok exports = %d, want %d", got, exports+1)
	}
	if got := selfmetrics.OTLPRequests.With("http", "logs", "unauthorized").Value(); got != unauthorized+1 {
		t.Errorf("unauthorized exports = %d, want %d", got, unauthorized+1)
	}
	if got := selfmetrics.RecordsStored.With("logs").Value(); got != stored+1 {
		t.Errorf("stored logs = %d, want %d", got, stored+1)
	}

	// API requests are timed by route pattern
	requests := selfmetrics.APIRequestSeconds.With("/api/services").Count()
	serve(server.apiRouter, authed(httptest.NewRequest(http.MethodGet, "/api/services", nil)))
	if got := selfmetrics.APIRequestSeconds.With("/api/services").Count(); got != requests+1 {
		t.Errorf("timed /api/services requests = %d, want %d", got, requests+1)
	}

	// Both forms need the token
	for _, path := range []string{"/metrics", "/api/internal/metrics"} {
		if rec := serve(server.apiRouter, httptest.NewRequest(http.MethodGet, path, nil)); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s without token: %d", path, rec.Code)
		}
	}

	rec := serve(server.apiRouter, authed(httptest.NewRequest(http.MethodGet, "/metrics", nil)))
	if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/plain; version=0.0.4") {
		t.Fatalf("/metrics: %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	text := rec.Body.String()
	for _, want := range []string{
		"# TYPE ai_observer_otlp_requests_total counter",
		`ai_observer_otlp_requests_total{transport="http",signal="logs",result="ok"}`,
		"# TYPE ai_observer_write_transaction_seconds histogram",
		`ai_observer_api_request_seconds_bucket{route="/api/services",le="+Inf"}`,
		"ai_observer_websocket_clients 0",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("/metrics lacks %q", want)
		}
	}

	rec = serve(server.apiRouter, authed(httptest.NewRequest(http.MethodGet, "/api/internal/metrics", nil)))
	var snapshot api.InternalMetricsResponse
	if err := json.NewDecoder(rec.Body).Decode(&snapshot); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("/api/internal/metrics: %d %v", rec.Code, err)
	}
	names := map[string]string{}
	for _, m := range snapshot.Metrics {
		names[m.Name] = m.Type
	}
	for name, typ := range map[string]string{
		"ai_observer_otlp_requests_total": "counter", "ai_observer_write_lock_wait_seconds": "histogram",
		"ai_observer_websocket_clients": "gauge", "ai_observer_retention_deleted_rows_total": "counter",
		"ai_observer_watcher_files_processed_total": "counter", "ai_observer_noise_spans_dropped_total": "counter",
	} {
		if names[name] != typ {
			t.Errorf("%s: type %q, want %s", name, names[name], typ)
		}
	}
}
