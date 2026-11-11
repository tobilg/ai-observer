package maintenance

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tobilg/ai-observer/internal/api"
	"github.com/tobilg/ai-observer/internal/diskspace"
	"github.com/tobilg/ai-observer/internal/storage"
)

// databaseWithFreeBlocks creates a database file of which half the
// telemetry was deleted, and returns its path and the remaining log count
func databaseWithFreeBlocks(t *testing.T) (string, int64) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ai-observer.duckdb")
	store, err := storage.NewDuckDBStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	end := time.Date(2025, 5, 1, 0, 0, 0, 0, time.UTC)
	body := strings.Repeat("some log text ", 30)
	for h := 0; h < 24; h++ {
		var logs []api.LogRecord
		var spans []api.Span
		for i := 0; i < 1000; i++ {
			at := end.Add(-time.Duration(h)*time.Hour - time.Duration(i)*time.Millisecond)
			logs = append(logs, api.LogRecord{Timestamp: at, ServiceName: "claude-code", Body: body,
				LogAttributes: map[string]string{"session.id": fmt.Sprint(h)}})
			spans = append(spans, api.Span{Timestamp: at, TraceID: fmt.Sprintf("t%d", h), SpanID: fmt.Sprintf("s%d-%d", h, i),
				SpanName: "op", ServiceName: "claude-code", SpanAttributes: map[string]string{"text": body}})
		}
		if err := store.InsertLogs(ctx, logs); err != nil {
			t.Fatal(err)
		}
		if err := store.InsertSpans(ctx, spans); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.CreateDashboard(ctx, &api.CreateDashboardRequest{Name: "kept"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DeleteAllInRange(ctx, time.Time{}, end.Add(-12*time.Hour), ""); err != nil {
		t.Fatal(err)
	}
	if err := store.Checkpoint(ctx); err != nil {
		t.Fatal(err)
	}
	var logs int64
	if err := store.DB().QueryRow("SELECT COUNT(*) FROM otel_logs").Scan(&logs); err != nil {
		t.Fatal(err)
	}
	return path, logs
}

func countLogs(t *testing.T, path string) int64 {
	t.Helper()
	store, err := storage.NewDuckDBStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var logs int64
	if err := store.DB().QueryRow("SELECT COUNT(*) FROM otel_logs").Scan(&logs); err != nil {
		t.Fatal(err)
	}
	return logs
}

func TestCompactShrinksTheDatabase(t *testing.T) {
	path, logs := databaseWithFreeBlocks(t)
	before := diskspace.FileSize(path)
	var out bytes.Buffer
	result, err := Compact(context.Background(), CompactOptions{DatabasePath: path, SkipConfirm: true, Out: &out})
	if err != nil {
		t.Fatalf("Compact: %v\n%s", err, out.String())
	}
	if result.After >= result.Before || diskspace.FileSize(path) >= before {
		t.Errorf("compaction did not shrink the database: %d -> %d bytes", result.Before, result.After)
	}
	if !strings.Contains(out.String(), "Compaction complete") {
		t.Errorf("output:\n%s", out.String())
	}
	for _, leftover := range []string{path + originalSuffix, path + compactSuffix, path + compactSuffix + ".partial", path + ".wal"} {
		if _, err := os.Stat(leftover); err == nil {
			t.Errorf("%s left behind", leftover)
		}
	}
	if got := countLogs(t, path); got != logs {
		t.Errorf("compacted database holds %d logs, want %d", got, logs)
	}
}

func TestCompactKeepsOriginalOnRequest(t *testing.T) {
	path, logs := databaseWithFreeBlocks(t)
	result, err := Compact(context.Background(), CompactOptions{DatabasePath: path, SkipConfirm: true, KeepOriginal: true, Out: &bytes.Buffer{}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Original != path+originalSuffix {
		t.Errorf("original kept at %q", result.Original)
	}
	if got := countLogs(t, result.Original); got != logs {
		t.Errorf("original holds %d logs, want %d", got, logs)
	}
}

func TestCompactDryRunAndDecline(t *testing.T) {
	path, _ := databaseWithFreeBlocks(t)
	before := diskspace.FileSize(path)
	var out bytes.Buffer
	if _, err := Compact(context.Background(), CompactOptions{DatabasePath: path, DryRun: true, Out: &out}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "free blocks") || !strings.Contains(out.String(), "Dry run") {
		t.Errorf("dry run output:\n%s", out.String())
	}
	out.Reset()
	if _, err := Compact(context.Background(), CompactOptions{DatabasePath: path, Out: &out, In: strings.NewReader("no\n")}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Aborted.") {
		t.Errorf("declined output:\n%s", out.String())
	}
	if diskspace.FileSize(path) != before {
		t.Error("the database changed")
	}
}

func TestCompactRefusesLeftovers(t *testing.T) {
	path, _ := databaseWithFreeBlocks(t)
	if err := os.WriteFile(path+originalSuffix, []byte("earlier"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Compact(context.Background(), CompactOptions{DatabasePath: path, SkipConfirm: true, Out: &bytes.Buffer{}})
	if err == nil || !strings.Contains(err.Error(), "interrupted compaction") {
		t.Fatalf("Compact with a leftover original: %v", err)
	}
	if data, _ := os.ReadFile(path + originalSuffix); string(data) != "earlier" {
		t.Error("the leftover was overwritten")
	}
}

func TestCompactMissingDatabase(t *testing.T) {
	_, err := Compact(context.Background(), CompactOptions{DatabasePath: filepath.Join(t.TempDir(), "none.duckdb"), Out: &bytes.Buffer{}})
	if err == nil || !strings.Contains(err.Error(), "no database") {
		t.Errorf("Compact of a missing database: %v", err)
	}
}

func TestBackupWritesACopy(t *testing.T) {
	path, logs := databaseWithFreeBlocks(t)
	output := filepath.Join(t.TempDir(), "backups", "copy.duckdb")
	var out bytes.Buffer
	result, err := Backup(context.Background(), BackupOptions{DatabasePath: path, Output: output, Out: &out})
	if err != nil {
		t.Fatalf("Backup: %v\n%s", err, out.String())
	}
	if result.Path != output || result.Tables["otel_logs"] != logs || result.Tables["dashboards"] != 1 {
		t.Errorf("result = %+v", result)
	}
	if got := countLogs(t, output); got != logs {
		t.Errorf("backup holds %d logs, want %d", got, logs)
	}
	if !strings.Contains(out.String(), "Backup complete") {
		t.Errorf("output:\n%s", out.String())
	}
	if _, err := Backup(context.Background(), BackupOptions{DatabasePath: path, Output: output, Out: &bytes.Buffer{}}); err == nil {
		t.Error("backup over an existing file succeeded")
	}
}
