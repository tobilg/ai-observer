package storage

import (
	"context"
	"fmt"

	"github.com/tobilg/ai-observer/internal/api"
)

func (s *DuckDBStore) GetServices(ctx context.Context) ([]string, error) {
	query := `
		SELECT DISTINCT ServiceName
		FROM (
			SELECT ServiceName FROM otel_traces
			UNION
			SELECT ServiceName FROM otel_logs
			UNION
			SELECT ServiceName FROM otel_metrics
		)
		ORDER BY ServiceName
	`

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("querying services: %w", err)
	}
	defer rows.Close()

	services := []string{} // clients read a list
	for rows.Next() {
		var service string
		if err := rows.Scan(&service); err != nil {
			return nil, fmt.Errorf("scanning service: %w", err)
		}
		services = append(services, service)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating services: %w", err)
	}

	return services, nil
}

// GetStats returns record counts over all stored telemetry
func (s *DuckDBStore) GetStats(ctx context.Context) (*api.StatsResponse, error) {
	return s.GetStatsInRange(ctx, TimeRange{})
}

// GetStatsInRange returns the record counts of the records in r. Services
// lists every service that sent telemetry, also outside r.
func (s *DuckDBStore) GetStatsInRange(ctx context.Context, r TimeRange) (*api.StatsResponse, error) {
	stats := &api.StatsResponse{}

	filter, filterArgs := r.filter("Timestamp")
	where := " WHERE true" + filter
	statsQuery := `
		SELECT
			(SELECT COUNT(*) FROM otel_traces` + where + `) as span_count,
			(SELECT COUNT(DISTINCT TraceId) FROM otel_traces` + where + `) as trace_count,
			(SELECT COUNT(*) FROM otel_logs` + where + `) as log_count,
			(SELECT COUNT(*) FROM otel_metrics` + where + `) as metric_count,
			(SELECT COUNT(*) FROM otel_traces` + where + ` AND StatusCode = 'ERROR') as error_count
	`
	var args []any
	for range 5 {
		args = append(args, filterArgs...)
	}

	var errorCount int64
	if err := s.db.QueryRowContext(ctx, statsQuery, args...).Scan(
		&stats.SpanCount,
		&stats.TraceCount,
		&stats.LogCount,
		&stats.MetricCount,
		&errorCount,
	); err != nil {
		return nil, fmt.Errorf("getting stats: %w", err)
	}

	// Get services (still needs separate query due to multiple rows)
	services, err := s.GetServices(ctx)
	if err != nil {
		return nil, err
	}
	stats.Services = services
	stats.ServiceCount = len(services)

	// Calculate error rate
	if stats.SpanCount > 0 {
		stats.ErrorRate = float64(errorCount) / float64(stats.SpanCount) * 100
	}

	return stats, nil
}
