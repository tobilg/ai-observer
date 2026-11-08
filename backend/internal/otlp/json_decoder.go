package otlp

import (
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"

	collogspb "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	colmetricspb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	coltracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	metricspb "go.opentelemetry.io/proto/otlp/metrics/v1"
	"google.golang.org/protobuf/encoding/protojson"
)

// JSONDecoder decodes OTLP JSON messages
type JSONDecoder struct{}

// DecodeTraces decodes a JSON-encoded traces request
func (d *JSONDecoder) DecodeTraces(r io.Reader) (*coltracepb.ExportTraceServiceRequest, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("reading body: %w", err)
	}

	req := &coltracepb.ExportTraceServiceRequest{}
	if err := protojson.Unmarshal(data, req); err != nil {
		return nil, fmt.Errorf("unmarshaling JSON traces: %w", err)
	}
	for _, rs := range req.GetResourceSpans() {
		for _, ss := range rs.GetScopeSpans() {
			for _, s := range ss.GetSpans() {
				s.TraceId = hexID(s.TraceId, traceIDSize)
				s.SpanId = hexID(s.SpanId, spanIDSize)
				s.ParentSpanId = hexID(s.ParentSpanId, spanIDSize)
				for _, l := range s.GetLinks() {
					l.TraceId = hexID(l.TraceId, traceIDSize)
					l.SpanId = hexID(l.SpanId, spanIDSize)
				}
			}
		}
	}

	return req, nil
}

// DecodeMetrics decodes a JSON-encoded metrics request
func (d *JSONDecoder) DecodeMetrics(r io.Reader) (*colmetricspb.ExportMetricsServiceRequest, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("reading body: %w", err)
	}

	req := &colmetricspb.ExportMetricsServiceRequest{}
	if err := protojson.Unmarshal(data, req); err != nil {
		return nil, fmt.Errorf("unmarshaling JSON metrics: %w", err)
	}
	for _, rm := range req.GetResourceMetrics() {
		for _, sm := range rm.GetScopeMetrics() {
			for _, m := range sm.GetMetrics() {
				for _, e := range metricExemplars(m) {
					e.TraceId = hexID(e.TraceId, traceIDSize)
					e.SpanId = hexID(e.SpanId, spanIDSize)
				}
			}
		}
	}

	return req, nil
}

// DecodeLogs decodes a JSON-encoded logs request
func (d *JSONDecoder) DecodeLogs(r io.Reader) (*collogspb.ExportLogsServiceRequest, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("reading body: %w", err)
	}

	req := &collogspb.ExportLogsServiceRequest{}
	if err := protojson.Unmarshal(data, req); err != nil {
		return nil, fmt.Errorf("unmarshaling JSON logs: %w", err)
	}
	for _, rl := range req.GetResourceLogs() {
		for _, sl := range rl.GetScopeLogs() {
			for _, lr := range sl.GetLogRecords() {
				lr.TraceId = hexID(lr.TraceId, traceIDSize)
				lr.SpanId = hexID(lr.SpanId, spanIDSize)
			}
		}
	}

	return req, nil
}

const (
	traceIDSize = 16
	spanIDSize  = 8
)

// hexID returns the ID an exporter sent. OTLP/JSON encodes trace and span
// IDs as hex (https://opentelemetry.io/docs/specs/otlp/#json-protobuf-encoding),
// while protojson reads bytes fields as base64, their proto3 JSON encoding.
// Hex text is valid base64 of 3/4 of its length, so a 16-byte trace ID sent
// as hex is read as 24 bytes and an 8-byte span ID as 12; encoded as base64
// again, they are the hex text that was sent. IDs of their proper length,
// from exporters that send base64, are kept.
func hexID(id []byte, size int) []byte {
	if len(id) != size*3/2 {
		return id
	}
	decoded, err := hex.DecodeString(base64.StdEncoding.EncodeToString(id))
	if err != nil {
		return id
	}
	return decoded
}

// metricExemplars returns the exemplars of a metric's data points
func metricExemplars(m *metricspb.Metric) []*metricspb.Exemplar {
	var exemplars []*metricspb.Exemplar
	for _, dp := range m.GetGauge().GetDataPoints() {
		exemplars = append(exemplars, dp.GetExemplars()...)
	}
	for _, dp := range m.GetSum().GetDataPoints() {
		exemplars = append(exemplars, dp.GetExemplars()...)
	}
	for _, dp := range m.GetHistogram().GetDataPoints() {
		exemplars = append(exemplars, dp.GetExemplars()...)
	}
	for _, dp := range m.GetExponentialHistogram().GetDataPoints() {
		exemplars = append(exemplars, dp.GetExemplars()...)
	}
	return exemplars
}
