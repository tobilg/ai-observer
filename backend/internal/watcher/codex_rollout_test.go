package watcher

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"
	"github.com/tobilg/ai-observer/internal/api"
	"github.com/tobilg/ai-observer/internal/importer"
	"github.com/tobilg/ai-observer/internal/storage"
	"github.com/tobilg/ai-observer/internal/transcript"
)

// compressLikeCodex replaces the session file at path by its compressed form
// as Codex's compression worker does: one zstd frame with the content size,
// the .jsonl's modification time, the .jsonl removed
func compressLikeCodex(t *testing.T, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	enc, _ := zstd.NewWriter(nil)
	if err := os.WriteFile(path+".zst", enc.EncodeAll(data, nil), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path+".zst", info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
}

// materializeAndAppend resumes a compressed session as Codex does: the .jsonl
// is written again with the content and time of the .zst, the .zst removed,
// and lines are appended
func materializeAndAppend(t *testing.T, path, appended string) {
	t.Helper()
	content, compressed, err := importer.OpenSessionFile(path)
	if err != nil || !compressed {
		t.Fatalf("OpenSessionFile = %v, %v", compressed, err)
	}
	data, err := io.ReadAll(content)
	content.Close()
	if err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(path + ".zst")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	os.Chtimes(path, info.ModTime(), info.ModTime())
	if err := os.Remove(path + ".zst"); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(appended)
	f.Close()
}

// codexRolloutLines returns the importer's tool fixture split into a first
// part and the lines a resumed session appends
func codexRolloutLines(t *testing.T) (first, appended string) {
	t.Helper()
	lines := readCodexToolsFixture(t)
	half := len(lines) / 2
	return strings.Join(lines[:half], ""), strings.Join(lines[half:], "") + "\n"
}

// storedRecords describes the stored logs and metrics, sorted, without what
// differs between import and watch (import_source) and the file's directory
func storedRecords(t *testing.T, store *storage.DuckDBStore) []string {
	t.Helper()
	rows, err := store.DB().Query(`
		SELECT concat_ws('|', regexp_extract(SourceFile, '[^/]+$'), strftime(Timestamp, '%H:%M:%S.%f'), Body,
			CAST(json_remove(LogAttributes, '$.import_source') AS VARCHAR))
		FROM otel_logs
		UNION ALL
		SELECT concat_ws('|', regexp_extract(SourceFile, '[^/]+$'), strftime(Timestamp, '%H:%M:%S.%f'), MetricName,
			CAST(Value AS VARCHAR), CAST(json_remove(Attributes, '$.import_source') AS VARCHAR))
		FROM otel_metrics`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		out = append(out, s)
	}
	slices.Sort(out)
	return out
}

func newStore(t *testing.T) *storage.DuckDBStore {
	t.Helper()
	store, err := storage.NewDuckDBStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

// importCodexDir imports the Codex sessions in dir into store
func importCodexDir(t *testing.T, store *storage.DuckDBStore, dir string) {
	t.Helper()
	t.Setenv("AI_OBSERVER_CODEX_PATH", dir)
	imp := importer.NewImporter(store, false)
	imp.SetIO(&strings.Builder{}, strings.NewReader(""))
	imp.RegisterParser(importer.NewCodexParser())
	if err := imp.Import(context.Background(), []importer.SourceType{importer.SourceCodex}, importer.Options{SkipConfirm: true}); err != nil {
		t.Fatal(err)
	}
}

// freshImport returns what an import of a plain file with content stores
func freshImport(t *testing.T, name, content string) []string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	store := newStore(t)
	importCodexDir(t, store, dir)
	return storedRecords(t, store)
}

// scanCodex runs the watcher's startup scan of dir
func scanCodex(t *testing.T, store *storage.DuckDBStore, dir string, backfill bool) {
	t.Helper()
	t.Setenv("AI_OBSERVER_CODEX_PATH", dir)
	w := New(store, nil, Config{Tools: []string{"codex"}, Backfill: backfill})
	if err := w.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	w.waitInitialScan()
	w.Stop()
}

func sameRecords(t *testing.T, label string, got, want []string) {
	t.Helper()
	if len(want) == 0 {
		t.Fatalf("%s: the reference stored nothing", label)
	}
	if !slices.Equal(got, want) {
		t.Errorf("%s: %d records, want %d", label, len(got), len(want))
		for _, r := range got {
			if !slices.Contains(want, r) {
				t.Logf("  unexpected: %.200s", r)
			}
		}
		for _, r := range want {
			if !slices.Contains(got, r) {
				t.Logf("  missing: %.200s", r)
			}
		}
	}
}

// --backfill reads compressed session files, by their plain path, like an
// import; without it the scan records their end
func TestCodexWatchScanReadsCompressedFiles(t *testing.T) {
	first, appended := codexRolloutLines(t)
	content := first + appended
	name := "rollout-2026-10-26T07-33-20-sess-tools.jsonl"
	want := freshImport(t, name, content)

	for _, backfill := range []bool{true, false} {
		dir := t.TempDir()
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		compressLikeCodex(t, path)
		store := newStore(t)
		scanCodex(t, store, dir, backfill)

		if backfill {
			sameRecords(t, "backfill", storedRecords(t, store), want)
		} else if got := storedRecords(t, store); len(got) != 0 {
			t.Errorf("scan without backfill stored %d records", len(got))
		}
		state, err := store.GetImportState(context.Background(), string(importer.SourceCodex), path)
		if err != nil || state == nil || state.ByteOffset != int64(len(content)) {
			t.Fatalf("backfill %v: state of the plain path %+v, %v; want offset %d", backfill, state, err, len(content))
		}
		if zst, _ := store.GetImportState(context.Background(), string(importer.SourceCodex), path+".zst"); zst != nil {
			t.Error("state stored for the compressed path")
		}
	}
}

// A file imported (as .jsonl or compressed) and then compressed is no change
// for the startup scan; once Codex resumes the session, watch continues after
// what the import read, without duplicates
func TestCodexWatchContinuesAResumedCompressedSession(t *testing.T) {
	first, appended := codexRolloutLines(t)
	name := "rollout-2026-10-26T07-33-20-sess-tools.jsonl"
	want := freshImport(t, name, first+appended)

	for _, compressedBeforeImport := range []bool{false, true} {
		dir := t.TempDir()
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(first), 0o644); err != nil {
			t.Fatal(err)
		}
		if compressedBeforeImport {
			compressLikeCodex(t, path)
		}
		store := newStore(t)
		importCodexDir(t, store, dir)
		imported := storedRecords(t, store)
		if !compressedBeforeImport {
			compressLikeCodex(t, path)
		}
		stateBefore, _ := store.GetImportState(context.Background(), string(importer.SourceCodex), path)

		scanCodex(t, store, dir, true)
		if got := storedRecords(t, store); !slices.Equal(got, imported) {
			t.Errorf("scan of the compressed file changed the records: %d, were %d", len(got), len(imported))
		}
		if state, _ := store.GetImportState(context.Background(), string(importer.SourceCodex), path); state.ParserState != stateBefore.ParserState || state.ByteOffset != stateBefore.ByteOffset {
			t.Error("scan of the compressed file read it")
		}

		materializeAndAppend(t, path, appended)
		scanCodex(t, store, dir, true)
		sameRecords(t, "resumed", storedRecords(t, store), want)
	}
}

