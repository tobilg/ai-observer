package storage

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/tobilg/ai-observer/internal/api"
	"github.com/tobilg/ai-observer/internal/logger"
)


func (s *DuckDBStore) InsertMetrics(ctx context.Context, metrics []api.MetricDataPoint) error {
	if len(metrics) == 0 {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO otel_metrics (
			Timestamp, ServiceName, MetricName, MetricDescription, MetricUnit,
			ResourceAttributes, ScopeName, ScopeVersion, Attributes, MetricType,
			Value, AggregationTemporality, IsMonotonic, Count, Sum,
			BucketCounts, ExplicitBounds, Scale, ZeroCount, PositiveOffset,
			PositiveBucketCounts, NegativeOffset, NegativeBucketCounts,
			QuantileValues, QuantileQuantiles, Min, Max
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("preparing statement: %w", err)
	}
	defer stmt.Close()

	for _, m := range metrics {
		_, err := stmt.ExecContext(ctx,
			m.Timestamp,
			m.ServiceName,
			m.MetricName,
			nullString(m.MetricDescription),
			nullString(m.MetricUnit),
			mapToString(m.ResourceAttributes),
			nullString(m.ScopeName),
			nullString(m.ScopeVersion),
			mapToString(m.Attributes),
			m.MetricType,
			nullFloat64(m.Value),
			nullInt32(m.AggregationTemporality),
			nullBool(m.IsMonotonic),
			nullUint64(m.Count),
			nullFloat64(m.Sum),
			uint64ArrayToString(m.BucketCounts),
			float64ArrayToString(m.ExplicitBounds),
			nullInt32(m.Scale),
			nullUint64(m.ZeroCount),
			nullInt32(m.PositiveOffset),
			uint64ArrayToString(m.PositiveBucketCounts),
			nullInt32(m.NegativeOffset),
			uint64ArrayToString(m.NegativeBucketCounts),
			float64ArrayToString(m.QuantileValues),
			float64ArrayToString(m.QuantileQuantiles),
			nullFloat64(m.Min),
			nullFloat64(m.Max),
		)
		if err != nil {
			return fmt.Errorf("inserting metric: %w", err)
		}
	}

	return tx.Commit()
}

func (s *DuckDBStore) QueryMetrics(ctx context.Context, service, metricName, metricType string, from, to time.Time, limit, offset int) (*api.MetricsResponse, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Format times as strings to avoid timezone issues with DuckDB's TIMESTAMP type
	fromStr := formatTimeForDB(from)
	toStr := formatTimeForDB(to)

	query := `
		SELECT
			Timestamp, ServiceName, MetricName, MetricDescription, MetricUnit,
			ResourceAttributes, ScopeName, ScopeVersion, Attributes, MetricType,
			Value, AggregationTemporality, IsMonotonic, Count, Sum,
			Min, Max
		FROM otel_metrics
		WHERE Timestamp >= ?::TIMESTAMP AND Timestamp <= ?::TIMESTAMP
	`
	args := []interface{}{fromStr, toStr}

	if service != "" {
		query += " AND ServiceName = ?"
		args = append(args, service)
	}

	if metricName != "" {
		query += " AND MetricName = ?"
		args = append(args, metricName)
	}

	if metricType != "" {
		query += " AND MetricType = ?"
		args = append(args, metricType)
	}

	// Get total count
	countQuery := "SELECT COUNT(*) FROM otel_metrics WHERE Timestamp >= ?::TIMESTAMP AND Timestamp <= ?::TIMESTAMP"
	countArgs := []interface{}{fromStr, toStr}
	if service != "" {
		countQuery += " AND ServiceName = ?"
		countArgs = append(countArgs, service)
	}
	if metricName != "" {
		countQuery += " AND MetricName = ?"
		countArgs = append(countArgs, metricName)
	}
	if metricType != "" {
		countQuery += " AND MetricType = ?"
		countArgs = append(countArgs, metricType)
	}

	var total int
	if err := s.db.QueryRowContext(ctx, countQuery, countArgs...).Scan(&total); err != nil {
		return nil, fmt.Errorf("counting metrics: %w", err)
	}

	query += fmt.Sprintf(" ORDER BY Timestamp DESC LIMIT %d OFFSET %d", limit, offset)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying metrics: %w", err)
	}
	defer rows.Close()

	var metrics []api.MetricDataPoint
	for rows.Next() {
		var m api.MetricDataPoint
		var desc, unit, scopeName, scopeVersion sql.NullString
		var resourceAttrs, attrs interface{}
		var value, sum, min, max sql.NullFloat64
		var aggregationTemporality sql.NullInt32
		var isMonotonic sql.NullBool
		var count sql.NullInt64

		if err := rows.Scan(
			&m.Timestamp, &m.ServiceName, &m.MetricName, &desc, &unit,
			&resourceAttrs, &scopeName, &scopeVersion, &attrs, &m.MetricType,
			&value, &aggregationTemporality, &isMonotonic, &count, &sum,
			&min, &max,
		); err != nil {
			return nil, fmt.Errorf("scanning metric: %w", err)
		}

		m.MetricDescription = desc.String
		m.MetricUnit = unit.String
		m.ScopeName = scopeName.String
		m.ScopeVersion = scopeVersion.String
		m.ResourceAttributes = scanJSONToMap(resourceAttrs)
		m.Attributes = scanJSONToMap(attrs)

		if value.Valid {
			m.Value = &value.Float64
		}
		if aggregationTemporality.Valid {
			at := int32(aggregationTemporality.Int32)
			m.AggregationTemporality = &at
		}
		if isMonotonic.Valid {
			m.IsMonotonic = &isMonotonic.Bool
		}
		if count.Valid {
			c := uint64(count.Int64)
			m.Count = &c
		}
		if sum.Valid {
			m.Sum = &sum.Float64
		}
		if min.Valid {
			m.Min = &min.Float64
		}
		if max.Valid {
			m.Max = &max.Float64
		}

		metrics = append(metrics, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating metrics: %w", err)
	}

	return &api.MetricsResponse{
		Metrics: metrics,
		Total:   total,
		HasMore: offset+len(metrics) < total,
	}, nil
}

func (s *DuckDBStore) GetMetricNames(ctx context.Context, service string) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := `
		SELECT DISTINCT MetricName
		FROM otel_metrics
	`
	args := []interface{}{}

	if service != "" {
		query += " WHERE ServiceName = ?"
		args = append(args, service)
	}

	query += " ORDER BY MetricName"

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying metric names: %w", err)
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("scanning metric name: %w", err)
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating metric names: %w", err)
	}

	return names, nil
}

