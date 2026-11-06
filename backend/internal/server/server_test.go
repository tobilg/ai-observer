package server

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	gorillaws "github.com/gorilla/websocket"
	"github.com/tobilg/ai-observer/internal/api"
	"github.com/tobilg/ai-observer/internal/config"
	"github.com/tobilg/ai-observer/internal/watcher"
)

// getTestConfig returns a config with test-appropriate ports
func getTestConfig(t *testing.T) *config.Config {
	t.Helper()

	// Create a temp directory for the test database
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.duckdb")

	return &config.Config{
		Host:         "127.0.0.1",
		APIPort:      18080, // Use non-standard ports for testing
		OTLPPort:     14318,
		DatabasePath: dbPath,
		FrontendURL:  "http://localhost:5173",
	}
}

func TestNewServer(t *testing.T) {
	cfg := getTestConfig(t)

	server, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	if server == nil {
		t.Fatal("Server is nil")
	}

	if server.storage == nil {
		t.Error("Server storage is nil")
	}

	if server.wsHub == nil {
		t.Error("Server wsHub is nil")
	}

	if server.config == nil {
		t.Error("Server config is nil")
	}

	// Cleanup
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	server.storage.Close()
	_ = ctx
}

func TestServerStartAndShutdown(t *testing.T) {
	cfg := getTestConfig(t)

	server, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	// Start server in goroutine
	serverErr := make(chan error, 1)
	go func() {
		err := server.ListenAndServe()
		if err != nil && err != http.ErrServerClosed {
			serverErr <- err
		}
		close(serverErr)
	}()

	// Wait for server to start
	time.Sleep(100 * time.Millisecond)

	// Check for startup errors
	select {
	case err := <-serverErr:
		if err != nil {
			t.Fatalf("Server failed to start: %v", err)
		}
	default:
		// No error, server is running
	}

	// Test that API endpoint is responding
	resp, err := http.Get("http://localhost:18080/health")
	if err != nil {
		t.Errorf("Failed to reach health endpoint: %v", err)
	} else {
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}
	}

	// Test graceful shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		t.Errorf("Shutdown returned error: %v", err)
	}
}

func TestServerShutdownTimeout(t *testing.T) {
	cfg := getTestConfig(t)

	server, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	// Start server
	go func() {
		_ = server.ListenAndServe()
	}()

	time.Sleep(100 * time.Millisecond)

	// Shutdown with very short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()

	// This may or may not error depending on how fast shutdown completes
	// The important thing is it doesn't hang
	start := time.Now()
	_ = server.Shutdown(ctx)
	elapsed := time.Since(start)

	// Should complete within a reasonable time (not hang forever)
	if elapsed > 1*time.Second {
		t.Errorf("Shutdown took too long: %v", elapsed)
	}
}

func TestServerShutdownBeforeStart(t *testing.T) {
	cfg := getTestConfig(t)

	server, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	// Shutdown without starting - should not panic
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	// This should succeed without error
	err = server.Shutdown(ctx)
	if err != nil {
		t.Errorf("Shutdown before start returned error: %v", err)
	}
}

func TestServerMultiplePorts(t *testing.T) {
	cfg := getTestConfig(t)

	server, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	go func() {
		_ = server.ListenAndServe()
	}()

	time.Sleep(200 * time.Millisecond)

	// Test API server port
	apiResp, err := http.Get("http://localhost:18080/health")
	if err != nil {
		t.Errorf("Failed to reach API server: %v", err)
	} else {
		apiResp.Body.Close()
	}

	// Test OTLP server port
	otlpResp, err := http.Post("http://localhost:14318/v1/traces", "application/json", nil)
	if err != nil {
		t.Errorf("Failed to reach OTLP server: %v", err)
	} else {
		otlpResp.Body.Close()
	}

	// Cleanup
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	server.Shutdown(ctx)
}

func TestServerDatabaseCreation(t *testing.T) {
	cfg := getTestConfig(t)

	server, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	// Check that database file was created
	if _, err := os.Stat(cfg.DatabasePath); os.IsNotExist(err) {
		t.Error("Database file was not created")
	}

	// Cleanup
	server.storage.Close()
}

