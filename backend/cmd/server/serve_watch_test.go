package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/tobilg/ai-observer/internal/config"
)

func TestParseServeWatchArgs(t *testing.T) {
	tests := []struct {
		args     []string
		watch    bool
		toolArg  string
		tools    []string
		backfill bool
	}{
		{nil, false, "", nil, false},
		{[]string{"--watch"}, true, "all", nil, false},
		{[]string{"--watch", "all"}, true, "all", nil, false},
		{[]string{"--watch", "codex", "--backfill"}, true, "codex", []string{"codex"}, true},
		{[]string{"claude-code", "--watch", "--mcp"}, true, "claude-code", []string{"claude-code"}, false},
	}
	for _, tt := range tests {
		parsed, err := parseServeArgs(tt.args)
		if err != nil {
			t.Errorf("%v: %v", tt.args, err)
			continue
		}
		if parsed.Watch != tt.watch || parsed.ToolArg != tt.toolArg || !slices.Equal(parsed.Tools, tt.tools) || parsed.Backfill != tt.backfill {
			t.Errorf("%v: parsed %+v", tt.args, parsed)
		}
	}

	for _, tt := range []struct {
		args []string
		want string
	}{
		{[]string{"codex"}, "use --watch codex"},
		{[]string{"--backfill"}, "--backfill requires --watch"},
		{[]string{"--watch", "copilot"}, "invalid tool"},
		{[]string{"--watch", "codex", "gemini"}, "unexpected argument: gemini"},
	} {
		if _, err := parseServeArgs(tt.args); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%v: error = %v, want %q", tt.args, err, tt.want)
		}
	}
}

// serve --watch serves OTLP and the API and imports session files: the
// status reports serve+watch
func TestRunServerWithWatch(t *testing.T) {
	_, apiPort, otlpPort := setupRunEnv(t)
	// A Codex session file for the watcher's backfill
	codexDir := filepath.Join(os.Getenv("AI_OBSERVER_CODEX_PATH"), "sessions", "2026", "11", "10")
	if err := os.MkdirAll(codexDir, 0o755); err != nil {
		t.Fatal(err)
	}
	session := `{"timestamp":"2026-11-10T10:00:00.000Z","type":"session_meta","payload":{"id":"c-watch","cwd":"/tmp","cli_version":"0.167.1"}}
{"timestamp":"2026-11-10T10:00:01.000Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]}}
`
	if err := os.WriteFile(filepath.Join(codexDir, "rollout-2026-11-10T10-00-00-c-watch.jsonl"), []byte(session), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- runServer(ctx, []string{"--watch", "codex", "--backfill"}) }()
	waitHealthy(t, apiPort, done)
	waitHealthy(t, otlpPort, done)

	var status struct {
		Mode         string   `json:"mode"`
		WatchedTools []string `json:"watchedTools"`
		Server       struct {
			OTLPEnabled     bool `json:"otlpEnabled"`
			OTLPGRPCEnabled bool `json:"otlpGrpcEnabled"`
		} `json:"server"`
		Imports []struct {
			Tool  string `json:"tool"`
			Files int    `json:"files"`
		} `json:"imports"`
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/api/status", apiPort))
		if err != nil {
			t.Fatal(err)
		}
		err = json.NewDecoder(resp.Body).Decode(&status)
		resp.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		imported := slices.ContainsFunc(status.Imports, func(i struct {
			Tool  string `json:"tool"`
			Files int    `json:"files"`
		}) bool {
			return i.Tool == "codex" && i.Files == 1
		})
		if imported || time.Now().After(deadline) {
			if !imported {
				t.Errorf("the session file was not imported: %+v", status.Imports)
			}
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if status.Mode != "serve+watch" || !status.Server.OTLPEnabled || !status.Server.OTLPGRPCEnabled || !slices.Equal(status.WatchedTools, []string{"codex"}) {
		t.Errorf("status = %+v", status)
	}

	cancel()
	waitStopped(t, done)
}

func TestOTLPGRPCPortFlag(t *testing.T) {
	t.Setenv("AI_OBSERVER_OTLP_GRPC_PORT", "4317")
	for _, tt := range []struct {
		arg  string
		want int
	}{{"14317", 14317}, {"0", 0}, {"off", 0}} {
		parsed, err := parseServeArgs([]string{"--otlp-grpc-port", tt.arg})
		if err != nil {
			t.Fatalf("--otlp-grpc-port %s: %v", tt.arg, err)
		}
		cfg := config.Load()
		parsed.Server.apply(cfg)
		if cfg.OTLPGRPCPort != tt.want {
			t.Errorf("--otlp-grpc-port %s: port %d, want %d", tt.arg, cfg.OTLPGRPCPort, tt.want)
		}
	}
	// Not given: the environment's port
	parsed, _ := parseServeArgs(nil)
	cfg := config.Load()
	parsed.Server.apply(cfg)
	if cfg.OTLPGRPCPort != 4317 {
		t.Errorf("port %d without the flag, want 4317", cfg.OTLPGRPCPort)
	}
	if _, err := parseServeArgs([]string{"--otlp-grpc-port", "70000"}); err == nil || !strings.Contains(err.Error(), "or 0 or off") {
		t.Errorf("invalid port: %v", err)
	}
	// watch serves no OTLP
	if _, err := parseWatchArgs([]string{"--otlp-grpc-port", "4317"}); err == nil {
		t.Error("watch accepted --otlp-grpc-port")
	}
}
