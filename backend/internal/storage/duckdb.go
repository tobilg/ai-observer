package storage

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "github.com/duckdb/duckdb-go/v2"
)

type DuckDBStore struct {
	db *sql.DB
	mu sync.RWMutex
}

func NewDuckDBStore(dbPath string) (*DuckDBStore, error) {
	// Ensure directory exists
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("creating database directory: %w", err)
	}

	db, err := sql.Open("duckdb", dbPath)
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	// Configure connection pool
	db.SetMaxOpenConns(25)              // Max concurrent connections
	db.SetMaxIdleConns(10)              // Max idle connections in pool
	db.SetConnMaxLifetime(5 * time.Minute)  // Max connection lifetime
	db.SetConnMaxIdleTime(1 * time.Minute)  // Max idle time before closing

	// Test connection
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("connecting to database: %w", err)
	}

	store := &DuckDBStore{db: db}

	// Initialize schema
	if err := store.initSchema(context.Background()); err != nil {
		db.Close()
		return nil, fmt.Errorf("initializing schema: %w", err)
	}

	return store, nil
}

func (s *DuckDBStore) Close() error {
	return s.db.Close()
}

func (s *DuckDBStore) DB() *sql.DB {
	return s.db
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
		schemaDashboards,
		schemaDashboardWidgets,
		schemaImportState,
		indexTraces,
		indexLogs,
		indexMetrics,
		indexDashboards,
		indexImportState,
	}

	for _, schema := range schemas {
		if _, err := s.db.ExecContext(ctx, schema); err != nil {
			return fmt.Errorf("executing schema: %w", err)
		}
	}

	return nil
}
