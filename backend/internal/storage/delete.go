package storage

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/tobilg/ai-observer/internal/tools"
)

// DeleteSummary contains counts of records that would be or were deleted
type DeleteSummary struct {
	LogCount    int64
	MetricCount int64
	TraceCount  int64
	SpanCount   int64
}

// telemetryRange selects literal stored service names. CLI filters are
// expanded at the caller; an inventory from an export already contains names.
func telemetryRange(from, to time.Time, services []string) conditions {
	var where conditions
	where.add("Timestamp >= ?::TIMESTAMP AND Timestamp <= ?::TIMESTAMP", formatTimeForDB(from), formatTimeForDB(to))
	if len(services) > 0 {
		where.add("ServiceName IN ("+placeholderList(len(services))+")", stringArgs(services)...)
	}
	return where
}

func (s *DuckDBStore) countRowsInRange(ctx context.Context, table string, where conditions) (int64, error) {
	var count int64
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table+where.clause("WHERE"), where.args...).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("counting %s: %w", table, err)
	}
	return count, nil
}

// CountLogsInRange counts logs matching the CLI service filter in the time range.
func (s *DuckDBStore) CountLogsInRange(ctx context.Context, from, to time.Time, service string) (int64, error) {
	return s.countRowsInRange(ctx, "otel_logs", telemetryRange(from, to, tools.FilterServices(service)))
}

// CountMetricsInRange counts metric points matching the CLI service filter in the time range.
func (s *DuckDBStore) CountMetricsInRange(ctx context.Context, from, to time.Time, service string) (int64, error) {
	return s.countRowsInRange(ctx, "otel_metrics", telemetryRange(from, to, tools.FilterServices(service)))
}

// CountTracesInRange counts distinct traces and spans matching the CLI service filter in the time range.
func (s *DuckDBStore) CountTracesInRange(ctx context.Context, from, to time.Time, service string) (traces, spans int64, err error) {
	return s.countTraces(ctx, telemetryRange(from, to, tools.FilterServices(service)))
}

func (s *DuckDBStore) countTraces(ctx context.Context, where conditions) (traces, spans int64, err error) {
	err = s.db.QueryRowContext(ctx, "SELECT COUNT(DISTINCT TraceId), COUNT(*) FROM otel_traces"+where.clause("WHERE"), where.args...).Scan(&traces, &spans)
	if err != nil {
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
		count, err = deleteInRange(ctx, w.tx, table, what, from, to, telemetryRange(from, to, tools.FilterServices(service)))
		return err
	})
	return count, err
}

// deleteInRange keeps the baseline for cumulative metrics before removing rows.
func deleteInRange(ctx context.Context, execer execContexter, table, what string, from, to time.Time, selection conditions) (int64, error) {
	where, args := selection.clause("WHERE"), selection.args

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

// baselineColumns are the columns of metric_baselines: a series, the time
// and value (a counter's value or a histogram's sum) of its point, and a
// histogram's count, bucket counts and bounds (NULL for other metrics and
// for baselines stored before migration 9)
const baselineColumns = "ServiceName, MetricName, Attributes, ResourceAttributes, Timestamp, Value, Count, BucketCounts, ExplicitBounds"

// baselineUpdate updates a series' baseline to another point (ON CONFLICT)
const baselineUpdate = `Timestamp = excluded.Timestamp, Value = excluded.Value, Count = excluded.Count,
			BucketCounts = excluded.BucketCounts, ExplicitBounds = excluded.ExplicitBounds`

// keepCumulativeBaselines records the last point of each cumulative series
// among the metric rows that where selects, before they are deleted. Metric
// series use it as the previous point of the series' next stored point, which
// would otherwise count as an increase of its whole running total. A series
// keeps its latest baseline. A deletion from the beginning (retention) drops
// baselines too old to be the previous point of any remaining one.
func keepCumulativeBaselines(ctx context.Context, execer execContexter, where string, args []any, from, to time.Time) error {
	// Histograms keep the count, bucket counts and bounds of the point too
	// (arg_max_null: those of the latest point, also when NULL)
	query := `
		INSERT INTO metric_baselines (` + baselineColumns + `)
		SELECT ServiceName, MetricName, CAST(Attributes AS VARCHAR), CAST(ResourceAttributes AS VARCHAR),
			MAX(Timestamp), arg_max(COALESCE(Value, Sum), Timestamp),
			arg_max_null(Count, Timestamp), arg_max_null(BucketCounts, Timestamp), arg_max_null(ExplicitBounds, Timestamp)
		FROM otel_metrics` + where + ` AND AggregationTemporality = 2 AND COALESCE(Value, Sum) IS NOT NULL
		GROUP BY ALL
		ON CONFLICT DO UPDATE SET ` + baselineUpdate + `
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
	return s.countAllInRange(ctx, telemetryRange(from, to, tools.FilterServices(service)))
}

func (s *DuckDBStore) countAllInRange(ctx context.Context, where conditions) (*DeleteSummary, error) {
	logCount, err := s.countRowsInRange(ctx, "otel_logs", where)
	if err != nil {
		return nil, err
	}

	metricCount, err := s.countRowsInRange(ctx, "otel_metrics", where)
	if err != nil {
		return nil, err
	}

	traceCount, spanCount, err := s.countTraces(ctx, where)
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
	return deleteSelectedRange(ctx, w, from, to, telemetryRange(from, to, tools.FilterServices(service)))
}

func deleteSelectedRange(ctx context.Context, w *writeTx, from, to time.Time, where conditions) (*DeleteSummary, error) {
	summary := &DeleteSummary{}
	var err error
	if summary.LogCount, err = deleteInRange(ctx, w.tx, "otel_logs", "logs", from, to, where); err != nil {
		return nil, err
	}
	if summary.MetricCount, err = deleteInRange(ctx, w.tx, "otel_metrics", "metrics", from, to, where); err != nil {
		return nil, err
	}
	if summary.SpanCount, err = deleteInRange(ctx, w.tx, "otel_traces", "traces", from, to, where); err != nil {
		return nil, err
	}
	return summary, nil
}
