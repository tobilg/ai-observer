package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/tobilg/ai-observer/internal/api"
)

func sumPoint(name string, ts time.Time, value float64, cumulative bool, attrs map[string]string) api.MetricDataPoint {
	temporality := int32(1)
	if cumulative {
		temporality = 2
	}
	return api.MetricDataPoint{
		Timestamp: ts, ServiceName: "svc", MetricName: name, MetricType: "sum",
		Value: &value, AggregationTemporality: &temporality, Attributes: attrs,
	}
}

func histogramPoint(name string, ts time.Time, cumulative bool, bounds []float64, counts []uint64, sum, lo, hi float64, attrs map[string]string) api.MetricDataPoint {
	temporality := int32(1)
	if cumulative {
		temporality = 2
	}
	var count uint64
	for _, c := range counts {
		count += c
	}
	return api.MetricDataPoint{
		Timestamp: ts, ServiceName: "svc", MetricName: name, MetricType: "histogram",
		AggregationTemporality: &temporality, Attributes: attrs,
		Count: &count, Sum: &sum, Min: &lo, Max: &hi, BucketCounts: counts, ExplicitBounds: bounds,
	}
}

func insertPoints(t *testing.T, store *DuckDBStore, points ...api.MetricDataPoint) {
	t.Helper()
	if err := store.InsertMetrics(context.Background(), points); err != nil {
		t.Fatalf("InsertMetrics: %v", err)
	}
}

func querySeries(t *testing.T, store *DuckDBStore, q api.MetricQuery, r SeriesRange) *api.TimeSeriesResponse {
	t.Helper()
	resp, err := store.QuerySeries(context.Background(), q, r)
	if err != nil {
		t.Fatalf("QuerySeries(%+v): %v", q, err)
	}
	return resp
}

// seriesTotals sums each series' points by its label of key
func seriesTotals(resp *api.TimeSeriesResponse, key string) map[string]float64 {
	totals := make(map[string]float64)
	for _, s := range resp.Series {
		for _, dp := range s.DataPoints {
			if !math.IsNaN(dp[1]) {
				totals[s.Labels[key]] += dp[1]
			}
		}
	}
	return totals
}

func wantCode(t *testing.T, err error, code string) {
	t.Helper()
	var invalid *MetricQueryError
	if !errors.As(err, &invalid) || invalid.Code != code {
		t.Fatalf("error = %v, want code %s", err, code)
	}
}

func TestQuerySeries_GroupByAndFilters(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	now := time.Now().UTC().Truncate(time.Hour)
	insertPoints(t, store,
		sumPoint("tokens", now.Add(time.Minute), 10, false, map[string]string{"model": "a", "type": "input", "repository": "r1"}),
		sumPoint("tokens", now.Add(2*time.Minute), 20, false, map[string]string{"model": "a", "type": "output", "repository": "r2"}),
		sumPoint("tokens", now.Add(3*time.Minute), 5, false, map[string]string{"model": "b", "type": "input", "repository": "r1"}),
		sumPoint("tokens", now.Add(4*time.Minute), 7, false, map[string]string{"type": "input"}),
	)
	r := SeriesRange{From: now, To: now.Add(time.Hour), Interval: 600}

	// Default: by type
	resp := querySeries(t, store, api.MetricQuery{Name: "tokens"}, r)
	if got := seriesTotals(resp, "type"); got["input"] != 22 || got["output"] != 20 || len(resp.Series) != 2 {
		t.Errorf("by type = %v (%d series)", got, len(resp.Series))
	}
	if resp.Interval != 600 || resp.TimeZone != "UTC" || resp.MetricType != "sum" || resp.Stat != "" {
		t.Errorf("response = interval %d, zone %q, type %q, stat %q", resp.Interval, resp.TimeZone, resp.MetricType, resp.Stat)
	}

	// By model: labelled by model instead of type; points without one form
	// their own series without the label
	resp = querySeries(t, store, api.MetricQuery{Name: "tokens", GroupBy: []string{"model"}}, r)
	if got := seriesTotals(resp, "model"); got["a"] != 30 || got["b"] != 5 || got[""] != 7 || len(resp.Series) != 3 {
		t.Errorf("by model = %v (%d series)", got, len(resp.Series))
	}
	for _, s := range resp.Series {
		if _, ok := s.Labels["type"]; ok {
			t.Errorf("series by model has a type label: %v", s.Labels)
		}
		if len(s.DataPoints) != 7 {
			t.Errorf("series %v has %d buckets, want 7", s.Labels, len(s.DataPoints))
		}
	}

	// Two keys
	resp = querySeries(t, store, api.MetricQuery{Name: "tokens", GroupBy: []string{"model", "repository"}, Aggregate: true}, r)
	if len(resp.Series) != 4 {
		t.Errorf("by model and repository: %d series, want 4", len(resp.Series))
	}
	for _, s := range resp.Series {
		if s.Labels["model"] == "a" && s.Labels["repository"] == "r2" && s.DataPoints[0][1] != 20 {
			t.Errorf("a/r2 = %v, want 20", s.DataPoints)
		}
	}

	// Filters: exact matches, all of them
	resp = querySeries(t, store, api.MetricQuery{Name: "tokens", Aggregate: true, Filters: map[string]string{"model": "a"}}, r)
	if got := seriesTotals(resp, "type"); got["input"] != 10 || got["output"] != 20 {
		t.Errorf("model a by type = %v", got)
	}
	resp = querySeries(t, store, api.MetricQuery{Name: "tokens", Aggregate: true, Filters: map[string]string{"repository": "r1", "type": "input"}}, r)
	if got := seriesTotals(resp, "model"); got["a"] != 10 || got["b"] != 5 || len(got) != 2 {
		t.Errorf("r1 input by model = %v", got)
	}
	resp = querySeries(t, store, api.MetricQuery{Name: "tokens", Aggregate: true, Filters: map[string]string{"model": "a'; DROP TABLE otel_metrics; --"}}, r)
	if len(resp.Series) != 0 {
		t.Errorf("a filter value matching nothing returned %d series", len(resp.Series))
	}
}

