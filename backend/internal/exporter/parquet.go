package exporter

import (
	"context"
	"fmt"
	"time"
)

// exportToParquet exports a table to Parquet format using DuckDB COPY TO
func (e *Exporter) exportToParquet(ctx context.Context, table, outputPath string, from, to *time.Time, service string) (int64, error) {
	var query string
	var args []interface{}

	// Build the query based on filters
	hasFilters := from != nil || to != nil || service != ""

	if hasFilters {
		// Build filtered query
		query = fmt.Sprintf("SELECT * FROM %s WHERE 1=1", table)

		if from != nil {
			query += " AND Timestamp >= ?"
			args = append(args, *from)
		}

		if to != nil {
			query += " AND Timestamp <= ?"
			args = append(args, *to)
		}

		if service != "" {
			query += " AND ServiceName = ?"
			args = append(args, service)
		}

		// Wrap in COPY statement
		query = fmt.Sprintf("COPY (%s) TO '%s' (FORMAT PARQUET, COMPRESSION 'ZSTD')", query, outputPath)
	} else {
		// Full table export (no filters)
		query = fmt.Sprintf("COPY %s TO '%s' (FORMAT PARQUET, COMPRESSION 'ZSTD')", table, outputPath)
	}

	// Execute the COPY command
	_, err := e.store.DB().ExecContext(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("executing COPY TO: %w", err)
	}

	// Count the rows in the output file to report
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM read_parquet('%s')", outputPath)
	var count int64
	if err := e.store.DB().QueryRowContext(ctx, countQuery).Scan(&count); err != nil {
		// If we can't count, just return 0 - the export still succeeded
		return 0, nil
	}

	return count, nil
}
