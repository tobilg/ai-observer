package storage

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// DeleteSummary contains counts of records that would be or were deleted
type DeleteSummary struct {
	LogCount    int64
	MetricCount int64
	TraceCount  int64
	SpanCount   int64
}

// CountLogsInRange returns the number of logs in the given time range
func (s *DuckDBStore) CountLogsInRange(ctx context.Context, from, to time.Time, service string) (int64, error) {
	fromStr := formatTimeForDB(from)
	toStr := formatTimeForDB(to)

	query := `SELECT COUNT(*) FROM otel_logs WHERE Timestamp >= ?::TIMESTAMP AND Timestamp <= ?::TIMESTAMP`
	args := []interface{}{fromStr, toStr}

	if service != "" {
		query += " AND ServiceName = ?"
		args = append(args, service)
	}

	var count int64
	if err := s.db.QueryRowContext(ctx, query, args...).Scan(&count); err != nil {
		return 0, fmt.Errorf("counting logs: %w", err)
	}

	return count, nil
}

// CountMetricsInRange returns the number of metrics in the given time range
func (s *DuckDBStore) CountMetricsInRange(ctx context.Context, from, to time.Time, service string) (int64, error) {
	fromStr := formatTimeForDB(from)
	toStr := formatTimeForDB(to)

	query := `SELECT COUNT(*) FROM otel_metrics WHERE Timestamp >= ?::TIMESTAMP AND Timestamp <= ?::TIMESTAMP`
	args := []interface{}{fromStr, toStr}

	if service != "" {
		query += " AND ServiceName = ?"
		args = append(args, service)
	}

	var count int64
	if err := s.db.QueryRowContext(ctx, query, args...).Scan(&count); err != nil {
		return 0, fmt.Errorf("counting metrics: %w", err)
	}

	return count, nil
}

// CountTracesInRange returns the number of traces and spans in the given time range
func (s *DuckDBStore) CountTracesInRange(ctx context.Context, from, to time.Time, service string) (traces int64, spans int64, err error) {
	fromStr := formatTimeForDB(from)
	toStr := formatTimeForDB(to)

	// Count total spans
	spanQuery := `SELECT COUNT(*) FROM otel_traces WHERE Timestamp >= ?::TIMESTAMP AND Timestamp <= ?::TIMESTAMP`
	args := []interface{}{fromStr, toStr}

	if service != "" {
		spanQuery += " AND ServiceName = ?"
		args = append(args, service)
	}

	if err := s.db.QueryRowContext(ctx, spanQuery, args...).Scan(&spans); err != nil {
		return 0, 0, fmt.Errorf("counting spans: %w", err)
	}

	// Count distinct traces
	traceQuery := `SELECT COUNT(DISTINCT TraceId) FROM otel_traces WHERE Timestamp >= ?::TIMESTAMP AND Timestamp <= ?::TIMESTAMP`
	traceArgs := []interface{}{fromStr, toStr}

	if service != "" {
		traceQuery += " AND ServiceName = ?"
		traceArgs = append(traceArgs, service)
	}

	if err := s.db.QueryRowContext(ctx, traceQuery, traceArgs...).Scan(&traces); err != nil {
		return 0, 0, fmt.Errorf("counting traces: %w", err)
	}

	return traces, spans, nil
}

// DeleteLogsInRange deletes logs in the given time range and returns the count deleted
func (s *DuckDBStore) DeleteLogsInRange(ctx context.Context, from, to time.Time, service string) (int64, error) {
	return s.deleteTableInRange(ctx, "otel_logs", "logs", from, to, service)
}

// DeleteMetricsInRange deletes metrics in the given time range and returns the count deleted
func (s *DuckDBStore) DeleteMetricsInRange(ctx context.Context, from, to time.Time, service string) (int64, error) {
	return s.deleteTableInRange(ctx, "otel_metrics", "metrics", from, to, service)
}

// DeleteTracesInRange deletes traces (spans) in the given time range and returns the count deleted
func (s *DuckDBStore) DeleteTracesInRange(ctx context.Context, from, to time.Time, service string) (int64, error) {
	return s.deleteTableInRange(ctx, "otel_traces", "traces", from, to, service)
}

func (s *DuckDBStore) deleteTableInRange(ctx context.Context, table, what string, from, to time.Time, service string) (int64, error) {
	var count int64
	err := s.inWriteTx(ctx, func(w *writeTx) error {
		var err error
		count, err = deleteInRange(ctx, w.tx, table, what, from, to, service)
		return err
	})
	return count, err
}