// Cumulative counters become increases per original series (all of its
// attributes) before grouping merges them
func TestQuerySeries_CumulativeGroupedAfterIncreases(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	now := time.Now().UTC().Truncate(time.Hour)
	in := map[string]string{"model": "a", "type": "input"}
	out := map[string]string{"model": "a", "type": "output"}
	other := map[string]string{"model": "b", "type": "input"}
	insertPoints(t, store,
		sumPoint("counter", now.Add(-time.Hour), 100, true, in), // before the range
		sumPoint("counter", now.Add(time.Minute), 150, true, in),
		sumPoint("counter", now.Add(2*time.Minute), 1000, true, out), // series start
		sumPoint("counter", now.Add(3*time.Minute), 1010, true, out),
		sumPoint("counter", now.Add(4*time.Minute), 3, true, other),
		sumPoint("counter", now.Add(5*time.Minute), 9, true, other),
	)
	r := SeriesRange{From: now, To: now.Add(time.Hour), Interval: 3600}
	resp := querySeries(t, store, api.MetricQuery{Name: "counter", GroupBy: []string{"model"}}, r)
	// a: 50 (input) + 1000 + 10 (output); b: 3 + 6. Interleaving the two
	// series of a by time would give the wrong increases.
	if got := seriesTotals(resp, "model"); got["a"] != 1060 || got["b"] != 9 {
		t.Errorf("by model = %v, want a 1060, b 9", got)
	}
	resp = querySeries(t, store, api.MetricQuery{Name: "counter", Aggregate: true, Filters: map[string]string{"type": "input"}}, r)
	if got := seriesTotals(resp, "model"); got["a"] != 50 || got["b"] != 9 {
		t.Errorf("input by model = %v, want a 50, b 9", got)
	}
}

func TestQuerySeries_Validation(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	now := time.Now().UTC()
	insertPoints(t, store, sumPoint("tokens", now, 1, false, nil))
	r := SeriesRange{From: now.Add(-time.Hour), To: now.Add(time.Hour)}
	cases := []struct {
		q    api.MetricQuery
		code string
	}{
		{api.MetricQuery{}, api.MetricErrorInvalidQuery},
		{api.MetricQuery{Name: "tokens", GroupBy: []string{"bad'key"}}, api.MetricErrorInvalidGroupBy},
		{api.MetricQuery{Name: "tokens", GroupBy: []string{"a", "b", "c"}}, api.MetricErrorInvalidGroupBy},
		{api.MetricQuery{Name: "tokens", GroupBy: []string{"model", "model"}}, api.MetricErrorInvalidGroupBy},
		{api.MetricQuery{Name: "tokens", GroupBy: []string{"service"}}, api.MetricErrorInvalidGroupBy},
		{api.MetricQuery{Name: "tokens", Filters: map[string]string{"bad key": "x"}}, api.MetricErrorInvalidFilter},
		{api.MetricQuery{Name: "tokens", Filters: map[string]string{"model": ""}}, api.MetricErrorInvalidFilter},
		{api.MetricQuery{Name: "tokens", Stat: "p42"}, api.MetricErrorInvalidStat},
		// A stat for a counter other than its sum
		{api.MetricQuery{Name: "tokens", Stat: StatAvg}, api.MetricErrorInvalidStat},
		{api.MetricQuery{Name: "tokens", Stat: StatP50}, api.MetricErrorInvalidStat},
	}
	for _, c := range cases {
		_, err := store.QuerySeries(context.Background(), c.q, r)
		wantCode(t, err, c.code)
		if strings.Contains(err.Error(), "SELECT") {
			t.Errorf("error leaks SQL: %v", err)
		}
	}
	_, err := store.QuerySeries(context.Background(), api.MetricQuery{Name: "tokens"}, SeriesRange{From: r.From, To: r.To, TimeZone: "Mars/Olympus"})
	wantCode(t, err, api.MetricErrorInvalidTimeZone)

	// The sum of a counter is what it shows anyway
	resp := querySeries(t, store, api.MetricQuery{Name: "tokens", Stat: StatSum, Aggregate: true}, r)
	if resp.Stat != "" || resp.Series[0].DataPoints[0][1] != 1 {
		t.Errorf("sum of a counter = %+v", resp)
	}
}

