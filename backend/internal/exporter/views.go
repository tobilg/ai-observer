package exporter

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"

	_ "github.com/duckdb/duckdb-go/v2"
)

// createViewsDatabase creates a new DuckDB database with views of the Parquet
// files. The views name the files relative to the database's directory, so
// the directory can be moved and shared: DuckDB resolves them against the
// current directory, so open the database from its directory. They are
// created with file_search_path set to the directory rather than by changing
// the process's working directory, which the server's other requests share.
func (e *Exporter) createViewsDatabase(ctx context.Context, dbPath string) error {
	dbPath, err := filepath.Abs(dbPath)
	if err != nil {
		return fmt.Errorf("resolving %s: %w", dbPath, err)
	}
	viewsDB, err := sql.Open("duckdb", dbPath)
	if err != nil {
		return fmt.Errorf("opening views database: %w", err)
	}
	defer viewsDB.Close()

	conn, err := viewsDB.Conn(ctx)
	if err != nil {
		return fmt.Errorf("opening views database: %w", err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, "SET file_search_path = "+sqlStringLiteral(filepath.Dir(dbPath))); err != nil {
		return fmt.Errorf("configuring views database: %w", err)
	}

	views := []struct {
		name        string
		parquetFile string
	}{
		{"traces", TracesFile},
		{"logs", LogsFile},
		{"metrics", MetricsFile},
	}
	for _, v := range views {
		query := fmt.Sprintf("CREATE VIEW %s AS SELECT * FROM read_parquet(%s)", v.name, sqlStringLiteral(v.parquetFile))
		if _, err := conn.ExecContext(ctx, query); err != nil {
			return fmt.Errorf("creating view %s: %w", v.name, err)
		}
	}
	return nil
}
