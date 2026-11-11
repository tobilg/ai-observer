package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	// Zone data for time.LoadLocation on systems without a zone database
	// (Windows without Go installed)
	_ "time/tzdata"

	"github.com/tobilg/ai-observer/internal/api"
	"github.com/tobilg/ai-observer/internal/logger"
)

// Bucket sizes of series in seconds: the default and the largest accepted
const (
	DefaultSeriesInterval int64 = 60
	MaxSeriesInterval     int64 = 31 * 24 * 60 * 60
)

// Limits of a series query
const (
	// MaxGroupBy is the number of attributes series can be grouped by
	MaxGroupBy = 2
	// MaxFilters is the number of attribute filters of a query
	MaxFilters = 10
	// MaxSeries is the number of series a bucketed query may return; every
	// series has a point per bucket, so grouping by an attribute with many
	// values (session.id) would return millions
	MaxSeries = 100
)

// Statistics of histogram series (api.MetricQuery.Stat)
const (
	StatAvg   = "avg"
	StatSum   = "sum"
	StatCount = "count"
	StatMin   = "min"
	StatMax   = "max"
	StatP50   = "p50"
	StatP90   = "p90"
	StatP95   = "p95"
	StatP99   = "p99"
)

// HistogramStats lists the statistics of histogram series. avg is the
// default: most histograms are durations or sizes, whose sum per bucket
// grows with traffic, while the mean (sum of the values over their count)
// is exact, unlike the percentiles, which are estimated from the buckets.
var HistogramStats = []string{StatAvg, StatSum, StatCount, StatMin, StatMax, StatP50, StatP90, StatP95, StatP99}

var statQuantiles = map[string]float64{StatP50: 0.5, StatP90: 0.9, StatP95: 0.95, StatP99: 0.99}

// MetricQueryError is an invalid metric query. Code is one of the
// api.MetricError* codes; Message names the parameter in the request's terms.
type MetricQueryError struct {
	Code    string
	Message string
}

func (e *MetricQueryError) Error() string { return e.Message }

func queryError(code, format string, args ...any) error {
	return &MetricQueryError{Code: code, Message: fmt.Sprintf(format, args...)}
}

// SeriesRange is the time range and bucketing of series queries
type SeriesRange struct {
	From, To time.Time
	// Interval is the bucket size in seconds; a range that would need more
	// than maxSeriesBuckets buckets gets a coarser one. 0 is
	// DefaultSeriesInterval.
	Interval int64
	// TimeZone is the IANA zone buckets are aligned to; "" is UTC.
	// Buckets follow the zone's clock: a day bucket runs from midnight to
	// midnight (23 or 25 hours when the clocks change), the hour the clocks
	// go back is one bucket of two hours, and the hour they skip has none.
	TimeZone string
}

// groupSpec is an attribute series are grouped by
type groupSpec struct {
	// label is the key of the value in the series' labels
	label string
	// expr is the SQL of the value, '' when a point has none
	expr string
}

// typeGroup groups by token type: type, or gen_ai.token.type
var typeGroup = groupSpec{"type",
	"COALESCE(NULLIF(Attributes->>'type', ''), NULLIF(Attributes->>'gen_ai.token.type', ''), '')"}

func attributeGroup(key string) (groupSpec, error) {
	path, err := metricAttributeJSONPath(key)
	if err != nil {
		return groupSpec{}, err
	}
	return groupSpec{key, "COALESCE(json_extract_string(Attributes, '" + path + "'), '')"}, nil
}

type attributeFilter struct {
	path, value string
}

// seriesQuery is a validated api.MetricQuery
type seriesQuery struct {
	name    string // the requested name, which the series keep
	source  string // the stored metric (seriesSourceMetric)
	service string
	// aggregate returns one point per series over the whole range
	aggregate bool
	// merge makes all points one series, not one per service and group
	merge   bool
	groups  []groupSpec
	filters []attributeFilter
	stat    string
}

