package handlers

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/tobilg/ai-observer/internal/storage"
	colmetricspb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	metricspb "go.opentelemetry.io/proto/otlp/metrics/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	"google.golang.org/protobuf/proto"
)

// Histograms, exponential histograms and summaries sent over OTLP are stored
// with their count, sum, min and max, and series read their sums
func TestHandleMetrics_DistributionTypesRoundTrip(t *testing.T) {
	h, cleanup := setupTestHandlers(t)
	defer cleanup()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	ts := uint64(now.Add(-time.Minute).UnixNano())
	delta := metricspb.AggregationTemporality_AGGREGATION_TEMPORALITY_DELTA

	req := &colmetricspb.ExportMetricsServiceRequest{ResourceMetrics: []*metricspb.ResourceMetrics{{
		Resource: &resourcepb.Resource{Attributes: []*commonpb.KeyValue{
			{Key: "service.name", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "gemini_cli"}}},
		}},
		ScopeMetrics: []*metricspb.ScopeMetrics{{Metrics: []*metricspb.Metric{
			{Name: "latency.histogram", Data: &metricspb.Metric_Histogram{Histogram: &metricspb.Histogram{
				AggregationTemporality: delta,
				DataPoints: []*metricspb.HistogramDataPoint{{
					TimeUnixNano: ts, Count: 3, Sum: proto.Float64(600), Min: proto.Float64(100), Max: proto.Float64(300),
					BucketCounts: []uint64{0, 2, 1}, ExplicitBounds: []float64{50, 250},
				}},
			}}},
			{Name: "latency.exponential", Data: &metricspb.Metric_ExponentialHistogram{ExponentialHistogram: &metricspb.ExponentialHistogram{
				AggregationTemporality: delta,
				DataPoints: []*metricspb.ExponentialHistogramDataPoint{{
					TimeUnixNano: ts, Count: 4, Sum: proto.Float64(40), Scale: 2, ZeroCount: 1,
					Positive: &metricspb.ExponentialHistogramDataPoint_Buckets{Offset: 1, BucketCounts: []uint64{3}},
					Min:      proto.Float64(0), Max: proto.Float64(20),
				}},
			}}},
			{Name: "latency.summary", Data: &metricspb.Metric_Summary{Summary: &metricspb.Summary{
				DataPoints: []*metricspb.SummaryDataPoint{{
					TimeUnixNano: ts, Count: 5, Sum: 50,
					QuantileValues: []*metricspb.SummaryDataPoint_ValueAtQuantile{{Quantile: 0.5, Value: 9}},
				}},
			}}},
		}}},
	}}}
	body, err := proto.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	httpReq := httptest.NewRequest(http.MethodPost, "/v1/metrics", bytes.NewReader(body))
	httpReq.Header.Set("Content-Type", "application/x-protobuf")
	rec := httptest.NewRecorder()
	h.HandleMetrics(rec, httpReq)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}

	from, to := now.Add(-time.Hour), now
	want := []struct {
		name, metricType string
		count            uint64
		sum              float64
		min, max         *float64
	}{
		{"latency.histogram", "histogram", 3, 600, proto.Float64(100), proto.Float64(300)},
		{"latency.exponential", "exponential_histogram", 4, 40, proto.Float64(0), proto.Float64(20)},
		{"latency.summary", "summary", 5, 50, nil, nil},
	}
	for _, w := range want {
		resp, err := h.store.QueryMetrics(ctx, "gemini_cli", w.name, "", from, to, storage.Page{Limit: 10})
		if err != nil {
			t.Fatal(err)
		}
		if len(resp.Metrics) != 1 {
			t.Fatalf("%s: points = %d, want 1", w.name, len(resp.Metrics))
		}
		p := resp.Metrics[0]
		if p.MetricType != w.metricType || p.Count == nil || *p.Count != w.count || p.Sum == nil || *p.Sum != w.sum {
			t.Errorf("%s: stored %+v, want type %s, count %d, sum %v", w.name, p, w.metricType, w.count, w.sum)
		}
		if (w.min == nil) != (p.Min == nil) || (w.min != nil && (*p.Min != *w.min || *p.Max != *w.max)) {
			t.Errorf("%s: min/max = %v/%v, want %v/%v", w.name, p.Min, p.Max, w.min, w.max)
		}

		series, err := h.store.QueryMetricSeries(ctx, w.name, "gemini_cli", from, to, 60, true)
		if err != nil {
			t.Fatal(err)
		}
		var total float64
		for _, s := range series.Series {
			for _, dp := range s.DataPoints {
				total += dp[1]
			}
		}
		if total != w.sum {
			t.Errorf("%s: series total = %v, want the sum %v", w.name, total, w.sum)
		}
	}
}
