package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"maps"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tobilg/ai-observer/internal/diskspace"
)

// DatabaseSize describes the space the database takes. DuckDB stores data
// in blocks; deleted data frees blocks inside the file, which later writes
// reuse, but the file only shrinks by the free blocks at its end. Only a
// rewrite (CopyDatabase, the compact command) returns the others.
type DatabaseSize struct {
	// Path is the database file
	Path string
	// FileBytes is the size of the database file and WALBytes that of its
	// write-ahead log (0 when there is none)
	FileBytes int64
	WALBytes  int64
	// BlockSize is the size of a block; TotalBlocks = UsedBlocks + FreeBlocks
	BlockSize   int64
	TotalBlocks int64
	UsedBlocks  int64
	FreeBlocks  int64
}

// UsedBytes is the space of the blocks that hold data
func (d DatabaseSize) UsedBytes() int64 { return d.UsedBlocks * d.BlockSize }

// FreeBytes is the space of the free blocks inside the file
func (d DatabaseSize) FreeBytes() int64 { return d.FreeBlocks * d.BlockSize }

// Compaction thresholds: compacting is suggested when at least this share
// of the file and this many bytes are free blocks
const (
	compactMinFreeShare = 0.2
	compactMinFreeBytes = 64 << 20
)

// CompactionWorthwhile reports whether so much of the file is free that
// compacting it (ai-observer compact) would return a noticeable amount of
// disk space: at least 20% of it and 64 MB
func (d DatabaseSize) CompactionWorthwhile() bool {
	total := d.TotalBlocks * d.BlockSize
	return total > 0 && d.FreeBytes() >= compactMinFreeBytes &&
		float64(d.FreeBytes()) >= compactMinFreeShare*float64(total)
}

// DatabaseSize returns the space the database takes (pragma_database_size)
func (s *DuckDBStore) DatabaseSize(ctx context.Context) (DatabaseSize, error) {
	var size DatabaseSize
	var path sql.NullString
	err := s.db.QueryRowContext(ctx, `
		SELECT d.path, p.block_size, p.total_blocks, p.used_blocks, p.free_blocks
		FROM pragma_database_size() p
		JOIN duckdb_databases() d ON d.database_name = p.database_name
		WHERE p.database_name = current_database()`).Scan(
		&path, &size.BlockSize, &size.TotalBlocks, &size.UsedBlocks, &size.FreeBlocks)
	if err != nil {
		return size, fmt.Errorf("reading database size: %w", err)
	}
	size.Path = path.String
	if size.Path != "" {
		size.FileBytes = diskspace.FileSize(size.Path)
		size.WALBytes = diskspace.FileSize(size.Path + ".wal")
	}
	return size, nil
}

// Checkpoint writes the write-ahead log into the database file and frees
// the blocks of deleted rows, so that later writes reuse them instead of
// growing the file. Measured after deleting 1.6 million of 3.2 million spans
// and storing as many again: with a checkpoint right after the deletion the
// file grew from 751 MB to 605 MB (it also drops the free blocks at its
// end), without it to 806 MB.
//
// A checkpoint drops the row groups whose rows were deleted, but DuckDB
// releases their blocks for reuse only at the next checkpoint, which it
// skips while the write-ahead log is empty: a catalog no-op (an unchanged
// table comment) makes the second checkpoint run (measured: 79 used blocks
// after the first, 68 after the second). Rows that transactions older than
// the deletion may still read stay until a checkpoint after those ended;
// DuckDB checkpoints by itself when the write-ahead log reaches 16 MB.
func (s *DuckDBStore) Checkpoint(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, stmt := range []string{"CHECKPOINT", "COMMENT ON TABLE metric_names IS NULL", "CHECKPOINT"} {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("checkpointing database: %w", err)
		}
	}
	return nil
}

// LatestSchemaVersion is the schema version this build migrates databases to
func LatestSchemaVersion() int {
	latest := 0
	for _, m := range migrations {
		latest = max(latest, m.version)
	}
	return latest
}

