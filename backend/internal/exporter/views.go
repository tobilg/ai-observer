package exporter

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "github.com/duckdb/duckdb-go/v2"
)

// createViewsDatabase creates a new DuckDB database with views pointing to Parquet files
// The views use relative paths so the entire directory can be moved/shared
func (e *Exporter) createViewsDatabase(ctx context.Context, dbPath string) error {
	// Change to the output directory so relative paths work correctly
	outputDir := filepath.Dir(dbPath)
	originalDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getting current directory: %w", err)
	}

	if err := os.Chdir(outputDir); err != nil {
		return fmt.Errorf("changing to output directory: %w", err)
	}
	defer os.Chdir(originalDir)

	// Open a new DuckDB database for views (use just the filename since we're in the dir)
	dbFilename := filepath.Base(dbPath)
	viewsDB, err := sql.Open("duckdb", dbFilename)
	if err != nil {
		return fmt.Errorf("opening views database: %w", err)
	}
	defer viewsDB.Close()

	// Create views with relative paths (files are in same directory)
	views := []struct {
		name        string
		parquetFile string
	}{
		{"traces", "traces.parquet"},
		{"logs", "logs.parquet"},
		{"metrics", "metrics.parquet"},
	}

	for _, v := range views {
		query := fmt.Sprintf("CREATE VIEW %s AS SELECT * FROM read_parquet('%s')", v.name, v.parquetFile)
		if _, err := viewsDB.ExecContext(ctx, query); err != nil {
			return fmt.Errorf("creating view %s: %w", v.name, err)
		}
	}

	return nil
}
