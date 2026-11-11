package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"

	"github.com/tobilg/ai-observer/internal/api"
)

// histogramBucketColumns is the number of bucket counts a percentile query
// reads per point at first. Histograms with more buckets are read again with
// enough columns; maxHistogramBuckets bounds them.
const (
	histogramBucketColumns = 64
	maxHistogramBuckets    = 1024
)

// The aggregates of a histogram cell, in the order the queries select them
const (
	histSum = iota // sum of the values
	histCount
	histSumCounted // sum of the values of points whose count is known
	histMin
	histMax
	histBuckets // number of bucket counts, then the counts (percentiles only)
)

// histogramSeries reads the series of a histogram statistic (q.stat).
//
// Every point holds the count, sum, min and max of the values recorded in
// its interval (DELTA) or since its series started (CUMULATIVE). Count, sum
// and bucket counts of cumulative points become increases over the previous
// point of their series, like counters (a series restarts when its count
// drops or its bucket bounds change); min and max are the extremes since the
// series started, as OTLP reports them, not those of the bucket. After a
// deleted range the last deleted point (metric_baselines) is the previous
// point; its count and buckets are unknown for baselines stored before
// migration 12, and the next point then counts for the sum only.
//
// avg is the sum over the count; percentiles are estimated from the summed
// bucket counts (estimateQuantile). Buckets without values have no avg, min,
// max or percentile (NaN), and 0 for sum and count.
func (s *DuckDBStore) histogramSeries(ctx context.Context, q *seriesQuery, r SeriesRange, typeInfo metricTypeInfo, b bucketing) ([]api.TimeSeries, error) {
	quantile, percentile := statQuantiles[q.stat]
	columns := 0
	if percentile {
		columns = histogramBucketColumns
	}
	for {
		series, buckets, err := s.histogramQuery(ctx, q, r, typeInfo, b, quantile, columns)
		if err != nil || buckets <= columns {
			return series, err
		}
		if buckets > maxHistogramBuckets {
			return nil, queryError(api.MetricErrorInvalidStat,
				"%s has histograms of %d buckets: percentiles support at most %d", q.name, buckets, maxHistogramBuckets)
		}
		columns = buckets
	}
}

