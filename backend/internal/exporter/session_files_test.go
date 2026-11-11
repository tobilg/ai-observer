package exporter

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tobilg/ai-observer/internal/api"
	"github.com/tobilg/ai-observer/internal/redact"
	"github.com/tobilg/ai-observer/internal/storage"
)

// parquetValue runs query over the Parquet file at path ({file} in query)
// and returns its single value
func parquetValue(t *testing.T, path, query string) any {
	t.Helper()
	db, err := sql.Open("duckdb", "")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var v any
	if err := db.QueryRow(strings.ReplaceAll(query, "{file}", sqlStringLiteral(path))).Scan(&v); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return v
}

// An export keeps the session file paths of the records and the import
// state of those files (format 2)
func TestExportCarriesSessionFiles(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	ctx := context.Background()
	at := time.Date(2025, 1, 2, 10, 0, 0, 0, time.UTC)
	state := &storage.ImportState{Source: "claude-code", FilePath: "/home/u/.claude/projects/p/s.jsonl", FileHash: "h",
		ImportedAt: at, RecordCount: 1, ByteOffset: 42, FileSize: 42, FileModTimeNs: 1, SourceTracked: true}
	if err := store.InsertImportedFile(ctx, []api.LogRecord{{Timestamp: at, ServiceName: "claude-code", Body: "x"}},
		nil, nil, state, nil); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	summary, err := NewExporter(store, false).Export(ctx, Options{Source: SourceAll, OutputDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if summary.ImportStateCount != 1 {
		t.Errorf("ImportStateCount = %d, want 1", summary.ImportStateCount)
	}
	if got := parquetValue(t, filepath.Join(dir, LogsFile), "SELECT any_value(SourceFile) FROM read_parquet({file})"); got != state.FilePath {
		t.Errorf("exported SourceFile = %v", got)
	}
	// Message keys are derived by the code of one version: restore
	// computes them again
	if got := parquetValue(t, filepath.Join(dir, LogsFile),
		"SELECT count(*) FROM (DESCRIBE SELECT * FROM read_parquet({file})) WHERE column_name = 'MessageKey'"); got != int64(0) {
		t.Errorf("logs.parquet has a MessageKey column")
	}
	if got := parquetValue(t, filepath.Join(dir, ImportStateFile),
		"SELECT file_path || ' ' || byte_offset || ' ' || source_tracked FROM read_parquet({file})"); got != state.FilePath+" 42 true" {
		t.Errorf("exported import state = %v", got)
	}
	m, ok, err := ReadManifest(dir)
	if err != nil || !ok || m.Format != 2 || m.Rows[ImportStateFile] != 1 {
		t.Errorf("manifest = %+v, %v, %v", m, ok, err)
	}
	// The views database still reads the files
	views, err := sql.Open("duckdb", filepath.Join(dir, "ai-observer-export-all-all.duckdb"))
	if err != nil {
		t.Fatal(err)
	}
	defer views.Close()
	conn, err := views.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, "SET file_search_path = "+sqlStringLiteral(dir)); err != nil {
		t.Fatal(err)
	}
	var source string
	if err := conn.QueryRowContext(ctx, "SELECT SourceFile FROM logs").Scan(&source); err != nil || source != state.FilePath {
		t.Errorf("views database: %q, %v", source, err)
	}
}

// An export read from session files carries their paths and import state
// too, and redacts their text as an import does
func TestRunFromFilesCarriesSessionFilesRedacted(t *testing.T) {
	root := t.TempDir()
	key := "sk-ant-api03-" + strings.Repeat("Ab3x", 11)
	path := filepath.Join(root, "claude", "-home-user-project", "secret.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	line := `{"parentUuid":null,"type":"user","message":{"role":"user","content":"use ` + key + `"},"uuid":"u1","timestamp":"2025-01-02T10:00:00.000Z","sessionId":"s1","cwd":"/home/user/project"}` + "\n" +
		claudeFixtureSession
	if err := os.WriteFile(path, []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AI_OBSERVER_CLAUDE_PATH", filepath.Join(root, "claude"))
	t.Setenv("AI_OBSERVER_CODEX_PATH", filepath.Join(root, "codex"))
	t.Setenv("AI_OBSERVER_GEMINI_PATH", filepath.Join(root, "gemini"))

	dir := t.TempDir()
	var out strings.Builder
	if err := runFromFiles(context.Background(), Options{Source: SourceClaude, OutputDir: dir, FromFiles: true, SkipConfirm: true,
		Redactor: redact.New(redact.Secrets), Out: &out}); err != nil {
		t.Fatalf("runFromFiles: %v\n%s", err, out.String())
	}
	logs := filepath.Join(dir, LogsFile)
	if got := parquetValue(t, logs, "SELECT COUNT(*) FROM read_parquet({file}) WHERE SourceFile = "+sqlStringLiteral(path)); got != int64(2) {
		t.Errorf("%v logs carry the session file path, want 2", got)
	}
	if got := parquetValue(t, logs, "SELECT COUNT(*) FROM read_parquet({file}) r WHERE contains(CAST(r AS VARCHAR), 'sk-ant-')"); got != int64(0) {
		t.Errorf("%v exported logs keep the secret", got)
	}
	if got := parquetValue(t, filepath.Join(dir, ImportStateFile), "SELECT any_value(file_path) FROM read_parquet({file})"); got != path {
		t.Errorf("exported import state of %v, want %s", got, path)
	}
}
