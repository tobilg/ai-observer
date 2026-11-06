package watcher

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tobilg/ai-observer/internal/importer"
	"github.com/tobilg/ai-observer/internal/storage"
)

const handoffLine1 = `{"type":"assistant","timestamp":"2025-01-02T10:00:00.000Z","sessionId":"s1","requestId":"req-1","message":{"id":"msg-1","model":"claude-sonnet-4-20250514","role":"assistant","type":"message","usage":{"input_tokens":100,"output_tokens":50}}}
`
const handoffLine2 = `{"type":"assistant","timestamp":"2025-01-02T10:05:00.000Z","sessionId":"s1","requestId":"req-2","message":{"id":"msg-2","model":"claude-sonnet-4-20250514","role":"assistant","type":"message","usage":{"input_tokens":10,"output_tokens":5}}}
`

// Lines written between a full import and the start of watch mode must be
// picked up by the watcher, and the imported lines must not be stored twice.
func TestWatchResumesWhereImportStopped(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "s1.jsonl")
	if err := os.WriteFile(path, []byte(handoffLine1), 0644); err != nil {
		t.Fatalf("writing session: %v", err)
	}
	t.Setenv("AI_OBSERVER_CLAUDE_PATH", dir)

	store, err := storage.NewDuckDBStore(":memory:")
	if err != nil {
		t.Fatalf("creating store: %v", err)
	}
	defer store.Close()

	imp := importer.NewImporter(store, false)
	imp.RegisterParser(importer.NewClaudeParser())
	if err := imp.Import(ctx, []importer.SourceType{importer.SourceClaude}, importer.Options{SkipConfirm: true}); err != nil {
		t.Fatalf("import: %v", err)
	}

	countTokens := func() float64 {
		t.Helper()
		var total float64
		if err := store.DB().QueryRowContext(ctx,
			"SELECT COALESCE(SUM(Value), 0) FROM otel_metrics WHERE MetricName = 'claude_code.token.usage'").Scan(&total); err != nil {
			t.Fatalf("summing tokens: %v", err)
		}
		return total
	}
	imported := countTokens()
	if imported == 0 {
		t.Fatal("import stored no token usage")
	}

	// Written after the import, before watching starts
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("opening session: %v", err)
	}
	f.WriteString(handoffLine2)
	f.Close()

	w := New(store, nil, Config{Tools: []string{"claude-code"}})
	if err := w.Start(ctx); err != nil {
		t.Fatalf("starting watcher: %v", err)
	}
	w.waitInitialScan()
	w.Stop()

	if got, want := countTokens(), imported+15; got != want {
		t.Errorf("token usage = %v after watching, want %v (imported %v + 15 from the new line)", got, want, imported)
	}
}

