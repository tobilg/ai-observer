package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/tobilg/ai-observer/internal/api"
)

func insertMetricPoints(t *testing.T, h *Handlers, now time.Time) {
	t.Helper()
	value := func(v float64) *float64 { return &v }
	delta := int32(1)
	count, sum, lo, hi := uint64(4), 200.0, 10.0, 90.0
	points := []api.MetricDataPoint{
		{Timestamp: now, ServiceName: "svc", MetricName: "tokens", MetricType: "sum", Value: value(3), AggregationTemporality: &delta,
			Attributes: map[string]string{"model": "a", "type": "input"}},
		{Timestamp: now, ServiceName: "svc", MetricName: "tokens", MetricType: "sum", Value: value(5), AggregationTemporality: &delta,
			Attributes: map[string]string{"model": "b", "type": "input"}},
		{Timestamp: now, ServiceName: "svc", MetricName: "latency", MetricType: "histogram", AggregationTemporality: &delta,
			Count: &count, Sum: &sum, Min: &lo, Max: &hi, ExplicitBounds: []float64{0, 100}, BucketCounts: []uint64{0, 4, 0}},
	}
	if err := h.store.InsertMetrics(context.Background(), points); err != nil {
		t.Fatal(err)
	}
}

func getSeries(t *testing.T, h *Handlers, params url.Values) (*httptest.ResponseRecorder, api.TimeSeriesResponse) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/metrics/series?"+params.Encode(), nil)
	rec := httptest.NewRecorder()
	h.QueryMetricSeries(rec, req)
	var resp api.TimeSeriesResponse
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decoding %s: %v", rec.Body, err)
		}
	}
	return rec, resp
}

func TestQueryMetricSeries_Parameters(t *testing.T) {
	h, cleanup := setupTestHandlers(t)
	defer cleanup()
	now := time.Now().UTC().Truncate(time.Hour).Add(time.Minute)
	insertMetricPoints(t, h, now)
	rangeParams := func(extra ...string) url.Values {
		v := url.Values{"from": {now.Add(-time.Minute).Format(time.RFC3339)}, "to": {now.Add(time.Hour).Format(time.RFC3339)}}
		for i := 0; i+1 < len(extra); i += 2 {
			v.Add(extra[i], extra[i+1])
		}
		return v
	}

	// groupBy and attr.* filters
	rec, resp := getSeries(t, h, rangeParams("name", "tokens", "aggregate", "true", "groupBy", "model"))
	if rec.Code != http.StatusOK || len(resp.Series) != 2 {
		t.Fatalf("by model: %d %s", rec.Code, rec.Body)
	}
	rec, resp = getSeries(t, h, rangeParams("name", "tokens", "aggregate", "true", "attr.model", "b"))
	if rec.Code != http.StatusOK || len(resp.Series) != 1 || resp.Series[0].DataPoints[0][1] != 5 {
		t.Errorf("model b: %d %s", rec.Code, rec.Body)
	}
	rec, resp = getSeries(t, h, rangeParams("name", "tokens", "groupBy", "model,type", "interval", "3600", "tz", "Europe/Berlin"))
	if rec.Code != http.StatusOK || len(resp.Series) != 2 || resp.Series[0].Labels["type"] != "input" || resp.Interval != 3600 || resp.TimeZone != "Europe/Berlin" {
		t.Errorf("by model and type: %d %s", rec.Code, rec.Body)
	}

	// Histograms: avg by default, other stats on request, null without values
	rec, resp = getSeries(t, h, rangeParams("name", "latency", "interval", "1800"))
	if rec.Code != http.StatusOK || resp.Stat != "avg" || resp.MetricType != "histogram" {
		t.Fatalf("latency: %d %s", rec.Code, rec.Body)
	}
	if dp := resp.Series[0].DataPoints; dp[0][1] != 50 || !math.IsNaN(dp[len(dp)-1][1]) {
		t.Errorf("latency avg = %v, want 50, then no value", dp)
	}
	if !strings.Contains(rec.Body.String(), ",null]") {
		t.Errorf("empty buckets are not null: %s", rec.Body)
	}
	rec, resp = getSeries(t, h, rangeParams("name", "latency", "aggregate", "true", "stat", "p50"))
	if rec.Code != http.StatusOK || resp.Series[0].DataPoints[0][1] != 50 {
		t.Errorf("latency p50: %d %s", rec.Code, rec.Body)
	}

	for _, c := range []struct {
		name   string
		params url.Values
		want   string
	}{
		{"missing name", rangeParams(), "name is required"},
		{"interval not a number", rangeParams("name", "tokens", "interval", "1 minute"), `invalid interval "1 minute"`},
		{"interval zero", rangeParams("name", "tokens", "interval", "0"), "invalid interval"},
		{"interval too large", rangeParams("name", "tokens", "interval", "99999999"), "invalid interval"},
		{"aggregate not a bool", rangeParams("name", "tokens", "aggregate", "yes"), "invalid aggregate"},
		{"groupBy key", rangeParams("name", "tokens", "groupBy", "bad'key"), "invalid groupBy"},
		{"three groupBy keys", rangeParams("name", "tokens", "groupBy", "a,b,c"), "at most 2"},
		{"filter key", rangeParams("name", "tokens", "attr.bad key", "x"), "invalid filter key"},
		{"filter twice", rangeParams("name", "tokens", "attr.model", "a", "attr.model", "b"), "attr.model is given 2 times"},
		{"unknown stat", rangeParams("name", "latency", "stat", "median"), `unknown stat "median"`},
		{"stat of a counter", rangeParams("name", "tokens", "stat", "avg"), "stat avg applies to histogram metrics"},
		{"time zone", rangeParams("name", "tokens", "tz", "Mars/Olympus"), "unknown time zone"},
	} {
		rec, _ := getSeries(t, h, c.params)
		var body api.ErrorResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if rec.Code != http.StatusBadRequest || !strings.Contains(body.Message, c.want) {
			t.Errorf("%s: %d %s, want 400 with %q", c.name, rec.Code, rec.Body, c.want)
		}
		if strings.Contains(strings.ToUpper(rec.Body.String()), "SELECT") {
			t.Errorf("%s: response leaks SQL: %s", c.name, rec.Body)
		}
	}
}