// resolveSeriesQuery validates a query. Series are grouped by service, and
// by type and model (aggregates) or type (bucketed) unless the query names
// attributes or merges all points into one series.
func resolveSeriesQuery(q api.MetricQuery) (*seriesQuery, error) {
	if q.Name == "" {
		return nil, queryError(api.MetricErrorInvalidQuery, "name is required")
	}
	sq := &seriesQuery{name: q.Name, source: seriesSourceMetric(q.Name), service: q.Service, aggregate: q.Aggregate, merge: q.Merge}
	if q.Merge && len(q.GroupBy) > 0 {
		return nil, queryError(api.MetricErrorInvalidGroupBy, "merge makes one series of all points: it takes no groupBy")
	}

	if len(q.GroupBy) > MaxGroupBy {
		return nil, queryError(api.MetricErrorInvalidGroupBy, "groupBy names %d attributes: at most %d", len(q.GroupBy), MaxGroupBy)
	}
	for i, key := range q.GroupBy {
		if key == "service" {
			return nil, queryError(api.MetricErrorInvalidGroupBy, `groupBy "service": series are always grouped by service`)
		}
		if i > 0 && key == q.GroupBy[0] {
			return nil, queryError(api.MetricErrorInvalidGroupBy, "groupBy names %q twice", key)
		}
		group, err := attributeGroup(key)
		if err != nil {
			return nil, queryError(api.MetricErrorInvalidGroupBy,
				"invalid groupBy %q: attribute keys consist of letters, digits, '.', '_' and '-'", key)
		}
		sq.groups = append(sq.groups, group)
	}
	if len(q.GroupBy) == 0 && !q.Merge {
		sq.groups = []groupSpec{typeGroup}
		if q.Aggregate {
			model, _ := attributeGroup("model")
			sq.groups = append(sq.groups, model)
		}
	}

	if len(q.Filters) > MaxFilters {
		return nil, queryError(api.MetricErrorInvalidFilter, "%d filters: at most %d", len(q.Filters), MaxFilters)
	}
	keys := make([]string, 0, len(q.Filters))
	for key := range q.Filters {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		path, err := metricAttributeJSONPath(key)
		if err != nil {
			return nil, queryError(api.MetricErrorInvalidFilter,
				"invalid filter key %q: attribute keys consist of letters, digits, '.', '_' and '-'", key)
		}
		if q.Filters[key] == "" {
			return nil, queryError(api.MetricErrorInvalidFilter, "filter %q has no value", key)
		}
		sq.filters = append(sq.filters, attributeFilter{path, q.Filters[key]})
	}

	if q.Stat != "" && !isHistogramStat(q.Stat) {
		return nil, queryError(api.MetricErrorInvalidStat, "unknown stat %q: use %s", q.Stat, strings.Join(HistogramStats, ", "))
	}
	sq.stat = q.Stat
	return sq, nil
}

func isHistogramStat(stat string) bool {
	for _, s := range HistogramStats {
		if s == stat {
			return true
		}
	}
	return false
}

// ValidateMetricQuery checks a query's name, groupBy, filters and stat. That
// the stat suits the metric's type is checked when the query runs.
func ValidateMetricQuery(q api.MetricQuery) error {
	_, err := resolveSeriesQuery(q)
	return err
}

func isHistogram(metricType string) bool {
	return metricType == api.MetricTypeHistogram || metricType == api.MetricTypeExponentialHistogram
}

// checkStat sets the stat of a histogram query (avg when it has none) and
// rejects stats that do not suit the metric's type. Other metrics accept
// only the aggregation they have anyway (sum, or avg for gauges), so a
// client that takes a metric for a histogram of tokens can ask for the sum.
func (q *seriesQuery) checkStat(metricType string) error {
	if !isHistogram(metricType) {
		natural := StatSum
		if metricType == api.MetricTypeGauge {
			natural = StatAvg
		}
		if q.stat != "" && q.stat != natural {
			return queryError(api.MetricErrorInvalidStat, "stat %s applies to histogram metrics: %s is a %s metric", q.stat, q.name, metricType)
		}
		q.stat = ""
		return nil
	}
	if q.stat == "" {
		q.stat = StatAvg
	}
	if _, ok := statQuantiles[q.stat]; ok && metricType == api.MetricTypeExponentialHistogram {
		return queryError(api.MetricErrorInvalidStat,
			"%s is an exponential histogram, whose percentiles are not supported: use avg, sum, count, min or max", q.name)
	}
	return nil
}

