package config

import (
	"os"
	"strings"
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

	t.Setenv("AI_OBSERVER_STORE_ALL_SPANS", "")
	if Load().StoreAllSpans {
		t.Error("AI_OBSERVER_STORE_ALL_SPANS is on by default")
	}
	t.Setenv("AI_OBSERVER_STORE_ALL_SPANS", "true")
	if !Load().StoreAllSpans {
		t.Error("AI_OBSERVER_STORE_ALL_SPANS=true is not read")
	}

	for value, want := range map[string]redact.Mode{"off": redact.Off, "all": redact.All, "bogus": redact.Secrets} {
		t.Setenv("AI_OBSERVER_REDACT", value)
		if got := Load().Redaction; got != want {
			t.Errorf("AI_OBSERVER_REDACT=%q: %q, want %q", value, got, want)
		}
	}
}

func TestLoad_DatabaseLimits(t *testing.T) {
	t.Setenv("AI_OBSERVER_DB_MEMORY_LIMIT", "")
	t.Setenv("AI_OBSERVER_DB_THREADS", "")
	if cfg := Load(); cfg.DBMemoryLimit != "" || cfg.DBThreads != 0 {
		t.Errorf("defaults: memory limit %q, threads %d; want DuckDB's (unset)", cfg.DBMemoryLimit, cfg.DBThreads)
	}

	for value, want := range map[string]string{
		"2GB": "2GB", " 512MiB ": "512MiB", "1.5 GB": "1.5 GB",
		"2": "", "lots": "", "80%": "", "1GB'; DROP TABLE otel_logs; --": "",
	} {
		t.Setenv("AI_OBSERVER_DB_MEMORY_LIMIT", value)
		if got := Load().DBMemoryLimit; got != want {
			t.Errorf("AI_OBSERVER_DB_MEMORY_LIMIT=%q: %q, want %q", value, got, want)
		}
	}
	for value, want := range map[string]int{"4": 4, "0": 0, "-1": 0, "many": 0} {
		t.Setenv("AI_OBSERVER_DB_THREADS", value)
		if got := Load().DBThreads; got != want {
			t.Errorf("AI_OBSERVER_DB_THREADS=%q: %d, want %d", value, got, want)
		}
	}

	t.Setenv("AI_OBSERVER_DB_MEMORY_LIMIT", "1GB")
	t.Setenv("AI_OBSERVER_DB_THREADS", "2")
	if opts := Load().StorageOptions(); opts.MemoryLimit != "1GB" || opts.Threads != 2 {
		t.Errorf("storage options = %+v", opts)
	}
}

func TestLogFormat(t *testing.T) {
	for value, want := range map[string]string{"": "text", "text": "text", "JSON": "json", " json ": "json"} {
		t.Setenv("AI_OBSERVER_LOG_FORMAT", value)
		if got, err := LogFormat(); got != want || err != nil {
			t.Errorf("%q: format = %q, err = %v", value, got, err)
		}
	}
	t.Setenv("AI_OBSERVER_LOG_FORMAT", "yaml")
	if got, err := LogFormat(); got != "text" || err == nil {
		t.Errorf("invalid format: %q, %v; want text and an error", got, err)
	}
}

// OTLP/gRPC listens on 4317 unless AI_OBSERVER_OTLP_GRPC_PORT names another
// port or turns it off
func TestLoad_OTLPGRPCPort(t *testing.T) {
	tests := []struct {
		value string
		want  int
	}{
		{"", 4317},
		{"14317", 14317},
		{"0", 0},
		{"off", 0},
		{" OFF ", 0},
		{"disabled", 0},
		{"70000", 4317}, // out of range: default, with a warning
		{"grpc", 4317},
	}
	for _, tt := range tests {
		t.Setenv("AI_OBSERVER_OTLP_GRPC_PORT", tt.value)
		if got := Load().OTLPGRPCPort; got != tt.want {
			t.Errorf("AI_OBSERVER_OTLP_GRPC_PORT=%q: OTLPGRPCPort = %d, want %d", tt.value, got, tt.want)
		}
	}
}

func TestLogLevel(t *testing.T) {
	for value, want := range map[string]string{"": "INFO", "debug": "DEBUG", " Warn ": "WARN", "WARNING": "WARN", "ERROR": "ERROR"} {
		t.Setenv("AI_OBSERVER_LOG_LEVEL", value)
		if got, err := LogLevel(); got != want || err != nil {
			t.Errorf("%q: level = %q, err = %v", value, got, err)
		}
	}
	t.Setenv("AI_OBSERVER_LOG_LEVEL", "verbose")
	if got, err := LogLevel(); got != "INFO" || err == nil {
		t.Errorf("invalid level: %q, %v; want INFO and an error", got, err)
	}
	t.Setenv("AI_OBSERVER_LOG_FORMAT", "yaml")
	if level, format, err := LogSettings(); level != "INFO" || format != "text" || err == nil ||
		!strings.Contains(err.Error(), "AI_OBSERVER_LOG_LEVEL") || !strings.Contains(err.Error(), "AI_OBSERVER_LOG_FORMAT") {
		t.Errorf("LogSettings = %q, %q, %v; want the defaults and both errors", level, format, err)
	}
	if cfg := Load(); cfg.LogLevel != "INFO" || cfg.LogFormat != "text" {
		t.Errorf("config log settings = %q, %q", cfg.LogLevel, cfg.LogFormat)
	}
}

// Invalid hosts, origins and host names are logged and replaced by the
// default (or left out), like invalid numbers
func TestLoad_InvalidNetworkSettings(t *testing.T) {
	for _, tt := range []struct {
		host, want string
	}{
		{"0.0.0.0", "0.0.0.0"},
		{"::", "::"},
		{"observer.local", "observer.local"},
		{"http://0.0.0.0", "127.0.0.1"},
		{"0.0.0.0:8080", "127.0.0.1"},
		{"bad host", "127.0.0.1"},
	} {
		t.Setenv("AI_OBSERVER_HOST", tt.host)
		if got := Load().Host; got != tt.want {
			t.Errorf("host %q: %q, want %q", tt.host, got, tt.want)
		}
	}
	for _, tt := range []struct {
		origin, want string
	}{
		{"https://observer.example.com:8443", "https://observer.example.com:8443"},
		{"http://localhost:5173/", "http://localhost:5173"},
		{"localhost:5173", "http://localhost:5173"},
		{"ftp://example.com", "http://localhost:5173"},
		{"https://example.com/app", "http://localhost:5173"},
	} {
		t.Setenv("AI_OBSERVER_FRONTEND_URL", tt.origin)
		if got := Load().FrontendURL; got != tt.want {
			t.Errorf("frontend URL %q: %q, want %q", tt.origin, got, tt.want)
		}
	}
	t.Setenv("AI_OBSERVER_ALLOWED_HOSTS", "observer.example.com, *, [::1], https://bad.example.com, host:8080, example.org.")
	if got := strings.Join(Load().AllowedHosts, ","); got != "observer.example.com,*,[::1],example.org." {
		t.Errorf("allowed hosts = %q", got)
	}
}