// Gemini rewrites the whole session file, so watching resumes by message
// count: after a full import only messages added later may be stored.
func TestGeminiWatchResumesAfterImportedMessages(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	chatDir := filepath.Join(dir, "abc123", "chats")
	if err := os.MkdirAll(chatDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	path := filepath.Join(chatDir, "session-x.json")
	t.Setenv("AI_OBSERVER_GEMINI_PATH", dir)

	message := func(id, ts string, input int) importer.GeminiMessage {
		return importer.GeminiMessage{ID: id, Timestamp: ts, Type: "gemini", Model: "gemini-2.0-flash",
			Tokens: &importer.GeminiTokens{Input: input, Total: input}}
	}
	writeSession := func(messages ...importer.GeminiMessage) {
		t.Helper()
		data, err := json.Marshal(importer.GeminiSession{SessionID: "x", ProjectHash: "abc123",
			StartTime: "2025-01-02T10:00:00.000Z", LastUpdated: "2025-01-02T10:05:00.000Z", Messages: messages})
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if err := os.WriteFile(path, data, 0644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	first := message("m1", "2025-01-02T10:00:00.000Z", 100)
	writeSession(first)

	store, err := storage.NewDuckDBStore(":memory:")
	if err != nil {
		t.Fatalf("creating store: %v", err)
	}
	defer store.Close()

	imp := importer.NewImporter(store, false)
	imp.RegisterParser(importer.NewGeminiParser())
	if err := imp.Import(ctx, []importer.SourceType{importer.SourceGemini}, importer.Options{SkipConfirm: true}); err != nil {
		t.Fatalf("import: %v", err)
	}

	inputTokens := func() float64 {
		t.Helper()
		var total float64
		if err := store.DB().QueryRowContext(ctx, `SELECT COALESCE(SUM(Value), 0) FROM otel_metrics
			WHERE MetricName = 'gemini_cli.token.usage' AND Attributes->>'type' = 'input'`).Scan(&total); err != nil {
			t.Fatalf("summing tokens: %v", err)
		}
		return total
	}
	if got := inputTokens(); got != 100 {
		t.Fatalf("imported input tokens = %v, want 100", got)
	}

	writeSession(first, message("m2", "2025-01-02T10:01:00.000Z", 7))
	w := New(store, nil, Config{Tools: []string{"gemini"}})
	if err := w.Start(ctx); err != nil {
		t.Fatalf("starting watcher: %v", err)
	}
	w.waitInitialScan()
	w.Stop()

	if got := inputTokens(); got != 107 {
		t.Errorf("input tokens = %v after watching, want 107 (only the new message added)", got)
	}
}

// An import after watching replaces what watch stored for the file instead of
// adding the whole file again
func TestImportAfterWatchDoesNotDuplicate(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "s1.jsonl")
	if err := os.WriteFile(path, []byte(handoffLine1), 0644); err != nil {
		t.Fatalf("writing session: %v", err)
	}
	t.Setenv("AI_OBSERVER_CLAUDE_PATH", dir)

	store, err := storage.NewDuckDBStore(":memory:")
	if err != nil {
		t.Fatalf("creating store: %v", err)
	}
	defer store.Close()

	w := New(store, nil, Config{Tools: []string{"claude-code"}, Backfill: true})
	if err := w.Start(ctx); err != nil {
		t.Fatalf("starting watcher: %v", err)
	}
	w.waitInitialScan()
	w.Stop()
	watched := tokenTotal(t, store)
	if watched != 150 {
		t.Fatalf("watch stored %v tokens, want 150", watched)
	}

	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
	f.WriteString(handoffLine2)
	f.Close()

	imp := importer.NewImporter(store, false)
	imp.RegisterParser(importer.NewClaudeParser())
	if err := imp.Import(ctx, []importer.SourceType{importer.SourceClaude}, importer.Options{SkipConfirm: true}); err != nil {
		t.Fatalf("import: %v", err)
	}
	if got := tokenTotal(t, store); got != 165 {
		t.Errorf("token usage = %v after importing, want 165 (each line once)", got)
	}
}

// A session file that is rewritten with less content is read again and its
// stored records are replaced
func TestWatchReplacesARewrittenFile(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "s1.jsonl")
	if err := os.WriteFile(path, []byte(handoffLine1+handoffLine2), 0644); err != nil {
		t.Fatalf("writing session: %v", err)
	}
	t.Setenv("AI_OBSERVER_CLAUDE_PATH", dir)

	store, err := storage.NewDuckDBStore(":memory:")
	if err != nil {
		t.Fatalf("creating store: %v", err)
	}
	defer store.Close()

	run := func() {
		t.Helper()
		w := New(store, nil, Config{Tools: []string{"claude-code"}, Backfill: true})
		if err := w.Start(ctx); err != nil {
			t.Fatalf("starting watcher: %v", err)
		}
		w.waitInitialScan()
		w.Stop()
	}
	run()
	if got := tokenTotal(t, store); got != 165 {
		t.Fatalf("watch stored %v tokens, want 165", got)
	}

	if err := os.WriteFile(path, []byte(handoffLine2), 0644); err != nil {
		t.Fatalf("rewriting session: %v", err)
	}
	run()
	if got := tokenTotal(t, store); got != 15 {
		t.Errorf("token usage = %v after the rewrite, want 15 (only the rewritten content)", got)
	}
}

func tokenTotal(t *testing.T, store *storage.DuckDBStore) float64 {
	t.Helper()
	var total float64
	if err := store.DB().QueryRowContext(context.Background(),
		"SELECT COALESCE(SUM(Value), 0) FROM otel_metrics WHERE MetricName = 'claude_code.token.usage'").Scan(&total); err != nil {
		t.Fatalf("summing tokens: %v", err)
	}
	return total
}

func TestClaudeParserStateKeepsOnlyRecentRequests(t *testing.T) {
	state := claudeParserState{SeenRequests: map[string]bool{"legacy": true}}
	state.indexRequests()
	if !state.seenRequest("legacy") || state.SeenRequests != nil {
		t.Fatalf("legacy keys not taken over: %+v", state)
	}
	for i := range maxRecentRequests {
		state.addRequest(string(rune('a'+i%26)) + string(rune(i)))
	}
	if len(state.RecentRequests) != maxRecentRequests {
		t.Errorf("kept %d keys, want %d", len(state.RecentRequests), maxRecentRequests)
	}
	if state.seenRequest("legacy") {
		t.Error("the oldest key should have been dropped")
	}

	data, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	var decoded claudeParserState
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	decoded.indexRequests()
	if !decoded.seenRequest(state.RecentRequests[0]) {
		t.Error("keys are not restored from the saved state")
	}
}

// An import of a date range that left out later records does not let watch
// skip them: watch reads the file again and replaces the imported records
func TestWatchReadsAPartialImportAgain(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "s1.jsonl")
	later := strings.ReplaceAll(handoffLine2, "2025-01-02", "2025-01-05")
	if err := os.WriteFile(path, []byte(handoffLine1+later), 0644); err != nil {
		t.Fatalf("writing session: %v", err)
	}
	t.Setenv("AI_OBSERVER_CLAUDE_PATH", dir)

	store, err := storage.NewDuckDBStore(":memory:")
	if err != nil {
		t.Fatalf("creating store: %v", err)
	}
	defer store.Close()

	imp := importer.NewImporter(store, false)
	imp.SetIO(&strings.Builder{}, strings.NewReader(""))
	imp.RegisterParser(importer.NewClaudeParser())
	to, _ := importer.ParseToDateArg("2025-01-03")
	if err := imp.Import(ctx, []importer.SourceType{importer.SourceClaude}, importer.Options{SkipConfirm: true, ToDate: to}); err != nil {
		t.Fatalf("import: %v", err)
	}
	if got := tokenTotal(t, store); got != 150 {
		t.Fatalf("ranged import stored %v tokens, want 150", got)
	}

	w := New(store, nil, Config{Tools: []string{"claude-code"}})
	if err := w.Start(ctx); err != nil {
		t.Fatalf("starting watcher: %v", err)
	}
	w.waitInitialScan()
	w.Stop()
	if got := tokenTotal(t, store); got != 165 {
		t.Errorf("token usage = %v after watching, want 165 (each line once)", got)
	}
}

