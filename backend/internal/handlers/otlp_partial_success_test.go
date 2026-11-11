package handlers

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	colmetricspb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	coltracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	metricspb "go.opentelemetry.io/proto/otlp/metrics/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// tracesWithInvalidSpan has one valid span and one with a short trace ID
func tracesWithInvalidSpan() *coltracepb.ExportTraceServiceRequest {
	span := func(name string, traceID []byte) *tracepb.Span {
		return &tracepb.Span{Name: name, TraceId: traceID, SpanId: bytes.Repeat([]byte{2}, 8),
			StartTimeUnixNano: 1_700_000_000_000_000_000, EndTimeUnixNano: 1_700_000_001_000_000_000}
	}
	return &coltracepb.ExportTraceServiceRequest{ResourceSpans: []*tracepb.ResourceSpans{{
		Resource:   testResource(),
		ScopeSpans: []*tracepb.ScopeSpans{{Spans: []*tracepb.Span{span("ok", bytes.Repeat([]byte{1}, 16)), span("bad", []byte{1})}}},
	}}}
}

// metricsWithInvalidPoint has one point with a value and one without
func metricsWithInvalidPoint() *colmetricspb.ExportMetricsServiceRequest {
	return &colmetricspb.ExportMetricsServiceRequest{ResourceMetrics: []*metricspb.ResourceMetrics{{
		Resource: testResource(),
		ScopeMetrics: []*metricspb.ScopeMetrics{{Metrics: []*metricspb.Metric{{
			Name: "test.gauge",
			Data: &metricspb.Metric_Gauge{Gauge: &metricspb.Gauge{DataPoints: []*metricspb.NumberDataPoint{
				{TimeUnixNano: 1_700_000_000_000_000_000, Value: &metricspb.NumberDataPoint_AsDouble{AsDouble: 1}},
				{TimeUnixNano: 1_700_000_000_000_000_000},
			}}},
		}}}},
	}}}
}

func post(t *testing.T, handler http.HandlerFunc, contentType string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/x", bytes.NewReader(body))
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()
	handler(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	return rec
}

// Invalid records are rejected and reported as partial success, in the
// encoding of the request; the valid ones are stored
func TestOTLPPartialSuccess(t *testing.T) {
	h, cleanup := setupTestHandlers(t)
	defer cleanup()

	t.Run("traces, protobuf", func(t *testing.T) {
		body, _ := proto.Marshal(tracesWithInvalidSpan())
		rec := post(t, h.HandleTraces, "application/x-protobuf", body)
		var resp coltracepb.ExportTraceServiceResponse
		if err := proto.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
		ps := resp.GetPartialSuccess()
		if ps.GetRejectedSpans() != 1 || !strings.Contains(ps.GetErrorMessage(), "invalid trace_id") {
			t.Errorf("partial success = %v", ps)
		}
	})
	t.Run("metrics, JSON", func(t *testing.T) {
		body, _ := protojson.Marshal(metricsWithInvalidPoint())
		rec := post(t, h.HandleMetrics, "application/json", body)
		var resp colmetricspb.ExportMetricsServiceResponse
		if err := protojson.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("%v: %s", err, rec.Body.String())
		}
		ps := resp.GetPartialSuccess()
		if ps.GetRejectedDataPoints() != 1 || !strings.Contains(ps.GetErrorMessage(), "no value") {
			t.Errorf("partial success = %v (%s)", ps, rec.Body.String())
		}
		// A retry of the batch is not stored again, and gets the same answer
		rec = post(t, h.HandleMetrics, "application/json", body)
		var retry colmetricspb.ExportMetricsServiceResponse
		protojson.Unmarshal(rec.Body.Bytes(), &retry)
		if !proto.Equal(&retry, &resp) {
			t.Errorf("retry answered %v, want %v", &retry, &resp)
		}
	})

	ctx := context.Background()
	traces, err := h.store.GetStats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if traces.SpanCount != 1 || traces.MetricCount != 1 {
		t.Errorf("stored %d spans and %d metric points, want 1 and 1", traces.SpanCount, traces.MetricCount)
	}
}

// A full success is the empty response: "{}" in JSON
func TestOTLPFullSuccessJSONIsEmptyObject(t *testing.T) {
	h, cleanup := setupTestHandlers(t)
	defer cleanup()
	req := tracesWithInvalidSpan()
	req.ResourceSpans[0].ScopeSpans[0].Spans = req.ResourceSpans[0].ScopeSpans[0].Spans[:1]
	body, _ := protojson.Marshal(req)
	rec := post(t, h.HandleTraces, "application/json", body)
	if got := strings.TrimSpace(rec.Body.String()); got != "{}" {
		t.Errorf("body = %s, want {}", got)
	}
}

// The ingestion functions report storage failures as *StoreError, which
// both transports turn into an answer the exporter retries
func TestIngestReportsStoreError(t *testing.T) {
	h, cleanup := setupTestHandlers(t)
	cleanup() // a closed store fails every write

	_, err := h.IngestTraces(context.Background(), tracesWithInvalidSpan())
	var storeErr *StoreError
	if !errors.As(err, &storeErr) || storeErr.Signal != "traces" {
		t.Errorf("error = %v, want a *StoreError for traces", err)
	}

	body, _ := proto.Marshal(metricsWithInvalidPoint())
	req := httptest.NewRequest(http.MethodPost, "/v1/metrics", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/x-protobuf")
	rec := httptest.NewRecorder()
	h.HandleMetrics(rec, req)
	if rec.Code != http.StatusServiceUnavailable || rec.Header().Get("Retry-After") == "" {
		t.Errorf("status %d, Retry-After %q; want 503 with Retry-After", rec.Code, rec.Header().Get("Retry-After"))
	}
}

// gRPC requests arrive decoded: their batch key is the hash of the
// deterministic encoding, the same for a retry, different per signal
func TestMessageBatchKey(t *testing.T) {
	a, err := MessageBatchKey("metrics", metricsWithInvalidPoint())
	if err != nil {
		t.Fatal(err)
	}
	b, _ := MessageBatchKey("metrics", metricsWithInvalidPoint())
	c, _ := MessageBatchKey("logs", metricsWithInvalidPoint())
	if a != b {
		t.Error("equal requests have different keys")
	}
	if a == c {
		t.Error("the signal is not part of the key")
	}
}
