package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newAuthServer(t *testing.T) *Server {
	t.Helper()
	cfg := getTestConfig(t)
	cfg.AuthToken = "s3cret"
	server, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	t.Cleanup(func() { server.storage.Close() })
	return server
}

func serve(router http.Handler, req *http.Request) *httptest.ResponseRecorder {
	req.Host = "localhost:8080"
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func TestTokenAuthOnOTLP(t *testing.T) {
	server := newAuthServer(t)
	body := `{"resourceLogs":[]}`

	tests := []struct {
		name   string
		header string
		want   int
	}{
		{"no token", "", http.StatusUnauthorized},
		{"wrong token", "Bearer nope", http.StatusUnauthorized},
		{"bearer token", "Bearer s3cret", http.StatusOK},
		{"case-insensitive scheme", "bearer s3cret", http.StatusOK},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/v1/logs", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			if tc.header != "" {
				req.Header.Set("Authorization", tc.header)
			}
			if rec := serve(server.otlpRouter, req); rec.Code != tc.want {
				t.Errorf("got %d, want %d", rec.Code, tc.want)
			}
		})
	}

	// Health checks need no token
	if rec := serve(server.otlpRouter, httptest.NewRequest(http.MethodGet, "/health", nil)); rec.Code != http.StatusOK {
		t.Errorf("health: got %d, want 200", rec.Code)
	}
}

func TestTokenAuthOnAPI(t *testing.T) {
	server := newAuthServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/services", nil)
	if rec := serve(server.apiRouter, req); rec.Code != http.StatusUnauthorized {
		t.Errorf("without token: got %d, want 401", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/services", nil)
	req.Header.Set("Authorization", "Bearer s3cret")
	if rec := serve(server.apiRouter, req); rec.Code != http.StatusOK {
		t.Errorf("with bearer token: got %d, want 200", rec.Code)
	}

	// Opening the dashboard with ?token= sets a session cookie and drops the
	// token from the URL
	req = httptest.NewRequest(http.MethodGet, "/sessions?token=s3cret&service=codex", nil)
	rec := serve(server.apiRouter, req)
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/sessions?service=codex" {
		t.Fatalf("token link: got %d to %q, want a redirect to /sessions?service=codex", rec.Code, rec.Header().Get("Location"))
	}
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != authCookieName || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode {
		t.Fatalf("unexpected cookies %+v", cookies)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/services", nil)
	req.AddCookie(cookies[0])
	if rec := serve(server.apiRouter, req); rec.Code != http.StatusOK {
		t.Errorf("with cookie: got %d, want 200", rec.Code)
	}

	// The cookie holds no token: it cannot be used to send telemetry
	if cookies[0].Value == "s3cret" {
		t.Error("the cookie holds the token")
	}
	req = httptest.NewRequest(http.MethodPost, "/v1/logs", nil)
	req.Header.Set("Authorization", "Bearer "+cookies[0].Value)
	if rec := serve(server.otlpRouter, req); rec.Code != http.StatusUnauthorized {
		t.Errorf("OTLP with the cookie value: got %d, want 401", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/?token=wrong", nil)
	if rec := serve(server.apiRouter, req); rec.Code != http.StatusUnauthorized {
		t.Errorf("wrong token link: got %d, want 401", rec.Code)
	}

	// CORS preflights carry no credentials
	req = httptest.NewRequest(http.MethodOptions, "/api/dashboards", nil)
	req.Header.Set("Origin", "http://localhost:5173")
	req.Header.Set("Access-Control-Request-Method", "POST")
	if rec := serve(server.apiRouter, req); rec.Code == http.StatusUnauthorized {
		t.Error("preflight was rejected")
	}
}

func TestNoTokenAuthByDefault(t *testing.T) {
	server, err := New(getTestConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	defer server.storage.Close()
	if rec := serve(server.apiRouter, httptest.NewRequest(http.MethodGet, "/api/services", nil)); rec.Code != http.StatusOK {
		t.Errorf("got %d, want 200", rec.Code)
	}
}

func TestAPIRejectsOversizedBodies(t *testing.T) {
	server, err := New(getTestConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	defer server.storage.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/dashboards", strings.NewReader(strings.Repeat("x", apiMaxPayloadBytes+1)))
	req.Header.Set("Content-Type", "application/json")
	if rec := serve(server.apiRouter, req); rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("got %d, want 413", rec.Code)
	}

	// A body without a declared length is cut off while it is decoded
	body := `{"name": "` + strings.Repeat("x", apiMaxPayloadBytes) + `"}`
	req = httptest.NewRequest(http.MethodPost, "/api/dashboards", strings.NewReader(body))
	req.ContentLength = -1
	req.Header.Set("Content-Type", "application/json")
	if rec := serve(server.apiRouter, req); rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("undeclared length: got %d, want 413", rec.Code)
	}
}
