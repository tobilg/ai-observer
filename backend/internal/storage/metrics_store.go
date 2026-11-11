package storage

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"strings"
	"time"

	"github.com/tobilg/ai-observer/internal/api"
	"github.com/tobilg/ai-observer/internal/otlp"
	"github.com/tobilg/ai-observer/internal/tools"
)

// isCodexPerEventUsageMetric reports Codex's derived usage metrics, which
// hold one value per response, not running totals
func isCodexPerEventUsageMetric(metricName string) bool {
	return metricName == otlp.CodexTokenUsageMetric || metricName == otlp.CodexCostUsageMetric
}

func (s *DuckDBStore) InsertMetrics(ctx context.Context, metrics []api.MetricDataPoint) error {
	if len(metrics) == 0 {
		return nil
	}

	return s.inWriteTx(ctx, func(w *writeTx) error {
		if err := insertMetricsTx(ctx, w, metrics, ""); err != nil {
			return err
		}
		// A session's token metrics complete what OTLP recorded of its
		// requests: count them only once (usage_overlap.go)
		_, err := reconcileUsage(ctx, w, nil, metrics, false)
		return err
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
	where.addService(service)
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

	metrics := []api.MetricDataPoint{} // clients read a list
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

	if cond, serviceArgs := serviceCondition(service); cond != "" {
		query += " WHERE " + cond
		args = append(args, serviceArgs...)
	}

	query += " ORDER BY MetricName"

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying metric names: %w", err)
	}
	defer rows.Close()

	names := []string{} // clients read a list
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
	if cond, serviceArgs := serviceCondition(service); cond != "" {
		filter += " AND " + cond
		args = append(args, serviceArgs...)
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

	values := []string{} // clients read a list
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

// GetMetricAttributeKeys returns the attribute keys of a metric's points,
// optionally of one service: the keys series can be grouped and filtered by
// (keys of other characters are left out). Like GetBreakdownValues it is not
// time-filtered and reads the distinct attribute sets, not every point.
func (s *DuckDBStore) GetMetricAttributeKeys(ctx context.Context, metricName, service string) ([]string, error) {
	var where conditions
	where.add("MetricName = ?", seriesSourceMetric(metricName))
	where.addService(service)
	query := `
		SELECT DISTINCT key FROM (
			SELECT UNNEST(json_keys(attrs)) AS key
			FROM (SELECT DISTINCT CAST(Attributes AS VARCHAR) AS attrs FROM otel_metrics` + where.clause("WHERE") + `)
		)
		ORDER BY key`
	rows, err := s.db.QueryContext(ctx, query, where.args...)
	if err != nil {
		return nil, fmt.Errorf("querying metric attribute keys: %w", err)
	}
	defer rows.Close()

	keys := []string{}
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, fmt.Errorf("scanning metric attribute key: %w", err)
		}
		if jsonAttributeKeyPattern.MatchString(key) {
			keys = append(keys, key)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating metric attribute keys: %w", err)
	}
	return keys, nil
}

// QueryMetricSeries returns the series of one metric with UTC buckets, by
// type (bucketed) or by type and model (aggregate)
func (s *DuckDBStore) QueryMetricSeries(ctx context.Context, metricName, service string, from, to time.Time, intervalSeconds int64, aggregate bool) (*api.TimeSeriesResponse, error) {
	return s.QuerySeries(ctx, api.MetricQuery{Name: metricName, Service: service, Aggregate: aggregate},
		SeriesRange{From: from, To: to, Interval: intervalSeconds})
}

// QueryMetricTotals returns the totals of a metric in [from, to] by service,
// type and the attribute groupBy (the model when ""), with cumulative
// counters turned into increases like series
func (s *DuckDBStore) QueryMetricTotals(ctx context.Context, metricName, service string, from, to time.Time, groupBy string) (*api.TimeSeriesResponse, error) {
	q := api.MetricQuery{Name: metricName, Service: service, Aggregate: true}
	sq, err := resolveSeriesQuery(q)
	if err != nil {
		return nil, err
	}
	if groupBy != "" {
		group, err := attributeGroup(groupBy)
		if err != nil {
			return nil, err
		}
		sq.groups = []groupSpec{typeGroup, group}
	}
	types, err := s.metricTypes(ctx, []api.MetricQuery{q}, from, to)
	if err != nil {
		return nil, err
	}
	return s.runSeriesQuery(ctx, sq, SeriesRange{From: from, To: to}, types)
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

// QueryBatchMetricSeries runs several series queries with UTC buckets
// (QuerySeriesBatch)
func (s *DuckDBStore) QueryBatchMetricSeries(ctx context.Context, queries []api.MetricQuery, from, to time.Time, intervalSeconds int64) *api.BatchMetricSeriesResponse {
	return s.QuerySeriesBatch(ctx, queries, SeriesRange{From: from, To: to, Interval: intervalSeconds})
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
	latest := make(map[metricTypeKey]time.Time)
	// merge adds a service's points to the info of key: the type of the
	// latest points, cumulative when any are
	merge := func(key metricTypeKey, info metricTypeInfo, last time.Time) {
		merged := types[key]
		if last.After(latest[key]) {
			latest[key] = last
			merged.metricType = info.metricType
		}
		merged.cumulative = merged.cumulative || info.cumulative
		types[key] = merged
	}
	perService := make(map[metricTypeKey]metricTypeInfo)
	perServiceLast := make(map[metricTypeKey]time.Time)
	for rows.Next() {
		var name, service, metricType string
		var cumulative sql.NullBool
		var last time.Time
		if err := rows.Scan(&name, &service, &metricType, &cumulative, &last); err != nil {
			return nil, fmt.Errorf("scanning metric type: %w", err)
		}
		cumulative.Bool = cumulative.Bool && !isCodexPerEventUsageMetric(name)
		info := metricTypeInfo{metricType: metricType, cumulative: cumulative.Bool}
		key := metricTypeKey{name, service}
		types[key], perService[key], perServiceLast[key] = info, info, last
		merge(metricTypeKey{name, ""}, info, last)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating metric types: %w", err)
	}

	// Per tool filter (codex), over the tool's services (serviceCondition)
	for _, q := range queries {
		services := tools.FilterServices(q.Service)
		if len(services) == 0 || (len(services) == 1 && services[0] == q.Service) {
			continue
		}
		name := seriesSourceMetric(q.Name)
		filterKey := metricTypeKey{name, q.Service}
		if _, done := latest[filterKey]; done {
			continue
		}
		for _, service := range services {
			if info, ok := perService[metricTypeKey{name, service}]; ok {
				merge(filterKey, info, perServiceLast[metricTypeKey{name, service}])
			}
		}
	}
	return types, nil
}

// seriesBucketSeconds returns the bucket size for a time-bucketed series: the
// requested interval (DefaultSeriesInterval when not positive, at most
// MaxSeriesInterval), made coarser when the range would need too many
// buckets
func seriesBucketSeconds(from, to time.Time, intervalSeconds int64) int64 {
	if intervalSeconds <= 0 {
		intervalSeconds = DefaultSeriesInterval
	}
	intervalSeconds = min(intervalSeconds, MaxSeriesInterval)
	rangeSeconds := int64(to.Sub(from).Seconds())
	if minInterval := (rangeSeconds + maxSeriesBuckets - 1) / maxSeriesBuckets; minInterval > intervalSeconds {
		return minInterval
	}
	return intervalSeconds
}
