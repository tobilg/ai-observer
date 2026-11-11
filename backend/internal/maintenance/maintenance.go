// Package maintenance runs the commands that copy or rewrite the database
// file: backup and compact
package maintenance

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tobilg/ai-observer/internal/diskspace"
	"github.com/tobilg/ai-observer/internal/prompt"
	"github.com/tobilg/ai-observer/internal/storage"
)

// ErrLocked is returned when another process, such as a running server, has
// the database open
var ErrLocked = errors.New("the database is in use by another process")

// openStore opens the database for a maintenance command; a database in use
// by another process is ErrLocked
func openStore(path string, opts storage.Options) (*storage.DuckDBStore, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("no database at %s: %w", path, err)
	}
	store, err := storage.NewDuckDBStoreWithOptions(path, opts)
	if err != nil {
		if storage.IsLocked(err) {
			return nil, fmt.Errorf("%w: %s", ErrLocked, path)
		}
		return nil, err
	}
	return store, nil
}

// BackupOptions configures a backup
type BackupOptions struct {
	DatabasePath string
	Storage      storage.Options
	// Output is the new database file to write; it must not exist
	Output string
	Out    io.Writer
}

// Backup writes a consistent copy of the database to opts.Output (a DuckDB
// database without free blocks, storage.CopyDatabase). The database must not
// be in use by another process (ErrLocked); a running server's admin API
// writes backups instead (POST /api/admin/backup).
func Backup(ctx context.Context, opts BackupOptions) (*storage.CopyResult, error) {
	out := writerOr(opts.Out)
	if opts.Output == "" {
		return nil, errors.New("--output is required")
	}
	store, err := openStore(opts.DatabasePath, opts.Storage)
	if err != nil {
		return nil, err
	}
	defer store.Close()

	fmt.Fprintf(out, "Backing up %s to %s...\n", opts.DatabasePath, opts.Output)
	result, err := store.CopyDatabase(ctx, opts.Output)
	if err != nil {
		return nil, err
	}
	fmt.Fprintf(out, "Backup complete: %s (%s, %s)\n", result.Path, diskspace.FormatBytes(result.Bytes), result.Duration.Round(time.Millisecond))
	printTables(out, result.Tables)
	fmt.Fprintln(out)
	fmt.Fprintln(out, "To restore it, stop AI Observer and copy it over the database file")
	fmt.Fprintln(out, "(or point AI_OBSERVER_DATABASE_PATH at it).")
	return result, nil
}

// CompactOptions configures a compaction
type CompactOptions struct {
	DatabasePath string
	Storage      storage.Options
	// KeepOriginal keeps the database as it was next to the compacted one
	// (<path>.pre-compact) instead of removing it once the copy is checked
	KeepOriginal bool
	DryRun       bool
	SkipConfirm  bool
	Out          io.Writer
	In           io.Reader
}

// CompactResult describes a compaction
type CompactResult struct {
	// Before and After are the sizes of the database file and its
	// write-ahead log before and after
	Before   int64
	After    int64
	Original string // the kept original, "" when removed
	Duration time.Duration
}

// compactSuffix and originalSuffix name the compacted copy while it is
// written and the original while the copy replaces it
const (
	compactSuffix  = ".compact"
	originalSuffix = ".pre-compact"
)

