package middleware

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestPayloadLimitRejectsDeclaredLargeBody(t *testing.T) {
	called := false
	handler := PayloadLimitMiddleware(10)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))

	req := httptest.NewRequest(http.MethodPost, "/v1/logs", strings.NewReader(strings.Repeat("x", 11)))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want 413", rec.Code)
	}
	if called {
		t.Error("handler ran for an oversized body")
	}
}

// Bodies without a declared length (chunked, or decompressed by the gzip
// middleware) are cut off while reading
func TestPayloadLimitCapsUndeclaredBody(t *testing.T) {
	var readErr error
	handler := PayloadLimitMiddleware(10)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, readErr = io.ReadAll(r.Body)
	}))

	req := httptest.NewRequest(http.MethodPost, "/v1/logs", strings.NewReader(strings.Repeat("x", 11)))
	req.ContentLength = -1
	handler.ServeHTTP(httptest.NewRecorder(), req)

	if readErr == nil {
		t.Error("reading more than the limit must fail")
	}
}

func TestPayloadLimitAllowsSmallBody(t *testing.T) {
	var body []byte
	handler := PayloadLimitMiddleware(10)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ = io.ReadAll(r.Body)
	}))

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/", strings.NewReader("small")))

	if string(body) != "small" {
		t.Errorf("body = %q", body)
	}
}

func TestContextTimeoutSetsDeadline(t *testing.T) {
	var deadline time.Time
	var hasDeadline bool
	handler := ContextTimeoutMiddleware(time.Minute)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		deadline, hasDeadline = r.Context().Deadline()
	}))

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/logs", nil))

	if !hasDeadline || time.Until(deadline) > time.Minute {
		t.Errorf("deadline = %v (set: %v), want within a minute", deadline, hasDeadline)
	}
}

// WebSocket connections live far longer than an API request
func TestContextTimeoutSkipsWebSocketUpgrade(t *testing.T) {
	hasDeadline := true
	handler := ContextTimeoutMiddleware(time.Minute)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, hasDeadline = r.Context().Deadline()
	}))

	req := httptest.NewRequest(http.MethodGet, "/ws", nil)
	req.Header.Set("Upgrade", "websocket")
	handler.ServeHTTP(httptest.NewRecorder(), req)

	if hasDeadline {
		t.Error("WebSocket upgrade requests must not get a deadline")
	}
}
