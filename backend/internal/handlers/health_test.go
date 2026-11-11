package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/tobilg/ai-observer/internal/api"
)

func TestHealthAndReady(t *testing.T) {
	h, cleanup := setupTestHandlers(t)

	get := func(handler http.HandlerFunc, v any) int {
		rec := httptest.NewRecorder()
		handler(rec, httptest.NewRequest(http.MethodGet, "/", nil))
		if err := json.NewDecoder(rec.Body).Decode(v); err != nil {
			t.Fatal(err)
		}
		return rec.Code
	}
	var health api.HealthResponse
	if code := get(h.Health, &health); code != http.StatusOK || health.Status != "ok" {
		t.Errorf("health = %d %+v", code, health)
	}
	var ready api.ReadyResponse
	if code := get(h.Ready, &ready); code != http.StatusOK || ready.Status != "ok" || ready.Database != "ok" {
		t.Errorf("ready = %d %+v", code, ready)
	}

	// A closed database is not ready; liveness is unaffected
	cleanup()
	if code := get(h.Ready, &ready); code != http.StatusServiceUnavailable || ready.Status != "unavailable" || ready.Error == "" {
		t.Errorf("ready with a closed database = %d %+v", code, ready)
	}
	if code := get(h.Health, &health); code != http.StatusOK {
		t.Errorf("health with a closed database = %d", code)
	}
}
