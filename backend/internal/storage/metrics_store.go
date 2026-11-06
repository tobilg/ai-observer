package storage

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/tobilg/ai-observer/internal/api"
	"github.com/tobilg/ai-observer/internal/logger"
)

func isCodexPerEventUsageMetric(metricName string) bool {
	return metricName == "codex_cli_rs.token.usage" || metricName == "codex_cli_rs.cost.usage"
}

func (s *DuckDBStore) InsertMetrics(ctx context.Context, metrics []api.MetricDataPoint) error {
	if len(metrics) == 0 {
		return nil
	}

	return s.inWriteTx(ctx, func(w *writeTx) error {
		return insertMetricsTx(ctx, w, metrics, "")
	})
}

var metricColumns = []string{
	"Timestamp", "ServiceName", "MetricName", "MetricDescription", "MetricUnit",
	"ResourceAttributes", "ScopeName", "ScopeVersion", "Attributes", "MetricType",
	"Value", "AggregationTemporality", "IsMonotonic", "Count", "Sum",
	"BucketCounts", "ExplicitBounds", "Scale", "ZeroCount", "PositiveOffset",
	"PositiveBucketCounts", "NegativeOffset", "NegativeBucketCounts",
	"QuantileValues", "QuantileQuantiles", "Min", "Max", "SourceFile",
}

func insertMetricsTx(ctx context.Context, w *writeTx, metrics []api.MetricDataPoint, sourceFile string) error {
	if err := addMetricNames(ctx, w.tx, metrics); err != nil {
		return err
	}
	return w.appendRows(ctx, "otel_metrics", metricColumns, len(metrics), func(i int) []driver.Value {
		m := metrics[i]
		// Deterministic, so the points of a series keep equal attributes
		m.Attributes = w.redact.Map(m.Attributes)
		m.ResourceAttributes = w.redactShared(m.ResourceAttributes)
		return []driver.Value{
			m.Timestamp,
			m.ServiceName,
			m.MetricName,
			nullableString(m.MetricDescription),
			nullableString(m.MetricUnit),
			rawJSON(mapToString(m.ResourceAttributes)),
			nullableString(m.ScopeName),
			nullableString(m.ScopeVersion),
			rawJSON(mapToString(m.Attributes)),
			m.MetricType,
			nullablePtr(m.Value),
			nullablePtr(m.AggregationTemporality),
			nullablePtr(m.IsMonotonic),
			nullablePtr(m.Count),
			nullablePtr(m.Sum),
			rawJSON(uint64ArrayToString(m.BucketCounts)),
			rawJSON(float64ArrayToString(m.ExplicitBounds)),
			nullablePtr(m.Scale),
			nullablePtr(m.ZeroCount),
			nullablePtr(m.PositiveOffset),
			rawJSON(uint64ArrayToString(m.PositiveBucketCounts)),
			nullablePtr(m.NegativeOffset),
			rawJSON(uint64ArrayToString(m.NegativeBucketCounts)),
			rawJSON(float64ArrayToString(m.QuantileValues)),
			rawJSON(float64ArrayToString(m.QuantileQuantiles)),
			nullablePtr(m.Min),
			nullablePtr(m.Max),
			nullableString(sourceFile),
		}
	})
}

// addMetricNames records the services and names of metrics in metric_names
func addMetricNames(ctx context.Context, execer execContexter, metrics []api.MetricDataPoint) error {
	type name struct{ service, metric string }
	seen := make(map[name]bool)
	var args []any
	for _, m := range metrics {
		n := name{m.ServiceName, m.MetricName}
		if !seen[n] {
			seen[n] = true
			args = append(args, n.service, n.metric)
		}
	}
	if len(args) == 0 {
		return nil
	}
	query := "INSERT OR IGNORE INTO metric_names VALUES (?, ?)" + strings.Repeat(", (?, ?)", len(args)/2-1)
	if _, err := execer.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("recording metric names: %w", err)
	}
	return nil
}

// pruneMetricNames removes the names of metrics that have no points left
func pruneMetricNames(ctx context.Context, execer execContexter) error {
	_, err := execer.ExecContext(ctx, `
		DELETE FROM metric_names WHERE NOT EXISTS (
			SELECT 1 FROM otel_metrics m
			WHERE m.ServiceName = metric_names.ServiceName AND m.MetricName = metric_names.MetricName
		)`)
	if err != nil {
		return fmt.Errorf("pruning metric names: %w", err)
	}
	return nil
}

