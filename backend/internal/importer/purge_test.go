package importer

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tobilg/ai-observer/internal/api"
)

// A purge must only replace the imported tool's data, and it must rebuild the
// data of unchanged files in the purged range instead of losing it.
func TestPurgeReplacesOnlyImportedToolData(t *testing.T) {
	imp, _, store, path := setupClaudeImport(t)
	ctx := context.Background()

	// First import: the unchanged file
	if err := imp.Import(ctx, []SourceType{SourceClaude}, Options{SkipConfirm: true}); err != nil {
		t.Fatalf("initial import failed: %v", err)
	}

	// Telemetry of another tool inside the purged range
	if err := store.InsertLogs(ctx, []api.LogRecord{{
		Timestamp:   time.Date(2025, 1, 2, 10, 30, 0, 0, time.UTC),
		ServiceName: "copilot-chat",
		Body:        "copilot event",
	}}); err != nil {
		t.Fatalf("inserting copilot log: %v", err)
	}

	// A new file whose records span the unchanged file's records
	newFile := filepath.Join(filepath.Dir(path), "new.jsonl")
	content := `{"type":"assistant","timestamp":"2025-01-02T09:00:00.000Z","sessionId":"s2","requestId":"req-a","message":{"id":"msg-a","model":"claude-sonnet-4-20250514","role":"assistant","type":"message","usage":{"input_tokens":1,"output_tokens":1}}}
{"type":"assistant","timestamp":"2025-01-02T11:00:00.000Z","sessionId":"s2","requestId":"req-b","message":{"id":"msg-b","model":"claude-sonnet-4-20250514","role":"assistant","type":"message","usage":{"input_tokens":1,"output_tokens":1}}}
`
	if err := os.WriteFile(newFile, []byte(content), 0644); err != nil {
		t.Fatalf("writing new session: %v", err)
	}

	countLogs := func(service string) int {
		t.Helper()
		var n int
		if err := store.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM otel_logs WHERE ServiceName = ?", service).Scan(&n); err != nil {
			t.Fatalf("counting logs: %v", err)
		}
		return n
	}
	parsedLogs := func(p string) int {
		t.Helper()
		result, err := NewClaudeParser().ParseFile(ctx, p)
		if err != nil {
			t.Fatalf("parsing %s: %v", p, err)
		}
		return len(result.Logs)
	}

	if err := imp.Import(ctx, []SourceType{SourceClaude}, Options{SkipConfirm: true, Purge: true}); err != nil {
		t.Fatalf("purge import failed: %v", err)
	}

	if got := countLogs("copilot-chat"); got != 1 {
		t.Errorf("copilot logs = %d, want 1 (purge must not delete other tools)", got)
	}
	want := parsedLogs(path) + parsedLogs(newFile)
	if got := countLogs("claude-code"); got != want {
		t.Errorf("claude logs = %d, want %d (unchanged file must be re-imported)", got, want)
	}
}

func TestPurgeRangesKeepsExplicitBounds(t *testing.T) {
	explicitFrom := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	summaries := []*ImportSummary{
		{Source: SourceClaude, Files: []FileSummary{{
			FirstTime: time.Date(2025, 1, 5, 0, 0, 0, 0, time.UTC),
			LastTime:  time.Date(2025, 1, 6, 0, 0, 0, 0, time.UTC),
		}}},
		{Source: SourceCodex}, // nothing to import and no explicit range
	}

	ranges := purgeRanges(summaries, Options{FromDate: &explicitFrom})

	claude, ok := ranges[SourceClaude]
	if !ok {
		t.Fatal("expected a purge range for claude-code")
	}
	if !claude.from.Equal(explicitFrom) {
		t.Errorf("from = %v, want explicit %v", claude.from, explicitFrom)
	}
	if want := time.Date(2025, 1, 6, 0, 0, 0, 0, time.UTC); !claude.to.Equal(want) {
		t.Errorf("to = %v, want derived %v", claude.to, want)
	}
	if _, ok := ranges[SourceCodex]; ok {
		t.Error("codex has no range and must not be purged")
	}
}