// histogramQuery runs a histogram query that reads up to columns bucket
// counts per point (none without a percentile). It returns the most bucket
// counts a point had, so a caller can read again with more columns.
func (s *DuckDBStore) histogramQuery(ctx context.Context, q *seriesQuery, r SeriesRange, typeInfo metricTypeInfo,
	b bucketing, quantile float64, columns int) ([]api.TimeSeries, int, error) {
	fromStr, toStr := formatTimeForDB(r.From), formatTimeForDB(r.To)
	filter, filterArgs := q.filter()
	groups := q.groupColumns("")
	percentile := columns > 0

	measures := "Sum AS s, CAST(Count AS DOUBLE) AS c, Min AS mn, Max AS mx"
	// A baseline (the last deleted point of a series) has the point's sum,
	// count and buckets, not its extremes; baselines stored before
	// migration 12 have only the sum
	baselineMeasures := "Value, CAST(Count AS DOUBLE), NULL, NULL"
	if percentile {
		measures += ", CAST(ExplicitBounds AS VARCHAR) AS bounds, CAST(BucketCounts AS BIGINT[]) AS bc"
		baselineMeasures += ", CAST(ExplicitBounds AS VARCHAR), CAST(BucketCounts AS BIGINT[])"
	}
	cumulative := typeInfo.cumulative

	var pointsCTE string
	var args []any
	if cumulative {
		lags := "LAG(s) OVER w AS prev_s, LAG(c) OVER w AS prev_c, LAG(baseline) OVER w AS prev_baseline"
		reset := "prev_c IS NULL OR c < prev_c"
		bucketCols := ""
		if percentile {
			lags += ", LAG(bc) OVER w AS prev_bc, LAG(bounds) OVER w AS prev_bounds"
			reset += " OR bounds IS DISTINCT FROM prev_bounds"
			bucketCols = ", bounds, bc, prev_bc, mode"
		}
		// mode: 0 the point's own values, 1 the increase over the previous
		// point, NULL unknown (the previous point was deleted before
		// baselines kept counts)
		pointsCTE = `
			raw AS (
				SELECT *, ` + lags + `
				FROM (
					SELECT Timestamp, ServiceName, CAST(Attributes AS VARCHAR) AS attrs,
						CAST(ResourceAttributes AS VARCHAR) AS resource_attrs,
						` + measures + q.groupSelect(false) + `,
						AggregationTemporality = 2 AS cumulative, false AS baseline
					FROM otel_metrics
					WHERE MetricName = ?
						AND Timestamp >= ?::TIMESTAMP AND Timestamp <= ?::TIMESTAMP
						AND Count IS NOT NULL` + filter + `
					UNION ALL
					SELECT Timestamp, ServiceName, Attributes, ResourceAttributes, ` + baselineMeasures + q.groupSelect(true) + `, true, true
					FROM metric_baselines
					WHERE MetricName = ?
						AND Timestamp >= ?::TIMESTAMP AND Timestamp <= ?::TIMESTAMP` + filter + `
				)
				WINDOW w AS (PARTITION BY ServiceName, attrs, resource_attrs ORDER BY Timestamp)
			),
			marked AS (
				SELECT *,
					CASE WHEN cumulative IS NOT TRUE THEN 0
					     WHEN prev_baseline AND prev_c IS NULL THEN NULL
					     WHEN ` + reset + ` THEN 0
					     ELSE 1 END AS mode
				FROM raw
				WHERE Timestamp >= ?::TIMESTAMP AND NOT baseline
			),
			points AS (
				SELECT Timestamp, ` + q.pointService() + groups + `,
					CASE WHEN mode = 0 THEN s
					     WHEN mode = 1 THEN s - prev_s
					     WHEN s >= prev_s THEN s - prev_s
					     ELSE s END AS s,
					CASE mode WHEN 0 THEN c WHEN 1 THEN c - prev_c END AS c,
					mn, mx` + bucketCols + `
				FROM marked
			)`
		source := concatArgs([]any{q.source, formatTimeForDB(r.From.Add(-cumulativeLookback)), toStr}, filterArgs)
		args = concatArgs(source, source, []any{fromStr})
	} else {
		pointsCTE = `
			points AS (
				SELECT Timestamp, ` + q.pointService() + q.groupSelect(false) + `, ` + measures + `
				FROM otel_metrics
				WHERE MetricName = ?
					AND Timestamp >= ?::TIMESTAMP AND Timestamp <= ?::TIMESTAMP
					AND Count IS NOT NULL` + filter + `
			)`
		args = concatArgs([]any{q.source, fromStr, toStr}, filterArgs)
	}

	aggs := []aggColumn{
		{"SUM(s)", "s"},
		{"SUM(c)", "c"},
		{"SUM(s) FILTER (WHERE c IS NOT NULL)", "sc"},
		{"MIN(mn)", "mn"},
		{"MAX(mx)", "mx"},
	}
	var extra []string
	if percentile {
		extra = []string{"bounds"}
		aggs = append(aggs, aggColumn{"CAST(MAX(len(bc)) AS DOUBLE)", "nb"})
		for i := 1; i <= columns; i++ {
			count := fmt.Sprintf("bc[%d]", i)
			if cumulative {
				count = fmt.Sprintf("CASE mode WHEN 0 THEN bc[%d] WHEN 1 THEN bc[%d] - prev_bc[%d] END", i, i, i)
			}
			aggs = append(aggs, aggColumn{"CAST(SUM(" + count + ") AS DOUBLE)", fmt.Sprintf("b%d", i)})
		}
	}
	query, args := q.selectSeries(pointsCTE, args, aggs, extra, b, fromStr, toStr)

	parsed := make(map[string][]float64)
	maxBuckets := 0
	series, err := q.scanCells(ctx, s.db, query, args, len(extra), len(aggs), func(rows []cellRow) float64 {
		var sum, count, sumCounted float64
		lo, hi := math.NaN(), math.NaN()
		var hists []bucketCounts
		for _, row := range rows {
			v := row.values
			sum += v[histSum].Float64
			count += v[histCount].Float64
			sumCounted += v[histSumCounted].Float64
			if v[histMin].Valid && !(v[histMin].Float64 >= lo) {
				lo = v[histMin].Float64
			}
			if v[histMax].Valid && !(v[histMax].Float64 <= hi) {
				hi = v[histMax].Float64
			}
			if !percentile || !row.extra[0].Valid || !v[histBuckets].Valid {
				continue
			}
			n := int(v[histBuckets].Float64)
			maxBuckets = max(maxBuckets, n)
			bounds, ok := parsed[row.extra[0].String]
			if !ok {
				if json.Unmarshal([]byte(row.extra[0].String), &bounds) != nil {
					bounds = nil
				}
				parsed[row.extra[0].String] = bounds
			}
			// Points whose counts do not match their bounds are malformed
			if n != len(bounds)+1 || n > columns {
				continue
			}
			counts := make([]float64, n)
			for i := range counts {
				counts[i] = max(v[histBuckets+1+i].Float64, 0)
			}
			hists = append(hists, bucketCounts{bounds: bounds, counts: counts})
		}
		switch q.stat {
		case StatSum:
			return sum
		case StatCount:
			return count
		case StatMin:
			return lo
		case StatMax:
			return hi
		case StatAvg:
			if count > 0 {
				return sumCounted / count
			}
			return math.NaN()
		default:
			return estimateQuantile(hists, quantile, lo, hi)
		}
	})
	return series, maxBuckets, err
}

