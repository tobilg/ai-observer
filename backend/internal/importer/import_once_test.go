package importer

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tobilg/ai-observer/internal/storage"
)

const claudeSessionLine = `{"type":"assistant","timestamp":"2025-01-02T10:00:00.000Z","sessionId":"s1","requestId":"req-1","message":{"id":"msg-1","model":"claude-sonnet-4-20250514","role":"assistant","type":"message","usage":{"input_tokens":100,"output_tokens":50}}}
`

// countingParser counts how often each file is parsed
type countingParser struct {
	SessionParser
	parses     int
	afterParse func(path string)
}

func (p *countingParser) ParseFile(ctx context.Context, path string) (*ImportResult, error) {
	p.parses++
	result, err := p.SessionParser.ParseFile(ctx, path)
	if p.afterParse != nil {
		p.afterParse(path)
	}
	return result, err
}

// setupClaudeImport writes one Claude session and returns an importer whose
// Claude parser counts parses
func setupClaudeImport(t *testing.T) (*Importer, *countingParser, *storage.DuckDBStore, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	if err := os.WriteFile(path, []byte(claudeSessionLine), 0644); err != nil {
		t.Fatalf("failed to write session: %v", err)
	}
	t.Setenv("AI_OBSERVER_CLAUDE_PATH", dir)

	store, err := storage.NewDuckDBStore(":memory:")
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	parser := &countingParser{SessionParser: NewClaudeParser()}
	imp := NewImporter(store, false)
	imp.RegisterParser(parser)
	return imp, parser, store, path
}

func TestImportParsesEachFileOnceWithoutPreview(t *testing.T) {
	tests := []struct {
		name       string
		opts       Options
		wantParses int
	}{
		{"--yes imports in one pass", Options{SkipConfirm: true}, 1},
		{"--dry-run only previews", Options{SkipConfirm: true, DryRun: true}, 1},
		{"--purge previews to derive the date range", Options{SkipConfirm: true, Purge: true}, 2},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			imp, parser, _, _ := setupClaudeImport(t)

			if err := imp.Import(context.Background(), []SourceType{SourceClaude}, tc.opts); err != nil {
				t.Fatalf("Import failed: %v", err)
			}
			if parser.parses != tc.wantParses {
				t.Errorf("file parsed %d times, want %d", parser.parses, tc.wantParses)
			}
		})
	}
}

func TestImportOnceStoresRecordsAndState(t *testing.T) {
	imp, _, store, path := setupClaudeImport(t)
	ctx := context.Background()

	if err := imp.Import(ctx, []SourceType{SourceClaude}, Options{SkipConfirm: true}); err != nil {
		t.Fatalf("Import failed: %v", err)
	}

	var logCount int
	if err := store.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM otel_logs").Scan(&logCount); err != nil {
		t.Fatalf("counting logs: %v", err)
	}
	if logCount == 0 {
		t.Error("expected logs to be imported")
	}

	status, err := imp.state.CheckFileStatus(ctx, SourceClaude, path)
	if err != nil {
		t.Fatalf("CheckFileStatus failed: %v", err)
	}
	if status != StatusCurrent {
		t.Errorf("expected file to be recorded as imported, got status %q", status)
	}
}

// A session that is still being written can grow between parsing and recording
// the import. The recorded hash must describe the parsed content, so the next
// import sees the file as modified and picks up the new lines.
func TestImportRecordsHashOfParsedContent(t *testing.T) {
	imp, parser, _, path := setupClaudeImport(t)
	ctx := context.Background()
	parser.afterParse = func(path string) {
		f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			t.Fatalf("failed to append to session: %v", err)
		}
		defer f.Close()
		f.WriteString(`{"type":"assistant","timestamp":"2025-01-02T10:05:00.000Z","requestId":"req-2","message":{"id":"msg-2","model":"claude-sonnet-4-20250514","role":"assistant","type":"message","usage":{"input_tokens":1,"output_tokens":1}}}` + "\n")
	}

	if err := imp.Import(ctx, []SourceType{SourceClaude}, Options{SkipConfirm: true}); err != nil {
		t.Fatalf("Import failed: %v", err)
	}

	status, err := imp.state.CheckFileStatus(ctx, SourceClaude, path)
	if err != nil {
		t.Fatalf("CheckFileStatus failed: %v", err)
	}
	if status != StatusModified {
		t.Errorf("expected the appended content to make the file modified, got status %q", status)
	}
}