// --- time zones ---

var zoneNamePattern = regexp.MustCompile(`^[A-Za-z0-9_+\-/]+$`)

// icuZones holds the time zones DuckDB's ICU extension knows, which are the
// same for every database of the process. A failed lookup is retried.
var icuZones struct {
	mu    sync.Mutex
	names map[string]bool
}

func (s *DuckDBStore) icuZoneNames() (map[string]bool, error) {
	icuZones.mu.Lock()
	defer icuZones.mu.Unlock()
	if icuZones.names != nil {
		return icuZones.names, nil
	}
	rows, err := s.db.QueryContext(context.Background(), "SELECT name FROM pg_timezone_names()")
	if err != nil {
		return nil, fmt.Errorf("listing time zones: %w", err)
	}
	defer rows.Close()
	names := make(map[string]bool)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("listing time zones: %w", err)
		}
		names[name] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("listing time zones: %w", err)
	}
	icuZones.names = names
	return names, nil
}

// ValidateTimeZone checks the IANA name of a zone series buckets can be
// aligned to: Go (time.LoadLocation) and DuckDB's ICU extension, which
// computes the buckets, must both know it. "" (UTC) is valid.
func (s *DuckDBStore) ValidateTimeZone(name string) error {
	if name == "" {
		return nil
	}
	invalid := queryError(api.MetricErrorInvalidTimeZone, "unknown time zone %q: use an IANA name such as Europe/Berlin", name)
	if name == "Local" || !zoneNamePattern.MatchString(name) {
		return invalid
	}
	if _, err := time.LoadLocation(name); err != nil {
		return invalid
	}
	zones, err := s.icuZoneNames()
	if err != nil {
		return err
	}
	if !zones[name] {
		return invalid
	}
	return nil
}

// localZone caches LocalTimeZone, which is the same for every database
var localZone struct {
	once sync.Once
	name string
}

// LocalTimeZone returns the IANA name of the server's time zone, as DuckDB's
// ICU extension detected it (from TZ or /etc/localtime), or UTC
func (s *DuckDBStore) LocalTimeZone() string {
	localZone.once.Do(func() {
		localZone.name = "UTC"
		var name string
		if err := s.db.QueryRowContext(context.Background(), "SELECT current_setting('TimeZone')").Scan(&name); err != nil {
			logger.Warn("Could not read the local time zone, using UTC", "error", err)
			return
		}
		if name != "" && s.ValidateTimeZone(name) == nil {
			localZone.name = name
		}
	})
	return localZone.name
}

// bucketing computes the buckets of a series in a time zone. Bucket keys are
// TIMESTAMPs of the zone's clock (UTC: the stored times), so buckets of a
// day start at the zone's midnight; output is the bucket start in UTC.
type bucketing struct {
	interval string // INTERVAL literal
	zone     string // validated zone name; "" for UTC
}

func newBucketing(seconds int64, zone string) bucketing {
	if zone == "UTC" {
		zone = ""
	}
	return bucketing{interval: fmt.Sprintf("INTERVAL '%d seconds'", seconds), zone: zone}
}

// local converts a stored (UTC) TIMESTAMP to the zone's clock
func (b bucketing) local(col string) string {
	if b.zone == "" {
		return col
	}
	return "timezone('" + b.zone + "', timezone('UTC', " + col + "))"
}

// key is the bucket of a stored TIMESTAMP
func (b bucketing) key(col string) string {
	return "time_bucket(" + b.interval + ", " + b.local(col) + ")"
}

// instant converts a bucket key to its start in UTC
func (b bucketing) instant(col string) string {
	if b.zone == "" {
		return col
	}
	return "timezone('UTC', timezone('" + b.zone + "', " + col + "))"
}

