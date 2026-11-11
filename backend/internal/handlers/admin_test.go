package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tobilg/ai-observer/internal/api"
	"github.com/tobilg/ai-observer/internal/exporter"
	"github.com/tobilg/ai-observer/internal/storage"
)

var adminBase = time.Date(2025, 4, 1, 12, 0, 0, 0, time.UTC)

func setupAdmin(t *testing.T) (*Admin, *storage.DuckDBStore, string) {
	t.Helper()
	dir := t.TempDir()
	store, err := storage.NewDuckDBStore(filepath.Join(dir, "db.duckdb"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	ctx := context.Background()
	var logs []api.LogRecord
	var spans []api.Span
	for h := 0; h < 72; h++ {
		at := adminBase.Add(time.Duration(h) * time.Hour)
		logs = append(logs, api.LogRecord{Timestamp: at, ServiceName: "claude-code", Body: "x"},
			api.LogRecord{Timestamp: at, ServiceName: "codex-app-server", Body: "y"})
		spans = append(spans, api.Span{Timestamp: at, TraceID: "t", SpanID: at.String(), SpanName: "s", ServiceName: "claude-code"})
	}
	if err := store.InsertLogs(ctx, logs); err != nil {
		t.Fatal(err)
	}
	if err := store.InsertSpans(ctx, spans); err != nil {
		t.Fatal(err)
	}
	backups := filepath.Join(dir, "backups")
	a := NewAdmin(store, backups)
	a.now = func() time.Time { return time.Date(2025, 5, 6, 7, 8, 9, 0, time.UTC) }
	return a, store, backups
}

func adminPost(t *testing.T, handler http.HandlerFunc, body string, v any) (int, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/admin/x", strings.NewReader(body))
	if body == "" {
		req = httptest.NewRequest(http.MethodPost, "/api/admin/x", http.NoBody)
	}
	rec := httptest.NewRecorder()
	handler(rec, req)
	if rec.Code == http.StatusOK && v != nil {
		if err := json.Unmarshal(rec.Body.Bytes(), v); err != nil {
			t.Fatalf("decoding %s: %v", rec.Body.String(), err)
		}
	}
	return rec.Code, rec.Body.String()
}

func adminCountLogs(t *testing.T, store *storage.DuckDBStore) int64 {
	t.Helper()
	n, err := store.CountLogsInRange(context.Background(), adminBase.Add(-time.Hour), adminBase.Add(100*time.Hour), "")
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func TestAdminDeleteDryRunAndDelete(t *testing.T) {
	a, store, _ := setupAdmin(t)
	body := `{"from": "2025-04-01T12:00:00Z", "to": "2025-04-03T11:59:59Z", "service": "codex", "dryRun": true}`
	var dry api.AdminDeleteResponse
	if code, msg := adminPost(t, a.Delete, body, &dry); code != http.StatusOK {
		t.Fatalf("dry run: %d %s", code, msg)
	}
	if !dry.DryRun || dry.Logs != 48 || dry.Spans != 0 || dry.Scope != "all" || dry.Service != "codex" || dry.Database != nil {
		t.Errorf("dry run = %+v", dry)
	}
	if adminCountLogs(t, store) != 144 {
		t.Fatal("dry run deleted logs")
	}

	var resp api.AdminDeleteResponse
	body = strings.Replace(body, `"dryRun": true`, `"dryRun": false`, 1)
	if code, msg := adminPost(t, a.Delete, body, &resp); code != http.StatusOK {
		t.Fatalf("delete: %d %s", code, msg)
	}
	if resp.DryRun || resp.Logs != 48 || resp.Database == nil || resp.Database.SizeBytes == 0 || resp.Database.WALSizeBytes != 0 {
		t.Errorf("delete = %+v (%+v)", resp, resp.Database)
	}
	if adminCountLogs(t, store) != 96 {
		t.Errorf("%d logs left, want 96", adminCountLogs(t, store))
	}
}

func TestAdminDeleteValidates(t *testing.T) {
	a, _, _ := setupAdmin(t)
	for body, want := range map[string]string{
		`{}`: "from and to are required",
		`{"from": "2025-04-03T00:00:00Z", "to": "2025-04-01T00:00:00Z"}`:                   "from must not be after to",
		`{"from": "2025-04-01T00:00:00Z", "to": "2025-04-02T00:00:00Z", "scope": "spans"}`: "invalid scope",
		`{"from": "2025-04-01T00:00:00Z", "to": "2025-04-02T00:00:00Z", "service": "vim"}`: "unknown tool or service",
		`{"from": "yesterday"}`: "invalid request body",
	} {
		code, msg := adminPost(t, a.Delete, body, nil)
		if code != http.StatusBadRequest || !strings.Contains(msg, want) {
			t.Errorf("%s: %d %s, want 400 %q", body, code, msg, want)
		}
	}
}

func TestAdminCheckpoint(t *testing.T) {
	a, _, _ := setupAdmin(t)
	var resp api.AdminCheckpointResponse
	if code, msg := adminPost(t, a.Checkpoint, "", &resp); code != http.StatusOK {
		t.Fatalf("checkpoint: %d %s", code, msg)
	}
	if resp.Database.SizeBytes == 0 || resp.Database.UsedBytes == 0 || resp.Database.WALSizeBytes != 0 {
		t.Errorf("checkpoint = %+v", resp)
	}
}

func TestAdminBackup(t *testing.T) {
	a, _, backups := setupAdmin(t)
	var resp api.AdminBackupResponse
	if code, msg := adminPost(t, a.Backup, "", &resp); code != http.StatusOK {
		t.Fatalf("backup: %d %s", code, msg)
	}
	want := filepath.Join(backups, "ai-observer-backup-20250506-070809.duckdb")
	if resolved, _ := filepath.Abs(want); resp.Path != resolved || resp.SizeBytes == 0 || resp.Tables["otel_logs"] != 144 || resp.SchemaVersion != storage.LatestSchemaVersion() {
		t.Errorf("backup = %+v", resp)
	}
	if _, err := os.Stat(resp.Path); err != nil {
		t.Errorf("backup file: %v", err)
	}
	// The same name again
	if code, msg := adminPost(t, a.Backup, "", nil); code != http.StatusConflict {
		t.Errorf("second backup with the same name: %d %s", code, msg)
	}
	if code, msg := adminPost(t, a.Backup, `{"name": "mine.duckdb"}`, &resp); code != http.StatusOK || filepath.Base(resp.Path) != "mine.duckdb" {
		t.Errorf("named backup: %d %s", code, msg)
	}
}

// Requests name files in the backup directory only
func TestAdminRejectsPaths(t *testing.T) {
	a, _, backups := setupAdmin(t)
	outside := filepath.Join(filepath.Dir(backups), "outside.duckdb")
	for _, name := range []string{"../outside.duckdb", outside, "a/b.duckdb", `a\b.duckdb`, ".hidden", "x..duckdb", "", " spaced", strings.Repeat("a", 200)} {
		if name == "" {
			continue // the default name
		}
		body, _ := json.Marshal(api.AdminBackupRequest{Name: name})
		if code, _ := adminPost(t, a.Backup, string(body), nil); code != http.StatusBadRequest {
			t.Errorf("backup named %q: %d, want 400", name, code)
		}
		body, _ = json.Marshal(api.AdminExportRequest{Name: name})
		if code, _ := adminPost(t, a.Export, string(body), nil); code != http.StatusBadRequest {
			t.Errorf("export named %q: %d, want 400", name, code)
		}
	}
	if _, err := os.Stat(outside); err == nil {
		t.Error("a file was written outside the backup directory")
	}
}

func TestAdminExport(t *testing.T) {
	a, _, backups := setupAdmin(t)
	var resp api.AdminExportResponse
	if code, msg := adminPost(t, a.Export, `{"source": "codex", "name": "codex-export"}`, &resp); code != http.StatusOK {
		t.Fatalf("export: %d %s", code, msg)
	}
	if resp.Logs != 72 || resp.Traces != 0 || filepath.Base(resp.Path) != "codex-export" || len(resp.Files) < 6 {
		t.Errorf("export = %+v", resp)
	}
	if _, err := os.Stat(filepath.Join(backups, "codex-export", "manifest.json")); err != nil {
		t.Errorf("manifest: %v", err)
	}
	if code, msg := adminPost(t, a.Export, `{"source": "vim"}`, nil); code != http.StatusBadRequest {
		t.Errorf("unknown source: %d %s", code, msg)
	}
	if code, msg := adminPost(t, a.Export, `{"zip": true}`, &resp); code != http.StatusOK || len(resp.Files) != 1 || !strings.HasSuffix(resp.Files[0], ".zip") {
		t.Errorf("zip export: %d %s %+v", code, msg, resp)
	}
}

// One operation at a time
func TestAdminBusy(t *testing.T) {
	a, _, _ := setupAdmin(t)
	a.busy.Lock()
	code, msg := adminPost(t, a.Checkpoint, "", nil)
	a.busy.Unlock()
	if code != http.StatusConflict || !strings.Contains(msg, "another maintenance operation") {
		t.Errorf("checkpoint while busy: %d %s", code, msg)
	}
}

func TestAdminDisabled(t *testing.T) {
	rec := httptest.NewRecorder()
	AdminDisabled(rec, httptest.NewRequest(http.MethodPost, "/api/admin/delete", bytes.NewBufferString("{}")))
	if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "AI_OBSERVER_ADMIN_API=true") {
		t.Errorf("disabled: %d %s", rec.Code, rec.Body.String())
	}
}

func TestGetStatusReportsSpaceAndLifecycleSettings(t *testing.T) {
	a, store, backups := setupAdmin(t)
	h := New(store, nil)
	h.SetServerInfo(func() ServerInfo {
		return ServerInfo{DatabasePath: "x", MaxDBSize: 20e9, AdminAPI: true, BackupDir: backups}
	})
	// The space of data written into the file
	if err := store.Checkpoint(context.Background()); err != nil {
		t.Fatal(err)
	}
	_ = a
	var status api.StatusResponse
	if code := getJSON(t, h.GetStatus, "/api/status", nil, &status); code != http.StatusOK {
		t.Fatalf("status %d", code)
	}
	db := status.Database
	if db.UsedBytes == 0 || db.GrowthBytesPerDay == 0 {
		t.Errorf("database = %+v", db)
	}
	if status.Settings.MaxDBSizeBytes != 20e9 || !status.Settings.AdminAPIEnabled || status.Settings.BackupDir != backups {
		t.Errorf("settings = %+v", status.Settings)
	}

	h.SetServerInfo(func() ServerInfo { return ServerInfo{BackupDir: backups} })
	status = api.StatusResponse{}
	if code := getJSON(t, h.GetStatus, "/api/status", nil, &status); code != http.StatusOK {
		t.Fatalf("status %d", code)
	}
	if status.Settings.AdminAPIEnabled || status.Settings.BackupDir != "" {
		t.Errorf("backup directory shown with the admin API disabled: %+v", status.Settings)
	}
}

// An export while telemetry is stored is one snapshot of the database:
// each write stores a log, a metric point and a span read from one session
// file together with the file's import state, and every export holds the
// records of the same writes in all of its files, as its manifest counts
func TestAdminExportIsOneSnapshot(t *testing.T) {
	a, store, backups := setupAdmin(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	written := make(chan int, 1)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; ctx.Err() == nil; i++ {
			at := adminBase.Add(time.Duration(i) * time.Second)
			value := float64(i)
			err := store.InsertImportedFile(ctx,
				[]api.LogRecord{{Timestamp: at, ServiceName: "gemini_cli", Body: "b"}},
				[]api.MetricDataPoint{{Timestamp: at, ServiceName: "gemini_cli", MetricName: "m", MetricType: "gauge", Value: &value}},
				[]api.Span{{Timestamp: at, TraceID: "snapshot", SpanID: fmt.Sprint(i), SpanName: "s", ServiceName: "gemini_cli"}},
				&storage.ImportState{Source: "gemini", FilePath: fmt.Sprintf("/sessions/%d.json", i), FileHash: "h", ImportedAt: at, SourceTracked: true},
				nil)
			if err != nil {
				if ctx.Err() == nil {
					t.Errorf("write %d: %v", i, err)
				}
				return
			}
			select {
			case written <- i:
			default:
			}
		}
	}()
	<-written

	count := func(file, where string) int64 {
		t.Helper()
		var n int64
		if err := store.DB().QueryRow("SELECT COUNT(*) FROM read_parquet(?) WHERE "+where, file).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	for e := 0; e < 5; e++ {
		var resp api.AdminExportResponse
		name := fmt.Sprintf("export-%d", e)
		if code, msg := adminPost(t, a.Export, `{"name": "`+name+`"}`, &resp); code != http.StatusOK {
			t.Fatalf("export: %d %s", code, msg)
		}
		dir := filepath.Join(backups, name)
		logs := count(filepath.Join(dir, "logs.parquet"), "ServiceName = 'gemini_cli'")
		metrics := count(filepath.Join(dir, "metrics.parquet"), "ServiceName = 'gemini_cli'")
		spans := count(filepath.Join(dir, "traces.parquet"), "ServiceName = 'gemini_cli'")
		states := count(filepath.Join(dir, "import_state.parquet"), "true")
		if logs == 0 || logs != metrics || logs != spans || logs != states {
			t.Errorf("export %d is not one snapshot: %d logs, %d metric points, %d spans, %d import states of the same writes",
				e, logs, metrics, spans, states)
		}
		if m, _, err := exporter.ReadManifest(dir); err != nil || m.Rows["logs.parquet"] != resp.Logs ||
			m.Rows["logs.parquet"] != count(filepath.Join(dir, "logs.parquet"), "true") ||
			m.Rows["import_state.parquet"] != states {
			t.Errorf("manifest %+v (%v) does not count the files", m.Rows, err)
		}
	}
	cancel()
	wg.Wait()
}