func postBatch(t *testing.T, h *Handlers, body any) (*httptest.ResponseRecorder, api.BatchMetricSeriesResponse) {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/metrics/batch-series", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.QueryBatchMetricSeries(rec, req)
	var resp api.BatchMetricSeriesResponse
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decoding %s: %v", rec.Body, err)
		}
	}
	return rec, resp
}

func TestQueryBatchMetricSeries_PerQueryErrors(t *testing.T) {
	h, cleanup := setupTestHandlers(t)
	defer cleanup()
	now := time.Now().UTC().Truncate(time.Minute)
	insertMetricPoints(t, h, now)
	from, to := now.Add(-time.Hour).Format(time.RFC3339), now.Add(time.Minute).Format(time.RFC3339)

	rec, resp := postBatch(t, h, map[string]any{
		"from": from, "to": to, "interval": 600, "timeZone": "America/New_York",
		"queries": []map[string]any{
			{"id": "by-model", "name": "tokens", "groupBy": []string{"model"}},
			{"id": "filtered", "name": "tokens", "aggregate": true, "filters": map[string]string{"model": "a"}},
			{"id": "p99", "name": "latency", "aggregate": true, "stat": "p99"},
			{"id": "bad-group", "name": "tokens", "groupBy": []string{"no good"}},
			{"id": "bad-filter", "name": "tokens", "filters": map[string]string{"x": ""}},
			{"id": "bad-stat", "name": "tokens", "stat": "max"},
		},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	byID := make(map[string]api.MetricQueryResult)
	for _, r := range resp.Results {
		byID[r.ID] = r
	}
	if r := byID["by-model"]; !r.Success || len(r.Series) != 2 || r.Series[0].Labels["model"] != "a" {
		t.Errorf("by-model = %+v", r)
	}
	if r := byID["filtered"]; !r.Success || len(r.Series) != 1 || r.Series[0].DataPoints[0][1] != 3 {
		t.Errorf("filtered = %+v", r)
	}
	if r := byID["p99"]; !r.Success || r.Stat != "p99" || r.MetricType != "histogram" || r.Series[0].DataPoints[0][1] != 89.2 {
		t.Errorf("p99 = %+v", r)
	}
	for id, code := range map[string]string{
		"bad-group":  api.MetricErrorInvalidGroupBy,
		"bad-filter": api.MetricErrorInvalidFilter,
		"bad-stat":   api.MetricErrorInvalidStat,
	} {
		if r := byID[id]; r.Success || r.Code != code || r.Error == "" {
			t.Errorf("%s = %+v, want code %s", id, r, code)
		}
	}

	for name, body := range map[string]map[string]any{
		"negative interval": {"interval": -5, "queries": []map[string]any{{"id": "q", "name": "tokens"}}},
		"huge interval":     {"interval": 99999999, "queries": []map[string]any{{"id": "q", "name": "tokens"}}},
		"interval string":   {"interval": "1 hour", "queries": []map[string]any{{"id": "q", "name": "tokens"}}},
		"time zone":         {"timeZone": "Nowhere/Land", "queries": []map[string]any{{"id": "q", "name": "tokens"}}},
	} {
		if rec, _ := postBatch(t, h, body); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400: %s", name, rec.Code, rec.Body)
		}
	}
}

func TestGetMetricAttributeKeys(t *testing.T) {
	h, cleanup := setupTestHandlers(t)
	defer cleanup()
	insertMetricPoints(t, h, time.Now().UTC())

	req := httptest.NewRequest(http.MethodGet, "/api/metrics/attribute-keys?name=tokens", nil)
	rec := httptest.NewRecorder()
	h.GetMetricAttributeKeys(rec, req)
	var resp api.MetricAttributeKeysResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if strings.Join(resp.Keys, ",") != "model,type" {
		t.Errorf("keys = %v", resp.Keys)
	}

	rec = httptest.NewRecorder()
	h.GetMetricAttributeKeys(rec, httptest.NewRequest(http.MethodGet, "/api/metrics/attribute-keys", nil))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("without name: %d", rec.Code)
	}
}
