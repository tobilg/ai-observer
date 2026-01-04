package storage

import (
	"context"
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
	s.mu.RLock()
	defer s.mu.RUnlock()

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
	s.mu.RLock()
	defer s.mu.RUnlock()

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
	s.mu.RLock()
	defer s.mu.RUnlock()

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
	s.mu.Lock()
	defer s.mu.Unlock()

	fromStr := formatTimeForDB(from)
	toStr := formatTimeForDB(to)

	query := `DELETE FROM otel_logs WHERE Timestamp >= ?::TIMESTAMP AND Timestamp <= ?::TIMESTAMP`
	args := []interface{}{fromStr, toStr}

	if service != "" {
		query += " AND ServiceName = ?"
		args = append(args, service)
	}

	result, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("deleting logs: %w", err)
	}

	count, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("getting rows affected: %w", err)
	}

	return count, nil
}

// DeleteMetricsInRange deletes metrics in the given time range and returns the count deleted
func (s *DuckDBStore) DeleteMetricsInRange(ctx context.Context, from, to time.Time, service string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	fromStr := formatTimeForDB(from)
	toStr := formatTimeForDB(to)

	query := `DELETE FROM otel_metrics WHERE Timestamp >= ?::TIMESTAMP AND Timestamp <= ?::TIMESTAMP`
	args := []interface{}{fromStr, toStr}

	if service != "" {
		query += " AND ServiceName = ?"
		args = append(args, service)
	}

	result, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("deleting metrics: %w", err)
	}

	count, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("getting rows affected: %w", err)
	}

	return count, nil
}

// DeleteTracesInRange deletes traces (spans) in the given time range and returns the count deleted
func (s *DuckDBStore) DeleteTracesInRange(ctx context.Context, from, to time.Time, service string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	fromStr := formatTimeForDB(from)
	toStr := formatTimeForDB(to)

	query := `DELETE FROM otel_traces WHERE Timestamp >= ?::TIMESTAMP AND Timestamp <= ?::TIMESTAMP`
	args := []interface{}{fromStr, toStr}

	if service != "" {
		query += " AND ServiceName = ?"
		args = append(args, service)
	}

	result, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("deleting traces: %w", err)
	}

	count, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("getting rows affected: %w", err)
	}

	return count, nil
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

// DeleteAllInRange deletes all records (logs, metrics, traces) in the given time range
func (s *DuckDBStore) DeleteAllInRange(ctx context.Context, from, to time.Time, service string) (*DeleteSummary, error) {
	logCount, err := s.DeleteLogsInRange(ctx, from, to, service)
	if err != nil {
		return nil, err
	}

	metricCount, err := s.DeleteMetricsInRange(ctx, from, to, service)
	if err != nil {
		return nil, err
	}

	spanCount, err := s.DeleteTracesInRange(ctx, from, to, service)
	if err != nil {
		return nil, err
	}

	return &DeleteSummary{
		LogCount:    logCount,
		MetricCount: metricCount,
		SpanCount:   spanCount,
	}, nil
}