func TestQuerySeriesBatch_ErrorsPerQuery(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	now := time.Now().UTC()
	insertPoints(t, store, sumPoint("tokens", now, 4, false, map[string]string{"model": "a"}))
	resp := store.QuerySeriesBatch(context.Background(), []api.MetricQuery{
		{ID: "ok", Name: "tokens", Aggregate: true},
		{ID: "group", Name: "tokens", GroupBy: []string{"no good"}},
		{ID: "stat", Name: "tokens", Stat: StatP99},
		{ID: "filtered", Name: "tokens", Aggregate: true, Filters: map[string]string{"model": "a"}},
	}, SeriesRange{From: now.Add(-time.Hour), To: now.Add(time.Hour)})
	byID := make(map[string]api.MetricQueryResult)
	for _, r := range resp.Results {
		byID[r.ID] = r
	}
	if r := byID["ok"]; !r.Success || len(r.Series) != 1 || r.MetricType != "sum" {
		t.Errorf("ok = %+v", r)
	}
	if r := byID["filtered"]; !r.Success || len(r.Series) != 1 || r.Series[0].DataPoints[0][1] != 4 {
		t.Errorf("filtered = %+v", r)
	}
	if r := byID["group"]; r.Success || r.Code != api.MetricErrorInvalidGroupBy || !strings.Contains(r.Error, `"no good"`) {
		t.Errorf("group = %+v", r)
	}
	if r := byID["stat"]; r.Success || r.Code != api.MetricErrorInvalidStat || !strings.Contains(r.Error, "histogram") {
		t.Errorf("stat = %+v", r)
	}

	// A timed-out batch reports it per query
	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	time.Sleep(time.Millisecond)
	resp = store.QuerySeriesBatch(ctx, []api.MetricQuery{{ID: "late", Name: "tokens"}}, SeriesRange{From: now.Add(-time.Hour), To: now})
	if r := resp.Results[0]; r.Success || r.Code != api.MetricErrorTimeout {
		t.Errorf("timed out = %+v", r)
	}
}

func TestQuerySeries_TooManySeries(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	now := time.Now().UTC().Truncate(time.Hour)
	var points []api.MetricDataPoint
	for i := 0; i <= MaxSeries; i++ {
		points = append(points, sumPoint("tokens", now.Add(time.Minute), 1, false, map[string]string{"session.id": string(rune('A'+i/26)) + string(rune('a'+i%26))}))
	}
	insertPoints(t, store, points...)
	r := SeriesRange{From: now, To: now.Add(time.Hour), Interval: 600}
	_, err := store.QuerySeries(context.Background(), api.MetricQuery{Name: "tokens", GroupBy: []string{"session.id"}}, r)
	wantCode(t, err, api.MetricErrorTooManySeries)
	// Totals are one point per series and have no limit
	resp := querySeries(t, store, api.MetricQuery{Name: "tokens", GroupBy: []string{"session.id"}, Aggregate: true}, r)
	if len(resp.Series) != MaxSeries+1 {
		t.Errorf("aggregate: %d series, want %d", len(resp.Series), MaxSeries+1)
	}
}

var latencyBounds = []float64{0, 10, 100, 1000}

