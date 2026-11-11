package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tobilg/ai-observer/internal/api"
	"github.com/tobilg/ai-observer/internal/watcher"
)

// GET /api/status is behind the token like the rest of the API, and names
// settings without their secrets
func TestStatusEndpoint(t *testing.T) {
	server := newAuthServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	if rec := serve(server.apiRouter, req); rec.Code != http.StatusUnauthorized {
		t.Errorf("without token: got %d, want 401", rec.Code)
	}

	get := func() (api.StatusResponse, string) {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
		req.Header.Set("Authorization", "Bearer s3cret")
		rec := serve(server.apiRouter, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("with token: got %d: %s", rec.Code, rec.Body.String())
		}
		body := rec.Body.String()
		var status api.StatusResponse
		if err := json.Unmarshal([]byte(body), &status); err != nil {
			t.Fatal(err)
		}
		return status, body
	}
	status, body := get()
	if strings.Contains(body, "s3cret") {
		t.Error("the status contains the token")
	}
	// The test configuration sets no redaction mode: nothing is redacted
	if status.Mode != "serve" || !status.Settings.AuthEnabled || status.Settings.Redaction != "off" ||
		status.Server.APIPort != 18080 || status.Server.OTLPPort != 14318 || status.Server.OTLPEnabled {
		t.Errorf("status = %+v", status)
	}
	if !strings.HasSuffix(status.Database.Path, "test.duckdb") || status.Database.SizeBytes == 0 {
		t.Errorf("database = %+v", status.Database)
	}

	// Watching session files: watch mode
	t.Setenv("AI_OBSERVER_CLAUDE_PATH", t.TempDir())
	t.Setenv("AI_OBSERVER_CODEX_PATH", t.TempDir())
	t.Setenv("AI_OBSERVER_GEMINI_PATH", t.TempDir())
	if err := server.StartWatcher(watcher.Config{Tools: []string{"claude-code"}}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { server.fileWatcher.Stop() })
	if status, _ := get(); status.Mode != "watch" {
		t.Errorf("mode with a file watcher = %q, want watch", status.Mode)
	}
}
