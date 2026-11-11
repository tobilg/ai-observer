package exporter

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/tobilg/ai-observer/internal/storage"
)

// exportColumns are the exported columns of the telemetry tables: all of
// them, including SourceFile, the path of the session file a record was
// imported from. Restore keeps it, so importing or watching the same files
// afterwards replaces their records instead of storing them again.
// Logs leave out MessageKey: it is derived by the code of one version
// (storage/message_keys.go), and restore computes it again.
func exportColumns(table string) string {
	if table == "otel_logs" {
		return "* EXCLUDE (MessageKey)"
	}
	return "*"
}

// snapshot runs the queries of an export in one read transaction, so all
// files show the database at one point in time while the server keeps
// storing telemetry
type snapshot struct {
	tx *sql.Tx
}

// exportToParquet exports the rows of a table in [from, to] of a service
// filter (a tool name selects all of the tool's services) to Parquet with
// DuckDB's COPY TO, and returns the number of rows written
func (s snapshot) exportToParquet(ctx context.Context, table, outputPath string, from, to *time.Time, service string) (int64, error) {
	var where []string
	var args []any
	if from != nil {
		where = append(where, "Timestamp >= ?::TIMESTAMP")
		args = append(args, storage.TimestampArg(*from))
	}
	if to != nil {
		where = append(where, "Timestamp <= ?::TIMESTAMP")
		args = append(args, storage.TimestampArg(*to))
	}
	if cond, serviceArgs := storage.ServiceCondition(service); cond != "" {
		where = append(where, cond)
		args = append(args, serviceArgs...)
	}
	query := fmt.Sprintf("SELECT %s FROM %s", exportColumns(table), table)
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	return s.copyToParquet(ctx, query, args, outputPath)
}

// exportBaselines exports the metric baselines the exported metrics need
// (storage.ExportBaselinesQuery)
func (s snapshot) exportBaselines(ctx context.Context, outputPath string, from, to *time.Time, service string) (int64, error) {
	query, args := storage.ExportBaselinesQuery(from, to, service)
	return s.copyToParquet(ctx, query, args, outputPath)
}

// exportImportState exports the import state of the session files whose
// records are exported (storage.ExportImportStateQuery)
func (s snapshot) exportImportState(ctx context.Context, outputPath string, from, to *time.Time, service string) (int64, error) {
	query, args := storage.ExportImportStateQuery(from, to, service)
	return s.copyToParquet(ctx, query, args, outputPath)
}

// copyToParquet writes the rows of query to a ZSTD-compressed Parquet file
// and returns their number
func (s snapshot) copyToParquet(ctx context.Context, query string, args []any, outputPath string) (int64, error) {
	copyQuery := fmt.Sprintf("COPY (%s) TO %s (FORMAT PARQUET, COMPRESSION 'ZSTD')", query, sqlStringLiteral(outputPath))
	if _, err := s.tx.ExecContext(ctx, copyQuery, args...); err != nil {
		return 0, fmt.Errorf("executing COPY TO: %w", err)
	}

	// Count the rows in the output file to report
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM read_parquet(%s)", sqlStringLiteral(outputPath))
	var count int64
	if err := s.tx.QueryRowContext(ctx, countQuery).Scan(&count); err != nil {
		return 0, fmt.Errorf("counting the rows of %s: %w", outputPath, err)
	}
	return count, nil
}

// sqlStringLiteral quotes s as a SQL string literal. COPY TO does not accept a
// parameter for its file name, and paths may contain quotes ("Bob's exports").
func sqlStringLiteral(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}
