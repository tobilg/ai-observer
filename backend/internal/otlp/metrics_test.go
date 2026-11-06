package otlp

import (
	"reflect"
	"testing"
	"time"

	"github.com/tobilg/ai-observer/internal/api"
	colmetricspb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	metricspb "go.opentelemetry.io/proto/otlp/metrics/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	"google.golang.org/protobuf/proto"
)

var metricTestTime = time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC)

func metricRequest(metrics ...*metricspb.Metric) *colmetricspb.ExportMetricsServiceRequest {
	return &colmetricspb.ExportMetricsServiceRequest{
		ResourceMetrics: []*metricspb.ResourceMetrics{{
			Resource: &resourcepb.Resource{Attributes: []*commonpb.KeyValue{
				{Key: "service.name", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "gemini_cli"}}},
			}},
			ScopeMetrics: []*metricspb.ScopeMetrics{{
				Scope:   &commonpb.InstrumentationScope{Name: "scope", Version: "1.0"},
				Metrics: metrics,
			}},
		}},
	}
}

func pointAttrs(key, value string) []*commonpb.KeyValue {
	return []*commonpb.KeyValue{{Key: key, Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: value}}}}
}

func convertOne(t *testing.T, m *metricspb.Metric) []api.MetricDataPoint {
	t.Helper()
	points := ConvertMetrics(metricRequest(m)).Metrics
	for i, p := range points {
		if p.ServiceName != "gemini_cli" || p.MetricName != m.GetName() || p.MetricUnit != m.GetUnit() ||
			p.ScopeName != "scope" || p.ScopeVersion != "1.0" || !p.Timestamp.Equal(metricTestTime) {
			t.Errorf("point %d metadata = %+v", i, p)
		}
	}
	return points
}

func TestConvertGaugeAndSum(t *testing.T) {
	gauge := convertOne(t, &metricspb.Metric{Name: "memory", Unit: "By", Data: &metricspb.Metric_Gauge{Gauge: &metricspb.Gauge{
		DataPoints: []*metricspb.NumberDataPoint{
			{TimeUnixNano: uint64(metricTestTime.UnixNano()), Value: &metricspb.NumberDataPoint_AsInt{AsInt: 42}, Attributes: pointAttrs("kind", "heap")},
		},
	}}})
	if len(gauge) != 1 || gauge[0].MetricType != "gauge" || *gauge[0].Value != 42 || gauge[0].Attributes["kind"] != "heap" {
		t.Errorf("gauge = %+v", gauge)
	}
	if gauge[0].AggregationTemporality != nil {
		t.Errorf("gauge temporality = %v, want none", *gauge[0].AggregationTemporality)
	}

	sum := convertOne(t, &metricspb.Metric{Name: "tokens", Unit: "1", Data: &metricspb.Metric_Sum{Sum: &metricspb.Sum{
		AggregationTemporality: metricspb.AggregationTemporality_AGGREGATION_TEMPORALITY_CUMULATIVE,
		IsMonotonic:            true,
		DataPoints: []*metricspb.NumberDataPoint{
			{TimeUnixNano: uint64(metricTestTime.UnixNano()), Value: &metricspb.NumberDataPoint_AsDouble{AsDouble: 1.5}},
		},
	}}})
	if len(sum) != 1 || sum[0].MetricType != "sum" || *sum[0].Value != 1.5 || *sum[0].AggregationTemporality != 2 || !*sum[0].IsMonotonic {
		t.Errorf("sum = %+v", sum)
	}
}

func TestConvertHistogram(t *testing.T) {
	points := convertOne(t, &metricspb.Metric{Name: "gemini_cli.api.request.latency", Unit: "ms", Data: &metricspb.Metric_Histogram{Histogram: &metricspb.Histogram{
		AggregationTemporality: metricspb.AggregationTemporality_AGGREGATION_TEMPORALITY_CUMULATIVE,
		DataPoints: []*metricspb.HistogramDataPoint{
			{
				TimeUnixNano:   uint64(metricTestTime.UnixNano()),
				Attributes:     pointAttrs("model", "gemini-2.5-pro"),
				Count:          3,
				Sum:            proto.Float64(600),
				Min:            proto.Float64(100),
				Max:            proto.Float64(300),
				BucketCounts:   []uint64{0, 2, 1},
				ExplicitBounds: []float64{50, 250},
			},
			// Sum, min and max are optional
			{TimeUnixNano: uint64(metricTestTime.UnixNano()), Count: 0},
		},
	}}})
	if len(points) != 2 {
		t.Fatalf("points = %d, want 2", len(points))
	}
	p := points[0]
	if p.MetricType != "histogram" || p.Value != nil || *p.Count != 3 || *p.Sum != 600 || *p.Min != 100 || *p.Max != 300 {
		t.Errorf("histogram = %+v", p)
	}
	if !reflect.DeepEqual(p.BucketCounts, []uint64{0, 2, 1}) || !reflect.DeepEqual(p.ExplicitBounds, []float64{50, 250}) {
		t.Errorf("buckets = %v, bounds = %v", p.BucketCounts, p.ExplicitBounds)
	}
	if *p.AggregationTemporality != 2 || p.Attributes["model"] != "gemini-2.5-pro" {
		t.Errorf("temporality = %d, attributes = %v", *p.AggregationTemporality, p.Attributes)
	}
	if empty := points[1]; empty.Sum != nil || empty.Min != nil || empty.Max != nil || *empty.Count != 0 {
		t.Errorf("histogram without sum/min/max = %+v", empty)
	}
}

