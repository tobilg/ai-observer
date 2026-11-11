package storage

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tobilg/ai-observer/internal/api"
)

// fileStore opens a database file in a temporary directory
func fileStore(t *testing.T, name string) (*DuckDBStore, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	store, err := NewDuckDBStore(path)
	if err != nil {
		t.Fatalf("opening %s: %v", path, err)
	}
	t.Cleanup(func() { store.Close() })
	return store, path
}

// fillStore stores n spans, logs and metric points per hour for hours hours
// ending at end, and a dashboard
func fillStore(t *testing.T, store *DuckDBStore, end time.Time, hours, n int) {
	t.Helper()
	ctx := context.Background()
	body := strings.Repeat("payload ", 40)
	for h := 0; h < hours; h++ {
		at := end.Add(-time.Duration(h) * time.Hour)
		spans := make([]api.Span, n)
		logs := make([]api.LogRecord, n)
		metrics := make([]api.MetricDataPoint, n)
		for i := 0; i < n; i++ {
			ts := at.Add(-time.Duration(i) * time.Millisecond)
			spans[i] = api.Span{
				TraceID: fmt.Sprintf("trace-%d-%d", h, i/10), SpanID: fmt.Sprintf("span-%d-%d", h, i),
				ServiceName: "claude-code", SpanName: "op", Timestamp: ts, Duration: 1000,
				SpanAttributes: map[string]string{"session.id": fmt.Sprintf("s-%d", h), "text": body},
			}
			logs[i] = api.LogRecord{
				Timestamp: ts, ServiceName: "claude-code", SeverityText: "INFO", Body: body,
				LogAttributes: map[string]string{"session.id": fmt.Sprintf("s-%d", h), "event.name": "user_prompt"},
			}
			value := float64(h*n + i)
			temporality := int32(2)
			metrics[i] = api.MetricDataPoint{
				Timestamp: ts, ServiceName: "claude-code", MetricName: "claude_code.token.usage", MetricType: "sum",
				Value: &value, AggregationTemporality: &temporality,
				Attributes: map[string]string{"type": "input", "session.id": fmt.Sprintf("s-%d", h)},
			}
		}
		if err := store.InsertSpans(ctx, spans); err != nil {
			t.Fatalf("InsertSpans: %v", err)
		}
		if err := store.InsertLogs(ctx, logs); err != nil {
			t.Fatalf("InsertLogs: %v", err)
		}
		if err := store.InsertMetrics(ctx, metrics); err != nil {
			t.Fatalf("InsertMetrics: %v", err)
		}
	}
	if _, err := store.CreateDashboard(ctx, &api.CreateDashboardRequest{Name: "Mine"}); err != nil {
		t.Fatalf("CreateDashboard: %v", err)
	}
}

func TestDatabaseSizeShowsBlocksFreedByDeletion(t *testing.T) {
	store, path := fileStore(t, "size.duckdb")
	ctx := context.Background()
	end := time.Now().UTC().Truncate(time.Hour)
	fillStore(t, store, end, 48, 2000)
	if err := store.Checkpoint(ctx); err != nil {
		t.Fatalf("Checkpoint: %v", err)
	}
	before, err := store.DatabaseSize(ctx)
	if err != nil {
		t.Fatalf("DatabaseSize: %v", err)
	}
	if resolved, _ := filepath.EvalSymlinks(path); before.Path != resolved || before.FileBytes == 0 || before.BlockSize == 0 || before.UsedBlocks == 0 {
		t.Fatalf("size before = %+v", before)
	}
	if before.TotalBlocks != before.UsedBlocks+before.FreeBlocks {
		t.Errorf("total %d != used %d + free %d", before.TotalBlocks, before.UsedBlocks, before.FreeBlocks)
	}

	if _, err := store.DeleteAllInRange(ctx, time.Time{}, end.Add(-24*time.Hour), ""); err != nil {
		t.Fatalf("DeleteAllInRange: %v", err)
	}
	if err := store.Checkpoint(ctx); err != nil {
		t.Fatalf("Checkpoint: %v", err)
	}
	after, err := store.DatabaseSize(ctx)
	if err != nil {
		t.Fatalf("DatabaseSize: %v", err)
	}
	if after.UsedBlocks >= before.UsedBlocks {
		t.Errorf("used blocks %d after deleting half the data, %d before", after.UsedBlocks, before.UsedBlocks)
	}
	if after.WALBytes != 0 {
		t.Errorf("write-ahead log of %d bytes after a checkpoint", after.WALBytes)
	}
}