// SchemaVersion returns the version of the last migration applied to the
// database
func (s *DuckDBStore) SchemaVersion(ctx context.Context) (int, error) {
	var version int
	if err := s.db.QueryRowContext(ctx, "SELECT COALESCE(MAX(version), 0) FROM schema_migrations").Scan(&version); err != nil {
		return 0, fmt.Errorf("reading schema version: %w", err)
	}
	return version, nil
}

// IsLocked reports whether err is DuckDB's refusal to open a database that
// another process has open (only one process may open a database file)
func IsLocked(err error) bool {
	return err != nil && strings.Contains(err.Error(), "Could not set lock")
}

var byteSizePattern = regexp.MustCompile(`^([0-9]+(?:\.[0-9]+)?)\s*([KMGT]i?B)$`)

var byteSizeUnits = map[string]float64{
	"KB": 1 << 10, "MB": 1 << 20, "GB": 1 << 30, "TB": 1 << 40,
	"KiB": 1 << 10, "MiB": 1 << 20, "GiB": 1 << 30, "TiB": 1 << 40,
}

// ParseByteSize parses a size such as "20GB" or "512 MiB". Both spellings
// are powers of 1024, like the sizes the dashboard and the logs show, so a
// limit of 20GB shows as 20.0 GB.
func ParseByteSize(s string) (int64, error) {
	m := byteSizePattern.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return 0, fmt.Errorf("invalid size %q: use a number and a unit, such as 20GB or 512MiB", s)
	}
	n, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 0, fmt.Errorf("invalid size %q: %w", s, err)
	}
	bytes := n * byteSizeUnits[m[2]]
	if bytes < 1 || bytes > math.MaxInt64/2 {
		return 0, fmt.Errorf("invalid size %q: out of range", s)
	}
	return int64(bytes), nil
}

// CopyResult describes a copy of the database (CopyDatabase)
type CopyResult struct {
	// Path is the copy's file and Bytes its size
	Path  string
	Bytes int64
	// Tables counts the rows of each table, equal in the copy and in the
	// database as of the start of the copy
	Tables map[string]int64
	// SchemaVersion is the copy's schema version (schema_migrations)
	SchemaVersion int
	Duration      time.Duration
}

// copyMu serializes copies, which attach their target to the database
// instance
var copyMu sync.Mutex

// copySeq names the attached copy targets
var copySeq atomic.Uint64

// CopySpaceNeeded estimates the disk space a copy of a database of this size
// needs: its used blocks (the copy has no free ones; measured: a 751 MB file
// with 583 MB of used blocks became 575 MB) and a quarter more for the
// write-ahead log while it is written
func CopySpaceNeeded(size DatabaseSize) uint64 {
	used := uint64(max(size.UsedBytes(), 0)) + uint64(max(size.WALBytes, 0))
	return used + used/4 + 16<<20
}

// CopyDatabase writes a copy of the database to a new DuckDB file at target
// (COPY FROM DATABASE): tables with their constraints and indexes, without
// free blocks. It is consistent as of the start of the copy, which runs in
// one transaction, so it can run while telemetry is stored. The copy is
// checked against the database before it is kept: the same tables with the
// same definitions and row counts, the same indexes and schema version. It
// is built in a private staging directory and published when complete, so target
// never holds a partial copy. target must not exist; the copy fails before
// it starts when the disk lacks the space (CopySpaceNeeded).
func (s *DuckDBStore) CopyDatabase(ctx context.Context, target string) (*CopyResult, error) {
	copyMu.Lock()
	defer copyMu.Unlock()
	start := time.Now()

	target, err := filepath.Abs(target)
	if err != nil {
		return nil, fmt.Errorf("resolving %s: %w", target, err)
	}
	for _, path := range []string{target, target + ".wal"} {
		if _, err := os.Lstat(path); err == nil {
			return nil, fmt.Errorf("%s already exists", path)
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	}
	size, err := s.DatabaseSize(ctx)
	if err != nil {
		return nil, err
	}
	if size.Path != "" && sameFile(size.Path, target) {
		return nil, fmt.Errorf("%s is the database itself", target)
	}
	dir := filepath.Dir(target)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("creating %s: %w", dir, err)
	}
	if err := checkDiskSpace(dir, CopySpaceNeeded(size), "the copy of the database"); err != nil {
		return nil, err
	}

	stage, err := os.MkdirTemp(dir, ".ai-observer-backup-*")
	if err != nil {
		return nil, fmt.Errorf("creating backup staging directory: %w", err)
	}
	defer os.RemoveAll(stage)
	partial := filepath.Join(stage, "copy.duckdb")
	result, err := s.copyInto(ctx, partial)
	if err != nil {
		return nil, err
	}
	if _, err := os.Lstat(partial + ".wal"); !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("cannot publish a database copy with an outstanding write-ahead log")
	}
	if err := os.Chmod(partial, 0o600); err != nil {
		return nil, err
	}
	if err := publishDatabaseCopy(partial, target); err != nil {
		return nil, fmt.Errorf("publishing the copy at %s: %w", target, err)
	}
	result.Path = target
	result.Bytes = diskspace.FileSize(target)
	result.Duration = time.Since(start)
	return result, nil
}