func TestQuerySeries_HistogramStats(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	now := time.Now().UTC().Truncate(time.Hour)
	// Bucket 1 (0:00-0:10): 10 values in (10, 100], sum 500; bucket 2 is
	// empty; bucket 3 (0:20-0:30): 4 values, 2 in (0, 10] and 2 in (100, 1000]
	insertPoints(t, store,
		histogramPoint("latency", now.Add(time.Minute), false, latencyBounds, []uint64{0, 0, 10, 0, 0}, 500, 20, 90, nil),
		histogramPoint("latency", now.Add(21*time.Minute), false, latencyBounds, []uint64{0, 1, 0, 1, 0}, 600, 5, 550, nil),
		histogramPoint("latency", now.Add(22*time.Minute), false, latencyBounds, []uint64{0, 1, 0, 1, 0}, 400, 4, 380, nil),
	)
	r := SeriesRange{From: now, To: now.Add(30 * time.Minute), Interval: 600}
	at := func(resp *api.TimeSeriesResponse, i int) float64 {
		t.Helper()
		if len(resp.Series) != 1 || len(resp.Series[0].DataPoints) != 4 {
			t.Fatalf("series = %+v, want 1 series of 4 buckets", resp.Series)
		}
		return resp.Series[0].DataPoints[i][1]
	}

	// The default is the average, and buckets without values have none
	resp := querySeries(t, store, api.MetricQuery{Name: "latency"}, r)
	if resp.Stat != StatAvg || resp.MetricType != "histogram" {
		t.Errorf("stat %q, type %q, want avg of a histogram", resp.Stat, resp.MetricType)
	}
	if v := at(resp, 0); v != 50 {
		t.Errorf("avg = %v, want 50", v)
	}
	if v := at(resp, 1); !math.IsNaN(v) {
		t.Errorf("avg of an empty bucket = %v, want NaN", v)
	}
	if v := at(resp, 2); v != 250 {
		t.Errorf("avg = %v, want 250", v)
	}
	body, err := json.Marshal(resp.Series[0])
	if err != nil || !strings.Contains(string(body), ",null]") {
		t.Errorf("JSON = %s (%v), want null for the empty bucket", body, err)
	}

	for stat, want := range map[string][4]float64{
		StatSum:   {500, 0, 1000, 0},
		StatCount: {10, 0, 4, 0},
		StatMin:   {20, math.NaN(), 4, math.NaN()},
		StatMax:   {90, math.NaN(), 550, math.NaN()},
		// 10 values in (10, 100] clamped to [20, 90]: the median at 20 + 0.5*70
		StatP50: {55, math.NaN(), 10, math.NaN()},
	} {
		resp := querySeries(t, store, api.MetricQuery{Name: "latency", Stat: stat}, r)
		for i, w := range want {
			if v := at(resp, i); v != w && !(math.IsNaN(v) && math.IsNaN(w)) {
				t.Errorf("%s bucket %d = %v, want %v", stat, i, v, w)
			}
		}
	}

	// p99 of bucket 3: the 3.96th of 4 values, the second-last of them in
	// (100, 1000] clamped to max 550: 100 + 0.98*450
	resp = querySeries(t, store, api.MetricQuery{Name: "latency", Stat: StatP99}, r)
	if v := at(resp, 2); math.Abs(v-541) > 1e-9 {
		t.Errorf("p99 = %v, want 541", v)
	}
	// Aggregate: all 14 values
	resp = querySeries(t, store, api.MetricQuery{Name: "latency", Aggregate: true, Stat: StatAvg}, r)
	if v := resp.Series[0].DataPoints[0][1]; v != 1500.0/14 {
		t.Errorf("aggregate avg = %v, want %v", v, 1500.0/14)
	}
}

// Cumulative histograms: count, sum and buckets become increases per series
func TestQuerySeries_CumulativeHistogram(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	now := time.Now().UTC().Truncate(time.Hour)
	attrs := map[string]string{"model": "m"}
	insertPoints(t, store,
		// Before the range: 2 values
		histogramPoint("latency", now.Add(-time.Minute), true, latencyBounds, []uint64{0, 2, 0, 0, 0}, 10, 5, 5, attrs),
		// +3 values in (100, 1000], sum +900
		histogramPoint("latency", now.Add(time.Minute), true, latencyBounds, []uint64{0, 2, 0, 3, 0}, 910, 5, 400, attrs),
		// +1 value in (10, 100], sum +50
		histogramPoint("latency", now.Add(11*time.Minute), true, latencyBounds, []uint64{0, 2, 1, 3, 0}, 960, 5, 400, attrs),
		// Reset: 1 value in (0, 10]
		histogramPoint("latency", now.Add(12*time.Minute), true, latencyBounds, []uint64{0, 1, 0, 0, 0}, 8, 8, 8, attrs),
	)
	r := SeriesRange{From: now, To: now.Add(20 * time.Minute), Interval: 600}
	for stat, want := range map[string][]float64{
		StatCount: {3, 2, 0},
		StatSum:   {900, 58, 0},
		StatAvg:   {300, 29, math.NaN()},
		// 3 values in (100, 400]: the median at 100 + 0.5*300
		StatP50: {250, 10, math.NaN()},
	} {
		resp := querySeries(t, store, api.MetricQuery{Name: "latency", Stat: stat, GroupBy: []string{"model"}}, r)
		if len(resp.Series) != 1 || resp.Series[0].Labels["model"] != "m" {
			t.Fatalf("%s: series = %+v", stat, resp.Series)
		}
		for i, w := range want {
			if v := resp.Series[0].DataPoints[i][1]; v != w && !(math.IsNaN(v) && math.IsNaN(w)) {
				t.Errorf("%s bucket %d = %v, want %v", stat, i, v, w)
			}
		}
	}
}