func TestServerConcurrentRequests(t *testing.T) {
	cfg := getTestConfig(t)

	server, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	go func() {
		_ = server.ListenAndServe()
	}()

	time.Sleep(200 * time.Millisecond)

	// Make concurrent requests
	const numRequests = 50
	results := make(chan error, numRequests)

	for i := 0; i < numRequests; i++ {
		go func() {
			resp, err := http.Get("http://localhost:18080/health")
			if err != nil {
				results <- err
				return
			}
			resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				results <- err
			} else {
				results <- nil
			}
		}()
	}

	// Collect results
	var errors int
	for i := 0; i < numRequests; i++ {
		if err := <-results; err != nil {
			errors++
		}
	}

	if errors > 0 {
		t.Errorf("%d/%d requests failed", errors, numRequests)
	}

	// Cleanup
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	server.Shutdown(ctx)
}

func TestHostCheck(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })

	tests := []struct {
		name    string
		allowed []string
		host    string
		want    int
	}{
		{"localhost with port", nil, "localhost:8080", http.StatusOK},
		{"localhost subdomain", nil, "app.localhost:8080", http.StatusOK},
		{"IPv4 loopback", nil, "127.0.0.1:8080", http.StatusOK},
		{"IPv6 loopback", nil, "[::1]:8080", http.StatusOK},
		{"LAN IP literal", nil, "192.168.1.20:8080", http.StatusOK},
		{"rebinding domain", nil, "attacker.example.com:8080", http.StatusForbidden},
		{"allowed name", []string{"observer.lan"}, "Observer.LAN:8080", http.StatusOK},
		{"other name with list", []string{"observer.lan"}, "evil.lan", http.StatusForbidden},
		{"wildcard disables check", []string{"*"}, "attacker.example.com", http.StatusOK},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/stats", nil)
			req.Host = tc.host
			rec := httptest.NewRecorder()

			HostCheck(tc.allowed)(ok).ServeHTTP(rec, req)

			if rec.Code != tc.want {
				t.Errorf("Host %q: got status %d, want %d", tc.host, rec.Code, tc.want)
			}
		})
	}
}

func TestServerAPIRejectsRebindingHost(t *testing.T) {
	cfg := getTestConfig(t)
	server, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	defer server.storage.Close()

	req := httptest.NewRequest(http.MethodGet, "/api/stats", nil)
	req.Host = "attacker.example.com"
	rec := httptest.NewRecorder()
	server.apiRouter.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403 for foreign host, got %d", rec.Code)
	}

	// The frontend URL's host is accepted
	req = httptest.NewRequest(http.MethodGet, "/health", nil)
	req.Host = "localhost:8080"
	rec = httptest.NewRecorder()
	server.apiRouter.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 for localhost, got %d", rec.Code)
	}
}

func TestServerListenAndServeFailsWhenOTLPPortBusy(t *testing.T) {
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to reserve port: %v", err)
	}
	defer busy.Close()

	cfg := getTestConfig(t)
	cfg.OTLPPort = busy.Addr().(*net.TCPAddr).Port
	server, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	defer server.Shutdown(context.Background())

	errCh := make(chan error, 1)
	go func() { errCh <- server.ListenAndServe() }()

	select {
	case err := <-errCh:
		if err == nil || err == http.ErrServerClosed {
			t.Fatalf("expected startup error for busy OTLP port, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ListenAndServe kept running although the OTLP port was unavailable")
	}
}

func TestServerDoesNotStartAfterShutdown(t *testing.T) {
	cfg := getTestConfig(t)
	server, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	if err := server.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown failed: %v", err)
	}

	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		t.Errorf("expected http.ErrServerClosed, got %v", err)
	}
	if err := server.StartWatcher(watcher.Config{}); err != http.ErrServerClosed {
		t.Errorf("StartWatcher after Shutdown: expected http.ErrServerClosed, got %v", err)
	}
}

