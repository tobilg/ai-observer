package storage

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// TelemetryBounds returns the times of the oldest and newest stored
// telemetry record (span, log or metric point), zero when nothing is
// stored. DuckDB answers MIN and MAX of a column from its statistics, so
// this does not scan the tables (3 ms for 3.5 million records).
func (s *DuckDBStore) TelemetryBounds(ctx context.Context) (oldest, newest time.Time, err error) {
	var minTime, maxTime sql.NullTime
	err = s.db.QueryRowContext(ctx, `
		SELECT MIN(t), MAX(t) FROM (
			SELECT MIN(Timestamp) AS t FROM otel_traces UNION ALL SELECT MAX(Timestamp) FROM otel_traces
			UNION ALL SELECT MIN(Timestamp) FROM otel_logs UNION ALL SELECT MAX(Timestamp) FROM otel_logs
			UNION ALL SELECT MIN(Timestamp) FROM otel_metrics UNION ALL SELECT MAX(Timestamp) FROM otel_metrics
		)`).Scan(&minTime, &maxTime)
	if err != nil {
		return oldest, newest, fmt.Errorf("querying telemetry bounds: %w", err)
	}
	return minTime.Time, maxTime.Time, nil
}

// ImportSummary sums up the import and watch state of a tool's session
// files (import_state)
type ImportSummary struct {
	// Source is the tool (importer source: claude-code, codex, gemini)
	Source string
	// Files counts the files imported or watched
	Files int
	// Records sums the records stored from them
	Records int64
	// LastImport is the time a file was last imported or read by watch
	LastImport time.Time
	// PartialFiles counts the files of which a date-ranged import stored
	// only some records (ImportState.Partial)
	PartialFiles int
}

// ImportSummaries returns the import summary of each tool with imported or
// watched files, sorted by tool
func (s *DuckDBStore) ImportSummaries(ctx context.Context) ([]ImportSummary, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT source, COUNT(*), COALESCE(SUM(record_count), 0), MAX(imported_at), COUNT(*) FILTER (WHERE partial)
		FROM import_state
		GROUP BY source
		ORDER BY source`)
	if err != nil {
		return nil, fmt.Errorf("querying import state: %w", err)
	}
	defer rows.Close()
	summaries := []ImportSummary{}
	for rows.Next() {
		var sum ImportSummary
		var last sql.NullTime
		if err := rows.Scan(&sum.Source, &sum.Files, &sum.Records, &last, &sum.PartialFiles); err != nil {
			return nil, fmt.Errorf("scanning import state: %w", err)
		}
		sum.LastImport = last.Time
		summaries = append(summaries, sum)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating import state: %w", err)
	}
	return summaries, nil
}