// GetBreakdownValues returns distinct values for a given attribute on a metric.
// This query is not time-filtered to return all historical values.
func (s *DuckDBStore) GetBreakdownValues(ctx context.Context, metricName, attribute, service string) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Build query to get distinct values for the specified attribute
	// Uses the MetricName index first, then extracts JSON attribute values
	// Use json_extract_string for reliable JSON text extraction
	query := fmt.Sprintf(`
		SELECT DISTINCT json_extract_string(Attributes, '$.%s') as attr_value
		FROM otel_metrics
		WHERE MetricName = ?
			AND json_extract_string(Attributes, '$.%s') IS NOT NULL
	`, attribute, attribute)
	args := []interface{}{metricName}

	if service != "" {
		query += " AND ServiceName = ?"
		args = append(args, service)
	}

	query += " ORDER BY attr_value"

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying breakdown values: %w", err)
	}
	defer rows.Close()

	var values []string
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return nil, fmt.Errorf("scanning breakdown value: %w", err)
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating breakdown values: %w", err)
	}

	return values, nil
}

func (s *DuckDBStore) QueryMetricSeries(ctx context.Context, metricName, service string, from, to time.Time, intervalSeconds int64, aggregate bool) (*api.TimeSeriesResponse, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Format times as strings to avoid timezone issues with DuckDB's TIMESTAMP type
	fromStr := formatTimeForDB(from)
	toStr := formatTimeForDB(to)

	// First, determine the metric type, aggregation temporality, and monotonicity
	typeQuery := `
		SELECT MetricType, IsMonotonic, AggregationTemporality
		FROM otel_metrics
		WHERE MetricName = ?
		LIMIT 1
	`
	var metricType string
	var isMonotonic sql.NullBool
	var aggregationTemporality sql.NullInt32
	if err := s.db.QueryRowContext(ctx, typeQuery, metricName).Scan(&metricType, &isMonotonic, &aggregationTemporality); err != nil {
		if err == sql.ErrNoRows {
			return &api.TimeSeriesResponse{Series: []api.TimeSeries{}}, nil
		}
		return nil, fmt.Errorf("getting metric type: %w", err)
	}

	// OTLP AggregationTemporality: 0=UNSPECIFIED, 1=DELTA, 2=CUMULATIVE
	isCumulative := aggregationTemporality.Valid && aggregationTemporality.Int32 == 2

	// Determine aggregation function based on metric type and mode
	// Use COALESCE(Value, Sum) to handle both gauge/sum (Value) and histogram (Sum) metrics
	var aggFunction string
	if aggregate {
		// Scalar aggregation over entire time range
		switch metricType {
		case "gauge":
			aggFunction = "AVG(COALESCE(Value, Sum))"
		case "sum":
			if isCumulative {
				// CUMULATIVE: value is running total, so total increase = MAX - MIN
				aggFunction = "(MAX(COALESCE(Value, Sum)) - MIN(COALESCE(Value, Sum)))"
			} else {
				// DELTA: each value is the change, so total = SUM
				aggFunction = "SUM(COALESCE(Value, Sum))"
			}
		case "histogram", "exp_histogram":
			// Histograms store their sum in the Sum column
			aggFunction = "SUM(Sum)"
		default:
			// Default to SUM for unknown types
			aggFunction = "SUM(COALESCE(Value, Sum))"
		}
	} else {
		// Time-bucketed aggregation
		switch metricType {
		case "gauge":
			aggFunction = "AVG(COALESCE(Value, Sum))"
		case "sum":
			if isCumulative {
				// CUMULATIVE: show the running total at end of each bucket
				aggFunction = "arg_max(COALESCE(Value, Sum), Timestamp)"
			} else if isMonotonic.Valid && isMonotonic.Bool {
				// DELTA monotonic counter: sum within bucket
				aggFunction = "SUM(COALESCE(Value, Sum))"
			} else {
				// DELTA non-monotonic: sum within bucket
				aggFunction = "SUM(COALESCE(Value, Sum))"
			}
		case "histogram", "exp_histogram":
			// Histograms store their sum in the Sum column
			aggFunction = "SUM(Sum)"
		default:
			// Default to SUM for unknown types
			aggFunction = "SUM(COALESCE(Value, Sum))"
		}
	}

	var query string
	args := []interface{}{fromStr, toStr, metricName}

	if aggregate {
		// Scalar aggregation - no time bucketing
		// Check multiple attribute keys for type breakdown (type, gen_ai.token.type)
		query = fmt.Sprintf(`
			SELECT
				ServiceName,
				COALESCE(Attributes->>'type', Attributes->>'gen_ai.token.type', 'default') as attr_type,
				%s as agg_value
			FROM otel_metrics
			WHERE Timestamp >= ?::TIMESTAMP AND Timestamp <= ?::TIMESTAMP
				AND MetricName = ?
				AND (Value IS NOT NULL OR Sum IS NOT NULL)
		`, aggFunction)

		if service != "" {
			query += " AND ServiceName = ?"
			args = append(args, service)
		}

		query += " GROUP BY ServiceName, attr_type"
	} else {
		// Construct interval string from seconds (e.g., "60 seconds")
		intervalStr := fmt.Sprintf("%d seconds", intervalSeconds)

		// Build service filter for the query
		serviceFilter := ""
		if service != "" {
			serviceFilter = " AND ServiceName = ?"
			args = append(args, service)
		}

		// Use CTEs with generate_series to create all time buckets and LEFT JOIN with data
		// This ensures all buckets are returned, with zeros for missing data
		// Note: generate_series returns an array in DuckDB, so we use UNNEST to expand it
		query = fmt.Sprintf(`
			WITH buckets AS (
				SELECT UNNEST(generate_series(
					time_bucket(INTERVAL '%[1]s', ?::TIMESTAMP),
					time_bucket(INTERVAL '%[1]s', ?::TIMESTAMP),
					INTERVAL '%[1]s'
				)) as bucket
			),
			series_labels AS (
				SELECT DISTINCT
					ServiceName,
					COALESCE(Attributes->>'type', Attributes->>'gen_ai.token.type', 'default') as attr_type
				FROM otel_metrics
				WHERE Timestamp >= ?::TIMESTAMP AND Timestamp <= ?::TIMESTAMP
					AND MetricName = ?
					AND (Value IS NOT NULL OR Sum IS NOT NULL)
					%[3]s
			),
			data AS (
				SELECT
					time_bucket(INTERVAL '%[1]s', Timestamp) as bucket,
					ServiceName,
					COALESCE(Attributes->>'type', Attributes->>'gen_ai.token.type', 'default') as attr_type,
					%[2]s as agg_value
				FROM otel_metrics
				WHERE Timestamp >= ?::TIMESTAMP AND Timestamp <= ?::TIMESTAMP
					AND MetricName = ?
					AND (Value IS NOT NULL OR Sum IS NOT NULL)
					%[3]s
				GROUP BY bucket, ServiceName, attr_type
			)
			SELECT
				b.bucket,
				s.ServiceName,
				s.attr_type,
				COALESCE(d.agg_value, 0) as agg_value
			FROM buckets b
			CROSS JOIN series_labels s
			LEFT JOIN data d ON b.bucket = d.bucket
				AND s.ServiceName = d.ServiceName
				AND s.attr_type = d.attr_type
			ORDER BY b.bucket, s.ServiceName, s.attr_type
		`, intervalStr, aggFunction, serviceFilter)

		// Update args: buckets CTE needs from, to; series_labels needs from, to, metricName, [service]; data needs from, to, metricName, [service]
		if service != "" {
			args = []interface{}{fromStr, toStr, fromStr, toStr, metricName, service, fromStr, toStr, metricName, service}
		} else {
			args = []interface{}{fromStr, toStr, fromStr, toStr, metricName, fromStr, toStr, metricName}
		}
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying metric series: %w", err)
	}
	defer rows.Close()

	seriesMap := make(map[string]*api.TimeSeries)

	if aggregate {
		// Scalar aggregation - single value per series
		for rows.Next() {
			var serviceName string
			var attrType string
			var value float64

			if err := rows.Scan(&serviceName, &attrType, &value); err != nil {
				return nil, fmt.Errorf("scanning metric aggregate: %w", err)
			}

			key := serviceName + ":" + attrType
			labels := map[string]string{"service": serviceName}
			if attrType != "default" {
				labels["type"] = attrType
			}
			seriesMap[key] = &api.TimeSeries{
				Name:       metricName,
				Labels:     labels,
				DataPoints: [][2]float64{{0, value}}, // timestamp=0 indicates aggregate
			}
		}
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("iterating metric aggregates: %w", err)
		}
	} else {
		// Time-bucketed series
		for rows.Next() {
			var bucket time.Time
			var serviceName string
			var attrType string
			var value float64

			if err := rows.Scan(&bucket, &serviceName, &attrType, &value); err != nil {
				return nil, fmt.Errorf("scanning metric series: %w", err)
			}

			key := serviceName + ":" + attrType
			if _, ok := seriesMap[key]; !ok {
				labels := map[string]string{"service": serviceName}
				if attrType != "default" {
					labels["type"] = attrType
				}
				seriesMap[key] = &api.TimeSeries{
					Name:       metricName,
					Labels:     labels,
					DataPoints: make([][2]float64, 0),
				}
			}
			seriesMap[key].DataPoints = append(seriesMap[key].DataPoints, [2]float64{
				float64(bucket.UnixMilli()),
				value,
			})
		}
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("iterating metric series: %w", err)
		}
	}

	series := make([]api.TimeSeries, 0, len(seriesMap))
	for _, s := range seriesMap {
		series = append(series, *s)
	}

	return &api.TimeSeriesResponse{Series: series}, nil
}

