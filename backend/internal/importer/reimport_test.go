package importer

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/tobilg/ai-observer/internal/storage"
)

// secondClaudeLine is a later turn of the session in claudeSessionLine
var secondClaudeLine = strings.NewReplacer("req-1", "req-2", "msg-1", "msg-2", "10:00:00", "10:05:00").Replace(claudeSessionLine)

func appendLine(t *testing.T, path, line string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("opening %s: %v", path, err)
	}
	defer f.Close()
	if _, err := f.WriteString(line); err != nil {
		t.Fatalf("appending: %v", err)
	}
}

// telemetryCounts returns the stored logs and metrics
func telemetryCounts(t *testing.T, store *storage.DuckDBStore) (logs, metrics int) {
	t.Helper()
	ctx := context.Background()
	if err := store.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM otel_logs").Scan(&logs); err != nil {
		t.Fatal(err)
	}
	if err := store.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM otel_metrics").Scan(&metrics); err != nil {
		t.Fatal(err)
	}
	return logs, metrics
}

// freshImportCounts imports the file at path into an empty store
func freshImportCounts(t *testing.T, path string) (logs, metrics int) {
	t.Helper()
	store, err := storage.NewDuckDBStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	imp := NewImporter(store, false)
	imp.SetIO(&strings.Builder{}, strings.NewReader(""))
	imp.RegisterParser(NewClaudeParser())
	if err := imp.Import(context.Background(), []SourceType{SourceClaude}, Options{SkipConfirm: true}); err != nil {
		t.Fatal(err)
	}
	return telemetryCounts(t, store)
}

func TestReimportingAGrownFileReplacesItsRecords(t *testing.T) {
	imp, _, store, path := setupClaudeImport(t)
	imp.SetIO(&strings.Builder{}, strings.NewReader(""))
	ctx := context.Background()

	if err := imp.Import(ctx, []SourceType{SourceClaude}, Options{SkipConfirm: true}); err != nil {
		t.Fatal(err)
	}
	appendLine(t, path, secondClaudeLine)
	if err := imp.Import(ctx, []SourceType{SourceClaude}, Options{SkipConfirm: true}); err != nil {
		t.Fatal(err)
	}

	gotLogs, gotMetrics := telemetryCounts(t, store)
	wantLogs, wantMetrics := freshImportCounts(t, path)
	if gotLogs != wantLogs || gotMetrics != wantMetrics {
		t.Errorf("after re-import: %d logs, %d metrics; a single import stores %d logs, %d metrics",
			gotLogs, gotMetrics, wantLogs, wantMetrics)
	}
}

func TestForcedImportDoesNotDuplicate(t *testing.T) {
	imp, _, store, path := setupClaudeImport(t)
	imp.SetIO(&strings.Builder{}, strings.NewReader(""))
	ctx := context.Background()

	for range 2 {
		if err := imp.Import(ctx, []SourceType{SourceClaude}, Options{SkipConfirm: true, Force: true}); err != nil {
			t.Fatal(err)
		}
	}
	gotLogs, gotMetrics := telemetryCounts(t, store)
	wantLogs, wantMetrics := freshImportCounts(t, path)
	if gotLogs != wantLogs || gotMetrics != wantMetrics {
		t.Errorf("after two forced imports: %d logs, %d metrics, want %d and %d", gotLogs, gotMetrics, wantLogs, wantMetrics)
	}
}

