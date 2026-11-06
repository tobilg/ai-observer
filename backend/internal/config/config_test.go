package config

import (
	"os"
	"testing"

	"github.com/tobilg/ai-observer/internal/redact"
)

func TestLoad_Defaults(t *testing.T) {
	// Clear relevant env vars
	os.Unsetenv("AI_OBSERVER_OTLP_PORT")
	os.Unsetenv("AI_OBSERVER_API_PORT")
	os.Unsetenv("AI_OBSERVER_DATABASE_PATH")
	os.Unsetenv("AI_OBSERVER_FRONTEND_URL")

	cfg := Load()

	if cfg.OTLPPort != 4318 {
		t.Errorf("OTLPPort = %d, want 4318", cfg.OTLPPort)
	}
	if cfg.APIPort != 8080 {
		t.Errorf("APIPort = %d, want 8080", cfg.APIPort)
	}
	if cfg.DatabasePath != "./data/ai-observer.duckdb" {
		t.Errorf("DatabasePath = %s, want ./data/ai-observer.duckdb", cfg.DatabasePath)
	}
	if cfg.FrontendURL != "http://localhost:5173" {
		t.Errorf("FrontendURL = %s, want http://localhost:5173", cfg.FrontendURL)
	}
}

func TestLoad_CustomValues(t *testing.T) {
	os.Setenv("AI_OBSERVER_OTLP_PORT", "9999")
	os.Setenv("AI_OBSERVER_API_PORT", "3000")
	os.Setenv("AI_OBSERVER_DATABASE_PATH", "/custom/path.duckdb")
	os.Setenv("AI_OBSERVER_FRONTEND_URL", "https://example.com")
	defer func() {
		os.Unsetenv("AI_OBSERVER_OTLP_PORT")
		os.Unsetenv("AI_OBSERVER_API_PORT")
		os.Unsetenv("AI_OBSERVER_DATABASE_PATH")
		os.Unsetenv("AI_OBSERVER_FRONTEND_URL")
	}()

	cfg := Load()

	if cfg.OTLPPort != 9999 {
		t.Errorf("OTLPPort = %d, want 9999", cfg.OTLPPort)
	}
	if cfg.APIPort != 3000 {
		t.Errorf("APIPort = %d, want 3000", cfg.APIPort)
	}
	if cfg.DatabasePath != "/custom/path.duckdb" {
		t.Errorf("DatabasePath = %s, want /custom/path.duckdb", cfg.DatabasePath)
	}
	if cfg.FrontendURL != "https://example.com" {
		t.Errorf("FrontendURL = %s, want https://example.com", cfg.FrontendURL)
	}
}

func TestLoad_InvalidIntFallsBackToDefault(t *testing.T) {
	os.Setenv("AI_OBSERVER_OTLP_PORT", "not-a-number")
	os.Setenv("AI_OBSERVER_API_PORT", "")
	defer func() {
		os.Unsetenv("AI_OBSERVER_OTLP_PORT")
		os.Unsetenv("AI_OBSERVER_API_PORT")
	}()

	cfg := Load()

	if cfg.OTLPPort != 4318 {
		t.Errorf("OTLPPort = %d, want 4318 (default on invalid)", cfg.OTLPPort)
	}
	if cfg.APIPort != 8080 {
		t.Errorf("APIPort = %d, want 8080 (default on empty)", cfg.APIPort)
	}
}

func TestLoad_OutOfRangeValuesFallBackToDefault(t *testing.T) {
	t.Setenv("AI_OBSERVER_OTLP_PORT", "70000")
	t.Setenv("AI_OBSERVER_API_PORT", "0")
	t.Setenv("AI_OBSERVER_RETENTION_DAYS", "-3")

	cfg := Load()

	if cfg.OTLPPort != 4318 {
		t.Errorf("OTLPPort = %d, want 4318 (default for a port above 65535)", cfg.OTLPPort)
	}
	if cfg.APIPort != 8080 {
		t.Errorf("APIPort = %d, want 8080 (default for port 0)", cfg.APIPort)
	}
	if cfg.RetentionDays != 0 {
		t.Errorf("RetentionDays = %d, want 0 (default for a negative value)", cfg.RetentionDays)
	}
}

func TestLoad_RetentionDays(t *testing.T) {
	for value, want := range map[string]int{"30": 30, " 7 ": 7, "30d": 0, "": 0} {
		t.Setenv("AI_OBSERVER_RETENTION_DAYS", value)
		if got := Load().RetentionDays; got != want {
			t.Errorf("RetentionDays for %q = %d, want %d", value, got, want)
		}
	}
}

func TestLoad_HostDefaultsToLoopback(t *testing.T) {
	t.Setenv("AI_OBSERVER_HOST", "")
	t.Setenv("AI_OBSERVER_ALLOWED_HOSTS", "")

	cfg := Load()

	if cfg.Host != "127.0.0.1" {
		t.Errorf("Host = %q, want 127.0.0.1", cfg.Host)
	}
	if len(cfg.AllowedHosts) != 0 {
		t.Errorf("AllowedHosts = %v, want empty", cfg.AllowedHosts)
	}
}

func TestLoad_HostAndAllowedHosts(t *testing.T) {
	t.Setenv("AI_OBSERVER_HOST", "0.0.0.0")
	t.Setenv("AI_OBSERVER_ALLOWED_HOSTS", " observer.lan, ,ai.example.com ")

	cfg := Load()

	if cfg.Host != "0.0.0.0" {
		t.Errorf("Host = %q, want 0.0.0.0", cfg.Host)
	}
	want := []string{"observer.lan", "ai.example.com"}
	if len(cfg.AllowedHosts) != len(want) {
		t.Fatalf("AllowedHosts = %v, want %v", cfg.AllowedHosts, want)
	}
	for i := range want {
		if cfg.AllowedHosts[i] != want[i] {
			t.Errorf("AllowedHosts[%d] = %q, want %q", i, cfg.AllowedHosts[i], want[i])
		}
	}
}

func TestLoad_MCPAndRedaction(t *testing.T) {
	t.Setenv("AI_OBSERVER_MCP", "")
	t.Setenv("AI_OBSERVER_REDACT", "")
	cfg := Load()
	if cfg.MCPEnabled || cfg.Redaction != redact.Secrets {
		t.Errorf("defaults: MCP %v, redaction %q; want off and secrets", cfg.MCPEnabled, cfg.Redaction)
	}

	for value, want := range map[string]bool{"true": true, "1": true, "ON": true, "no": false, "maybe": false} {
		t.Setenv("AI_OBSERVER_MCP", value)
		if got := Load().MCPEnabled; got != want {
			t.Errorf("AI_OBSERVER_MCP=%q: %v, want %v", value, got, want)
		}
	}

	for value, want := range map[string]redact.Mode{"off": redact.Off, "all": redact.All, "bogus": redact.Secrets} {
		t.Setenv("AI_OBSERVER_REDACT", value)
		if got := Load().Redaction; got != want {
			t.Errorf("AI_OBSERVER_REDACT=%q: %q, want %q", value, got, want)
		}
	}
}