// metricTypeInfo holds cached metric type information for batch queries
type metricTypeInfo struct {
	metricType             string
	isMonotonic            sql.NullBool
	aggregationTemporality sql.NullInt32
}

// QueryBatchMetricSeries executes multiple metric series queries in parallel
func (s *DuckDBStore) QueryBatchMetricSeries(ctx context.Context, queries []api.MetricQuery, from, to time.Time, intervalSeconds int64) *api.BatchMetricSeriesResponse {
	if len(queries) == 0 {
		return &api.BatchMetricSeriesResponse{Results: []api.MetricQueryResult{}}
	}

	// Pre-fetch metric types for all unique metric names (batched)
	metricTypes := s.batchGetMetricTypes(ctx, queries)

	// Execute queries in parallel
	results := make([]api.MetricQueryResult, len(queries))
	var wg sync.WaitGroup

	for i, query := range queries {
		wg.Add(1)
		go func(idx int, q api.MetricQuery) {
			defer wg.Done()

			result := api.MetricQueryResult{ID: q.ID}

			// Get cached metric type info
			typeInfo, ok := metricTypes[q.Name]
			if !ok {
				// No data found for this metric - return empty series
				result.Success = true
				result.Series = []api.TimeSeries{}
				results[idx] = result
				return
			}

			// Execute the query using internal method
			resp, err := s.queryMetricSeriesInternal(ctx, q.Name, q.Service, from, to, intervalSeconds, q.Aggregate, typeInfo)
			if err != nil {
				result.Success = false
				result.Error = err.Error()
			} else {
				result.Success = true
				result.Series = resp.Series
			}
			results[idx] = result
		}(i, query)
	}
	wg.Wait()

	return &api.BatchMetricSeriesResponse{Results: results}
}