// bucketCounts are the counts of an explicit-bounds histogram: counts[i]
// values in (bounds[i-1], bounds[i]], the first and last bucket open
type bucketCounts struct {
	bounds []float64
	counts []float64
}

// interval is a range of values holding n of them, spread evenly; lo == hi
// is a point
type interval struct {
	lo, hi, n float64
}

// estimateQuantile returns the q-quantile (0 < q < 1) of the values the
// histograms count, assuming the values of a bucket spread evenly over it
// (linear interpolation within the bucket). The open first and last buckets
// end at the smallest and largest value (lo, hi; NaN when unknown), else at
// 0 or the first bound (below a positive first bound, as for durations) and
// at the last bound. Buckets are clamped to [lo, hi]. Histograms with
// different bounds are merged through their cumulative distributions. NaN
// without values.
func estimateQuantile(hists []bucketCounts, q, lo, hi float64) float64 {
	var intervals []interval
	total := 0.0
	for _, h := range hists {
		n := len(h.bounds)
		for k, count := range h.counts {
			if count <= 0 {
				continue
			}
			var iv interval
			switch {
			case n == 0:
				// A single bucket of all values: only min and max bound it
				if math.IsNaN(lo) || math.IsNaN(hi) {
					continue
				}
				iv = interval{lo, hi, count}
			case k == 0:
				iv.hi = h.bounds[0]
				switch {
				case !math.IsNaN(lo) && lo <= iv.hi:
					iv.lo = lo
				case iv.hi > 0:
					iv.lo = 0
				default:
					iv.lo = iv.hi
				}
			case k == n:
				iv.lo = h.bounds[n-1]
				iv.hi = iv.lo
				if !math.IsNaN(hi) && hi >= iv.lo {
					iv.hi = hi
				}
			default:
				iv.lo, iv.hi = h.bounds[k-1], h.bounds[k]
			}
			iv.n = count
			if !math.IsNaN(lo) {
				iv.lo, iv.hi = math.Max(iv.lo, lo), math.Max(iv.hi, lo)
			}
			if !math.IsNaN(hi) {
				iv.lo, iv.hi = math.Min(iv.lo, hi), math.Min(iv.hi, hi)
			}
			intervals = append(intervals, iv)
			total += count
		}
	}
	if total == 0 {
		return math.NaN()
	}
	target := q * total

	if len(hists) == 1 {
		// One histogram's buckets are ordered and do not overlap
		cum := 0.0
		for _, iv := range intervals {
			if cum+iv.n >= target {
				return iv.lo + (target-cum)/iv.n*(iv.hi-iv.lo)
			}
			cum += iv.n
		}
		return intervals[len(intervals)-1].hi
	}

	// The merged distribution is linear between the bucket edges and jumps at
	// point buckets: find the edge where it reaches the target, then
	// interpolate back from it
	edges := make([]float64, 0, 2*len(intervals))
	for _, iv := range intervals {
		edges = append(edges, iv.lo, iv.hi)
	}
	sort.Float64s(edges)
	below := func(x float64, inclusive bool) float64 {
		f := 0.0
		for _, iv := range intervals {
			switch {
			case x > iv.hi || (inclusive && x == iv.hi):
				f += iv.n
			case x > iv.lo:
				f += iv.n * (x - iv.lo) / (iv.hi - iv.lo)
			}
		}
		return f
	}
	prev, prevF := edges[0], below(edges[0], true)
	if prevF >= target {
		return prev
	}
	for _, x := range edges[1:] {
		if x == prev {
			continue
		}
		fx := below(x, true)
		if fx >= target {
			// below(x, false) excludes the points at x
			if left := below(x, false); left >= target && left > prevF {
				return prev + (target-prevF)/(left-prevF)*(x-prev)
			}
			return x
		}
		prev, prevF = x, fx
	}
	return edges[len(edges)-1]
}