// QueryMetrics returns a page of the metric points in [from, to] matching
// the filters, newest first
func (s *DuckDBStore) QueryMetrics(ctx context.Context, service, metricName, metricType string, from, to time.Time, page Page) (*api.MetricsResponse, error) {
	var where conditions
	where.add("Timestamp >= ?::TIMESTAMP AND Timestamp <= ?::TIMESTAMP", formatTimeForDB(from), formatTimeForDB(to))
	if service != "" {
		where.add("ServiceName = ?", service)
	}
	if metricName != "" {
		// A ".delta" alias lists the points of its counter
		where.add("MetricName = ?", seriesSourceMetric(metricName))
	}
	if metricType != "" {
		where.add("MetricType = ?", metricType)
	}

	limit, offset, err := pageLimit(page)
	if err != nil {
		return nil, err
	}

	var total int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM otel_metrics"+where.clause("WHERE"), where.args...).Scan(&total); err != nil {
		return nil, fmt.Errorf("counting metrics: %w", err)
	}

	pageWhere := where
	if page.Cursor != "" {
		c, err := decodeCursor(page.Cursor, 0)
		if err != nil {
			return nil, err
		}
		cond, args := newestFirstAfter("Timestamp", []string{"rowid"}, c.Time, []any{c.RowID})
		pageWhere = where.and(cond, args...)
	}
	query := `
		SELECT
			rowid, Timestamp, ServiceName, MetricName, MetricDescription, MetricUnit,
			ResourceAttributes, ScopeName, ScopeVersion, Attributes, MetricType,
			Value, AggregationTemporality, IsMonotonic, Count, Sum,
			Min, Max
		FROM otel_metrics` + pageWhere.clause("WHERE") + `
		ORDER BY Timestamp DESC, rowid DESC
		LIMIT ? OFFSET ?`

	rows, err := s.db.QueryContext(ctx, query, append(pageWhere.args, limit+1, offset)...)
	if err != nil {
		return nil, fmt.Errorf("querying metrics: %w", err)
	}
	defer rows.Close()

	var metrics []api.MetricDataPoint
	var rowIDs []int64
	for rows.Next() {
		var m api.MetricDataPoint
		var rowID int64
		var desc, unit, scopeName, scopeVersion sql.NullString
		var resourceAttrs, attrs interface{}
		var value, sum, min, max sql.NullFloat64
		var aggregationTemporality sql.NullInt32
		var isMonotonic sql.NullBool
		var count sql.NullInt64

		if err := rows.Scan(
			&rowID, &m.Timestamp, &m.ServiceName, &m.MetricName, &desc, &unit,
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
		rowIDs = append(rowIDs, rowID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating metrics: %w", err)
	}

	resp := &api.MetricsResponse{Metrics: metrics, Total: total}
	if len(metrics) > limit {
		resp.Metrics = metrics[:limit]
		resp.HasMore = true
		last := limit - 1
		resp.NextCursor = encodeCursor(pageCursor{Time: formatTimeForDB(metrics[last].Timestamp), RowID: rowIDs[last]})
	}
	return resp, nil
}

// GetMetricNames lists the names of the stored metrics, optionally of one
// service. The ".delta" aliases of earlier versions are not listed: their
// counters are, and return the same series.
func (s *DuckDBStore) GetMetricNames(ctx context.Context, service string) ([]string, error) {
	query := `
		SELECT DISTINCT MetricName
		FROM metric_names
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
	attributePath, err := metricAttributeJSONPath(attribute)
	if err != nil {
		return nil, err
	}

	// Build query to get distinct values for the specified attribute
	// Uses the MetricName index first, then extracts JSON attribute values
	// Use json_extract_string for reliable JSON text extraction
	// Points share few attribute sets (one per series), so the values are
	// extracted from the distinct sets instead of every point
	filter := "MetricName = ?"
	args := []interface{}{seriesSourceMetric(metricName)}
	if service != "" {
		filter += " AND ServiceName = ?"
		args = append(args, service)
	}
	query := fmt.Sprintf(`
		SELECT DISTINCT json_extract_string(attrs, '%s') AS attr_value
		FROM (SELECT DISTINCT CAST(Attributes AS VARCHAR) AS attrs FROM otel_metrics WHERE %s)
		WHERE json_extract_string(attrs, '%s') IS NOT NULL
		ORDER BY attr_value`, attributePath, filter, attributePath)

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

// QueryMetricSeries returns the series of one metric
func (s *DuckDBStore) QueryMetricSeries(ctx context.Context, metricName, service string, from, to time.Time, intervalSeconds int64, aggregate bool) (*api.TimeSeriesResponse, error) {
	return s.queryOneMetric(ctx, api.MetricQuery{Name: metricName, Service: service, Aggregate: aggregate}, from, to, intervalSeconds, "")
}

// QueryMetricTotals returns the totals of a metric in [from, to] by service,
// type and the attribute groupBy (the model when ""), with cumulative
// counters turned into increases like series
func (s *DuckDBStore) QueryMetricTotals(ctx context.Context, metricName, service string, from, to time.Time, groupBy string) (*api.TimeSeriesResponse, error) {
	return s.queryOneMetric(ctx, api.MetricQuery{Name: metricName, Service: service, Aggregate: true}, from, to, 0, groupBy)
}

// queryOneMetric reads the points' type and then the series of one metric
func (s *DuckDBStore) queryOneMetric(ctx context.Context, query api.MetricQuery, from, to time.Time, intervalSeconds int64, groupBy string) (*api.TimeSeriesResponse, error) {
	types, err := s.metricTypes(ctx, []api.MetricQuery{query}, from, to)
	if err != nil {
		return nil, err
	}
	typeInfo, ok := types[metricTypeKey{seriesSourceMetric(query.Name), query.Service}]
	if !ok {
		return &api.TimeSeriesResponse{Series: []api.TimeSeries{}}, nil
	}
	return s.queryMetricSeriesInternal(ctx, query, from, to, intervalSeconds, typeInfo, groupBy)
}

// metricTypeInfo describes the points of a metric in a query's time range
type metricTypeInfo struct {
	metricType string
	// cumulative is set when some points have CUMULATIVE temporality, so
	// increases must be computed per series
	cumulative bool
}

// metricTypeKey identifies the points a query reads: a metric, optionally of
// one service
type metricTypeKey struct {
	name, service string
}

// maxSeriesBuckets bounds the buckets of a time-bucketed series. Longer
// ranges get a coarser interval instead of millions of buckets.
const maxSeriesBuckets = 2000

// cumulativeLookback is how far before a query's start the previous point of
// a cumulative series is looked for. Exporters report cumulative series every
// export interval, so a series without a point in this window is new and its
// first point counts in full.
const cumulativeLookback = 24 * time.Hour

// maxConcurrentSeriesQueries bounds the queries of a batch that run at once.
// DuckDB already parallelizes each query, so more would only compete.
const maxConcurrentSeriesQueries = 4

// QueryBatchMetricSeries executes multiple metric series queries in parallel
func (s *DuckDBStore) QueryBatchMetricSeries(ctx context.Context, queries []api.MetricQuery, from, to time.Time, intervalSeconds int64) *api.BatchMetricSeriesResponse {
	if len(queries) == 0 {
		return &api.BatchMetricSeriesResponse{Results: []api.MetricQueryResult{}}
	}

	results := make([]api.MetricQueryResult, len(queries))
	types, err := s.metricTypes(ctx, queries, from, to)
	if err != nil {
		logger.Error("Batch metric type lookup failed", "error", err)
		for i, q := range queries {
			results[i] = api.MetricQueryResult{ID: q.ID, Error: queryErrorMessage(err)}
		}
		return &api.BatchMetricSeriesResponse{Results: results}
	}

	var wg sync.WaitGroup
	sem := make(chan struct{}, maxConcurrentSeriesQueries)
	for i, q := range queries {
		typeInfo, ok := types[metricTypeKey{seriesSourceMetric(q.Name), q.Service}]
		if !ok {
			// No data in the range
			results[i] = api.MetricQueryResult{ID: q.ID, Success: true, Series: []api.TimeSeries{}}
			continue
		}

		wg.Add(1)
		go func(idx int, q api.MetricQuery) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			result := api.MetricQueryResult{ID: q.ID}
			resp, err := s.queryMetricSeriesInternal(ctx, q, from, to, intervalSeconds, typeInfo, "")
			if err != nil {
				logger.Error("Batch metric series query failed", "id", q.ID, "metric", q.Name, "error", err)
				result.Error = queryErrorMessage(err)
			} else {
				result.Success = true
				result.Series = resp.Series
			}
			results[idx] = result
		}(i, q)
	}
	wg.Wait()

	return &api.BatchMetricSeriesResponse{Results: results}
}

// queryErrorMessage is the error reported for a failed batch query
func queryErrorMessage(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "query timed out"
	}
	return "failed to query metric series"
}

// metricTypes looks up the type and temporality of the points each query
// reads within the time range, keyed by the stored metric name
// (seriesSourceMetric). Queries without points are not in the result.
func (s *DuckDBStore) metricTypes(ctx context.Context, queries []api.MetricQuery, from, to time.Time) (map[metricTypeKey]metricTypeInfo, error) {
	names := make([]any, 0, len(queries))
	seen := make(map[string]bool)
	for _, q := range queries {
		name := seriesSourceMetric(q.Name)
		if !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}

	query := `
		SELECT MetricName, ServiceName, arg_max(MetricType, Timestamp), bool_or(AggregationTemporality = 2), MAX(Timestamp)
		FROM otel_metrics
		WHERE MetricName IN (?` + strings.Repeat(", ?", len(names)-1) + `)
			AND Timestamp >= ?::TIMESTAMP AND Timestamp <= ?::TIMESTAMP
		GROUP BY MetricName, ServiceName
	`
	args := append(names, formatTimeForDB(from), formatTimeForDB(to))
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying metric types: %w", err)
	}
	defer rows.Close()

	// Per metric and service, and per metric across all services (service "")
	types := make(map[metricTypeKey]metricTypeInfo)
	latest := make(map[string]time.Time)
	for rows.Next() {
		var name, service, metricType string
		var cumulative sql.NullBool
		var last time.Time
		if err := rows.Scan(&name, &service, &metricType, &cumulative, &last); err != nil {
			return nil, fmt.Errorf("scanning metric type: %w", err)
		}
		cumulative.Bool = cumulative.Bool && !isCodexPerEventUsageMetric(name)
		types[metricTypeKey{name, service}] = metricTypeInfo{metricType: metricType, cumulative: cumulative.Bool}

		all := types[metricTypeKey{name, ""}]
		if last.After(latest[name]) {
			latest[name] = last
			all.metricType = metricType
		}
		all.cumulative = all.cumulative || cumulative.Bool
		types[metricTypeKey{name, ""}] = all
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating metric types: %w", err)
	}
	return types, nil
}

// seriesBucketSeconds returns the bucket size for a time-bucketed series: the
// requested interval, made coarser when the range would need too many buckets
func seriesBucketSeconds(from, to time.Time, intervalSeconds int64) int64 {
	intervalSeconds = normalizeIntervalSeconds(intervalSeconds)
	rangeSeconds := int64(to.Sub(from).Seconds())
	if minInterval := (rangeSeconds + maxSeriesBuckets - 1) / maxSeriesBuckets; minInterval > intervalSeconds {
		return minInterval
	}
	return intervalSeconds
}

// queryMetricSeriesInternal is the core query logic, using pre-fetched type
// info. A ".delta" alias reads its cumulative counter (deltaAliases); the
// series keep the requested name. Aggregates are grouped by the attribute
// groupBy, or by the model when it is "".
func (s *DuckDBStore) queryMetricSeriesInternal(ctx context.Context, q api.MetricQuery, from, to time.Time, intervalSeconds int64, typeInfo metricTypeInfo, groupBy string) (*api.TimeSeriesResponse, error) {
	fromStr := formatTimeForDB(from)
	toStr := formatTimeForDB(to)

	// The value of a point: histograms keep their sum in Sum
	valueExpr := "COALESCE(Value, Sum)"
	if typeInfo.metricType == "histogram" || typeInfo.metricType == "exponential_histogram" {
		valueExpr = "Sum"
	}

	serviceFilter := ""
	if q.Service != "" {
		serviceFilter = " AND ServiceName = ?"
	}
	typeExpr := "COALESCE(NULLIF(Attributes->>'type', ''), NULLIF(Attributes->>'gen_ai.token.type', ''), 'default')"
	modelExpr := "COALESCE(NULLIF(Attributes->>'model', ''), 'default')"
	groupLabel := "model"
	if groupBy != "" {
		path, err := metricAttributeJSONPath(groupBy)
		if err != nil {
			return nil, err
		}
		modelExpr = "COALESCE(NULLIF(json_extract_string(Attributes, '" + path + "'), ''), 'default')"
		groupLabel = groupBy
	}

	// points holds one row per data point in [from, to] with its value v.
	// Cumulative points carry running totals, so v becomes the increase over
	// the previous point of the same series (identified by its attributes),
	// or the full value at a series start or reset. DELTA points are kept.
	var pointsCTE string
	var args []interface{}
	if typeInfo.cumulative && typeInfo.metricType != "gauge" {
		// Deleted points leave a baseline (metric_baselines), which serves as
		// the previous point of the series but is no point itself
		pointsCTE = `
			raw AS (
				SELECT Timestamp, ServiceName, attr_type, attr_model, v, cumulative, baseline,
					LAG(v) OVER (PARTITION BY ServiceName, attrs, resource_attrs ORDER BY Timestamp) AS prev
				FROM (
					SELECT Timestamp, ServiceName, CAST(Attributes AS VARCHAR) AS attrs,
						CAST(ResourceAttributes AS VARCHAR) AS resource_attrs,
						` + typeExpr + ` AS attr_type, ` + modelExpr + ` AS attr_model,
						` + valueExpr + ` AS v,
						AggregationTemporality = 2 AS cumulative, false AS baseline
					FROM otel_metrics
					WHERE MetricName = ?
						AND Timestamp >= ?::TIMESTAMP AND Timestamp <= ?::TIMESTAMP
						AND ` + valueExpr + ` IS NOT NULL` + serviceFilter + `
					UNION ALL
					SELECT Timestamp, ServiceName, Attributes, ResourceAttributes, NULL, NULL, Value, true, true
					FROM metric_baselines
					WHERE MetricName = ?
						AND Timestamp >= ?::TIMESTAMP AND Timestamp <= ?::TIMESTAMP` + serviceFilter + `
				)
			),
			points AS (
				SELECT Timestamp, ServiceName, attr_type, attr_model,
					CASE WHEN cumulative IS NOT TRUE THEN v
					     WHEN prev IS NULL OR v < prev THEN v
					     ELSE v - prev END AS v
				FROM raw
				WHERE Timestamp >= ?::TIMESTAMP AND NOT baseline
			)`
		source := []interface{}{seriesSourceMetric(q.Name), formatTimeForDB(from.Add(-cumulativeLookback)), toStr}
		if q.Service != "" {
			source = append(source, q.Service)
		}
		// The points and the baselines are read with the same filter
		points, baselines := source, source
		args = concatArgs(points, baselines, []any{fromStr})
	} else {
		pointsCTE = `
			points AS (
				SELECT Timestamp, ServiceName, ` + typeExpr + ` AS attr_type, ` + modelExpr + ` AS attr_model,
					` + valueExpr + ` AS v
				FROM otel_metrics
				WHERE MetricName = ?
					AND Timestamp >= ?::TIMESTAMP AND Timestamp <= ?::TIMESTAMP
					AND ` + valueExpr + ` IS NOT NULL` + serviceFilter + `
			)`
		args = []interface{}{seriesSourceMetric(q.Name), fromStr, toStr}
		if q.Service != "" {
			args = append(args, q.Service)
		}
	}

	// Gauges are levels and are averaged; counters and histograms add up
	aggFunction := "SUM(v)"
	if typeInfo.metricType == "gauge" {
		aggFunction = "AVG(v)"
	}

	var query string
	if q.Aggregate {
		// Scalar aggregation over the whole range
		query = `WITH ` + pointsCTE + `
			SELECT ServiceName, attr_type, attr_model, ` + aggFunction + ` AS agg_value
			FROM points
			GROUP BY ServiceName, attr_type, attr_model`
	} else {
		// Every bucket of the range is returned, with zeros for buckets without data
		interval := fmt.Sprintf("INTERVAL '%d seconds'", seriesBucketSeconds(from, to, intervalSeconds))
		query = `WITH ` + pointsCTE + `,
			buckets AS (
				SELECT UNNEST(generate_series(
					time_bucket(` + interval + `, ?::TIMESTAMP),
					time_bucket(` + interval + `, ?::TIMESTAMP),
					` + interval + `
				)) AS bucket
			),
			series_labels AS (
				SELECT DISTINCT ServiceName, attr_type FROM points
			),
			data AS (
				SELECT time_bucket(` + interval + `, Timestamp) AS bucket, ServiceName, attr_type, ` + aggFunction + ` AS agg_value
				FROM points
				GROUP BY bucket, ServiceName, attr_type
			)
			SELECT b.bucket, s.ServiceName, s.attr_type, COALESCE(d.agg_value, 0) AS agg_value
			FROM buckets b
			CROSS JOIN series_labels s
			LEFT JOIN data d ON b.bucket = d.bucket
				AND s.ServiceName = d.ServiceName
				AND s.attr_type = d.attr_type
			ORDER BY b.bucket, s.ServiceName, s.attr_type`
		args = append(args, fromStr, toStr)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying metric series: %w", err)
	}
	defer rows.Close()

	seriesMap := make(map[string]*api.TimeSeries)
	for rows.Next() {
		var bucket time.Time
		var serviceName, attrType string
		attrModel := "default"
		var value float64

		dest := []any{&serviceName, &attrType, &attrModel, &value}
		if !q.Aggregate {
			dest = []any{&bucket, &serviceName, &attrType, &value}
		}
		if err := rows.Scan(dest...); err != nil {
			return nil, fmt.Errorf("scanning metric series: %w", err)
		}

		key := serviceName + ":" + attrType + ":" + attrModel
		series, ok := seriesMap[key]
		if !ok {
			labels := map[string]string{"service": serviceName}
			if attrType != "default" {
				labels["type"] = attrType
			}
			if attrModel != "default" {
				labels[groupLabel] = attrModel
			}
			series = &api.TimeSeries{Name: q.Name, Labels: labels, DataPoints: make([][2]float64, 0)}
			seriesMap[key] = series
		}
		if q.Aggregate {
			series.DataPoints = append(series.DataPoints, [2]float64{0, value}) // timestamp 0 marks an aggregate
		} else {
			series.DataPoints = append(series.DataPoints, [2]float64{float64(bucket.UnixMilli()), value})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating metric series: %w", err)
	}

	// Sort for a stable legend and color order across refreshes
	keys := make([]string, 0, len(seriesMap))
	for key := range seriesMap {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	series := make([]api.TimeSeries, 0, len(keys))
	for _, key := range keys {
		series = append(series, *seriesMap[key])
	}

	return &api.TimeSeriesResponse{Series: series}, nil
}

// uint64ArrayToString serializes counts as a JSON array
func uint64ArrayToString(arr []uint64) string {
	if len(arr) == 0 {
		return "[]"
	}
	var b strings.Builder
	b.WriteByte('[')
	for i, v := range arr {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(strconv.FormatUint(v, 10))
	}
	b.WriteByte(']')
	return b.String()
}

// float64ArrayToString serializes values as a JSON array in their shortest
// exact form. JSON has no NaN or infinity, so those become null instead of
// failing the whole insert.
func float64ArrayToString(arr []float64) string {
	if len(arr) == 0 {
		return "[]"
	}
	var b strings.Builder
	b.WriteByte('[')
	for i, v := range arr {
		if i > 0 {
			b.WriteString(", ")
		}
		if math.IsNaN(v) || math.IsInf(v, 0) {
			b.WriteString("null")
			continue
		}
		// Like encoding/json: plain notation except for very small or large values
		format := byte('f')
		if abs := math.Abs(v); abs != 0 && (abs < 1e-6 || abs >= 1e21) {
			format = 'e'
		}
		b.WriteString(strconv.FormatFloat(v, format, -1, 64))
	}
	b.WriteByte(']')
	return b.String()
}