// grid selects the bucket keys from the bucket of the first ? to the bucket
// of the second. Keys of the zone's clock that do not last (the hour the
// clocks skip) are left out.
func (b bucketing) grid() string {
	series := "SELECT UNNEST(generate_series(" + b.key("?::TIMESTAMP") + ", " + b.key("?::TIMESTAMP") + ", " + b.interval + ")) AS bucket"
	if b.zone == "" {
		return series
	}
	return "SELECT bucket FROM (" + series + ") WHERE timezone('" + b.zone + "', bucket) < timezone('" + b.zone + "', bucket + " + b.interval + ")"
}

// --- queries ---

// QuerySeries returns the series of a metric query
func (s *DuckDBStore) QuerySeries(ctx context.Context, q api.MetricQuery, r SeriesRange) (*api.TimeSeriesResponse, error) {
	sq, err := resolveSeriesQuery(q)
	if err != nil {
		return nil, err
	}
	if err := s.ValidateTimeZone(r.TimeZone); err != nil {
		return nil, err
	}
	types, err := s.metricTypes(ctx, []api.MetricQuery{q}, r.From, r.To)
	if err != nil {
		return nil, err
	}
	return s.runSeriesQuery(ctx, sq, r, types)
}

// QuerySeriesBatch runs several series queries, at most
// maxConcurrentSeriesQueries at once. A query that fails (invalid, timed out)
// has its error and code in its result; the others are not affected.
func (s *DuckDBStore) QuerySeriesBatch(ctx context.Context, queries []api.MetricQuery, r SeriesRange) *api.BatchMetricSeriesResponse {
	results := make([]api.MetricQueryResult, len(queries))
	resolved := make([]*seriesQuery, len(queries))
	var valid []api.MetricQuery
	zoneErr := s.ValidateTimeZone(r.TimeZone)
	for i, q := range queries {
		results[i].ID = q.ID
		sq, err := resolveSeriesQuery(q)
		if err == nil {
			err = zoneErr
		}
		if err != nil {
			results[i].Code, results[i].Error = seriesErrorOf(ctx, err)
			continue
		}
		resolved[i] = sq
		valid = append(valid, q)
	}
	if len(valid) == 0 {
		return &api.BatchMetricSeriesResponse{Results: results}
	}

	types, err := s.metricTypes(ctx, valid, r.From, r.To)
	if err != nil {
		logger.Error("Batch metric type lookup failed", "error", err)
		code, message := seriesErrorOf(ctx, err)
		for i := range results {
			if resolved[i] != nil {
				results[i].Code, results[i].Error = code, message
			}
		}
		return &api.BatchMetricSeriesResponse{Results: results}
	}

	var wg sync.WaitGroup
	sem := make(chan struct{}, maxConcurrentSeriesQueries)
	for i, sq := range resolved {
		if sq == nil {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			resp, err := s.runSeriesQuery(ctx, sq, r, types)
			if err != nil {
				var invalid *MetricQueryError
				if !errors.As(err, &invalid) {
					logger.Error("Batch metric series query failed", "id", queries[i].ID, "metric", sq.name, "error", err)
				}
				results[i].Code, results[i].Error = seriesErrorOf(ctx, err)
				return
			}
			results[i].Success = true
			results[i].Series = resp.Series
			results[i].MetricType = resp.MetricType
			results[i].Stat = resp.Stat
		}()
	}
	wg.Wait()
	return &api.BatchMetricSeriesResponse{Results: results}
}

// seriesErrorOf returns the code and message of a failed query: its own
// for invalid queries, a generic one otherwise (never SQL or internals)
func seriesErrorOf(ctx context.Context, err error) (code, message string) {
	var invalid *MetricQueryError
	switch {
	case errors.As(err, &invalid):
		return invalid.Code, invalid.Message
	case errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded):
		return api.MetricErrorTimeout, "query timed out: choose a shorter range or a larger interval"
	default:
		return api.MetricErrorFailed, "failed to query metric series"
	}
}

