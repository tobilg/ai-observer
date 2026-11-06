package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/tobilg/ai-observer/internal/middleware"
)

// A batch that could not be stored is answered with 503, which OTLP
// exporters retry (they drop batches answered with 500)
func TestOTLPStoreFailureIsRetryable(t *testing.T) {
	h, cleanup := setupTestHandlers(t)
	defer cleanup()
	if _, err := h.store.DB().Exec("DROP TABLE otel_logs"); err != nil {
		t.Fatal(err)
	}

	body, _ := json.Marshal(createLogsPayload())
	req := httptest.NewRequest(http.MethodPost, "/v1/logs", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.HandleLogs(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Error("missing Retry-After header")
	}
}

// A body over the payload limit without a Content-Length is a 413, not a
// decoding error
func TestOTLPOversizedPayloadIs413(t *testing.T) {
	h, cleanup := setupTestHandlers(t)
	defer cleanup()
	handler := middleware.PayloadLimitMiddleware(64)(http.HandlerFunc(h.HandleMetrics))

	body, _ := json.Marshal(createMetricsPayload())
	req := httptest.NewRequest(http.MethodPost, "/v1/metrics", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.ContentLength = -1 // chunked: only the body reader can enforce the limit
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want 413: %s", rec.Code, rec.Body.String())
	}
}