func TestQuerySeries_ExponentialHistogram(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	now := time.Now().UTC()
	count, sum, scale, offset := uint64(4), 40.0, int32(0), int32(0)
	temporality := int32(1)
	insertPoints(t, store, api.MetricDataPoint{
		Timestamp: now, ServiceName: "svc", MetricName: "exp", MetricType: "exponential_histogram",
		AggregationTemporality: &temporality, Count: &count, Sum: &sum, Scale: &scale,
		PositiveOffset: &offset, PositiveBucketCounts: []uint64{1, 3},
	})
	r := SeriesRange{From: now.Add(-time.Minute), To: now.Add(time.Minute)}
	resp := querySeries(t, store, api.MetricQuery{Name: "exp", Aggregate: true}, r)
	if v := resp.Series[0].DataPoints[0][1]; v != 10 || resp.Stat != StatAvg {
		t.Errorf("avg = %v (%s), want 10", v, resp.Stat)
	}
	_, err := store.QuerySeries(context.Background(), api.MetricQuery{Name: "exp", Stat: StatP95}, r)
	wantCode(t, err, api.MetricErrorInvalidStat)
}

func TestQuerySeries_GaugeEmptyBuckets(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	now := time.Now().UTC().Truncate(time.Hour)
	v := 3.0
	insertPoints(t, store, api.MetricDataPoint{Timestamp: now.Add(time.Minute), ServiceName: "svc", MetricName: "level", MetricType: "gauge", Value: &v})
	resp := querySeries(t, store, api.MetricQuery{Name: "level"}, SeriesRange{From: now, To: now.Add(15 * time.Minute), Interval: 600})
	dp := resp.Series[0].DataPoints
	if len(dp) != 2 || dp[0][1] != 3 || !math.IsNaN(dp[1][1]) {
		t.Errorf("gauge buckets = %v, want 3 and no value", dp)
	}
}

// Buckets in a time zone follow its clock across daylight saving changes
func TestQuerySeries_TimeZoneBuckets(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	berlin, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatal(err)
	}
	local := func(month time.Month, day, hour, minute int) time.Time {
		return time.Date(2026, month, day, hour, minute, 0, 0, berlin)
	}
	// Clocks go forward on 29 March (02:00 → 03:00) and back on 25 October
	// (03:00 → 02:00)
	insertPoints(t, store,
		sumPoint("events", local(3, 28, 23, 30), 1, false, nil),
		sumPoint("events", local(3, 29, 0, 30), 2, false, nil),
		sumPoint("events", local(3, 29, 23, 30), 4, false, nil),
		sumPoint("events", local(10, 25, 0, 30), 8, false, nil),
		sumPoint("events", local(10, 25, 23, 30), 16, false, nil),
	)
	day := int64(24 * 3600)

	// Daily buckets start at Berlin's midnights; 29 March has 23 hours
	resp := querySeries(t, store, api.MetricQuery{Name: "events"},
		SeriesRange{From: local(3, 28, 12, 0), To: local(3, 30, 12, 0), Interval: day, TimeZone: "Europe/Berlin"})
	if resp.TimeZone != "Europe/Berlin" {
		t.Errorf("time zone = %q", resp.TimeZone)
	}
	want := []struct {
		start time.Time
		value float64
	}{{local(3, 28, 0, 0), 1}, {local(3, 29, 0, 0), 6}, {local(3, 30, 0, 0), 0}}
	dp := resp.Series[0].DataPoints
	if len(dp) != len(want) {
		t.Fatalf("buckets = %v, want %d", dp, len(want))
	}
	for i, w := range want {
		if int64(dp[i][0]) != w.start.UnixMilli() || dp[i][1] != w.value {
			t.Errorf("bucket %d = %v at %v, want %v at %v", i, dp[i][1], time.UnixMilli(int64(dp[i][0])).In(berlin), w.value, w.start)
		}
	}
	// UTC splits them at UTC midnight instead
	resp = querySeries(t, store, api.MetricQuery{Name: "events"}, SeriesRange{From: local(3, 28, 12, 0), To: local(3, 30, 12, 0), Interval: day})
	if got := resp.Series[0].DataPoints[0]; int64(got[0]) != time.Date(2026, 3, 28, 0, 0, 0, 0, time.UTC).UnixMilli() || got[1] != 3 {
		t.Errorf("UTC first day = %v, want 3 at UTC midnight", got)
	}

	// 25 October has 25 hours
	resp = querySeries(t, store, api.MetricQuery{Name: "events"},
		SeriesRange{From: local(10, 25, 0, 0), To: local(10, 25, 23, 59), Interval: day, TimeZone: "Europe/Berlin"})
	if dp := resp.Series[0].DataPoints; len(dp) != 1 || int64(dp[0][0]) != local(10, 25, 0, 0).UnixMilli() || dp[0][1] != 24 {
		t.Errorf("25 October = %v, want 24 at local midnight", dp)
	}

	// Hourly buckets on 29 March: the skipped hour has no bucket, and no
	// two buckets share a start
	resp = querySeries(t, store, api.MetricQuery{Name: "events"},
		SeriesRange{From: local(3, 29, 0, 0), To: local(3, 29, 5, 0), Interval: 3600, TimeZone: "Europe/Berlin"})
	dp = resp.Series[0].DataPoints
	if len(dp) != 5 {
		t.Errorf("hourly buckets = %d, want 5 (00, 01, 03, 04, 05)", len(dp))
	}
	for i := 1; i < len(dp); i++ {
		if dp[i][0]-dp[i-1][0] != 3600_000 {
			t.Errorf("bucket %d starts %v ms after the previous one", i, dp[i][0]-dp[i-1][0])
		}
	}
}