// deleteInRange deletes the rows of table in the time range, optionally of one service
func deleteInRange(ctx context.Context, execer execContexter, table, what string, from, to time.Time, service string) (int64, error) {
	where := ` WHERE Timestamp >= ?::TIMESTAMP AND Timestamp <= ?::TIMESTAMP`
	args := []interface{}{formatTimeForDB(from), formatTimeForDB(to)}
	if service != "" {
		where += " AND ServiceName = ?"
		args = append(args, service)
	}

	if table == "otel_metrics" {
		if err := keepCumulativeBaselines(ctx, execer, where, args, from, to); err != nil {
			return 0, err
		}
	}

	result, err := execer.ExecContext(ctx, `DELETE FROM `+table+where, args...)
	if err != nil {
		return 0, fmt.Errorf("deleting %s: %w", what, err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("getting rows affected: %w", err)
	}
	if table == "otel_metrics" && count > 0 {
		if err := pruneMetricNames(ctx, execer); err != nil {
			return 0, err
		}
	}
	return count, nil
}

// keepCumulativeBaselines records the last point of each cumulative series
// among the metric rows that where selects, before they are deleted. Metric
// series use it as the previous point of the series' next stored point, which
// would otherwise count as an increase of its whole running total. A series
// keeps its latest baseline. A deletion from the beginning (retention) drops
// baselines too old to be the previous point of any remaining one.
func keepCumulativeBaselines(ctx context.Context, execer execContexter, where string, args []any, from, to time.Time) error {
	query := `
		INSERT INTO metric_baselines
		SELECT ServiceName, MetricName, CAST(Attributes AS VARCHAR), CAST(ResourceAttributes AS VARCHAR),
			MAX(Timestamp), arg_max(COALESCE(Value, Sum), Timestamp)
		FROM otel_metrics` + where + ` AND AggregationTemporality = 2 AND COALESCE(Value, Sum) IS NOT NULL
		GROUP BY ALL
		ON CONFLICT DO UPDATE SET Timestamp = excluded.Timestamp, Value = excluded.Value
			WHERE excluded.Timestamp > metric_baselines.Timestamp`
	if _, err := execer.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("keeping cumulative baselines: %w", err)
	}
	if from.IsZero() {
		if _, err := execer.ExecContext(ctx, `DELETE FROM metric_baselines WHERE Timestamp < ?::TIMESTAMP`,
			formatTimeForDB(to.Add(-cumulativeLookback))); err != nil {
			return fmt.Errorf("pruning cumulative baselines: %w", err)
		}
	}
	return nil
}

// OldestTimestamp returns the time of the oldest stored log, metric or span;
// ok is false when nothing is stored
func (s *DuckDBStore) OldestTimestamp(ctx context.Context) (oldest time.Time, ok bool, err error) {
	var t sql.NullTime
	err = s.db.QueryRowContext(ctx, `
		SELECT MIN(ts) FROM (
			SELECT MIN(Timestamp) AS ts FROM otel_logs
			UNION ALL SELECT MIN(Timestamp) FROM otel_metrics
			UNION ALL SELECT MIN(Timestamp) FROM otel_traces
		)`).Scan(&t)
	if err != nil {
		return time.Time{}, false, fmt.Errorf("querying oldest record: %w", err)
	}
	return t.Time, t.Valid, nil
}

// CountAllInRange returns a summary of all record counts in the given time range
func (s *DuckDBStore) CountAllInRange(ctx context.Context, from, to time.Time, service string) (*DeleteSummary, error) {
	logCount, err := s.CountLogsInRange(ctx, from, to, service)
	if err != nil {
		return nil, err
	}

	metricCount, err := s.CountMetricsInRange(ctx, from, to, service)
	if err != nil {
		return nil, err
	}

	traceCount, spanCount, err := s.CountTracesInRange(ctx, from, to, service)
	if err != nil {
		return nil, err
	}

	return &DeleteSummary{
		LogCount:    logCount,
		MetricCount: metricCount,
		TraceCount:  traceCount,
		SpanCount:   spanCount,
	}, nil
}

// DeleteAllInRange deletes all records (logs, metrics, traces) in the given
// time range in one transaction: either all of them are deleted or none
func (s *DuckDBStore) DeleteAllInRange(ctx context.Context, from, to time.Time, service string) (*DeleteSummary, error) {
	var summary *DeleteSummary
	err := s.inWriteTx(ctx, func(w *writeTx) error {
		var err error
		summary, err = deleteAllInRange(ctx, w, from, to, service)
		return err
	})
	if err != nil {
		return nil, err
	}
	return summary, nil
}

func deleteAllInRange(ctx context.Context, w *writeTx, from, to time.Time, service string) (*DeleteSummary, error) {
	summary := &DeleteSummary{}
	var err error
	if summary.LogCount, err = deleteInRange(ctx, w.tx, "otel_logs", "logs", from, to, service); err != nil {
		return nil, err
	}
	if summary.MetricCount, err = deleteInRange(ctx, w.tx, "otel_metrics", "metrics", from, to, service); err != nil {
		return nil, err
	}
	if summary.SpanCount, err = deleteInRange(ctx, w.tx, "otel_traces", "traces", from, to, service); err != nil {
		return nil, err
	}
	return summary, nil
}