func TestConvertExponentialHistogram(t *testing.T) {
	points := convertOne(t, &metricspb.Metric{Name: "latency", Unit: "ms", Data: &metricspb.Metric_ExponentialHistogram{ExponentialHistogram: &metricspb.ExponentialHistogram{
		AggregationTemporality: metricspb.AggregationTemporality_AGGREGATION_TEMPORALITY_DELTA,
		DataPoints: []*metricspb.ExponentialHistogramDataPoint{
			{
				TimeUnixNano: uint64(metricTestTime.UnixNano()),
				Count:        6,
				Sum:          proto.Float64(12.5),
				Scale:        3,
				ZeroCount:    1,
				Positive:     &metricspb.ExponentialHistogramDataPoint_Buckets{Offset: 2, BucketCounts: []uint64{1, 3}},
				Negative:     &metricspb.ExponentialHistogramDataPoint_Buckets{Offset: -1, BucketCounts: []uint64{1}},
				Min:          proto.Float64(-0.5),
				Max:          proto.Float64(8),
			},
			// Without buckets of a sign, their offset stays unset
			{TimeUnixNano: uint64(metricTestTime.UnixNano()), Count: 1, ZeroCount: 1},
		},
	}}})
	if len(points) != 2 {
		t.Fatalf("points = %d, want 2", len(points))
	}
	p := points[0]
	if p.MetricType != "exponential_histogram" || *p.Count != 6 || *p.Sum != 12.5 || *p.Scale != 3 || *p.ZeroCount != 1 {
		t.Errorf("exponential histogram = %+v", p)
	}
	if *p.PositiveOffset != 2 || !reflect.DeepEqual(p.PositiveBucketCounts, []uint64{1, 3}) ||
		*p.NegativeOffset != -1 || !reflect.DeepEqual(p.NegativeBucketCounts, []uint64{1}) {
		t.Errorf("buckets = %+v", p)
	}
	if *p.Min != -0.5 || *p.Max != 8 || *p.AggregationTemporality != 1 {
		t.Errorf("min = %v, max = %v, temporality = %d", *p.Min, *p.Max, *p.AggregationTemporality)
	}
	if bare := points[1]; bare.PositiveOffset != nil || bare.NegativeOffset != nil || bare.Sum != nil {
		t.Errorf("exponential histogram without buckets = %+v", bare)
	}
}

func TestConvertSummary(t *testing.T) {
	points := convertOne(t, &metricspb.Metric{Name: "duration", Unit: "s", Data: &metricspb.Metric_Summary{Summary: &metricspb.Summary{
		DataPoints: []*metricspb.SummaryDataPoint{
			{
				TimeUnixNano: uint64(metricTestTime.UnixNano()),
				Count:        4,
				Sum:          10,
				QuantileValues: []*metricspb.SummaryDataPoint_ValueAtQuantile{
					{Quantile: 0.5, Value: 2},
					{Quantile: 0.99, Value: 4},
				},
			},
		},
	}}})
	if len(points) != 1 {
		t.Fatalf("points = %d, want 1", len(points))
	}
	p := points[0]
	if p.MetricType != "summary" || *p.Count != 4 || *p.Sum != 10 || p.AggregationTemporality != nil {
		t.Errorf("summary = %+v", p)
	}
	if !reflect.DeepEqual(p.QuantileQuantiles, []float64{0.5, 0.99}) || !reflect.DeepEqual(p.QuantileValues, []float64{2, 4}) {
		t.Errorf("quantiles = %v, values = %v", p.QuantileQuantiles, p.QuantileValues)
	}
}

// Metrics without data and data points of an unknown kind produce no points
func TestConvertMetricsWithoutData(t *testing.T) {
	result := ConvertMetrics(metricRequest(&metricspb.Metric{Name: "empty"}))
	if len(result.Metrics) != 0 {
		t.Errorf("points = %+v, want none", result.Metrics)
	}
}