// A running watcher ignores Codex compressing a file it read (the .jsonl is
// removed, the .zst created) and continues the file when it is resumed
func TestCodexLiveWatchIgnoresCompression(t *testing.T) {
	first, appended := codexRolloutLines(t)
	name := "rollout-2026-10-26T07-33-20-sess-tools.jsonl"
	want := freshImport(t, name, first+appended)

	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(first), 0o644); err != nil {
		t.Fatal(err)
	}
	store := newStore(t)
	t.Setenv("AI_OBSERVER_CODEX_PATH", dir)
	w := New(store, nil, Config{Tools: []string{"codex"}, Backfill: true})
	if err := w.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer w.Stop()
	w.waitInitialScan()
	read := storedRecords(t, store)
	stateBefore, _ := store.GetImportState(context.Background(), string(importer.SourceCodex), path)

	compressLikeCodex(t, path)
	time.Sleep(600 * time.Millisecond) // longer than the Codex debounce
	if got := storedRecords(t, store); !slices.Equal(got, read) {
		t.Errorf("compression changed the records: %d, were %d", len(got), len(read))
	}
	if state, _ := store.GetImportState(context.Background(), string(importer.SourceCodex), path); state.ByteOffset != stateBefore.ByteOffset || state.ParserState != stateBefore.ParserState {
		t.Error("compression changed the watch state")
	}

	materializeAndAppend(t, path, appended)
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) && len(storedRecords(t, store)) < len(want) {
		time.Sleep(100 * time.Millisecond)
	}
	time.Sleep(300 * time.Millisecond)
	sameRecords(t, "resumed", storedRecords(t, store), want)
}