// A rewritten file imported by an earlier version, whose records carry no
// SourceFile, has those records replaced as well
func TestWatchReplacesARewrittenLegacyFile(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "s1.jsonl")
	if err := os.WriteFile(path, []byte(handoffLine1+handoffLine2), 0644); err != nil {
		t.Fatalf("writing session: %v", err)
	}
	t.Setenv("AI_OBSERVER_CLAUDE_PATH", dir)

	store, err := storage.NewDuckDBStore(":memory:")
	if err != nil {
		t.Fatalf("creating store: %v", err)
	}
	defer store.Close()

	run := func() {
		t.Helper()
		w := New(store, nil, Config{Tools: []string{"claude-code"}, Backfill: true})
		if err := w.Start(ctx); err != nil {
			t.Fatalf("starting watcher: %v", err)
		}
		w.waitInitialScan()
		w.Stop()
	}
	run()
	for _, stmt := range []string{
		"UPDATE otel_logs SET SourceFile = NULL",
		"UPDATE otel_metrics SET SourceFile = NULL",
		"UPDATE import_state SET source_tracked = false",
	} {
		if _, err := store.DB().ExecContext(ctx, stmt); err != nil {
			t.Fatal(err)
		}
	}

	if err := os.WriteFile(path, []byte(handoffLine2), 0644); err != nil {
		t.Fatalf("rewriting session: %v", err)
	}
	run()
	// The first line's records are no longer in the file and cannot be told
	// apart from other data, so they stay; the second line's are replaced
	if got := tokenTotal(t, store); got != 165 {
		t.Errorf("token usage = %v after the rewrite, want 165 (second line once)", got)
	}
}