// batchGetMetricTypes fetches type info for multiple metrics in a single query
func (s *DuckDBStore) batchGetMetricTypes(ctx context.Context, queries []api.MetricQuery) map[string]metricTypeInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Get unique metric names
	nameSet := make(map[string]struct{})
	for _, q := range queries {
		nameSet[q.Name] = struct{}{}
	}

	result := make(map[string]metricTypeInfo)

	if len(nameSet) == 0 {
		return result
	}

	// Build list of unique names
	names := make([]string, 0, len(nameSet))
	for name := range nameSet {
		names = append(names, name)
	}

	// Build query with placeholders
	placeholders := make([]string, len(names))
	args := make([]interface{}, len(names))
	for i, name := range names {
		placeholders[i] = "?"
		args[i] = name
	}

	// Query all metric types at once using a subquery to get one row per metric name
	query := fmt.Sprintf(`
		SELECT MetricName, MetricType, IsMonotonic, AggregationTemporality
		FROM (
			SELECT MetricName, MetricType, IsMonotonic, AggregationTemporality,
				   ROW_NUMBER() OVER (PARTITION BY MetricName ORDER BY Timestamp DESC) as rn
			FROM otel_metrics
			WHERE MetricName IN (%s)
		) sub
		WHERE rn = 1
	`, strings.Join(placeholders, ", "))

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return result
	}
	defer rows.Close()

	for rows.Next() {
		var name, metricType string
		var isMonotonic sql.NullBool
		var aggTemp sql.NullInt32
		if err := rows.Scan(&name, &metricType, &isMonotonic, &aggTemp); err != nil {
			continue
		}
		result[name] = metricTypeInfo{
			metricType:             metricType,
			isMonotonic:            isMonotonic,
			aggregationTemporality: aggTemp,
		}
	}

	return result
}