// Compact rewrites the database into a new file without free blocks and
// replaces the database with it. DuckDB reuses the blocks of deleted data
// for new data but only returns those at the end of the file, so after
// deleting much (delete, retention) the file keeps its size until it is
// rewritten. The original is kept until the copy has been checked (tables,
// row counts, indexes and schema version, once in the copy and once more
// after it replaced the database). The database must not be in use by
// another process (ErrLocked).
func Compact(ctx context.Context, opts CompactOptions) (*CompactResult, error) {
	out := writerOr(opts.Out)
	start := time.Now()
	path, err := filepath.Abs(opts.DatabasePath)
	if err != nil {
		return nil, err
	}
	store, err := openStore(path, opts.Storage)
	if err != nil {
		return nil, err
	}
	closed := false
	closeStore := func() error {
		if closed {
			return nil
		}
		closed = true
		return store.Close()
	}
	defer closeStore()

	if err := store.Checkpoint(ctx); err != nil {
		return nil, err
	}
	size, err := store.DatabaseSize(ctx)
	if err != nil {
		return nil, err
	}
	before := size.FileBytes + size.WALBytes
	needed := storage.CopySpaceNeeded(size)
	fmt.Fprintf(out, "Database: %s\n", path)
	fmt.Fprintf(out, "Size: %s, of which %s free blocks (%d of %d)\n",
		diskspace.FormatBytes(before), diskspace.FormatBytes(size.FreeBytes()), size.FreeBlocks, size.TotalBlocks)
	fmt.Fprintf(out, "Expected size after compaction: about %s\n", diskspace.FormatBytes(size.UsedBytes()))
	avail, err := diskspace.Available(filepath.Dir(path))
	if err == nil {
		fmt.Fprintf(out, "Disk space needed: about %s (%s available)\n", diskspace.FormatBytes(int64(needed)), diskspace.FormatBytes(int64(avail)))
	}
	if size.FreeBlocks == 0 {
		fmt.Fprintln(out, "The database has no free blocks: nothing to compact.")
		return &CompactResult{Before: before, After: before, Duration: time.Since(start)}, nil
	}
	if opts.DryRun {
		fmt.Fprintln(out)
		fmt.Fprintln(out, "Dry run - the database was not changed.")
		return &CompactResult{Before: before, After: before, Duration: time.Since(start)}, nil
	}
	if !opts.SkipConfirm && !confirm(out, opts.In, "Compact the database?") {
		fmt.Fprintln(out, "Aborted.")
		return nil, nil
	}

	compacted := path + compactSuffix
	original := path + originalSuffix
	for _, leftover := range []string{compacted, original} {
		if _, err := os.Stat(leftover); err == nil {
			return nil, fmt.Errorf("%s exists, left by an interrupted compaction: check it and remove it first", leftover)
		}
	}

	fmt.Fprintln(out)
	fmt.Fprintln(out, "Copying the database...")
	copied, err := store.CopyDatabase(ctx, compacted)
	if err != nil {
		return nil, err
	}
	if err := closeStore(); err != nil {
		os.Remove(compacted)
		return nil, fmt.Errorf("closing the database: %w", err)
	}
	if fi, err := os.Stat(path + ".wal"); err == nil && fi.Size() > 0 {
		os.Remove(compacted)
		return nil, fmt.Errorf("the database kept a write-ahead log after it was closed; it was not changed")
	}

	// Swap: the original stays until the compacted database is checked
	if err := os.Rename(path, original); err != nil {
		os.Remove(compacted)
		return nil, fmt.Errorf("moving the database aside: %w", err)
	}
	os.Remove(path + ".wal")
	if err := os.Rename(compacted, path); err != nil {
		if restoreErr := os.Rename(original, path); restoreErr != nil {
			return nil, fmt.Errorf("replacing the database: %w; the original is at %s and the compacted copy at %s",
				err, original, compacted)
		}
		os.Remove(compacted)
		return nil, fmt.Errorf("replacing the database: %w; it was not changed", err)
	}
	if err := verifyCompacted(path, opts.Storage, copied); err != nil {
		// Put the original back
		failed := path + ".compact-failed"
		if renameErr := os.Rename(path, failed); renameErr == nil {
			os.Remove(path + ".wal")
			if restoreErr := os.Rename(original, path); restoreErr == nil {
				os.Remove(failed)
				return nil, fmt.Errorf("the compacted database failed its check, the original was put back: %w", err)
			}
		}
		return nil, fmt.Errorf("the compacted database failed its check: %w; the original is at %s", err, original)
	}

	result := &CompactResult{Before: before, After: diskspace.FileSize(path) + diskspace.FileSize(path+".wal"), Duration: time.Since(start)}
	if opts.KeepOriginal {
		result.Original = original
	} else if err := os.Remove(original); err != nil {
		result.Original = original
		fmt.Fprintf(out, "Warning: could not remove the original: %v\n", err)
	}

	fmt.Fprintf(out, "Compaction complete (%s): %s -> %s, %s returned\n", result.Duration.Round(time.Millisecond),
		diskspace.FormatBytes(result.Before), diskspace.FormatBytes(result.After), diskspace.FormatBytes(max(result.Before-result.After, 0)))
	if result.Original != "" {
		fmt.Fprintf(out, "The original database is kept at %s\n", result.Original)
	}
	return result, nil
}

// verifyCompacted opens the compacted database at path, which runs the
// schema (recreating any index of schema.go that is missing), and checks
// that it holds the rows and schema version of the copy
func verifyCompacted(path string, opts storage.Options, copied *storage.CopyResult) error {
	opts.SkipMigrationBackup = true // the copy has the original's schema version
	store, err := storage.NewDuckDBStoreWithOptions(path, opts)
	if err != nil {
		return err
	}
	defer store.Close()
	ctx := context.Background()
	version, err := store.SchemaVersion(ctx)
	if err != nil {
		return err
	}
	if version != copied.SchemaVersion {
		return fmt.Errorf("schema version %d instead of %d", version, copied.SchemaVersion)
	}
	for table, want := range copied.Tables {
		var got int64
		if err := store.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM "`+strings.ReplaceAll(table, `"`, `""`)+`"`).Scan(&got); err != nil {
			return fmt.Errorf("counting %s: %w", table, err)
		}
		if got != want {
			return fmt.Errorf("table %s has %d rows instead of %d", table, got, want)
		}
	}
	return nil
}

func printTables(out io.Writer, tables map[string]int64) {
	for _, table := range []string{"otel_traces", "otel_logs", "otel_metrics", "dashboards", "dashboard_widgets"} {
		if n, ok := tables[table]; ok {
			fmt.Fprintf(out, "  %-18s %d rows\n", table, n)
		}
	}
}

func confirm(out io.Writer, in io.Reader, question string) bool {
	if in == nil {
		in = os.Stdin
	}
	fmt.Fprintln(out)
	fmt.Fprintf(out, "%s [y/N] ", question)
	return prompt.Confirmed(in)
}

func writerOr(w io.Writer) io.Writer {
	if w == nil {
		return os.Stdout
	}
	return w
}
