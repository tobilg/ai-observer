package importer

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tobilg/ai-observer/internal/api"
	"github.com/tobilg/ai-observer/internal/otlp"
	"github.com/tobilg/ai-observer/internal/storage"
)

// fileUsagePoints counts the token and cost points imported from files at
// ts (a session's first entry also has its session.count point)
func fileUsagePoints(t *testing.T, store *storage.DuckDBStore, ts time.Time) int {
	t.Helper()
	var n int
	err := store.DB().QueryRow(`SELECT COUNT(*) FROM otel_metrics WHERE SourceFile IS NOT NULL AND Timestamp = ?::TIMESTAMP
		AND (MetricName LIKE '%.token.usage%' OR MetricName LIKE '%.cost.usage%')`,
		ts.UTC().Format("2006-01-02 15:04:05.999999")).Scan(&n)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// The importer's records of a Claude Code request carry what storage needs
// to count a request OTLP recorded as well only once (usage_overlap.go)
func TestImportCountsClaudeRequestsRecordedOverOTLPOnce(t *testing.T) {
	imp, _, store, path := setupClaudeImport(t)
	imp.SetIO(&strings.Builder{}, strings.NewReader(""))
	appendLine(t, path, secondClaudeLine)
	ctx := context.Background()
	first := time.Date(2025, 1, 2, 10, 0, 0, 0, time.UTC)
	second := first.Add(5 * time.Minute)

	// Claude Code sent req-1 over OTLP: its api_request event, then the
	// session's token metrics
	delta := int32(1)
	value := 150.0
	if err := store.InsertTelemetry(ctx, []api.LogRecord{{
		Timestamp: first.Add(3 * time.Second), ServiceName: "claude-code", Body: "api_request",
		LogAttributes: map[string]string{"event.name": "api_request", "session.id": "s1", "request_id": "req-1", "duration_ms": "3000"},
	}}, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := store.InsertMetrics(ctx, []api.MetricDataPoint{{
		Timestamp: first.Add(time.Minute), ServiceName: "claude-code", MetricName: "claude_code.token.usage", MetricType: "sum",
		Value: &value, AggregationTemporality: &delta, Attributes: map[string]string{"session.id": "s1", "type": "input", "model": "m"},
	}}); err != nil {
		t.Fatal(err)
	}

	if err := imp.Import(ctx, []SourceType{SourceClaude}, Options{SkipConfirm: true}); err != nil {
		t.Fatal(err)
	}
	if got := fileUsagePoints(t, store, first); got != 0 {
		t.Errorf("file usage points of req-1 = %d, want 0 (OTLP recorded it)", got)
	}
	if got := fileUsagePoints(t, store, second); got == 0 {
		t.Error("file usage of req-2, which only the file records, was not stored")
	}
}

const codexOverlapSession = `{"timestamp":"2026-11-10T22:14:00.000Z","type":"session_meta","payload":{"id":"01a1c786-ad51","cwd":"/tmp","model":"gpt-6-astra"}}
{"timestamp":"2026-11-10T22:14:19.285Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":23049,"cached_input_tokens":20000,"output_tokens":300,"reasoning_output_tokens":64}}}}
{"timestamp":"2026-11-10T22:14:31.140Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":49028,"cached_input_tokens":43000,"output_tokens":420,"reasoning_output_tokens":64}}}}
`

// Codex: the session file's token_count and the response.completed event
// AI Observer derives OTLP usage from carry the same counts
func TestImportCountsCodexResponsesRecordedOverOTLPOnce(t *testing.T) {
	dir := t.TempDir()
	sessions := filepath.Join(dir, "sessions", "2026", "11", "10")
	if err := os.MkdirAll(sessions, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sessions, "rollout-2026-11-10T23-14-14-01a1c786-ad51.jsonl"), []byte(codexOverlapSession), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AI_OBSERVER_CODEX_PATH", dir)
	store, err := storage.NewDuckDBStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()

	// The app server's response.completed event of the first response, 2 ms
	// before the session file's token_count
	first := time.Date(2026, 11, 10, 22, 14, 19, 285_000_000, time.UTC)
	derived := otlp.ExtractCodexMetrics(map[string]string{
		"event.name": "codex.sse_event", "event.kind": "response.completed", "conversation.id": "01a1c786-ad51",
		"model": "gpt-6-astra", "input_token_count": "23049", "cached_token_count": "20000",
		"output_token_count": "300", "reasoning_token_count": "64",
	}, first.Add(-2*time.Millisecond), "codex-app-server", map[string]string{"service.name": "codex-app-server"}, "", "")
	if len(derived) == 0 || derived[0].Attributes["session.id"] != "01a1c786-ad51" {
		t.Fatalf("derived usage = %+v, want points with the session", derived)
	}
	if err := store.InsertTelemetry(ctx, nil, derived, nil); err != nil {
		t.Fatal(err)
	}

	imp := NewImporter(store, false)
	imp.SetIO(&strings.Builder{}, strings.NewReader(""))
	imp.RegisterParser(NewCodexParser())
	if err := imp.Import(ctx, []SourceType{SourceCodex}, Options{SkipConfirm: true}); err != nil {
		t.Fatal(err)
	}
	if got := fileUsagePoints(t, store, first); got != 0 {
		t.Errorf("file usage points of the first response = %d, want 0 (OTLP recorded it)", got)
	}
	if got := fileUsagePoints(t, store, time.Date(2026, 11, 10, 22, 14, 31, 140_000_000, time.UTC)); got == 0 {
		t.Error("file usage of the second response, which only the file records, was not stored")
	}
}