// queryMetricSeriesInternal is the core query logic, using pre-fetched type info
func (s *DuckDBStore) queryMetricSeriesInternal(ctx context.Context, metricName, service string, from, to time.Time, intervalSeconds int64, aggregate bool, typeInfo metricTypeInfo) (*api.TimeSeriesResponse, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Format times as strings to avoid timezone issues with DuckDB's TIMESTAMP type
	fromStr := formatTimeForDB(from)
	toStr := formatTimeForDB(to)

	// OTLP AggregationTemporality: 0=UNSPECIFIED, 1=DELTA, 2=CUMULATIVE
	isCumulative := typeInfo.aggregationTemporality.Valid && typeInfo.aggregationTemporality.Int32 == 2

	// Determine aggregation function based on metric type and mode
	// Use COALESCE(Value, Sum) to handle both gauge/sum (Value) and histogram (Sum) metrics
	var aggFunction string
	if aggregate {
		// Scalar aggregation over entire time range
		switch typeInfo.metricType {
		case "gauge":
			aggFunction = "AVG(COALESCE(Value, Sum))"
		case "sum":
			if isCumulative {
				aggFunction = "(MAX(COALESCE(Value, Sum)) - MIN(COALESCE(Value, Sum)))"
			} else {
				aggFunction = "SUM(COALESCE(Value, Sum))"
			}
		case "histogram", "exp_histogram":
			aggFunction = "SUM(Sum)"
		default:
			aggFunction = "SUM(COALESCE(Value, Sum))"
		}
	} else {
		// Time-bucketed aggregation
		switch typeInfo.metricType {
		case "gauge":
			aggFunction = "AVG(COALESCE(Value, Sum))"
		case "sum":
			if isCumulative {
				aggFunction = "arg_max(COALESCE(Value, Sum), Timestamp)"
			} else if typeInfo.isMonotonic.Valid && typeInfo.isMonotonic.Bool {
				aggFunction = "SUM(COALESCE(Value, Sum))"
			} else {
				aggFunction = "SUM(COALESCE(Value, Sum))"
			}
		case "histogram", "exp_histogram":
			aggFunction = "SUM(Sum)"
		default:
			aggFunction = "SUM(COALESCE(Value, Sum))"
		}
	}

	var query string
	args := []interface{}{fromStr, toStr, metricName}

	if aggregate {
		// Check multiple attribute keys for type breakdown (type, gen_ai.token.type)
		query = fmt.Sprintf(`
			SELECT
				ServiceName,
				COALESCE(Attributes->>'type', Attributes->>'gen_ai.token.type', 'default') as attr_type,
				%s as agg_value
			FROM otel_metrics
			WHERE Timestamp >= ?::TIMESTAMP AND Timestamp <= ?::TIMESTAMP
				AND MetricName = ?
				AND (Value IS NOT NULL OR Sum IS NOT NULL)
		`, aggFunction)

		if service != "" {
			query += " AND ServiceName = ?"
			args = append(args, service)
		}

		query += " GROUP BY ServiceName, attr_type"
	} else {
		// Construct interval string from seconds (e.g., "60 seconds")
		intervalStr := fmt.Sprintf("%d seconds", intervalSeconds)

		// Build service filter for the query
		serviceFilter := ""
		if service != "" {
			serviceFilter = " AND ServiceName = ?"
			args = append(args, service)
		}

		// Use CTEs with generate_series to create all time buckets and LEFT JOIN with data
		// This ensures all buckets are returned, with zeros for missing data
		// Note: generate_series returns an array in DuckDB, so we use UNNEST to expand it
		query = fmt.Sprintf(`
			WITH buckets AS (
				SELECT UNNEST(generate_series(
					time_bucket(INTERVAL '%[1]s', ?::TIMESTAMP),
					time_bucket(INTERVAL '%[1]s', ?::TIMESTAMP),
					INTERVAL '%[1]s'
				)) as bucket
			),
			series_labels AS (
				SELECT DISTINCT
					ServiceName,
					COALESCE(Attributes->>'type', Attributes->>'gen_ai.token.type', 'default') as attr_type
				FROM otel_metrics
				WHERE Timestamp >= ?::TIMESTAMP AND Timestamp <= ?::TIMESTAMP
					AND MetricName = ?
					AND (Value IS NOT NULL OR Sum IS NOT NULL)
					%[3]s
			),
			data AS (
				SELECT
					time_bucket(INTERVAL '%[1]s', Timestamp) as bucket,
					ServiceName,
					COALESCE(Attributes->>'type', Attributes->>'gen_ai.token.type', 'default') as attr_type,
					%[2]s as agg_value
				FROM otel_metrics
				WHERE Timestamp >= ?::TIMESTAMP AND Timestamp <= ?::TIMESTAMP
					AND MetricName = ?
					AND (Value IS NOT NULL OR Sum IS NOT NULL)
					%[3]s
				GROUP BY bucket, ServiceName, attr_type
			)
			SELECT
				b.bucket,
				s.ServiceName,
				s.attr_type,
				COALESCE(d.agg_value, 0) as agg_value
			FROM buckets b
			CROSS JOIN series_labels s
			LEFT JOIN data d ON b.bucket = d.bucket
				AND s.ServiceName = d.ServiceName
				AND s.attr_type = d.attr_type
			ORDER BY b.bucket, s.ServiceName, s.attr_type
		`, intervalStr, aggFunction, serviceFilter)

		// Update args: buckets CTE needs from, to; series_labels needs from, to, metricName, [service]; data needs from, to, metricName, [service]
		if service != "" {
			args = []interface{}{fromStr, toStr, fromStr, toStr, metricName, service, fromStr, toStr, metricName, service}
		} else {
			args = []interface{}{fromStr, toStr, fromStr, toStr, metricName, fromStr, toStr, metricName}
		}
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying metric series: %w", err)
	}
	defer rows.Close()

	seriesMap := make(map[string]*api.TimeSeries)

	if aggregate {
		for rows.Next() {
			var serviceName string
			var attrType string
			var value float64

			if err := rows.Scan(&serviceName, &attrType, &value); err != nil {
				return nil, fmt.Errorf("scanning metric aggregate: %w", err)
			}

			key := serviceName + ":" + attrType
			labels := map[string]string{"service": serviceName}
			if attrType != "default" {
				labels["type"] = attrType
			}
			seriesMap[key] = &api.TimeSeries{
				Name:       metricName,
				Labels:     labels,
				DataPoints: [][2]float64{{0, value}},
			}
		}
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("iterating metric aggregates: %w", err)
		}
	} else {
		for rows.Next() {
			var bucket time.Time
			var serviceName string
			var attrType string
			var value float64

			if err := rows.Scan(&bucket, &serviceName, &attrType, &value); err != nil {
				return nil, fmt.Errorf("scanning metric series: %w", err)
			}

			key := serviceName + ":" + attrType
			if _, ok := seriesMap[key]; !ok {
				labels := map[string]string{"service": serviceName}
				if attrType != "default" {
					labels["type"] = attrType
				}
				seriesMap[key] = &api.TimeSeries{
					Name:       metricName,
					Labels:     labels,
					DataPoints: make([][2]float64, 0),
				}
			}
			seriesMap[key].DataPoints = append(seriesMap[key].DataPoints, [2]float64{
				float64(bucket.UnixMilli()),
				value,
			})
		}
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("iterating metric series: %w", err)
		}
	}

	series := make([]api.TimeSeries, 0, len(seriesMap))
	for _, s := range seriesMap {
		series = append(series, *s)
	}

	return &api.TimeSeriesResponse{Series: series}, nil
}