func TestParseFileHashesParsedContent(t *testing.T) {
	dir := t.TempDir()
	write := func(rel, content string) string {
		path := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatalf("write: %v", err)
		}
		return path
	}

	tests := []struct {
		name   string
		parser SessionParser
		path   string
	}{
		{"claude", NewClaudeParser(), write("claude.jsonl", claudeSessionLine)},
		{"codex", NewCodexParser(), write("rollout-2025-01-02T10-00-00-x.jsonl",
			`{"timestamp":"2025-01-02T10:00:00.000Z","type":"session_meta","payload":{"id":"x","timestamp":"2025-01-02T10:00:00.000Z","cwd":"/tmp","model":"gpt-4o"}}`+"\n")},
		{"gemini", NewGeminiParser(), write(filepath.Join("p", "chats", "session-x.json"),
			`{"sessionId":"x","projectHash":"p","startTime":"2025-01-02T10:00:00.000Z","lastUpdated":"2025-01-02T10:00:00.000Z","messages":[]}`)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := tc.parser.ParseFile(context.Background(), tc.path)
			if err != nil {
				t.Fatalf("ParseFile failed: %v", err)
			}
			want, err := computeFileHash(tc.path)
			if err != nil {
				t.Fatalf("computeFileHash failed: %v", err)
			}
			if result.FileHash != want {
				t.Errorf("FileHash = %q, want %q", result.FileHash, want)
			}
		})
	}
}

func TestCheckFileStatusDoesNotReadNewFiles(t *testing.T) {
	store, err := storage.NewDuckDBStore(":memory:")
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	// The file does not exist: a new file must be classified without reading it
	status, err := NewStateManager(store).CheckFileStatus(context.Background(), SourceClaude, filepath.Join(t.TempDir(), "missing.jsonl"))
	if err != nil {
		t.Fatalf("CheckFileStatus read the file: %v", err)
	}
	if status != StatusNew {
		t.Errorf("status = %q, want %q", status, StatusNew)
	}
}

// failingParser fails to parse every file
type failingParser struct{ SessionParser }

func (failingParser) ParseFile(context.Context, string) (*ImportResult, error) {
	return nil, errors.New("broken file")
}

// An import with files that could not be imported must fail, so scripts and
// scheduled jobs notice
func TestImportReportsFailedFiles(t *testing.T) {
	// With --purge the preview parses (and fails on) the file first
	for _, opts := range []Options{{SkipConfirm: true}, {SkipConfirm: true, Purge: true}, {DryRun: true}} {
		imp, _, _, _ := setupClaudeImport(t)
		imp.RegisterParser(failingParser{NewClaudeParser()})

		err := imp.Import(context.Background(), []SourceType{SourceClaude}, opts)
		if err == nil || !strings.Contains(err.Error(), "1 files could not be imported") {
			t.Errorf("Import error = %v, want the failed file reported", err)
		}
	}
}

func TestImportAsksForConfirmationOnConfiguredInput(t *testing.T) {
	imp, _, store, _ := setupClaudeImport(t)
	var out strings.Builder
	imp.SetIO(&out, strings.NewReader("n\n"))

	if err := imp.Import(context.Background(), []SourceType{SourceClaude}, Options{}); err != nil {
		t.Fatalf("Import: %v", err)
	}
	if !strings.Contains(out.String(), "Continue? [y/N]") || !strings.Contains(out.String(), "Aborted.") {
		t.Errorf("output = %q, want prompt and abort", out.String())
	}

	var logs int
	if err := store.DB().QueryRow("SELECT COUNT(*) FROM otel_logs").Scan(&logs); err != nil {
		t.Fatalf("counting logs: %v", err)
	}
	if logs != 0 {
		t.Errorf("declined import stored %d logs", logs)
	}
}