// runSeriesQuery reads the series of a validated query, given the types of
// the metrics in the range (metricTypes)
func (s *DuckDBStore) runSeriesQuery(ctx context.Context, q *seriesQuery, r SeriesRange, types map[metricTypeKey]metricTypeInfo) (*api.TimeSeriesResponse, error) {
	resp := &api.TimeSeriesResponse{Series: []api.TimeSeries{}}
	if !q.aggregate {
		resp.Interval = seriesBucketSeconds(r.From, r.To, r.Interval)
		resp.TimeZone = r.TimeZone
		if resp.TimeZone == "" {
			resp.TimeZone = "UTC"
		}
	}
	typeInfo, ok := types[metricTypeKey{q.source, q.service}]
	if !ok {
		// No points in the range: nothing to check the stat against
		resp.Stat = q.stat
		return resp, nil
	}
	if err := q.checkStat(typeInfo.metricType); err != nil {
		return nil, err
	}
	resp.MetricType, resp.Stat = typeInfo.metricType, q.stat

	b := newBucketing(resp.Interval, r.TimeZone)
	var series []api.TimeSeries
	var err error
	if isHistogram(typeInfo.metricType) {
		series, err = s.histogramSeries(ctx, q, r, typeInfo, b)
	} else {
		series, err = s.valueSeries(ctx, q, r, typeInfo, b)
	}
	if err != nil {
		return nil, err
	}
	resp.Series = series
	return resp, nil
}

// groupColumns returns ", g0, g1" (with prefix: ", p.g0, p.g1"), or ""
// without groups
func (q *seriesQuery) groupColumns(prefix string) string {
	var cols strings.Builder
	for i := range q.groups {
		fmt.Fprintf(&cols, ", %sg%d", prefix, i)
	}
	return cols.String()
}

// groupSelect returns ", <expr> AS g0, ..." (NULLs for baseline rows)
func (q *seriesQuery) groupSelect(null bool) string {
	var cols strings.Builder
	for i, g := range q.groups {
		expr := g.expr
		if null {
			expr = "NULL"
		}
		fmt.Fprintf(&cols, ", %s AS g%d", expr, i)
	}
	return cols.String()
}

// pointService is the service column of the points: ” when merged, so
// all points form one series
func (q *seriesQuery) pointService() string {
	if q.merge {
		return "'' AS ServiceName"
	}
	return "ServiceName"
}

// filter returns the conditions (each starting with " AND ") of the
// query's service and attribute filters, and their arguments
func (q *seriesQuery) filter() (string, []any) {
	var sql strings.Builder
	cond, args := serviceCondition(q.service)
	if cond != "" {
		sql.WriteString(" AND " + cond)
	}
	for _, f := range q.filters {
		sql.WriteString(" AND json_extract_string(Attributes, '" + f.path + "') = ?")
		args = append(args, f.value)
	}
	return sql.String(), args
}

// labelsOf returns the labels of a series: its service and the values of
// its groups that are set
func (q *seriesQuery) labelsOf(service string, values []string) map[string]string {
	labels := map[string]string{}
	if service != "" {
		labels["service"] = service
	}
	for i, v := range values {
		if v != "" {
			labels[q.groups[i].label] = v
		}
	}
	return labels
}

// --- counters, gauges and summaries ---

