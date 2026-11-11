package importer

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"
	"github.com/tobilg/ai-observer/internal/storage"
)

// compressLikeCodex replaces the session file at path by its compressed form
// as Codex's compression worker does (rollout/src/compression.rs): one zstd
// frame with the content size in its header (withSize), the modification time
// of the .jsonl, and the .jsonl removed
func compressLikeCodex(t *testing.T, path string, withSize bool) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	var compressed []byte
	if withSize {
		enc, _ := zstd.NewWriter(nil)
		compressed = enc.EncodeAll(data, nil)
	} else {
		var buf bytes.Buffer
		// Flushing before the end writes the frame header without the size
		enc, _ := zstd.NewWriter(&buf)
		enc.Write(data[:1])
		enc.Flush()
		enc.Write(data[1:])
		enc.Close()
		compressed = buf.Bytes()
	}
	var header zstd.Header
	if err := header.Decode(compressed); err != nil || header.HasFCS != withSize {
		t.Fatalf("frame header: %+v, %v; want content size %v", header, err, withSize)
	}
	zst := path + ".zst"
	if err := os.WriteFile(zst, compressed, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(zst, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	return zst
}

// materializeLikeCodex turns a compressed session file back into the .jsonl
// Codex appends to when the session is resumed: same content and
// modification time, the .zst removed
func materializeLikeCodex(t *testing.T, path string) {
	t.Helper()
	content, compressed, err := OpenSessionFile(path)
	if err != nil || !compressed {
		t.Fatalf("OpenSessionFile = %v, %v; want the compressed file", compressed, err)
	}
	data, err := io.ReadAll(content)
	content.Close()
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path + ".zst")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path + ".zst"); err != nil {
		t.Fatal(err)
	}
}

// codexSessionLines returns the tool fixture's lines as a session with the
// given ID, split in a first part and the lines a resumed session appends
func codexSessionLines(t *testing.T, id string) (first, appended string) {
	t.Helper()
	data, err := os.ReadFile(codexToolsFixture)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.SplitAfter(strings.ReplaceAll(string(data), "sess-tools", id), "\n")
	half := len(lines) / 2
	return strings.Join(lines[:half], ""), strings.Join(lines[half:], "")
}

// codexImporter returns an importer of the Codex sessions in dir, its
// parse counter and store
func codexImporter(t *testing.T, dir string) (*Importer, *countingParser, *storage.DuckDBStore) {
	t.Helper()
	t.Setenv("AI_OBSERVER_CODEX_PATH", dir)
	store, err := storage.NewDuckDBStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	parser := &countingParser{SessionParser: NewCodexParser()}
	imp := NewImporter(store, false)
	imp.SetIO(&strings.Builder{}, strings.NewReader(""))
	imp.RegisterParser(parser)
	return imp, parser, store
}

func importCodex(t *testing.T, imp *Importer, opts Options) {
	t.Helper()
	opts.SkipConfirm = true
	if err := imp.Import(context.Background(), []SourceType{SourceCodex}, opts); err != nil {
		t.Fatal(err)
	}
}

// storedCodexRecords describes the stored logs and metrics, by session file
// name, sorted: equal for equal content however it was read
func storedCodexRecords(t *testing.T, store *storage.DuckDBStore) []string {
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
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	slices.Sort(out)
	return out
}

// freshCodexImport imports plain session files with the given content into
// an empty store and returns what it stored
func freshCodexImport(t *testing.T, files map[string]string) []string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	imp, _, store := codexImporter(t, dir)
	importCodex(t, imp, Options{})
	return storedCodexRecords(t, store)
}

