package storage

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"sync/atomic"
	"time"

	_ "github.com/duckdb/duckdb-go/v2"
	"github.com/tobilg/ai-observer/internal/redact"
)

type DuckDBStore struct {
	db *sql.DB
	// mu serializes writes, so concurrent writers never hit DuckDB transaction
	// conflicts. Reads take no lock: DuckDB's MVCC gives each query a consistent
	// snapshot, so dashboards are not blocked while large imports are written.
	mu sync.Mutex
	// redactor replaces secrets (and personal data) in the text of stored
	// telemetry; nil stores it as it is
	redactor *redact.Redactor
	// storeAllSpans keeps the tools' noise spans of OTLP exports
	// (dropNoiseSpans)
	storeAllSpans bool
	// writes counts the committed write transactions (WriteGeneration)
	writes atomic.Uint64
	// Background work (StartMessageKeyRefill), stopped by Close
	bgMu     sync.Mutex
	bgCancel context.CancelFunc
	bgDone   chan struct{}
	closed   bool
}

// SetStoreAllSpans makes InsertTraces store the tools' noise spans too
// (AI_OBSERVER_STORE_ALL_SPANS). It must be called before telemetry is
// written.
func (s *DuckDBStore) SetStoreAllSpans(all bool) {
	s.storeAllSpans = all
}

// SetRedactor sets what is redacted in telemetry before it is stored
// (AI_OBSERVER_REDACT). It must be called before telemetry is written.
func (s *DuckDBStore) SetRedactor(r *redact.Redactor) {
	s.redactor = r
}

// Redactor returns the redactor applied to stored telemetry
func (s *DuckDBStore) Redactor() *redact.Redactor {
	return s.redactor
}

// Options are optional settings of the database
type Options struct {
	// MemoryLimit bounds the memory DuckDB uses for data and query
	// processing, as a size such as "2GB" (DuckDB's memory_limit). Empty
	// keeps DuckDB's default, 80% of the machine's memory.
	MemoryLimit string
	// Threads is the number of threads DuckDB runs queries on. 0 keeps
	// DuckDB's default, one per CPU core.
	Threads int
	// SkipMigrationBackup migrates a database that holds data without
	// copying it first (AI_OBSERVER_SKIP_MIGRATION_BACKUP;
	// backupBeforeMigration)
	SkipMigrationBackup bool
}

var memoryLimitPattern = regexp.MustCompile(`^[0-9]+(\.[0-9]+)?\s*([KMGT]i?B)$`)

// ValidMemoryLimit reports whether limit is a size Options.MemoryLimit
// accepts: a number and a unit (KB, MB, GB, TB, or KiB, MiB, GiB, TiB)
func ValidMemoryLimit(limit string) bool {
	return memoryLimitPattern.MatchString(limit)
}

// NewDuckDBStore opens the database at dbPath with DuckDB's default limits
func NewDuckDBStore(dbPath string) (*DuckDBStore, error) {
	return NewDuckDBStoreWithOptions(dbPath, Options{})
}

// NewDuckDBStoreWithOptions opens the database at dbPath, creating or
// migrating its schema
func NewDuckDBStoreWithOptions(dbPath string, opts Options) (*DuckDBStore, error) {
	// Ensure directory exists
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("creating database directory: %w", err)
	}

	db, err := sql.Open("duckdb", dbPath)
	if err != nil {
		return nil, openError(dbPath, err)
	}

	// Configure connection pool
	db.SetMaxOpenConns(25)                 // Max concurrent connections
	db.SetMaxIdleConns(10)                 // Max idle connections in pool
	db.SetConnMaxLifetime(5 * time.Minute) // Max connection lifetime
	db.SetConnMaxIdleTime(1 * time.Minute) // Max idle time before closing

	// Test connection
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, openError(dbPath, err)
	}

	// List pages fetch one row more than they show (to know whether more
	// follow). DuckDB reads wide columns such as log bodies only for the rows
	// of a "ORDER BY ... LIMIT n" result up to n = late_materialization_max_rows
	// (default 50); above that it reads them for every row in the time range,
	// which made a 51-row page of logs 15x slower than a 50-row one.
	if _, err := db.Exec(fmt.Sprintf("SET GLOBAL late_materialization_max_rows = %d", maxPageSize+1)); err != nil {
		db.Close()
		return nil, fmt.Errorf("configuring database: %w", err)
	}

	// Set before the schema runs, so migrations of a large database stay
	// within the limits too. Neither setting takes parameters.
	if opts.MemoryLimit != "" {
		if !ValidMemoryLimit(opts.MemoryLimit) {
			db.Close()
			return nil, fmt.Errorf("invalid memory limit %q: use a size such as 2GB", opts.MemoryLimit)
		}
		if _, err := db.Exec("SET GLOBAL memory_limit = '" + opts.MemoryLimit + "'"); err != nil {
			db.Close()
			return nil, fmt.Errorf("setting memory limit %s: %w", opts.MemoryLimit, err)
		}
	}
	if opts.Threads > 0 {
		if _, err := db.Exec(fmt.Sprintf("SET GLOBAL threads = %d", opts.Threads)); err != nil {
			db.Close()
			return nil, fmt.Errorf("setting threads to %d: %w", opts.Threads, err)
		}
	}

	store := &DuckDBStore{db: db}

	// Migrations rewrite stored data in place: copy a database that holds
	// data before they run
	if !opts.SkipMigrationBackup {
		if _, err := store.backupBeforeMigration(context.Background(), dbPath); err != nil {
			db.Close()
			return nil, err
		}
	}

	// Initialize schema
	if err := store.initSchema(context.Background()); err != nil {
		db.Close()
		return nil, fmt.Errorf("initializing schema: %w", err)
	}
	return store, nil
}