// Files imported by versions that did not link records to their file are
// replaced too: their imported records equal to the new ones are deleted,
// while live telemetry of the tool is kept
func TestReimportReplacesRecordsOfEarlierVersions(t *testing.T) {
	imp, _, store, path := setupClaudeImport(t)
	imp.SetIO(&strings.Builder{}, strings.NewReader(""))
	ctx := context.Background()

	if err := imp.Import(ctx, []SourceType{SourceClaude}, Options{SkipConfirm: true}); err != nil {
		t.Fatal(err)
	}
	// What an earlier version stored: records without SourceFile
	for _, stmt := range []string{
		"UPDATE otel_logs SET SourceFile = NULL",
		"UPDATE otel_metrics SET SourceFile = NULL",
		"UPDATE import_state SET source_tracked = false, file_size = 0",
		// Live telemetry at the same time, which must survive
		`INSERT INTO otel_logs (Timestamp, ServiceName, Body, LogAttributes)
			SELECT Timestamp, ServiceName, 'live', '{"event.name": "api_request"}' FROM otel_logs LIMIT 1`,
	} {
		if _, err := store.DB().ExecContext(ctx, stmt); err != nil {
			t.Fatal(err)
		}
	}

	appendLine(t, path, secondClaudeLine)
	if err := imp.Import(ctx, []SourceType{SourceClaude}, Options{SkipConfirm: true}); err != nil {
		t.Fatalf("Import = %v", err)
	}
	gotLogs, gotMetrics := telemetryCounts(t, store)
	wantLogs, wantMetrics := freshImportCounts(t, path)
	if gotLogs != wantLogs+1 || gotMetrics != wantMetrics {
		t.Errorf("after re-import: %d logs, %d metrics, want %d (one live) and %d", gotLogs, gotMetrics, wantLogs+1, wantMetrics)
	}
	var live int
	if err := store.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM otel_logs WHERE Body = 'live'").Scan(&live); err != nil || live != 1 {
		t.Errorf("live logs = %d (%v), want 1", live, err)
	}
	state, err := store.GetImportState(ctx, string(SourceClaude), path)
	if err != nil || state == nil || !state.SourceTracked {
		t.Errorf("re-imported file should be source-tracked, got %+v (%v)", state, err)
	}
}

// An import of a date range marks the file partial, so a later import reads
// the records it left out, and watch reads the file again when records after
// the range were left out
func TestRangedImportIsCompletedLater(t *testing.T) {
	imp, _, store, path := setupClaudeImport(t)
	imp.SetIO(&strings.Builder{}, strings.NewReader(""))
	ctx := context.Background()
	appendLine(t, path, strings.ReplaceAll(secondClaudeLine, "2025-01-02", "2025-01-05"))

	to, _ := ParseToDateArg("2025-01-03")
	if err := imp.Import(ctx, []SourceType{SourceClaude}, Options{SkipConfirm: true, ToDate: to}); err != nil {
		t.Fatal(err)
	}
	state, err := store.GetImportState(ctx, string(SourceClaude), path)
	if err != nil || state == nil {
		t.Fatalf("import state: %+v (%v)", state, err)
	}
	if !state.Partial || state.ByteOffset != 0 {
		t.Errorf("state after a ranged import: partial=%v offset=%d, want partial and offset 0", state.Partial, state.ByteOffset)
	}

	if err := imp.Import(ctx, []SourceType{SourceClaude}, Options{SkipConfirm: true}); err != nil {
		t.Fatal(err)
	}
	gotLogs, gotMetrics := telemetryCounts(t, store)
	wantLogs, wantMetrics := freshImportCounts(t, path)
	if gotLogs != wantLogs || gotMetrics != wantMetrics {
		t.Errorf("after the full import: %d logs, %d metrics, want %d and %d", gotLogs, gotMetrics, wantLogs, wantMetrics)
	}
	if state, _ := store.GetImportState(ctx, string(SourceClaude), path); state == nil || state.Partial || state.ByteOffset == 0 {
		t.Errorf("state after the full import: %+v, want complete", state)
	}
}

// When a file that rebuilds a purged range cannot be imported, the purge is
// rolled back, so the data of that file is not lost
func TestFailedPurgeChangesNothing(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can read unreadable files")
	}
	imp, _, store, path := setupClaudeImport(t)
	imp.SetIO(&strings.Builder{}, strings.NewReader(""))
	ctx := context.Background()

	if err := imp.Import(ctx, []SourceType{SourceClaude}, Options{SkipConfirm: true}); err != nil {
		t.Fatal(err)
	}
	wantLogs, wantMetrics := telemetryCounts(t, store)
	other := strings.Replace(path, "session.jsonl", "other.jsonl", 1)
	if err := os.WriteFile(other, []byte(secondClaudeLine), 0o000); err != nil {
		t.Fatal(err)
	}

	from, _ := ParseDateArg("2025-01-01")
	to, _ := ParseToDateArg("2025-01-31")
	err := imp.Import(ctx, []SourceType{SourceClaude}, Options{SkipConfirm: true, Purge: true, FromDate: from, ToDate: to})
	if !errors.Is(err, ErrIncomplete) {
		t.Fatalf("Import = %v, want ErrIncomplete", err)
	}
	if logs, metrics := telemetryCounts(t, store); logs != wantLogs || metrics != wantMetrics {
		t.Errorf("after a failed purge: %d logs, %d metrics, want %d and %d", logs, metrics, wantLogs, wantMetrics)
	}
}

