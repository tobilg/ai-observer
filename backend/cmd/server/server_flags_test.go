package main

import (
	"strings"
	"testing"

	"github.com/tobilg/ai-observer/internal/config"
)

func TestServeFlagsOverrideEnvironment(t *testing.T) {
	t.Setenv("AI_OBSERVER_HOST", "127.0.0.1")
	t.Setenv("AI_OBSERVER_API_PORT", "8081")
	t.Setenv("AI_OBSERVER_OTLP_PORT", "4319")
	t.Setenv("AI_OBSERVER_DATABASE_PATH", "/env/db.duckdb")

	parsed, err := parseServeArgs([]string{"--host", "0.0.0.0", "--api-port", "9090", "--otlp-port=9318", "--db", "/flag/db.duckdb", "--mcp"})
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Load()
	parsed.Server.apply(cfg)
	if cfg.Host != "0.0.0.0" || cfg.APIPort != 9090 || cfg.OTLPPort != 9318 || cfg.DatabasePath != "/flag/db.duckdb" || !parsed.MCP {
		t.Errorf("config = %+v", cfg)
	}

	// Flags not given keep the environment's values
	parsed, err = parseServeArgs([]string{"--api-port", "9091"})
	if err != nil {
		t.Fatal(err)
	}
	cfg = config.Load()
	parsed.Server.apply(cfg)
	if cfg.Host != "127.0.0.1" || cfg.APIPort != 9091 || cfg.OTLPPort != 4319 || cfg.DatabasePath != "/env/db.duckdb" {
		t.Errorf("config = %+v", cfg)
	}
}

func TestServeFlagsRejectInvalidPorts(t *testing.T) {
	for _, port := range []string{"0", "65536", "http", "-1"} {
		if _, err := parseServeArgs([]string{"--api-port", port}); err == nil || !strings.Contains(err.Error(), "expected a port from 1 to 65535") {
			t.Errorf("--api-port %s: err = %v", port, err)
		}
	}
}

func TestWatchFlags(t *testing.T) {
	parsed, err := parseWatchArgs([]string{"codex", "--host", "::1", "--api-port", "9092", "--db", "w.duckdb"})
	if err != nil {
		t.Fatal(err)
	}
	if parsed.ToolArg != "codex" || parsed.Server.Host != "::1" || parsed.Server.APIPort != 9092 || parsed.Server.DB != "w.duckdb" {
		t.Errorf("parsed = %+v", parsed)
	}
	// watch has no OTLP server
	if _, err := parseWatchArgs([]string{"--otlp-port", "4318"}); err == nil {
		t.Error("watch accepted --otlp-port")
	}
}

func TestDBFlagOfDataCommands(t *testing.T) {
	imp, err := parseImportFlags([]string{"all", "--db", "i.duckdb"})
	if err != nil || imp.DB != "i.duckdb" || imp.Tool != "all" {
		t.Errorf("import = %+v, %v", imp, err)
	}
	exp, err := parseExportFlags([]string{"all", "--output", "out", "--db", "e.duckdb"})
	if err != nil || exp.DB != "e.duckdb" {
		t.Errorf("export = %+v, %v", exp, err)
	}
	del, err := parseDeleteFlags([]string{"all", "--from", "2025-01-01", "--to", "2025-01-02", "--db", "d.duckdb"})
	if err != nil || del.DB != "d.duckdb" {
		t.Errorf("delete = %+v, %v", del, err)
	}
}

func TestServeHelpShowsServerFlags(t *testing.T) {
	out := captureOutput(func() {
		parseServeArgs([]string{"--help"})
	})
	for _, want := range []string{"--host host", "--api-port port", "--otlp-port port", "--db file", "AI_OBSERVER_DATABASE_PATH"} {
		if !strings.Contains(out, want) {
			t.Errorf("help lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "(default: 0)") {
		t.Errorf("help shows a zero default:\n%s", out)
	}
}