// openError explains a failure to open the database. DuckDB allows one
// process per database file, so a lock conflict means another ai-observer
// process has it open.
func openError(dbPath string, err error) error {
	if IsLocked(err) {
		return fmt.Errorf("database %s is in use by another process, such as a running ai-observer server or watch. "+
			"Only one process can open it: stop that process before running import, export, delete, backup, restore or compact. "+
			"While a server runs, its admin API (AI_OBSERVER_ADMIN_API=true) can delete, export and back up instead: %w", dbPath, err)
	}
	return fmt.Errorf("opening database: %w", err)
}

func (s *DuckDBStore) Close() error {
	s.stopBackground()
	return s.db.Close()
}

func (s *DuckDBStore) DB() *sql.DB {
	return s.db
}

// TimestampArg formats t as a query argument for a TIMESTAMP column, to be
// cast with ?::TIMESTAMP. The columns hold UTC times without a zone.
func TimestampArg(t time.Time) string {
	return formatTimeForDB(t)
}

// formatTimeForDB formats a time.Time for DuckDB TIMESTAMP comparison.
// DuckDB TIMESTAMP is timezone-naive, so we format as UTC without timezone suffix.
func formatTimeForDB(t time.Time) string {
	return t.UTC().Format("2006-01-02 15:04:05.999999")
}

func (s *DuckDBStore) initSchema(ctx context.Context) error {
	schemas := []string{
		schemaTraces,
		schemaLogs,
		schemaMetrics,
		schemaMetricNames,
		schemaDashboards,
		schemaDashboardWidgets,
		schemaImportState,
		indexTraces,
		indexLogs,
		indexMetrics,
		indexDashboards,
		indexImportState,
		migrateImportStateForWatch,
	}

	for _, schema := range schemas {
		if _, err := s.db.ExecContext(ctx, schema); err != nil {
			return fmt.Errorf("executing schema: %w", err)
		}
	}

	return s.migrate(ctx, migrations)
}

// migrate applies the migrations that the database has not recorded yet
func (s *DuckDBStore) migrate(ctx context.Context, migrations []migration) error {
	if _, err := s.db.ExecContext(ctx, schemaMigrations); err != nil {
		return fmt.Errorf("creating schema_migrations: %w", err)
	}

	var current int
	if err := s.db.QueryRowContext(ctx, "SELECT COALESCE(MAX(version), 0) FROM schema_migrations").Scan(&current); err != nil {
		return fmt.Errorf("reading schema version: %w", err)
	}

	for _, m := range migrations {
		if m.version <= current {
			continue
		}
		err := s.inWriteTx(ctx, func(w *writeTx) error {
			for _, stmt := range m.stmts {
				if _, err := w.tx.ExecContext(ctx, stmt); err != nil {
					return err
				}
			}
			if m.run != nil {
				if err := m.run(ctx, w); err != nil {
					return err
				}
			}
			_, err := w.tx.ExecContext(ctx,
				"INSERT INTO schema_migrations (version, name, applied_at) VALUES (?, ?, ?)",
				m.version, m.name, time.Now().UTC())
			return err
		})
		if err != nil {
			return fmt.Errorf("applying migration %d (%s): %w", m.version, m.name, err)
		}
	}
	return nil
}