func sameRecords(t *testing.T, label string, got, want []string) {
	t.Helper()
	if len(want) == 0 {
		t.Fatalf("%s: the reference import stored nothing", label)
	}
	if !slices.Equal(got, want) {
		t.Errorf("%s: %d records, want the %d of a fresh import of the content", label, len(got), len(want))
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

// Session files Codex compressed are found by their plain path, once, and
// imported like the plain file; Codex's temporary files are ignored
func TestCodexImportReadsCompressedSessionFiles(t *testing.T) {
	dir := t.TempDir()
	day := filepath.Join(dir, "2026", "10", "26")
	if err := os.MkdirAll(day, 0o755); err != nil {
		t.Fatal(err)
	}
	plainA, _ := codexSessionLines(t, "sess-a")
	contentB, appendedB := codexSessionLines(t, "sess-b")
	contentB += appendedB
	contentC, _ := codexSessionLines(t, "sess-c")
	a := filepath.Join(day, "rollout-2026-10-26T07-00-00-a.jsonl")
	b := filepath.Join(day, "rollout-2026-10-26T08-00-00-b.jsonl")
	c := filepath.Join(day, "rollout-2026-10-26T09-00-00-c.jsonl")
	for path, content := range map[string]string{a: plainA, b: contentB, c: contentC} {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	compressLikeCodex(t, b, true)
	// Mid-publication: Codex wrote the .zst and has not removed the .jsonl
	zstC := compressLikeCodex(t, c, true)
	if err := os.WriteFile(c, []byte(contentC), 0o644); err != nil {
		t.Fatal(err)
	}
	// Temporary files of compression and materialization
	for _, tmp := range []string{"rollout-compress-x1.tmp", "rollout-2026-10-26T10-00-00-d.jsonl.zst.tmp", ".rollout-materialize-x2"} {
		if err := os.WriteFile(filepath.Join(day, tmp), []byte("{}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	files, err := (&CodexParser{sessionsPath: dir}).FindSessionFiles(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{a, b, c}; !slices.Equal(files, want) {
		t.Fatalf("FindSessionFiles = %v, want %v", files, want)
	}

	imp, _, store := codexImporter(t, dir)
	importCodex(t, imp, Options{})
	sameRecords(t, "import", storedCodexRecords(t, store), freshCodexImport(t, map[string]string{
		filepath.Base(a): plainA, filepath.Base(b): contentB, filepath.Base(c): contentC,
	}))

	// Import state by the plain path, with the content's size and hash
	state, err := store.GetImportState(context.Background(), string(SourceCodex), b)
	if err != nil || state == nil {
		t.Fatalf("import state of %s: %v, %v", b, state, err)
	}
	if state.FileSize != int64(len(contentB)) || state.ByteOffset != int64(len(contentB)) {
		t.Errorf("state size %d, offset %d; want the content size %d", state.FileSize, state.ByteOffset, len(contentB))
	}
	if want, _ := computeFileHash(b); state.FileHash != want || want == "" {
		t.Errorf("state hash %q, want the content's %q", state.FileHash, want)
	}
	if zst, _ := store.GetImportState(context.Background(), string(SourceCodex), zstC); zst != nil {
		t.Errorf("import state stored for the compressed path %s", zstC)
	}
}

// A file imported as .jsonl that Codex compressed later is unchanged: it is
// neither read again nor are its records replaced. With the content size in
// the frame header (as Codex writes it) not even its hash is computed.
func TestCodexCompressedFileImportedBeforeIsUnchanged(t *testing.T) {
	for _, withSize := range []bool{true, false} {
		t.Run(map[bool]string{true: "content size in header", false: "no content size"}[withSize], func(t *testing.T) {
			dir := t.TempDir()
			content, _ := codexSessionLines(t, "sess-a")
			path := filepath.Join(dir, "rollout-2026-10-26T07-00-00-a.jsonl")
			if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
			old := time.Now().Add(-8 * 24 * time.Hour)
			if err := os.Chtimes(path, old, old); err != nil {
				t.Fatal(err)
			}
			imp, parser, store := codexImporter(t, dir)
			importCodex(t, imp, Options{})
			before := storedCodexRecords(t, store)
			stateBefore, _ := store.GetImportState(context.Background(), string(SourceCodex), path)

			zst := compressLikeCodex(t, path, withSize)
			if !withSize {
				// Without the size only the content's hash tells
				later := old.Add(time.Hour)
				if err := os.Chtimes(zst, later, later); err != nil {
					t.Fatal(err)
				}
			} else {
				// Keep the header, break the rest: unchanged must be
				// recognized without decompressing
				data, _ := os.ReadFile(zst)
				if err := os.WriteFile(zst, data[:zstd.HeaderMaxSize], 0o644); err != nil {
					t.Fatal(err)
				}
				if err := os.Chtimes(zst, old, old); err != nil {
					t.Fatal(err)
				}
			}

			status, err := imp.state.CheckFileStatus(context.Background(), SourceCodex, path)
			if err != nil || status != StatusCurrent {
				t.Fatalf("CheckFileStatus = %q, %v; want current", status, err)
			}
			parses := parser.parses
			importCodex(t, imp, Options{})
			if parser.parses != parses {
				t.Error("the compressed file was read again")
			}
			if after := storedCodexRecords(t, store); !slices.Equal(after, before) {
				t.Errorf("records changed: %d, were %d", len(after), len(before))
			}
			stateAfter, _ := store.GetImportState(context.Background(), string(SourceCodex), path)
			if !stateAfter.ImportedAt.Equal(stateBefore.ImportedAt) {
				t.Error("the import state was rewritten")
			}
		})
	}
}

// A compressed session that Codex resumes becomes a .jsonl again with lines
// appended: the import replaces its records with those of the whole file
func TestCodexResumedCompressedSessionIsReplaced(t *testing.T) {
	dir := t.TempDir()
	first, appended := codexSessionLines(t, "sess-a")
	path := filepath.Join(dir, "rollout-2026-10-26T07-00-00-a.jsonl")
	if err := os.WriteFile(path, []byte(first), 0o644); err != nil {
		t.Fatal(err)
	}
	compressLikeCodex(t, path, true)

	imp, _, store := codexImporter(t, dir)
	importCodex(t, imp, Options{})
	name := filepath.Base(path)
	sameRecords(t, "compressed", storedCodexRecords(t, store), freshCodexImport(t, map[string]string{name: first}))

	materializeLikeCodex(t, path)
	if status, err := imp.state.CheckFileStatus(context.Background(), SourceCodex, path); err != nil || status != StatusCurrent {
		t.Fatalf("materialized, before appending: CheckFileStatus = %q, %v; want current", status, err)
	}
	appendLine(t, path, appended)
	importCodex(t, imp, Options{})
	sameRecords(t, "resumed", storedCodexRecords(t, store), freshCodexImport(t, map[string]string{name: first + appended}))

	// And compressed again later: unchanged
	compressLikeCodex(t, path, true)
	if status, err := imp.state.CheckFileStatus(context.Background(), SourceCodex, path); err != nil || status != StatusCurrent {
		t.Fatalf("compressed again: CheckFileStatus = %q, %v; want current", status, err)
	}
}

// Decompression is a stream: reading a large compressed file does not hold
// its content in memory
func TestOpenSessionFileStreams(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rollout-big.jsonl")
	f, err := os.Create(path + ".zst")
	if err != nil {
		t.Fatal(err)
	}
	enc, _ := zstd.NewWriter(f)
	line := []byte(`{"timestamp":"2026-10-26T07:00:00Z","type":"event_msg","payload":{"type":"x","text":"` + strings.Repeat("abcdefgh", 128) + `"}}` + "\n")
	const size = 64 << 20
	for written := 0; written < size; written += len(line) {
		enc.Write(line)
	}
	enc.Close()
	f.Close()

	content, compressed, err := OpenSessionFile(path)
	if err != nil || !compressed {
		t.Fatalf("OpenSessionFile = %v, %v", compressed, err)
	}
	defer content.Close()
	var before, after runtimeMem
	before.read()
	n, err := io.Copy(io.Discard, content)
	after.read()
	if err != nil || n < size {
		t.Fatalf("read %d bytes, %v", n, err)
	}
	if grown := after.totalAlloc - before.totalAlloc; grown > size/4 {
		t.Errorf("reading %d MB allocated %d MB", n>>20, grown>>20)
	}
}

// runtimeMem reads the bytes allocated so far
type runtimeMem struct{ totalAlloc uint64 }

func (m *runtimeMem) read() {
	var s runtime.MemStats
	runtime.ReadMemStats(&s)
	m.totalAlloc = s.TotalAlloc
}
