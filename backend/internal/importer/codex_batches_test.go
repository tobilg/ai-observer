package importer

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// readInBatches makes every file be read in batches of n records
func readInBatches(t *testing.T, n int) {
	t.Helper()
	size, records := batchFileSize, importBatchRecords
	batchFileSize, importBatchRecords = -1, n
	t.Cleanup(func() { batchFileSize, importBatchRecords = size, records })
}

// writeCodexSessions writes two sessions into a new directory, one of them
// compressed, and returns it
func writeCodexSessions(t *testing.T) (dir string, files map[string]string) {
	t.Helper()
	dir = t.TempDir()
	first, appended := codexSessionLines(t, "sess-a")
	other, _ := codexSessionLines(t, "sess-b")
	files = map[string]string{
		"rollout-2026-10-26T07-00-00-a.jsonl": first + appended,
		"rollout-2026-10-26T08-00-00-b.jsonl": other,
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	compressLikeCodex(t, filepath.Join(dir, "rollout-2026-10-26T08-00-00-b.jsonl"), true)
	return dir, files
}

// A file read in batches stores what reading it whole stores, with the same
// import state, also when it replaces the records of an earlier import
func TestCodexImportInBatchesStoresLikeWholeFiles(t *testing.T) {
	dir, files := writeCodexSessions(t)
	want := freshCodexImport(t, files)
	wholeImp, _, whole := codexImporter(t, dir)
	importCodex(t, wholeImp, Options{})

	readInBatches(t, 7)
	imp, _, store := codexImporter(t, dir)
	var preview strings.Builder
	imp.SetIO(&preview, strings.NewReader(""))
	if err := imp.Import(context.Background(), []SourceType{SourceCodex}, Options{DryRun: true}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(preview.String(), "Files: 2 total (2 new") {
		t.Errorf("preview:\n%s", preview.String())
	}

	importCodex(t, imp, Options{})
	sameRecords(t, "batched", storedCodexRecords(t, store), want)
	for name := range files {
		path := filepath.Join(dir, name)
		got, _ := store.GetImportState(context.Background(), string(SourceCodex), path)
		ref, _ := whole.GetImportState(context.Background(), string(SourceCodex), path)
		if got == nil || ref == nil || got.FileHash != ref.FileHash || got.RecordCount != ref.RecordCount ||
			got.ByteOffset != ref.ByteOffset || got.MessageCount != ref.MessageCount ||
			got.FileSize != ref.FileSize || got.FileModTimeNs != ref.FileModTimeNs || !got.SourceTracked || got.Partial {
			t.Errorf("%s: state %+v, want %+v", name, got, ref)
		}
	}

	// Forced: replaced batch by batch, no duplicates
	importCodex(t, imp, Options{Force: true})
	sameRecords(t, "forced", storedCodexRecords(t, store), want)
}

// The preview counts a file read in batches like one read whole
func TestCodexPreviewCountsBatchedFiles(t *testing.T) {
	dir, _ := writeCodexSessions(t)
	imp, _, _ := codexImporter(t, dir)
	whole, err := imp.scanSource(context.Background(), imp.parsers[SourceCodex], Options{})
	if err != nil {
		t.Fatal(err)
	}
	readInBatches(t, 5)
	batched, err := imp.scanSource(context.Background(), imp.parsers[SourceCodex], Options{})
	if err != nil {
		t.Fatal(err)
	}
	if whole.TotalLogs == 0 || batched.TotalLogs != whole.TotalLogs || batched.TotalMetrics != whole.TotalMetrics ||
		!slices.EqualFunc(batched.Files, whole.Files, func(a, b FileSummary) bool { return a == b }) {
		t.Errorf("batched preview %+v, whole %+v", batched, whole)
	}
}

// failingBatches fails after storing its first batch
type failingBatches struct{ *CodexParser }

func (p failingBatches) ParseFileInBatches(ctx context.Context, path string, n int, emit func(*ImportResult) error) (*ImportResult, error) {
	calls := 0
	return p.CodexParser.ParseFileInBatches(ctx, path, n, func(b *ImportResult) error {
		if calls++; calls > 1 {
			return errors.New("disk full")
		}
		return emit(b)
	})
}

// A file whose batches fail midway changes nothing: its earlier records and
// import state stay as they were
func TestCodexBatchedImportIsAtomic(t *testing.T) {
	dir, _ := writeCodexSessions(t)
	imp, _, store := codexImporter(t, dir)
	importCodex(t, imp, Options{})
	before := storedCodexRecords(t, store)
	path := filepath.Join(dir, "rollout-2026-10-26T07-00-00-a.jsonl")
	stateBefore, _ := store.GetImportState(context.Background(), string(SourceCodex), path)

	readInBatches(t, 5)
	info, _ := StatSessionFile(path)
	_, _, err := imp.importInBatches(context.Background(), failingBatches{NewCodexParser()}, SourceCodex, path, info, stateBefore, store)
	if err == nil {
		t.Fatal("expected the failure")
	}
	if after := storedCodexRecords(t, store); !slices.Equal(after, before) {
		t.Errorf("records changed: %d, were %d", len(after), len(before))
	}
	if state, _ := store.GetImportState(context.Background(), string(SourceCodex), path); *state != *stateBefore {
		t.Errorf("state changed: %+v, was %+v", state, stateBefore)
	}
}