// valueSeries reads the series of a non-histogram metric. Cumulative points
// carry running totals, so their value is the increase over the previous
// point of the same series (same attributes and resource attributes, which
// grouping may merge), or the full value at a series start or reset; DELTA
// points are kept. Counters add up; gauges are levels and are averaged, with
// no value in buckets without points.
func (s *DuckDBStore) valueSeries(ctx context.Context, q *seriesQuery, r SeriesRange, typeInfo metricTypeInfo, b bucketing) ([]api.TimeSeries, error) {
	fromStr, toStr := formatTimeForDB(r.From), formatTimeForDB(r.To)
	filter, filterArgs := q.filter()
	valueExpr := "COALESCE(Value, Sum)"
	groups := q.groupColumns("")

	var pointsCTE string
	var args []any
	if typeInfo.cumulative && typeInfo.metricType != api.MetricTypeGauge {
		// Deleted points leave a baseline (metric_baselines), which serves as
		// the previous point of the series but is no point itself
		pointsCTE = `
			raw AS (
				SELECT Timestamp, ServiceName` + groups + `, v, cumulative, baseline,
					LAG(v) OVER (PARTITION BY ServiceName, attrs, resource_attrs ORDER BY Timestamp) AS prev
				FROM (
					SELECT Timestamp, ServiceName, CAST(Attributes AS VARCHAR) AS attrs,
						CAST(ResourceAttributes AS VARCHAR) AS resource_attrs,
						` + valueExpr + ` AS v` + q.groupSelect(false) + `,
						AggregationTemporality = 2 AS cumulative, false AS baseline
					FROM otel_metrics
					WHERE MetricName = ?
						AND Timestamp >= ?::TIMESTAMP AND Timestamp <= ?::TIMESTAMP
						AND ` + valueExpr + ` IS NOT NULL` + filter + `
					UNION ALL
					SELECT Timestamp, ServiceName, Attributes, ResourceAttributes, Value` + q.groupSelect(true) + `, true, true
					FROM metric_baselines
					WHERE MetricName = ?
						AND Timestamp >= ?::TIMESTAMP AND Timestamp <= ?::TIMESTAMP` + filter + `
				)
			),
			points AS (
				SELECT Timestamp, ` + q.pointService() + groups + `,
					CASE WHEN cumulative IS NOT TRUE THEN v
					     WHEN prev IS NULL OR v < prev THEN v
					     ELSE v - prev END AS v
				FROM raw
				WHERE Timestamp >= ?::TIMESTAMP AND NOT baseline
			)`
		source := concatArgs([]any{q.source, formatTimeForDB(r.From.Add(-cumulativeLookback)), toStr}, filterArgs)
		args = concatArgs(source, source, []any{fromStr})
	} else {
		pointsCTE = `
			points AS (
				SELECT Timestamp, ` + q.pointService() + q.groupSelect(false) + `, ` + valueExpr + ` AS v
				FROM otel_metrics
				WHERE MetricName = ?
					AND Timestamp >= ?::TIMESTAMP AND Timestamp <= ?::TIMESTAMP
					AND ` + valueExpr + ` IS NOT NULL` + filter + `
			)`
		args = concatArgs([]any{q.source, fromStr, toStr}, filterArgs)
	}

	gauge := typeInfo.metricType == api.MetricTypeGauge
	agg := aggColumn{"SUM(v)", "v"}
	if gauge {
		agg.expr = "AVG(v)"
	}
	query, args := q.selectSeries(pointsCTE, args, []aggColumn{agg}, nil, b, fromStr, toStr)
	return q.scanCells(ctx, s.db, query, args, 0, 1, func(rows []cellRow) float64 {
		switch v := rows[0].values[0]; {
		case v.Valid:
			return v.Float64
		case gauge:
			return math.NaN()
		default:
			return 0
		}
	})
}

// aggColumn is an aggregate of the points of a series (in a bucket)
type aggColumn struct {
	expr, name string
}

// selectSeries completes the query of a series: the aggregates per series,
// and per bucket for bucketed queries, where every bucket of the range has a
// row per series (with NULL aggregates when it has no points). The extra
// columns of the points (such as histogram bounds) split a series' rows
// further.
func (q *seriesQuery) selectSeries(pointsCTE string, args []any, aggs []aggColumn, extra []string, b bucketing, fromStr, toStr string) (string, []any) {
	groups := q.groupColumns("")
	extraCols, dataCols := "", ""
	for _, col := range extra {
		extraCols += ", " + col
		dataCols += ", d." + col
	}
	aggCols := make([]string, len(aggs))
	for i, a := range aggs {
		aggCols[i] = a.expr + " AS " + a.name
		dataCols += ", d." + a.name
	}
	if q.aggregate {
		return `WITH ` + pointsCTE + `
			SELECT ServiceName` + groups + extraCols + `, ` + strings.Join(aggCols, ", ") + `
			FROM points
			GROUP BY ALL
			ORDER BY ServiceName` + groups, args
	}

	join := "b.bucket = d.bucket AND s.ServiceName = d.ServiceName"
	for i := range q.groups {
		join += fmt.Sprintf(" AND s.g%d = d.g%d", i, i)
	}
	query := `WITH ` + pointsCTE + `,
		buckets AS (` + b.grid() + `),
		series_labels AS (
			SELECT DISTINCT ServiceName` + groups + ` FROM points
			ORDER BY ALL
			LIMIT ` + fmt.Sprint(MaxSeries+1) + `
		),
		data AS (
			SELECT ` + b.key("Timestamp") + ` AS bucket, ServiceName` + groups + extraCols + `, ` + strings.Join(aggCols, ", ") + `
			FROM points
			GROUP BY ALL
		)
		SELECT ` + b.instant("b.bucket") + `, s.ServiceName` + q.groupColumns("s.") + dataCols + `
		FROM buckets b
		CROSS JOIN series_labels s
		LEFT JOIN data d ON ` + join + `
		ORDER BY b.bucket, s.ServiceName` + q.groupColumns("s.")
	return query, append(args, fromStr, toStr)
}

