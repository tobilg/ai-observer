package exporter

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/tobilg/ai-observer/internal/storage"
)

// exportColumns leaves out SourceFile, the local path of the session file a
// record was imported from, which only serves re-imports
const exportColumns = "* EXCLUDE (SourceFile)"

// exportToParquet exports a table to Parquet format using DuckDB COPY TO
func (e *Exporter) exportToParquet(ctx context.Context, table, outputPath string, from, to *time.Time, service string) (int64, error) {
	var query string
	var args []interface{}

	// Build the query based on filters
	hasFilters := from != nil || to != nil || service != ""

	if hasFilters {
		// Build filtered query
		query = fmt.Sprintf("SELECT %s FROM %s WHERE 1=1", exportColumns, table)

		if from != nil {
			query += " AND Timestamp >= ?::TIMESTAMP"
			args = append(args, storage.TimestampArg(*from))
		}

		if to != nil {
			query += " AND Timestamp <= ?::TIMESTAMP"
			args = append(args, storage.TimestampArg(*to))
		}

		if service != "" {
			query += " AND ServiceName = ?"
			args = append(args, service)
		}

		// Wrap in COPY statement
		query = fmt.Sprintf("COPY (%s) TO %s (FORMAT PARQUET, COMPRESSION 'ZSTD')", query, sqlStringLiteral(outputPath))
	} else {
		// Full table export (no filters)
		query = fmt.Sprintf("COPY (SELECT %s FROM %s) TO %s (FORMAT PARQUET, COMPRESSION 'ZSTD')", exportColumns, table, sqlStringLiteral(outputPath))
	}

	// Execute the COPY command
	_, err := e.store.DB().ExecContext(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("executing COPY TO: %w", err)
	}

	// Count the rows in the output file to report
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM read_parquet(%s)", sqlStringLiteral(outputPath))
	var count int64
	if err := e.store.DB().QueryRowContext(ctx, countQuery).Scan(&count); err != nil {
		// If we can't count, just return 0 - the export still succeeded
		return 0, nil
	}

	return count, nil
}

// sqlStringLiteral quotes s as a SQL string literal. COPY TO does not accept a
// parameter for its file name, and paths may contain quotes ("Bob's exports").
func sqlStringLiteral(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}