func TestDatabaseSizeInMemory(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	size, err := store.DatabaseSize(context.Background())
	if err != nil {
		t.Fatalf("DatabaseSize: %v", err)
	}
	if size.FileBytes != 0 || size.WALBytes != 0 {
		t.Errorf("in-memory database has file sizes: %+v", size)
	}
}

func TestCompactionWorthwhile(t *testing.T) {
	const block = 256 << 10
	for _, tc := range []struct {
		used, free int64
		want       bool
	}{
		{used: 2225, free: 641, want: true}, // the 751 MB database: 22% free
		{used: 2225, free: 100, want: false},
		{used: 10, free: 10, want: false}, // half free, but only 2.5 MB
		{used: 0, free: 0, want: false},
	} {
		size := DatabaseSize{BlockSize: block, UsedBlocks: tc.used, FreeBlocks: tc.free, TotalBlocks: tc.used + tc.free}
		if got := size.CompactionWorthwhile(); got != tc.want {
			t.Errorf("used %d free %d: CompactionWorthwhile = %v, want %v", tc.used, tc.free, got, tc.want)
		}
	}
}

func TestCopyDatabaseKeepsTablesIndexesAndRows(t *testing.T) {
	store, _ := fileStore(t, "source.duckdb")
	ctx := context.Background()
	end := time.Now().UTC().Truncate(time.Hour)
	fillStore(t, store, end, 24, 500)
	// Free blocks for the copy to leave out
	if _, err := store.DeleteAllInRange(ctx, time.Time{}, end.Add(-12*time.Hour), ""); err != nil {
		t.Fatalf("DeleteAllInRange: %v", err)
	}
	if err := store.Checkpoint(ctx); err != nil {
		t.Fatalf("Checkpoint: %v", err)
	}

	target := filepath.Join(t.TempDir(), "sub", "copy.duckdb")
	result, err := store.CopyDatabase(ctx, target)
	if err != nil {
		t.Fatalf("CopyDatabase: %v", err)
	}
	if result.Path != target || result.Bytes == 0 || result.SchemaVersion != LatestSchemaVersion() {
		t.Errorf("result = %+v", result)
	}
	if result.Tables["otel_traces"] == 0 || result.Tables["dashboards"] != 1 {
		t.Errorf("row counts = %v", result.Tables)
	}
	for _, leftover := range []string{target + ".partial", target + ".partial.wal", target + ".wal"} {
		if _, err := os.Stat(leftover); err == nil {
			t.Errorf("%s left behind", leftover)
		}
	}

	// The copy opens as a database of this version, with the same content
	copied, err := NewDuckDBStore(target)
	if err != nil {
		t.Fatalf("opening the copy: %v", err)
	}
	defer copied.Close()
	want, err := describeDatabase(ctx, store.db, "source")
	if err != nil {
		t.Fatal(err)
	}
	got, err := describeDatabase(ctx, copied.db, "copy")
	if err != nil {
		t.Fatal(err)
	}
	if err := want.compare(got); err != nil {
		t.Errorf("copy differs: %v", err)
	}
	if len(got.indexes) == 0 {
		t.Error("the copy has no indexes")
	}
	wantSessions, err := store.QuerySessions(ctx, "", end.Add(-48*time.Hour), end.Add(time.Hour), Page{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	gotSessions, err := copied.QuerySessions(ctx, "", end.Add(-48*time.Hour), end.Add(time.Hour), Page{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if gotSessions.Total != wantSessions.Total || gotSessions.Total == 0 {
		t.Errorf("copy lists %d sessions, the database %d", gotSessions.Total, wantSessions.Total)
	}
}

func TestCopyDatabaseRefusesExistingTarget(t *testing.T) {
	store, path := fileStore(t, "source.duckdb")
	ctx := context.Background()
	existing := filepath.Join(t.TempDir(), "exists.duckdb")
	if err := os.WriteFile(existing, []byte("keep me"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CopyDatabase(ctx, existing); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("copy over an existing file: %v", err)
	}
	if data, _ := os.ReadFile(existing); string(data) != "keep me" {
		t.Error("the existing file was changed")
	}
	if _, err := store.CopyDatabase(ctx, path); err == nil {
		t.Error("copy over the database itself succeeded")
	}
}

func TestCopyDatabaseCancelledLeavesNothing(t *testing.T) {
	store, _ := fileStore(t, "source.duckdb")
	fillStore(t, store, time.Now().UTC(), 2, 100)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	target := filepath.Join(t.TempDir(), "copy.duckdb")
	if _, err := store.CopyDatabase(ctx, target); err == nil {
		t.Fatal("cancelled copy succeeded")
	}
	entries, _ := os.ReadDir(filepath.Dir(target))
	if len(entries) > 0 {
		t.Errorf("cancelled copy left %v", entries)
	}
	// The database is still usable and has nothing attached
	var attached int
	if err := store.db.QueryRow("SELECT COUNT(*) FROM duckdb_databases() WHERE database_name LIKE 'ai_observer_copy_%'").Scan(&attached); err != nil {
		t.Fatal(err)
	}
	if attached != 0 {
		t.Errorf("%d copy targets still attached", attached)
	}
}

// A copy taken while telemetry is stored is consistent: it holds the rows
// stored before it started and not those of transactions still running
func TestCopyDatabaseWhileWriting(t *testing.T) {
	store, _ := fileStore(t, "source.duckdb")
	ctx := context.Background()
	fillStore(t, store, time.Now().UTC().Add(-time.Hour), 6, 500)

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			if err := store.InsertLogs(ctx, []api.LogRecord{{Timestamp: time.Now(), ServiceName: "gemini_cli", Body: fmt.Sprint(i)}}); err != nil {
				t.Errorf("InsertLogs during copy: %v", err)
				return
			}
		}
	}()
	result, err := store.CopyDatabase(ctx, filepath.Join(t.TempDir(), "copy.duckdb"))
	close(stop)
	wg.Wait()
	if err != nil {
		t.Fatalf("CopyDatabase while writing: %v", err)
	}
	copied, err := NewDuckDBStore(result.Path)
	if err != nil {
		t.Fatal(err)
	}
	defer copied.Close()
	var logs int64
	if err := copied.db.QueryRow("SELECT COUNT(*) FROM otel_logs").Scan(&logs); err != nil {
		t.Fatal(err)
	}
	if logs != result.Tables["otel_logs"] {
		t.Errorf("copy holds %d logs, result says %d", logs, result.Tables["otel_logs"])
	}
}

func TestParseByteSize(t *testing.T) {
	for in, want := range map[string]int64{
		"20GB":    20 << 30,
		"512 MiB": 512 << 20,
		"1.5GiB":  3 << 29,
		" 100MB ": 100 << 20,
		"1TB":     1 << 40,
		"64KiB":   64 << 10,
		"64KB":    64 << 10,
	} {
		got, err := ParseByteSize(in)
		if err != nil || got != want {
			t.Errorf("ParseByteSize(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
	for _, in := range []string{"", "20", "20G", "-1GB", "0GB", "1e3MB", "lots"} {
		if _, err := ParseByteSize(in); err == nil {
			t.Errorf("ParseByteSize(%q) succeeded", in)
		}
	}
}

func TestIsLocked(t *testing.T) {
	if IsLocked(nil) || IsLocked(fmt.Errorf("other")) {
		t.Error("IsLocked of other errors")
	}
	// DuckDB's error when another process has the database open
	if !IsLocked(fmt.Errorf("opening database: IO Error: Could not set lock on file \"/data/x.duckdb\": Conflicting lock is held")) {
		t.Error("lock error not recognized")
	}
}

func TestGrowthPerDay(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	if got := GrowthPerDay(700e6, start, start.Add(48*time.Hour)); got != 350e6 {
		t.Errorf("GrowthPerDay over two days = %d, want 350 MB", got)
	}
	if got := GrowthPerDay(700e6, start, start.Add(12*time.Hour)); got != 0 {
		t.Errorf("GrowthPerDay over half a day = %d, want 0", got)
	}
	if got := GrowthPerDay(700e6, time.Time{}, start); got != 0 {
		t.Errorf("GrowthPerDay without records = %d, want 0", got)
	}
}