// publishDatabaseCopy links the finished file under a new name atomically.
// On filesystems without hard links, reserve the name before renaming.
func publishDatabaseCopy(staged, target string) error {
	if err := os.Link(staged, target); err == nil || errors.Is(err, os.ErrExist) {
		return err
	}
	f, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if err = f.Close(); err == nil {
		err = os.Rename(staged, target)
	}
	if err != nil {
		os.Remove(target)
	}
	return err
}

// copyInto copies the database into a new database file at path, attached
// for the copy and detached afterwards
func (s *DuckDBStore) copyInto(ctx context.Context, path string) (*CopyResult, error) {
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquiring connection: %w", err)
	}
	defer conn.Close()

	var source string
	if err := conn.QueryRowContext(ctx, "SELECT current_database()").Scan(&source); err != nil {
		return nil, fmt.Errorf("reading database name: %w", err)
	}
	alias := fmt.Sprintf("ai_observer_copy_%d", copySeq.Add(1))
	if _, err := conn.ExecContext(ctx, "ATTACH "+sqlLiteral(path)+" AS "+quoteIdent(alias)); err != nil {
		return nil, fmt.Errorf("creating %s: %w", path, err)
	}
	attached := true
	detach := func() error {
		if !attached {
			return nil
		}
		attached = false
		// Also after a cancelled copy: the target must not stay attached
		_, err := conn.ExecContext(context.Background(), "DETACH "+quoteIdent(alias))
		return err
	}
	defer detach()

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("beginning copy: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "COPY FROM DATABASE "+quoteIdent(source)+" TO "+quoteIdent(alias)); err != nil {
		return nil, fmt.Errorf("copying the database: %w", err)
	}
	// Read in the copy's transaction, so the database is compared as of the
	// snapshot that was copied
	want, err := describeDatabase(ctx, tx, source)
	if err != nil {
		return nil, err
	}
	got, err := describeDatabase(ctx, tx, alias)
	if err != nil {
		return nil, err
	}
	if err := want.compare(got); err != nil {
		return nil, fmt.Errorf("the copy differs from the database: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("writing the copy: %w", err)
	}
	if _, err := conn.ExecContext(ctx, "CHECKPOINT "+quoteIdent(alias)); err != nil {
		return nil, fmt.Errorf("checkpointing the copy: %w", err)
	}
	if err := detach(); err != nil {
		return nil, fmt.Errorf("closing the copy: %w", err)
	}
	return &CopyResult{Tables: want.rows, SchemaVersion: want.schemaVersion}, nil
}

// databaseDescription is what a copy must keep of a database
type databaseDescription struct {
	// tables maps table names to their definitions, rows to their row counts
	tables map[string]string
	rows   map[string]int64
	// indexes maps index names to their definitions
	indexes       map[string]string
	schemaVersion int
}