// cellRow is a row of a cell (a series in a bucket, or an aggregate
// series): its extra strings and its aggregates
type cellRow struct {
	extra  []sql.NullString
	values []sql.NullFloat64
}

// scanCells runs a series query and turns its rows into series. A row holds
// the bucket (bucketed queries), the service, the group values, nExtra
// strings and nValues aggregates; the rows of a cell are adjacent, and value
// folds them into the cell's point.
func (q *seriesQuery) scanCells(ctx context.Context, db *sql.DB, query string, args []any, nExtra, nValues int,
	value func([]cellRow) float64) ([]api.TimeSeries, error) {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying metric series: %w", err)
	}
	defer rows.Close()

	seriesByKey := make(map[string]*api.TimeSeries)
	var bucket time.Time
	var key string
	var cellRows []cellRow
	var labels map[string]string
	flush := func() {
		if len(cellRows) == 0 {
			return
		}
		series, ok := seriesByKey[key]
		if !ok {
			series = &api.TimeSeries{Name: q.name, Labels: labels, DataPoints: make([][2]float64, 0)}
			seriesByKey[key] = series
		}
		t := 0.0 // timestamp 0 marks an aggregate
		if !q.aggregate {
			t = float64(bucket.UnixMilli())
		}
		series.DataPoints = append(series.DataPoints, [2]float64{t, value(cellRows)})
		cellRows = nil
	}

	groups := make([]string, len(q.groups))
	for rows.Next() {
		var rowBucket time.Time
		var service string
		row := cellRow{extra: make([]sql.NullString, nExtra), values: make([]sql.NullFloat64, nValues)}
		dest := make([]any, 0, 2+len(groups)+nExtra+nValues)
		if !q.aggregate {
			dest = append(dest, &rowBucket)
		}
		dest = append(dest, &service)
		for i := range groups {
			dest = append(dest, &groups[i])
		}
		for i := range row.extra {
			dest = append(dest, &row.extra[i])
		}
		for i := range row.values {
			dest = append(dest, &row.values[i])
		}
		if err := rows.Scan(dest...); err != nil {
			return nil, fmt.Errorf("scanning metric series: %w", err)
		}
		rowKey := service + "\x00" + strings.Join(groups, "\x00")
		if rowKey != key || !rowBucket.Equal(bucket) || len(cellRows) == 0 {
			flush()
			bucket, key, labels = rowBucket, rowKey, q.labelsOf(service, groups)
		}
		cellRows = append(cellRows, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating metric series: %w", err)
	}
	flush()

	if !q.aggregate && len(seriesByKey) > MaxSeries {
		return nil, queryError(api.MetricErrorTooManySeries,
			"the query has more than %d series: group by an attribute with fewer values, or add a filter", MaxSeries)
	}

	// Sorted for a stable legend and color order across refreshes
	keys := make([]string, 0, len(seriesByKey))
	for k := range seriesByKey {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	series := make([]api.TimeSeries, 0, len(keys))
	for _, k := range keys {
		series = append(series, *seriesByKey[k])
	}
	return series, nil
}
