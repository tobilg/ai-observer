package otlp

import (
	"bytes"
	"strings"
	"testing"

	colmetricspb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	coltracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	metricspb "go.opentelemetry.io/proto/otlp/metrics/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
)

func TestDropInvalidSpans(t *testing.T) {
	valid := func(name string) *tracepb.Span {
		return &tracepb.Span{Name: name, TraceId: bytes.Repeat([]byte{1}, 16), SpanId: bytes.Repeat([]byte{2}, 8)}
	}
	shortTrace := valid("short trace")
	shortTrace.TraceId = []byte{1, 2, 3}
	zeroTrace := valid("zero trace")
	zeroTrace.TraceId = make([]byte, 16)
	noSpan := valid("no span id")
	noSpan.SpanId = nil
	zeroSpan := valid("zero span")
	zeroSpan.SpanId = make([]byte, 8)

	req := &coltracepb.ExportTraceServiceRequest{ResourceSpans: []*tracepb.ResourceSpans{{
		ScopeSpans: []*tracepb.ScopeSpans{{Spans: []*tracepb.Span{valid("a"), shortTrace, zeroTrace, noSpan, valid("b"), zeroSpan}}},
	}}}
	r := DropInvalidSpans(req)

	if r.Count != 4 {
		t.Errorf("rejected = %d, want 4", r.Count)
	}
	var kept []string
	for _, s := range req.ResourceSpans[0].ScopeSpans[0].Spans {
		kept = append(kept, s.Name)
	}
	if strings.Join(kept, ",") != "a,b" {
		t.Errorf("kept spans %v, want a,b", kept)
	}
	if msg := r.Message(); !strings.Contains(msg, "2 span(s) rejected: invalid trace_id") || !strings.Contains(msg, "2 span(s) rejected: invalid span_id") {
		t.Errorf("message = %q", msg)
	}
}

func TestDropInvalidSpansKeepsValidRequestUnchanged(t *testing.T) {
	req := &coltracepb.ExportTraceServiceRequest{ResourceSpans: []*tracepb.ResourceSpans{{
		ScopeSpans: []*tracepb.ScopeSpans{{Spans: []*tracepb.Span{{TraceId: bytes.Repeat([]byte{1}, 16), SpanId: bytes.Repeat([]byte{2}, 8)}}}},
	}}}
	if r := DropInvalidSpans(req); !r.Empty() || r.Count != 0 || r.Message() != "" {
		t.Errorf("rejections = %+v for a valid request", r)
	}
	if len(req.ResourceSpans[0].ScopeSpans[0].Spans) != 1 {
		t.Error("a valid span was dropped")
	}
}

func TestDropInvalidDataPoints(t *testing.T) {
	number := func(v *float64) *metricspb.NumberDataPoint {
		dp := &metricspb.NumberDataPoint{TimeUnixNano: 1}
		if v != nil {
			dp.Value = &metricspb.NumberDataPoint_AsDouble{AsDouble: *v}
		}
		return dp
	}
	one := 1.0
	req := &colmetricspb.ExportMetricsServiceRequest{ResourceMetrics: []*metricspb.ResourceMetrics{{
		ScopeMetrics: []*metricspb.ScopeMetrics{{Metrics: []*metricspb.Metric{
			{Name: "gauge", Data: &metricspb.Metric_Gauge{Gauge: &metricspb.Gauge{DataPoints: []*metricspb.NumberDataPoint{number(&one), number(nil)}}}},
			{Name: "sum", Data: &metricspb.Metric_Sum{Sum: &metricspb.Sum{DataPoints: []*metricspb.NumberDataPoint{number(nil), number(&one)}}}},
			{Name: "histogram", Data: &metricspb.Metric_Histogram{Histogram: &metricspb.Histogram{DataPoints: []*metricspb.HistogramDataPoint{
				{Count: 3, BucketCounts: []uint64{1, 2}, ExplicitBounds: []float64{10}}, // valid
				{Count: 3}, // no buckets: valid
				{Count: 3, BucketCounts: []uint64{1, 2}, ExplicitBounds: []float64{10, 20}}, // invalid
			}}}},
			{Name: "no data"},
		}}},
	}}}
	r := DropInvalidDataPoints(req)

	if r.Count != 3 {
		t.Errorf("rejected = %d, want 3", r.Count)
	}
	metrics := req.ResourceMetrics[0].ScopeMetrics[0].Metrics
	if n := len(metrics[0].GetGauge().DataPoints) + len(metrics[1].GetSum().DataPoints); n != 2 {
		t.Errorf("number points kept = %d, want 2", n)
	}
	if n := len(metrics[2].GetHistogram().DataPoints); n != 2 {
		t.Errorf("histogram points kept = %d, want 2", n)
	}
	msg := r.Message()
	for _, want := range []string{"2 number data point(s) rejected", "1 histogram data point(s) rejected", "1 metric(s) without data"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message %q lacks %q", msg, want)
		}
	}
}

// A metric without data holds no data points: a warning, nothing rejected
func TestMetricWithoutDataIsAWarning(t *testing.T) {
	req := &colmetricspb.ExportMetricsServiceRequest{ResourceMetrics: []*metricspb.ResourceMetrics{{
		ScopeMetrics: []*metricspb.ScopeMetrics{{Metrics: []*metricspb.Metric{{Name: "empty"}}}},
	}}}
	r := DropInvalidDataPoints(req)
	if r.Count != 0 || r.Empty() || !strings.Contains(r.Message(), "without data") {
		t.Errorf("rejections = %d, message %q; want 0 and a warning", r.Count, r.Message())
	}
}