func TestValidateTimeZone(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	for _, name := range []string{"", "UTC", "Europe/Berlin", "America/Los_Angeles", "Asia/Kolkata", "Etc/GMT+5"} {
		if err := store.ValidateTimeZone(name); err != nil {
			t.Errorf("ValidateTimeZone(%q) = %v", name, err)
		}
	}
	for _, name := range []string{"Local", "Mars/Olympus", "Europe/Berlin'; --", "../etc/passwd", "+01:00"} {
		wantCode(t, store.ValidateTimeZone(name), api.MetricErrorInvalidTimeZone)
	}
	if zone := store.LocalTimeZone(); store.ValidateTimeZone(zone) != nil || zone == "" {
		t.Errorf("LocalTimeZone() = %q, not a valid zone", zone)
	}
}

func TestGetMetricAttributeKeys(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	now := time.Now().UTC()
	insertPoints(t, store,
		sumPoint("tokens", now, 1, false, map[string]string{"model": "a", "type": "input"}),
		sumPoint("tokens", now, 1, false, map[string]string{"model": "b", "repository": "r", "bad key": "x"}),
		sumPoint("other", now, 1, false, map[string]string{"other": "x"}),
	)
	keys, err := store.GetMetricAttributeKeys(context.Background(), "tokens", "")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(keys, ",") != "model,repository,type" {
		t.Errorf("keys = %v", keys)
	}
	keys, err = store.GetMetricAttributeKeys(context.Background(), "tokens", "nobody")
	if err != nil || keys == nil || len(keys) != 0 {
		t.Errorf("keys of another service = %v, %v; want an empty list", keys, err)
	}
}

