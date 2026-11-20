package storage

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tobilg/ai-observer/internal/diskspace"
)

// legacyDatabase creates a database as written before migrations existed
// (schema version 0), holding three logs and a span
func legacyDatabase(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "legacy.duckdb")
	db, err := sql.Open("duckdb", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, stmt := range []string{
		schemaLogs, schemaTraces,
		`INSERT INTO otel_logs (Timestamp, ServiceName, Body, LogAttributes) VALUES
			('2025-01-02 10:00:00', 'claude-code', 'a', '{"session.id": "s1"}'),
			('2025-01-02 10:01:00', 'claude-code', 'b', '{"session.id": "s1"}'),
			('2025-01-02 10:02:00', 'gemini_cli', 'c', '{}')`,
		`INSERT INTO otel_traces (Timestamp, TraceId, SpanId, SpanName, ServiceName, SpanAttributes) VALUES
			('2025-01-02 10:00:00', 't1', 'sp1', 'chat', 'copilot-chat', '{}')`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	return path
}

func TestMigrationBackupBeforeMigratingData(t *testing.T) {
	path := legacyDatabase(t)
	// A backup of an earlier upgrade is replaced by the new one
	older := MigrationBackupPath(path, 7)
	if err := os.WriteFile(older, []byte("old backup"), 0o644); err != nil {
		t.Fatal(err)
	}

	backup := MigrationBackupPath(path, 0)
	for _, path := range []string{older + ".wal", older + ".saved", backup + ".partial", backup + ".partial.wal"} {
		if err := os.WriteFile(path, []byte("keep"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}

	store, err := NewDuckDBStore(path)
	if err != nil {
		t.Fatalf("opening legacy database: %v", err)
	}
	version, err := store.SchemaVersion(context.Background())
	store.Close()
	if err != nil || version != LatestSchemaVersion() {
		t.Fatalf("migrated to %d (%v), want %d", version, err, LatestSchemaVersion())
	}

	checkPrivateBackup(t, backup)
	for _, path := range []string{older + ".saved", backup + ".partial", backup + ".partial.wal"} {
		if data, err := os.ReadFile(path); err != nil || string(data) != "keep" {
			t.Errorf("unrelated file %s changed: %q, %v", path, data, err)
		}
	}
	for _, leftover := range []string{older, older + ".wal", backup + ".wal"} {
		if _, err := os.Stat(leftover); err == nil {
			t.Errorf("%s left behind", leftover)
		}
	}

	// The backup is the database before the migrations: rows without the
	// columns migrations add, and no schema_migrations
	db, err := sql.Open("duckdb", backup)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var logs int
	if err := db.QueryRow("SELECT COUNT(*) FROM otel_logs").Scan(&logs); err != nil || logs != 3 {
		t.Fatalf("backup holds %d logs (%v), want 3", logs, err)
	}
	var migrated int
	if err := db.QueryRow(`SELECT COUNT(*) FROM duckdb_columns() WHERE table_name = 'otel_logs' AND column_name = 'SessionId'`).Scan(&migrated); err != nil {
		t.Fatal(err)
	}
	if migrated != 0 {
		t.Error("the backup has the columns of the migrations")
	}
	var tables int
	if err := db.QueryRow(`SELECT COUNT(*) FROM duckdb_tables() WHERE table_name = 'schema_migrations'`).Scan(&tables); err != nil || tables != 0 {
		t.Errorf("the backup has schema_migrations (%v)", err)
	}
}

func TestMigrationBackupNotNeeded(t *testing.T) {
	// A new database, and a database at the current version
	path := filepath.Join(t.TempDir(), "new.duckdb")
	for i := 0; i < 2; i++ {
		store, err := NewDuckDBStore(path)
		if err != nil {
			t.Fatal(err)
		}
		store.Close()
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	for _, e := range entries {
		if strings.Contains(e.Name(), migrationBackupSuffix) {
			t.Errorf("backup %s of a database without pending migrations", e.Name())
		}
	}

	// A database at an older version without data
	empty := filepath.Join(t.TempDir(), "empty.duckdb")
	db, err := sql.Open("duckdb", empty)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(schemaLogs); err != nil {
		t.Fatal(err)
	}
	db.Close()
	store, err := NewDuckDBStore(empty)
	if err != nil {
		t.Fatal(err)
	}
	store.Close()
	if _, err := os.Stat(MigrationBackupPath(empty, 0)); err == nil {
		t.Error("backup of a database without data")
	}
}

func TestMigrationBackupSkipped(t *testing.T) {
	path := legacyDatabase(t)
	store, err := NewDuckDBStoreWithOptions(path, Options{SkipMigrationBackup: true})
	if err != nil {
		t.Fatal(err)
	}
	store.Close()
	if _, err := os.Stat(MigrationBackupPath(path, 0)); err == nil {
		t.Error("backup taken with SkipMigrationBackup")
	}
}

// Without the space for the backup the database is neither copied nor
// migrated, and the error says how to go on
func TestMigrationBackupFailsWithoutDiskSpace(t *testing.T) {
	path := legacyDatabase(t)
	checkDiskSpace = func(dir string, needed uint64, what string) error {
		return &diskspace.Error{What: what, Dir: dir, Needed: needed, Available: 1}
	}
	defer func() { checkDiskSpace = diskspace.Check }()

	_, err := NewDuckDBStore(path)
	if err == nil || !strings.Contains(err.Error(), "not enough disk space") || !strings.Contains(err.Error(), "AI_OBSERVER_SKIP_MIGRATION_BACKUP") {
		t.Fatalf("NewDuckDBStore without disk space: %v", err)
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	for _, e := range entries {
		if strings.Contains(e.Name(), migrationBackupSuffix) {
			t.Errorf("%s written without disk space", e.Name())
		}
	}
	db, err := sql.Open("duckdb", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var tables int
	if err := db.QueryRow(`SELECT COUNT(*) FROM duckdb_tables() WHERE table_name = 'schema_migrations'`).Scan(&tables); err != nil || tables != 0 {
		t.Errorf("the database was migrated without a backup (%v)", err)
	}
}