// The incremental parser reads a compressed file from an offset like the
// plain file, with the parser state kept and with it rebuilt from the prefix
func TestCodexIncrementalParserReadsCompressedFile(t *testing.T) {
	first, appended := codexRolloutLines(t)
	dir := t.TempDir()
	plain := filepath.Join(dir, "plain", "rollout-x.jsonl")
	zst := filepath.Join(dir, "zst", "rollout-x.jsonl")
	for _, p := range []string{plain, zst} {
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(first+appended), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	compressLikeCodex(t, zst)

	parse := func(path string, state storage.ImportState) []string {
		t.Helper()
		state.FilePath = path
		result, err := (&codexIncrementalParser{}).ParseIncremental(context.Background(), path, &state)
		if err != nil {
			t.Fatal(err)
		}
		if state.ByteOffset != int64(len(first+appended)) {
			t.Errorf("offset %d after reading %s, want %d", state.ByteOffset, path, len(first+appended))
		}
		return comparableLogs(result.Logs)
	}
	// State after reading the first part, kept by watch
	part := filepath.Join(dir, "rollout-x.jsonl")
	if err := os.WriteFile(part, []byte(first), 0o644); err != nil {
		t.Fatal(err)
	}
	kept := storage.ImportState{FilePath: part}
	if _, err := (&codexIncrementalParser{}).ParseIncremental(context.Background(), part, &kept); err != nil {
		t.Fatal(err)
	}
	for label, state := range map[string]storage.ImportState{
		"kept state":     kept,
		"after import":   {ByteOffset: int64(len(first))},
		"from the start": {},
	} {
		want := parse(plain, state)
		if len(want) == 0 {
			t.Fatalf("%s: nothing read", label)
		}
		if got := parse(zst, state); !slices.Equal(got, want) {
			t.Errorf("%s: compressed file gave %d logs, the plain file %d", label, len(got), len(want))
		}
	}
}

// messageKeys returns the stored logs' message keys, sorted
func messageKeys(t *testing.T, store *storage.DuckDBStore) []string {
	t.Helper()
	rows, err := store.DB().Query("SELECT COALESCE(MessageKey, '<null>') FROM otel_logs ORDER BY 1")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var keys []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			t.Fatal(err)
		}
		keys = append(keys, k)
	}
	return keys
}

// Tool calls watch stores get the message keys an import gives them, so a
// watched session whose calls also arrived over OTLP is listed with as many
// messages as its transcript shows
func TestCodexWatchedToolCallsCountLikeTranscript(t *testing.T) {
	first, appended := codexRolloutLines(t)
	name := "rollout-2026-10-26T07-33-20-sess-tools.jsonl"
	t0 := time.Date(2026, 10, 26, 7, 33, 20, 0, time.UTC)
	otlp := func(offset time.Duration, event string, attrs map[string]string) api.LogRecord {
		attrs["event.name"] = event
		attrs["conversation.id"] = "sess-tools"
		return api.LogRecord{Timestamp: t0.Add(offset), ServiceName: "codex-app-server", Body: event, LogAttributes: attrs}
	}
	events := []api.LogRecord{
		otlp(1200*time.Millisecond, "codex.tool_decision", map[string]string{"call_id": "exec-1", "tool_name": "exec_command", "decision": "approved"}),
		otlp(3990*time.Millisecond, "codex.tool_result", map[string]string{"call_id": "exec-1", "tool_name": "exec_command", "output": "ok", "duration_ms": "2490", "success": "true"}),
		otlp(5690*time.Millisecond, "codex.tool_result", map[string]string{"call_id": "call_exec1", "tool_name": "exec", "output": "ok", "duration_ms": "4690", "success": "true"}),
		otlp(8305*time.Millisecond, "codex.tool_result", map[string]string{"call_id": "call_cmd1", "tool_name": "exec_command", "output": "ls: missing", "duration_ms": "260", "success": "true"}),
	}

	// Imported, for the keys
	importDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(importDir, name), []byte(first+appended), 0o644); err != nil {
		t.Fatal(err)
	}
	imported := newStore(t)
	importCodexDir(t, imported, importDir)

	// Watched in two runs, the second after the session was compressed and
	// resumed, with the OTLP events in between
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(first), 0o644); err != nil {
		t.Fatal(err)
	}
	store := newStore(t)
	scanCodex(t, store, dir, true)
	ctx := context.Background()
	if err := store.InsertTelemetry(ctx, events, nil, nil); err != nil {
		t.Fatal(err)
	}
	compressLikeCodex(t, path)
	materializeAndAppend(t, path, appended)
	scanCodex(t, store, dir, true)

	if err := imported.InsertTelemetry(ctx, events, nil, nil); err != nil {
		t.Fatal(err)
	}
	if got, want := messageKeys(t, store), messageKeys(t, imported); !slices.Equal(got, want) || slices.Contains(got, "<null>") {
		t.Errorf("watch stored %d message keys unlike the import's %d", len(got), len(want))
	}

	got, err := transcript.Load(ctx, store, "sess-tools")
	if err != nil {
		t.Fatal(err)
	}
	list, err := store.QuerySessions(ctx, "", t0.Add(-time.Hour), t0.Add(24*time.Hour), storage.Page{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	listed := 0
	for _, s := range list.Sessions {
		if s.SessionID == "sess-tools" {
			listed += s.MessageCount
		}
	}
	if len(got.Messages) == 0 || listed != len(got.Messages) {
		t.Errorf("session list counts %d messages, transcript shows %d", listed, len(got.Messages))
	}
}
