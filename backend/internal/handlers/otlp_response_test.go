package handlers

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	collogspb "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	colmetricspb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	coltracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
	metricspb "go.opentelemetry.io/proto/otlp/metrics/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/proto"
)

func testResource() *resourcepb.Resource {
	return &resourcepb.Resource{Attributes: []*commonpb.KeyValue{{
		Key:   "service.name",
		Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "test-service"}},
	}}}
}

// Protobuf requests must get a protobuf Export*ServiceResponse back, per the OTLP/HTTP spec.
func TestOTLPHandlers_ProtobufRequestGetsProtobufResponse(t *testing.T) {
	h, cleanup := setupTestHandlers(t)
	defer cleanup()

	tests := []struct {
		name    string
		path    string
		request proto.Message
		handler http.HandlerFunc
		resp    proto.Message
	}{
		{
			name: "traces", path: "/v1/traces", handler: h.HandleTraces, resp: &coltracepb.ExportTraceServiceResponse{},
			request: &coltracepb.ExportTraceServiceRequest{ResourceSpans: []*tracepb.ResourceSpans{{
				Resource: testResource(),
				ScopeSpans: []*tracepb.ScopeSpans{{Spans: []*tracepb.Span{{
					TraceId: bytes.Repeat([]byte{1}, 16), SpanId: bytes.Repeat([]byte{2}, 8), Name: "span",
					StartTimeUnixNano: 1_700_000_000_000_000_000, EndTimeUnixNano: 1_700_000_001_000_000_000,
				}}}},
			}}},
		},
		{
			name: "metrics", path: "/v1/metrics", handler: h.HandleMetrics, resp: &colmetricspb.ExportMetricsServiceResponse{},
			request: &colmetricspb.ExportMetricsServiceRequest{ResourceMetrics: []*metricspb.ResourceMetrics{{
				Resource: testResource(),
				ScopeMetrics: []*metricspb.ScopeMetrics{{Metrics: []*metricspb.Metric{{
					Name: "test.gauge",
					Data: &metricspb.Metric_Gauge{Gauge: &metricspb.Gauge{DataPoints: []*metricspb.NumberDataPoint{{
						TimeUnixNano: 1_700_000_000_000_000_000,
						Value:        &metricspb.NumberDataPoint_AsDouble{AsDouble: 1},
					}}}},
				}}}},
			}}},
		},
		{
			name: "logs", path: "/v1/logs", handler: h.HandleLogs, resp: &collogspb.ExportLogsServiceResponse{},
			request: &collogspb.ExportLogsServiceRequest{ResourceLogs: []*logspb.ResourceLogs{{
				Resource: testResource(),
				ScopeLogs: []*logspb.ScopeLogs{{LogRecords: []*logspb.LogRecord{{
					TimeUnixNano: 1_700_000_000_000_000_000,
					Body:         &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "hello"}},
				}}}},
			}}},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			body, err := proto.Marshal(tc.request)
			if err != nil {
				t.Fatalf("marshal request: %v", err)
			}
			req := httptest.NewRequest(http.MethodPost, tc.path, bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/x-protobuf")
			rec := httptest.NewRecorder()

			tc.handler(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
			}
			if ct := rec.Header().Get("Content-Type"); ct != "application/x-protobuf" {
				t.Errorf("expected Content-Type application/x-protobuf, got %q", ct)
			}
			if err := proto.Unmarshal(rec.Body.Bytes(), tc.resp); err != nil {
				t.Errorf("response is not a valid protobuf export response: %v", err)
			}
		})
	}
}
