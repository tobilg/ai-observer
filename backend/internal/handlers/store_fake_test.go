package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/tobilg/ai-observer/internal/api"
	"github.com/tobilg/ai-observer/internal/storage"
)

// failingStore is a Store whose calls fail with err. Calls it does not
// override panic (the embedded nil Store), so a test notices a call it did
// not expect.
type failingStore struct {
	Store
	err error
}

func (f failingStore) QueryLogs(context.Context, string, string, string, string, time.Time, time.Time, storage.Page) (*api.LogsResponse, error) {
	return nil, f.err
}

func (f failingStore) QueryTraces(context.Context, storage.TraceFilter, time.Time, time.Time, storage.Page) (*api.TracesResponse, error) {
	return nil, f.err
}

func (f failingStore) GetDashboards(context.Context) ([]api.Dashboard, error) {
	return nil, f.err
}

func (f failingStore) GetStats(context.Context) (*api.StatsResponse, error) {
	return nil, f.err
}

func (f failingStore) InsertTelemetry(context.Context, []api.LogRecord, []api.MetricDataPoint, []api.Span) error {
	return f.err
}

func (f failingStore) InsertMetrics(context.Context, []api.MetricDataPoint) error {
	return f.err
}

func (f failingStore) InsertTraces(context.Context, []api.Span, func([]api.Span) []api.MetricDataPoint) ([]api.Span, []api.MetricDataPoint, error) {
	return nil, nil, f.err
}

func (f failingStore) Ping(context.Context) error {
	return f.err
}

func decodeError(t *testing.T, rec *httptest.ResponseRecorder) api.ErrorResponse {
	t.Helper()
	var body api.ErrorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("error body %q: %v", rec.Body, err)
	}
	return body
}

// Query errors map to the status the client must act on: a timeout is a
// 504 (not retried), an invalid cursor or a too deep offset a 400, any other
// failure a 500 with a generic message
func TestQueryErrorMappingWithFailingStore(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{"timeout", fmt.Errorf("querying logs: %w", context.DeadlineExceeded), http.StatusGatewayTimeout},
		{"invalid cursor", storage.ErrInvalidCursor, http.StatusBadRequest},
		{"offset too large", storage.ErrOffsetTooLarge, http.StatusBadRequest},
		{"store failure", errors.New("IO Error: disk full"), http.StatusInternalServerError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := New(failingStore{err: tt.err}, nil)
			for _, route := range []struct {
				path    string
				handler http.HandlerFunc
			}{
				{"/api/logs", h.QueryLogs},
				{"/api/traces", h.QueryTraces},
			} {
				rec := httptest.NewRecorder()
				route.handler(rec, httptest.NewRequest(http.MethodGet, route.path, nil))
				if rec.Code != tt.want {
					t.Fatalf("%s: status %d, want %d: %s", route.path, rec.Code, tt.want, rec.Body)
				}
				if body := decodeError(t, rec); strings.Contains(body.Message, "disk full") {
					t.Errorf("%s: the store's error reaches the client: %q", route.path, body.Message)
				}
			}
		})
	}
}

// A request whose client has gone away gets no answer written
func TestQueryCanceledByClientWritesNothing(t *testing.T) {
	h := New(failingStore{err: context.Canceled}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	rec := httptest.NewRecorder()
	h.QueryLogs(rec, httptest.NewRequest(http.MethodGet, "/api/logs", nil).WithContext(ctx))
	if rec.Body.Len() != 0 {
		t.Fatalf("wrote %q for a canceled request", rec.Body)
	}
}

// Dashboards and stats report store failures as 500
func TestDashboardAndStatsStoreFailure(t *testing.T) {
	h := New(failingStore{err: errors.New("broken")}, nil)
	for path, handler := range map[string]http.HandlerFunc{
		"/api/dashboards": h.ListDashboards,
		"/api/stats":      h.GetStats,
		"/api/status":     h.GetStatus,
	} {
		rec := httptest.NewRecorder()
		handler(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusInternalServerError {
			t.Errorf("%s: status %d, want 500: %s", path, rec.Code, rec.Body)
		}
	}
}

// Telemetry that cannot be stored is answered 503 with Retry-After, which
// OTLP exporters retry (a 500 would make them drop the batch)
func TestOTLPStoreFailureIs503(t *testing.T) {
	h := New(failingStore{err: errors.New("database is closed")}, nil)
	logs, _ := json.Marshal(createLogsPayload())
	metrics, _ := json.Marshal(createMetricsPayload())
	traces, _ := json.Marshal(createTracesPayload())
	for _, tc := range []struct {
		path    string
		body    []byte
		handler http.HandlerFunc
	}{
		{"/v1/logs", logs, h.HandleLogs},
		{"/v1/metrics", metrics, h.HandleMetrics},
		{"/v1/traces", traces, h.HandleTraces},
	} {
		rec := postOTLP(tc.handler, tc.path, tc.body, false)
		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("%s: status %d, want 503: %s", tc.path, rec.Code, rec.Body)
		}
		if rec.Header().Get("Retry-After") == "" {
			t.Errorf("%s: no Retry-After", tc.path)
		}
	}
}

// GET /ready is 503 while the database does not answer
func TestReadyWithFailingStore(t *testing.T) {
	h := New(failingStore{err: context.DeadlineExceeded}, nil)
	rec := httptest.NewRecorder()
	h.Ready(rec, httptest.NewRequest(http.MethodGet, "/ready", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status %d, want 503", rec.Code)
	}
}