// --purge with a range rebuilds the range even when no file changed
func TestPurgeWithRangeRebuildsUnchangedFiles(t *testing.T) {
	imp, _, store, _ := setupClaudeImport(t)
	imp.SetIO(&strings.Builder{}, strings.NewReader(""))
	ctx := context.Background()

	if err := imp.Import(ctx, []SourceType{SourceClaude}, Options{SkipConfirm: true}); err != nil {
		t.Fatal(err)
	}
	wantLogs, wantMetrics := telemetryCounts(t, store)
	if _, err := store.DB().ExecContext(ctx, "DELETE FROM otel_logs"); err != nil {
		t.Fatal(err)
	}

	from, _ := ParseDateArg("2025-01-01")
	to, _ := ParseToDateArg("2025-01-31")
	if err := imp.Import(ctx, []SourceType{SourceClaude}, Options{SkipConfirm: true, Purge: true, FromDate: from, ToDate: to}); err != nil {
		t.Fatal(err)
	}
	if logs, metrics := telemetryCounts(t, store); logs != wantLogs || metrics != wantMetrics {
		t.Errorf("after purge: %d logs, %d metrics, want %d and %d", logs, metrics, wantLogs, wantMetrics)
	}
}

// An unchanged file is recognized by size and modification time, without
// reading it to hash it
func TestUnchangedFilesAreNotHashed(t *testing.T) {
	imp, _, _, path := setupClaudeImport(t)
	imp.SetIO(&strings.Builder{}, strings.NewReader(""))
	ctx := context.Background()

	if err := imp.Import(ctx, []SourceType{SourceClaude}, Options{SkipConfirm: true}); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(path, 0644) })

	status, err := imp.state.CheckFileStatus(ctx, SourceClaude, path)
	if err != nil || status != StatusCurrent {
		t.Fatalf("CheckFileStatus = %q, %v; want current without reading the file", status, err)
	}

	// A new modification time makes the import hash the file again
	later := time.Now().Add(time.Minute)
	if err := os.Chtimes(path, later, later); err != nil {
		t.Fatal(err)
	}
	if _, err := imp.state.CheckFileStatus(ctx, SourceClaude, path); err == nil {
		t.Error("expected hashing the unreadable file to fail once its modification time changed")
	}
}

// A changed file is hashed once per run, although the preview and the import
// both check its status
func TestChangedFileIsHashedOncePerRun(t *testing.T) {
	imp, _, _, path := setupClaudeImport(t)
	imp.SetIO(&strings.Builder{}, strings.NewReader(""))
	ctx := context.Background()

	if err := imp.Import(ctx, []SourceType{SourceClaude}, Options{SkipConfirm: true}); err != nil {
		t.Fatal(err)
	}
	appendLine(t, path, secondClaudeLine)
	if status, err := imp.state.CheckFileStatus(ctx, SourceClaude, path); err != nil || status != StatusModified {
		t.Fatalf("CheckFileStatus = %q, %v; want modified", status, err)
	}
	// Unreadable now, but unchanged since it was hashed
	if err := os.Chmod(path, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(path, 0644) })
	if status, err := imp.state.CheckFileStatus(ctx, SourceClaude, path); err != nil || status != StatusModified {
		t.Errorf("second CheckFileStatus = %q, %v; want modified without hashing again", status, err)
	}
}

func TestDateArgsAreLocalDays(t *testing.T) {
	from, err := ParseDateArg("2025-03-30")
	if err != nil {
		t.Fatal(err)
	}
	to, err := ParseToDateArg("2025-03-30")
	if err != nil {
		t.Fatal(err)
	}
	if from.Location() != time.Local || from.Hour() != 0 {
		t.Errorf("from = %v, want local midnight", from)
	}
	if y, m, d := to.Date(); y != 2025 || m != 3 || d != 30 || to.Hour() != 23 || to.Minute() != 59 {
		t.Errorf("to = %v, want the end of 2025-03-30 local time", to)
	}
}