func TestOriginPolicy(t *testing.T) {
	policy := NewOriginPolicy([]string{"http://localhost:5173"}, []string{"observer.lan"})

	tests := []struct {
		name   string
		origin string
		host   string
		want   bool
	}{
		{"no origin (CLI, OTLP exporter)", "", "localhost:4318", true},
		{"same origin via IP", "http://127.0.0.1:8080", "127.0.0.1:8080", true},
		{"same origin via LAN IP", "http://192.168.1.5:8080", "192.168.1.5:8080", true},
		{"configured origin", "http://localhost:5173", "localhost:8080", true},
		{"loopback dev server", "http://localhost:5174", "localhost:8080", true},
		{"allowed host name", "https://observer.lan", "127.0.0.1:8080", true},
		{"foreign site", "https://attacker.example.com", "localhost:4318", false},
		{"opaque origin", "null", "localhost:4318", false},
		{"foreign IP", "http://192.168.1.9:8080", "192.168.1.5:8080", false},
		{"rebound host name (DNS rebinding)", "http://attacker.example.com:4318", "attacker.example.com:4318", false},
		{"same origin via allowed host name", "http://observer.lan:8080", "observer.lan:8080", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/v1/logs", nil)
			req.Host = tc.host
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}
			if got := policy.Allowed(req); got != tc.want {
				t.Errorf("Allowed(origin %q, host %q) = %v, want %v", tc.origin, tc.host, got, tc.want)
			}
		})
	}

	any := NewOriginPolicy(nil, []string{"*"})
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("Origin", "https://attacker.example.com")
	if !any.Allowed(req) {
		t.Error(`"*" in allowed hosts must accept every origin`)
	}
}

func TestServerRejectsCrossSiteOTLPWrites(t *testing.T) {
	cfg := getTestConfig(t)
	server, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	defer server.storage.Close()

	// A web page can send this without a CORS preflight
	body := `{"resourceLogs":[]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/logs", strings.NewReader(body))
	req.Header.Set("Content-Type", "text/plain")
	req.Header.Set("Origin", "https://attacker.example.com")
	rec := httptest.NewRecorder()
	server.otlpRouter.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("cross-site OTLP write: got %d, want 403", rec.Code)
	}

	// Exporters send no Origin
	req = httptest.NewRequest(http.MethodPost, "/v1/logs", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	server.otlpRouter.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("exporter write: got %d, want 200", rec.Code)
	}
}

func TestServerAcceptsSameOriginWebSocketViaIP(t *testing.T) {
	cfg := getTestConfig(t)
	server, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	defer server.storage.Close()

	srv := httptest.NewServer(server.apiRouter)
	defer srv.Close()

	// The dashboard opened at http://127.0.0.1:<port>
	header := http.Header{"Origin": []string{srv.URL}}
	conn, resp, err := gorillaws.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http")+"/ws", header)
	if err != nil {
		status := 0
		if resp != nil {
			status = resp.StatusCode
		}
		t.Fatalf("same-origin WebSocket rejected (status %d): %v", status, err)
	}
	conn.Close()

	header = http.Header{"Origin": []string{"https://attacker.example.com"}}
	if conn, _, err := gorillaws.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http")+"/ws", header); err == nil {
		conn.Close()
		t.Error("cross-site WebSocket must be rejected")
	}
}

func TestRetentionDeletesExpiredTelemetry(t *testing.T) {
	cfg := getTestConfig(t)
	server, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	defer server.storage.Close()

	ctx := context.Background()
	now := time.Now()
	if err := server.storage.InsertLogs(ctx, []api.LogRecord{
		// Deleted a day at a time, from the oldest record up to the cutoff
		{Timestamp: now.AddDate(0, 0, -40), ServiceName: "old"},
		{Timestamp: now.AddDate(0, 0, -30).Add(-time.Minute), ServiceName: "old"},
		{Timestamp: now.Add(-time.Hour), ServiceName: "recent"},
	}); err != nil {
		t.Fatalf("InsertLogs: %v", err)
	}

	server.applyRetention(ctx, now.AddDate(0, 0, -30))

	services, err := server.storage.GetServices(ctx)
	if err != nil {
		t.Fatalf("GetServices: %v", err)
	}
	if len(services) != 1 || services[0] != "recent" {
		t.Errorf("services after retention = %v, want only recent", services)
	}
}

func TestRetentionStopsOnShutdown(t *testing.T) {
	cfg := getTestConfig(t)
	cfg.RetentionDays = 30
	server, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	server.startRetention()
	if server.stopRetention == nil {
		t.Fatal("retention did not start")
	}
	if err := server.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
}
