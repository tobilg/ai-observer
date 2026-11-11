package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tobilg/ai-observer/internal/api"
	"github.com/tobilg/ai-observer/internal/config"
	"github.com/tobilg/ai-observer/internal/exporter"
	"github.com/tobilg/ai-observer/internal/maintenance"
	"github.com/tobilg/ai-observer/internal/storage"
)

func TestHelpListsMaintenanceCommands(t *testing.T) {
	output := captureOutput(printHelp)
	for _, s := range []string{"backup", "restore", "compact", "AI_OBSERVER_MAX_DB_SIZE", "AI_OBSERVER_ADMIN_API",
		"AI_OBSERVER_BACKUP_DIR", "AI_OBSERVER_SKIP_MIGRATION_BACKUP"} {
		if !strings.Contains(output, s) {
			t.Errorf("help does not mention %q", s)
		}
	}
}

func TestParseMaintenanceFlags(t *testing.T) {
	compact, err := parseCompactFlags([]string{"--dry-run", "--keep-original", "--yes", "--db", "x.duckdb"})
	if err != nil || !compact.DryRun || !compact.KeepOriginal || !compact.Yes || compact.DB != "x.duckdb" {
		t.Errorf("compact flags = %+v, %v", compact, err)
	}
	if _, err := parseCompactFlags([]string{"now"}); err == nil {
		t.Error("compact accepted an argument")
	}

	backup, err := parseBackupFlags([]string{"--output", "b.duckdb"})
	if err != nil || backup.Output != "b.duckdb" {
		t.Errorf("backup flags = %+v, %v", backup, err)
	}
	if _, err := parseBackupFlags(nil); err == nil || !strings.Contains(err.Error(), "--output is required") {
		t.Errorf("backup without --output: %v", err)
	}

	restore, err := parseRestoreFlags([]string{"./export", "--replace", "--dry-run", "--yes"})
	if err != nil || restore.Path != "./export" || !restore.Replace || !restore.DryRun || !restore.Yes {
		t.Errorf("restore flags = %+v, %v", restore, err)
	}
	if _, err := parseRestoreFlags([]string{"--replace"}); err == nil {
		t.Error("restore without an export")
	}
}

func TestAdminCurl(t *testing.T) {
	cfg := &config.Config{Host: "0.0.0.0", APIPort: 9090}
	got := adminCurl(cfg, "delete", map[string]any{"scope": "all", "dryRun": true})
	want := `curl -X POST http://127.0.0.1:9090/api/admin/delete -H 'Content-Type: application/json' -d '{"dryRun":true,"scope":"all"}'`
	if got != want {
		t.Errorf("adminCurl =\n%s\nwant\n%s", got, want)
	}

	cfg = &config.Config{Host: "::1", APIPort: 8443, TLSCert: "c.pem", AuthToken: "secret-token"}
	got = adminCurl(cfg, "checkpoint", nil)
	if got != `curl -X POST https://[::1]:8443/api/admin/checkpoint -H "Authorization: Bearer $AI_OBSERVER_AUTH_TOKEN"` {
		t.Errorf("adminCurl with TLS and token = %s", got)
	}
	if strings.Contains(got, "secret-token") {
		t.Error("the token is printed")
	}
	// Quotes in values stay inside the shell quoting
	got = adminCurl(&config.Config{Host: "127.0.0.1", APIPort: 1}, "backup", map[string]any{"name": "it's.duckdb"})
	if !strings.Contains(got, `-d '{"name":"it'\''s.duckdb"}'`) {
		t.Errorf("quoted body: %s", got)
	}
}

func TestLockedError(t *testing.T) {
	cfg := &config.Config{Host: "127.0.0.1", APIPort: 8080, DatabasePath: "data/x.duckdb"}
	other := errors.New("disk on fire")
	if err := lockedError(other, cfg, "delete", nil, ""); err != other {
		t.Errorf("other error changed: %v", err)
	}
	locked := errors.New(`opening database: IO Error: Could not set lock on file "data/x.duckdb": Conflicting lock is held`)
	err := lockedError(locked, cfg, "delete", map[string]any{"dryRun": true}, "note")
	for _, want := range []string{"data/x.duckdb is in use", "AI_OBSERVER_ADMIN_API=true", "curl -X POST http://127.0.0.1:8080/api/admin/delete", "note"} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("locked error lacks %q: %v", want, err)
		}
	}
	if err := lockedError(maintenance.ErrLocked, cfg, "backup", nil, ""); err == nil || !strings.Contains(err.Error(), "/api/admin/backup") {
		t.Errorf("maintenance.ErrLocked: %v", err)
	}
}

// backup, compact and restore run on the database --db names
func TestMaintenanceCommands(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "source.duckdb")
	store, err := storage.NewDuckDBStore(db)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	if err := store.InsertLogs(ctx, []api.LogRecord{
		{Timestamp: now.Add(-48 * time.Hour), ServiceName: "claude-code", Body: "old", LogAttributes: map[string]string{"session.id": "s1"}},
		{Timestamp: now, ServiceName: "claude-code", Body: "new", LogAttributes: map[string]string{"session.id": "s2"}},
	}); err != nil {
		t.Fatal(err)
	}
	exportDir := filepath.Join(dir, "export")
	if _, err := exporter.NewExporter(store, false).Export(ctx, exporter.Options{Source: exporter.SourceAll, OutputDir: exportDir}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DeleteAllInRange(ctx, time.Time{}, now.Add(-time.Hour), ""); err != nil {
		t.Fatal(err)
	}
	store.Close()

	backup := filepath.Join(dir, "backup.duckdb")
	if out := captureOutput(func() {
		if err := runBackup([]string{"--db", db, "--output", backup}); err != nil {
			t.Errorf("backup: %v", err)
		}
	}); !strings.Contains(out, "Backup complete") {
		t.Errorf("backup output:\n%s", out)
	}
	if _, err := os.Stat(backup); err != nil {
		t.Errorf("backup file: %v", err)
	}
	if out := captureOutput(func() {
		if err := runCompact([]string{"--db", db, "--yes"}); err != nil {
			t.Errorf("compact: %v", err)
		}
	}); !strings.Contains(out, "Database:") {
		t.Errorf("compact output:\n%s", out)
	}

	// The export into a new database
	restored := filepath.Join(dir, "restored.duckdb")
	if out := captureOutput(func() {
		if err := runRestore([]string{exportDir, "--db", restored, "--yes"}); err != nil {
			t.Errorf("restore: %v", err)
		}
	}); !strings.Contains(out, "Restore complete") {
		t.Errorf("restore output:\n%s", out)
	}
	// Again: refused, the records are there
	captureOutput(func() {
		if err := runRestore([]string{exportDir, "--db", restored, "--yes"}); err == nil || !strings.Contains(err.Error(), "nothing was restored") {
			t.Errorf("second restore: %v", err)
		}
	})
	check, err := storage.NewDuckDBStore(restored)
	if err != nil {
		t.Fatal(err)
	}
	defer check.Close()
	if n, err := check.CountLogsInRange(ctx, now.Add(-72*time.Hour), now.Add(time.Hour), ""); err != nil || n != 2 {
		t.Errorf("restored %d logs (%v), want 2", n, err)
	}
}