func TestEstimateQuantile(t *testing.T) {
	nan := math.NaN()
	h := bucketCounts{bounds: []float64{0, 10, 100}, counts: []float64{0, 4, 4, 2}}
	cases := []struct {
		name     string
		hists    []bucketCounts
		q        float64
		lo, hi   float64
		want     float64
		wantsNaN bool
	}{
		{"interpolates within a bucket", []bucketCounts{h}, 0.25, nan, nan, 6.25, false},
		{"bucket edge", []bucketCounts{h}, 0.4, nan, nan, 10, false},
		{"second bucket", []bucketCounts{h}, 0.6, nan, nan, 55, false},
		// The open last bucket ends at the last bound without a max
		{"open last bucket", []bucketCounts{h}, 0.99, nan, nan, 100, false},
		{"open last bucket ends at max", []bucketCounts{h}, 0.9, nan, 300, 200, false},
		{"clamped to min", []bucketCounts{h}, 0.25, 6, nan, 8.5, false},
		// Below a positive first bound values start at 0
		{"open first bucket", []bucketCounts{{bounds: []float64{10}, counts: []float64{2, 0}}}, 0.5, nan, nan, 5, false},
		{"no values", []bucketCounts{{bounds: []float64{10}, counts: []float64{0, 0}}}, 0.5, nan, nan, 0, true},
		{"no histograms", nil, 0.5, nan, nan, 0, true},
		// Different bounds merge: 4 values in (0, 10], 4 in (0, 20]; the
		// distribution reaches 4 of 8 at x where x/10*4 + x/20*4 = 4
		{"merged bounds", []bucketCounts{
			{bounds: []float64{0, 10}, counts: []float64{0, 4, 0}},
			{bounds: []float64{0, 20}, counts: []float64{0, 4, 0}},
		}, 0.5, nan, nan, 20.0 / 3, false},
		// A point bucket (values of exactly 0) holds the median
		{"point mass", []bucketCounts{
			{bounds: []float64{0, 10}, counts: []float64{6, 2, 0}},
			{bounds: []float64{0, 20}, counts: []float64{0, 2, 0}},
		}, 0.5, nan, nan, 0, false},
	}
	for _, c := range cases {
		got := estimateQuantile(c.hists, c.q, c.lo, c.hi)
		if c.wantsNaN {
			if !math.IsNaN(got) {
				t.Errorf("%s: %v, want NaN", c.name, got)
			}
			continue
		}
		if math.Abs(got-c.want) > 1e-9 {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}

func TestSeriesBucketSeconds(t *testing.T) {
	now := time.Now()
	for _, c := range []struct {
		rng      time.Duration
		interval int64
		want     int64
	}{
		{time.Hour, 0, DefaultSeriesInterval},
		{time.Hour, 300, 300},
		// Longer than a day: weekly buckets for a half-year chart
		{180 * 24 * time.Hour, 7 * 86400, 7 * 86400},
		{time.Hour, MaxSeriesInterval * 2, MaxSeriesInterval},
		// Too many buckets get coarser
		{30 * 24 * time.Hour, 60, (30*86400 + maxSeriesBuckets - 1) / maxSeriesBuckets},
	} {
		if got := seriesBucketSeconds(now.Add(-c.rng), now, c.interval); got != c.want {
			t.Errorf("seriesBucketSeconds(%v, %d) = %d, want %d", c.rng, c.interval, got, c.want)
		}
	}
}

// After deleted points the last of them (metric_baselines) is the previous
// point of a cumulative histogram's next point: its count, sum and buckets
// count their increase over it. Baselines stored before migration 9 lack
// the count and buckets: the next point then counts for the sum only.
func TestQuerySeries_CumulativeHistogramAfterDelete(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		t.Run(fmt.Sprintf("legacy=%v", legacy), func(t *testing.T) {
			store, cleanup := setupTestStore(t)
			defer cleanup()
			ctx := context.Background()
			now := time.Now().UTC().Truncate(time.Hour)
			insertPoints(t, store,
				histogramPoint("latency", now.Add(-30*time.Minute), true, latencyBounds, []uint64{0, 2, 0, 0, 0}, 10, 5, 5, nil),
				histogramPoint("latency", now.Add(time.Minute), true, latencyBounds, []uint64{0, 2, 0, 3, 0}, 910, 5, 400, nil),
				histogramPoint("latency", now.Add(2*time.Minute), true, latencyBounds, []uint64{0, 2, 1, 3, 0}, 960, 5, 400, nil),
			)
			if _, err := store.DeleteMetricsInRange(ctx, now.Add(-time.Hour), now.Add(-time.Minute), ""); err != nil {
				t.Fatal(err)
			}
			var count sql.NullInt64
			var buckets sql.NullString
			if err := store.db.QueryRow(`SELECT Count, CAST(BucketCounts AS VARCHAR) FROM metric_baselines WHERE MetricName = 'latency'`).Scan(&count, &buckets); err != nil {
				t.Fatal(err)
			}
			if count.Int64 != 2 || buckets.String != "[0,2,0,0,0]" {
				t.Fatalf("baseline count %v, buckets %v: want those of the deleted point", count, buckets)
			}
			want := map[string]float64{
				StatSum:   950, // 900 over the baseline, then 50
				StatCount: 4,   // 3, then 1
				StatAvg:   237.5,
				StatP50:   200, // 1 value in (10, 100], 3 in (100, 1000] cut at the max, 400
			}
			if legacy {
				if _, err := store.db.Exec(`UPDATE metric_baselines SET Count = NULL, BucketCounts = NULL, ExplicitBounds = NULL`); err != nil {
					t.Fatal(err)
				}
				want = map[string]float64{
					StatSum:   950,
					StatCount: 1, // only the second point's increase is known
					StatAvg:   50,
					StatP50:   55, // the one value in (10, 100]
				}
			}
			r := SeriesRange{From: now, To: now.Add(5 * time.Minute)}
			for stat, want := range want {
				resp := querySeries(t, store, api.MetricQuery{Name: "latency", Aggregate: true, Stat: stat}, r)
				if v := resp.Series[0].DataPoints[0][1]; math.Abs(v-want) > 1e-9 {
					t.Errorf("%s = %v, want %v", stat, v, want)
				}
			}
		})
	}
}

// Merged: one series of all points, across services and attributes
func TestQuerySeries_Merge(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	now := time.Now().UTC().Truncate(time.Hour)
	other := histogramPoint("latency", now.Add(2*time.Minute), false, latencyBounds, []uint64{0, 0, 0, 2, 0}, 1000, 200, 800, map[string]string{"model": "b"})
	other.ServiceName = "other"
	insertPoints(t, store,
		histogramPoint("latency", now.Add(time.Minute), false, latencyBounds, []uint64{0, 0, 2, 0, 0}, 100, 20, 80, map[string]string{"model": "a"}),
		other,
		sumPoint("tokens", now.Add(time.Minute), 3, false, map[string]string{"type": "input"}),
		sumPoint("tokens", now.Add(time.Minute), 4, false, map[string]string{"type": "output"}),
	)
	r := SeriesRange{From: now, To: now.Add(10 * time.Minute), Interval: 600}
	resp := querySeries(t, store, api.MetricQuery{Name: "latency", Aggregate: true, Merge: true}, r)
	if len(resp.Series) != 1 || len(resp.Series[0].Labels) != 0 || resp.Series[0].DataPoints[0][1] != 275 {
		t.Errorf("merged avg = %+v, want one unlabelled series of 275", resp.Series)
	}
	resp = querySeries(t, store, api.MetricQuery{Name: "latency", Merge: true, Stat: StatMax, Filters: map[string]string{"model": "a"}}, r)
	if len(resp.Series) != 1 || resp.Series[0].DataPoints[0][1] != 80 {
		t.Errorf("merged max of a = %+v", resp.Series)
	}
	resp = querySeries(t, store, api.MetricQuery{Name: "tokens", Merge: true}, r)
	if len(resp.Series) != 1 || resp.Series[0].DataPoints[0][1] != 7 {
		t.Errorf("merged tokens = %+v", resp.Series)
	}
	_, err := store.QuerySeries(context.Background(), api.MetricQuery{Name: "tokens", Merge: true, GroupBy: []string{"type"}}, r)
	wantCode(t, err, api.MetricErrorInvalidGroupBy)
}

func TestQuerySeriesRepeatedHour(t *testing.T) {
	s, cleanup := setupTestStore(t)
	defer cleanup()
	first := time.Date(2026, 10, 25, 0, 15, 0, 0, time.UTC)
	second := first.Add(time.Hour)
	insertPoints(t, s, sumPoint("events", first, 1, false, nil), sumPoint("events", second, 2, false, nil))
	resp := querySeries(t, s, api.MetricQuery{Name: "events"}, SeriesRange{From: first.Truncate(time.Hour), To: second, Interval: 3600, TimeZone: "Europe/Berlin"})
	points := resp.Series[0].DataPoints
	if len(points) != 2 || int64(points[0][0]) != first.Truncate(time.Hour).UnixMilli() || int64(points[1][0]) != second.Truncate(time.Hour).UnixMilli() ||
		points[0][1] != 1 || points[1][1] != 2 {
		t.Errorf("repeated local hour: got %v, want separate UTC hours with values 1 and 2", points)
	}
}

func TestQuerySeriesSubdayLocalAlignment(t *testing.T) {
	s, cleanup := setupTestStore(t)
	defer cleanup()
	ts := time.Date(2026, 7, 1, 4, 47, 0, 0, time.UTC)
	insertPoints(t, s, sumPoint("events", ts, 1, false, nil))
	for _, tc := range []struct {
		zone    string
		seconds int64
		start   time.Time
	}{
		{"Europe/Berlin", 6 * 3600, ts.Truncate(time.Hour)}, // 06:00 in summer
		{"Asia/Kolkata", 3600, ts.Truncate(time.Hour).Add(30 * time.Minute)},
	} {
		resp := querySeries(t, s, api.MetricQuery{Name: "events"}, SeriesRange{From: ts, To: ts, Interval: tc.seconds, TimeZone: tc.zone})
		points := resp.Series[0].DataPoints
		if len(points) != 1 || int64(points[0][0]) != tc.start.UnixMilli() || points[0][1] != 1 {
			t.Errorf("%s grid: got %v, want one event at %v", tc.zone, points, tc.start)
		}
	}
}