// Helper functions for nullable types
func nullFloat64(f *float64) sql.NullFloat64 {
	if f == nil {
		return sql.NullFloat64{}
	}
	return sql.NullFloat64{Float64: *f, Valid: true}
}

func nullInt32(i *int32) sql.NullInt32 {
	if i == nil {
		return sql.NullInt32{}
	}
	return sql.NullInt32{Int32: *i, Valid: true}
}

func nullBool(b *bool) sql.NullBool {
	if b == nil {
		return sql.NullBool{}
	}
	return sql.NullBool{Bool: *b, Valid: true}
}

func nullUint64(u *uint64) sql.NullInt64 {
	if u == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: int64(*u), Valid: true}
}

func uint64ArrayToString(arr []uint64) string {
	if len(arr) == 0 {
		return "[]"
	}
	result := "["
	for i, v := range arr {
		if i > 0 {
			result += ", "
		}
		result += fmt.Sprintf("%d", v)
	}
	return result + "]"
}

func float64ArrayToString(arr []float64) string {
	if len(arr) == 0 {
		return "[]"
	}
	result := "["
	for i, v := range arr {
		if i > 0 {
			result += ", "
		}
		result += fmt.Sprintf("%f", v)
	}
	return result + "]"
}

// GetLatestMetricValue looks up the most recent value for a metric series.
// Used for cumulative-to-delta conversion at ingestion time.
// Returns the value and true if found, or 0 and false if not found.
func (s *DuckDBStore) GetLatestMetricValue(ctx context.Context, metricName, serviceName string, attributes map[string]string) (float64, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Build query to find the latest value matching the exact attributes
	// Use json_extract_string for reliable JSON key extraction in DuckDB
	query := `
		SELECT Value
		FROM otel_metrics
		WHERE MetricName = ?
			AND ServiceName = ?
			AND Value IS NOT NULL
	`
	args := []interface{}{metricName, serviceName}

	// Add attribute filters using json_extract_string for reliable extraction
	for k, v := range attributes {
		query += fmt.Sprintf(" AND CAST(json_extract_string(Attributes, '$.%s') AS VARCHAR) = ?", k)
		args = append(args, v)
	}

	query += " ORDER BY Timestamp DESC LIMIT 1"

	var value float64
	err := s.db.QueryRowContext(ctx, query, args...).Scan(&value)
	if err != nil {
		logger.Debug("GetLatestMetricValue: no previous value", "metric", metricName, "service", serviceName, "attrs", attributes, "error", err)
		return 0, false
	}

	logger.Debug("GetLatestMetricValue: found previous value", "value", value, "metric", metricName, "service", serviceName, "attrs", attributes)
	return value, true
}
