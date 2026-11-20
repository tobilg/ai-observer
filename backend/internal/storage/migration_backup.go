package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/tobilg/ai-observer/internal/diskspace"
	"github.com/tobilg/ai-observer/internal/logger"
)

// checkDiskSpace checks the space for copies of the database (a variable
// for tests)
var checkDiskSpace = diskspace.Check

// migrationBackupSuffix names backups taken before migrations:
// <database>.pre-migration-v<schema version of the backup>
const migrationBackupSuffix = ".pre-migration-v"

// MigrationBackupPath is the backup of the database at dbPath taken before
// migrating it from schema version
func MigrationBackupPath(dbPath string, version int) string {
	return fmt.Sprintf("%s%s%d", dbPath, migrationBackupSuffix, version)
}

// backupBeforeMigration copies a database that holds data and has
// migrations pending to MigrationBackupPath before the schema runs, since
// migrations rewrite stored data in place. The copy is taken after a
// checkpoint, so the file alone is consistent, and is the file as it was:
// an earlier AI Observer version can open it again. Only the latest backup
// is kept. It fails, before copying anything, when the disk lacks the
// space. It returns the backup's path, "" when none was needed.
func (s *DuckDBStore) backupBeforeMigration(ctx context.Context, dbPath string) (string, error) {
	if dbPath == "" || dbPath == ":memory:" || strings.HasPrefix(dbPath, ":memory:") {
		return "", nil
	}
	current, err := s.storedSchemaVersion(ctx)
	if err != nil {
		return "", err
	}
	latest := LatestSchemaVersion()
	if current >= latest {
		return "", nil
	}
	hasData, err := s.holdsData(ctx)
	if err != nil || !hasData {
		return "", err
	}

	start := time.Now()
	if _, err := s.db.ExecContext(ctx, "CHECKPOINT"); err != nil {
		return "", fmt.Errorf("checkpointing the database before backing it up: %w", err)
	}
	backup := MigrationBackupPath(dbPath, current)
	size := diskspace.FileSize(dbPath) + diskspace.FileSize(dbPath+".wal")
	if err := checkDiskSpace(filepath.Dir(backup), uint64(size)+16<<20, "the backup before migrating the database"); err != nil {
		return "", fmt.Errorf("%w; free disk space, or set AI_OBSERVER_SKIP_MIGRATION_BACKUP=true to migrate without a backup", err)
	}

	how := "file copy"
	if err := copyDatabaseFiles(dbPath, backup); err != nil {
		// The file could not be read while open (Windows locks it): let
		// DuckDB write the copy instead
		logger.Warn("Copying the database file failed, writing the backup with DuckDB instead", "error", err)
		stage, stageErr := os.MkdirTemp(filepath.Dir(backup), ".ai-observer-migration-*")
		if stageErr != nil {
			return "", stageErr
		}
		defer os.RemoveAll(stage)
		staged := filepath.Join(stage, "backup.duckdb")
		if _, copyErr := s.CopyDatabase(ctx, staged); copyErr != nil {
			return "", fmt.Errorf("backing up the database before migrating it: %w", errors.Join(err, copyErr))
		}
		if err := os.Rename(staged, backup); err != nil {
			return "", err
		}
		if err := os.Remove(backup + ".wal"); err != nil && !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		how = "DuckDB copy"
	}
	removeOlderMigrationBackups(dbPath, backup)

	logger.Info("Backed up the database before migrating it",
		"backup", backup, "schema_version", current, "migrating_to", latest, "method", how,
		"size", diskspace.FormatBytes(size), "duration", time.Since(start).Round(time.Millisecond),
		"restore", fmt.Sprintf("to undo the upgrade, stop AI Observer, replace %s with the backup (delete %s.wal if present) and run the previous version", dbPath, dbPath),
		"skip", "AI_OBSERVER_SKIP_MIGRATION_BACKUP=true")
	return backup, nil
}

// storedSchemaVersion returns the schema version the database was last
// migrated to, 0 when it has no schema_migrations table (a new database, or
// one created before migrations existed)
func (s *DuckDBStore) storedSchemaVersion(ctx context.Context) (int, error) {
	var exists bool
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) > 0 FROM duckdb_tables()
		WHERE database_name = current_database() AND schema_name = 'main' AND table_name = 'schema_migrations'`).Scan(&exists); err != nil {
		return 0, fmt.Errorf("reading schema version: %w", err)
	}
	if !exists {
		return 0, nil
	}
	return s.SchemaVersion(ctx)
}

// holdsData reports whether the database stores telemetry, dashboards or
// import state: a new or empty database needs no backup
func (s *DuckDBStore) holdsData(ctx context.Context) (bool, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT table_name FROM duckdb_tables()
		WHERE database_name = current_database() AND schema_name = 'main'
			AND table_name IN ('otel_traces', 'otel_logs', 'otel_metrics', 'dashboards', 'import_state')`)
	if err != nil {
		return false, fmt.Errorf("listing tables: %w", err)
	}
	var tables []string
	for rows.Next() {
		var table string
		if err := rows.Scan(&table); err != nil {
			rows.Close()
			return false, fmt.Errorf("listing tables: %w", err)
		}
		tables = append(tables, table)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("listing tables: %w", err)
	}
	for _, table := range tables {
		var found bool
		if err := s.db.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM "+quoteIdent(table)+")").Scan(&found); err != nil {
			return false, fmt.Errorf("reading %s: %w", table, err)
		}
		if found {
			return true, nil
		}
	}
	return false, nil
}

// copyDatabaseFiles copies the database file and its write-ahead log, if
// any, to target (target.wal), each through a private temporary file renamed when
// complete
func copyDatabaseFiles(dbPath, target string) error {
	if err := copyFileAtomic(dbPath, target); err != nil {
		return err
	}
	fi, err := os.Stat(dbPath + ".wal")
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err == nil && fi.Size() > 0 {
		return copyFileAtomic(dbPath+".wal", target+".wal")
	}
	if err := os.Remove(target + ".wal"); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// copyFileAtomic copies src to dst, replacing dst only once the copy is
// complete and synced
func copyFileAtomic(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.CreateTemp(filepath.Dir(dst), ".ai-observer-migration-*")
	if err != nil {
		return err
	}
	partial := out.Name()
	_, err = io.Copy(out, in)
	if err == nil {
		err = out.Sync()
	}
	if closeErr := out.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(partial, dst)
	}
	if err != nil {
		os.Remove(partial)
		return fmt.Errorf("copying %s to %s: %w", src, dst, err)
	}
	return nil
}

// removeOlderMigrationBackups removes the backups of dbPath taken before
// earlier migrations, all but keep
func removeOlderMigrationBackups(dbPath, keep string) {
	dir, base := filepath.Split(dbPath)
	if dir == "" {
		dir = "."
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	prefix := base + migrationBackupSuffix
	keep = filepath.Clean(keep)
	for _, e := range entries {
		name := e.Name()
		version, ok := strings.CutPrefix(strings.TrimSuffix(name, ".wal"), prefix)
		n, err := strconv.ParseUint(version, 10, 64)
		if !ok || err != nil || strconv.FormatUint(n, 10) != version || !e.Type().IsRegular() {
			continue
		}
		path := filepath.Join(dir, name)
		if path == keep || path == keep+".wal" {
			continue
		}
		if err := os.Remove(path); err == nil {
			logger.Info("Removed an older migration backup", "backup", path)
		}
	}
}