// describeDatabase reads the tables, indexes, row counts and schema version
// of the attached database name
func describeDatabase(ctx context.Context, q queryer, name string) (*databaseDescription, error) {
	d := &databaseDescription{tables: map[string]string{}, rows: map[string]int64{}, indexes: map[string]string{}}
	if err := readPairs(ctx, q, `SELECT table_name, sql FROM duckdb_tables()
		WHERE database_name = ? AND schema_name = 'main' AND NOT temporary`, name, d.tables); err != nil {
		return nil, fmt.Errorf("listing tables of %s: %w", name, err)
	}
	if err := readPairs(ctx, q, `SELECT index_name, sql FROM duckdb_indexes()
		WHERE database_name = ? AND schema_name = 'main'`, name, d.indexes); err != nil {
		return nil, fmt.Errorf("listing indexes of %s: %w", name, err)
	}
	for table := range d.tables {
		var n int64
		if err := q.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+quoteIdent(name)+".main."+quoteIdent(table)).Scan(&n); err != nil {
			return nil, fmt.Errorf("counting rows of %s.%s: %w", name, table, err)
		}
		d.rows[table] = n
	}
	if _, ok := d.tables["schema_migrations"]; ok {
		if err := q.QueryRowContext(ctx, "SELECT COALESCE(MAX(version), 0) FROM "+quoteIdent(name)+".main.schema_migrations").Scan(&d.schemaVersion); err != nil {
			return nil, fmt.Errorf("reading schema version of %s: %w", name, err)
		}
	}
	return d, nil
}

// compare returns what other lacks or holds differently
func (d *databaseDescription) compare(other *databaseDescription) error {
	var problems []string
	for _, table := range slices.Sorted(maps.Keys(d.tables)) {
		switch def, ok := other.tables[table]; {
		case !ok:
			problems = append(problems, "table "+table+" is missing")
		case def != d.tables[table]:
			problems = append(problems, "table "+table+" is defined differently")
		case other.rows[table] != d.rows[table]:
			problems = append(problems, fmt.Sprintf("table %s has %d rows instead of %d", table, other.rows[table], d.rows[table]))
		}
	}
	for _, index := range slices.Sorted(maps.Keys(d.indexes)) {
		if def, ok := other.indexes[index]; !ok {
			problems = append(problems, "index "+index+" is missing")
		} else if def != d.indexes[index] {
			problems = append(problems, "index "+index+" is defined differently")
		}
	}
	if other.schemaVersion != d.schemaVersion {
		problems = append(problems, fmt.Sprintf("schema version %d instead of %d", other.schemaVersion, d.schemaVersion))
	}
	if len(problems) > 0 {
		return errors.New(strings.Join(problems, "; "))
	}
	return nil
}

type queryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// readPairs reads the two string columns of query's rows into m
func readPairs(ctx context.Context, q queryer, query, arg string, m map[string]string) error {
	rows, err := q.QueryContext(ctx, query, arg)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var key string
		var value sql.NullString
		if err := rows.Scan(&key, &value); err != nil {
			return err
		}
		m[key] = value.String
	}
	return rows.Err()
}

// sameFile reports whether a and b name the same file (a must exist)
func sameFile(a, b string) bool {
	fa, err := os.Stat(a)
	if err != nil {
		return false
	}
	fb, err := os.Stat(b)
	if err != nil {
		return filepath.Clean(a) == filepath.Clean(b)
	}
	return os.SameFile(fa, fb)
}

// sqlLiteral quotes s as a SQL string literal, for statements that take no
// parameters (ATTACH, COPY)
func sqlLiteral(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// quoteIdent quotes s as a SQL identifier
func quoteIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

// GrowthPerDay estimates how much the database grows per day: its used
// bytes over the days between the oldest and newest record, 0 while those
// are less than a day apart
func GrowthPerDay(usedBytes int64, oldest, newest time.Time) int64 {
	span := newest.Sub(oldest)
	if oldest.IsZero() || newest.IsZero() || span < 24*time.Hour {
		return 0
	}
	return int64(float64(usedBytes) / span.Hours() * 24)
}
