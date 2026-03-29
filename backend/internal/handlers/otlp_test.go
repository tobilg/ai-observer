package handlers

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// OTLP JSON payload structures for testing
type otlpTracesRequest struct {
	ResourceSpans []resourceSpan `json:"resourceSpans"`
}

type resourceSpan struct {
	Resource   resource    `json:"resource"`
	ScopeSpans []scopeSpan `json:"scopeSpans"`
}

type resource struct {
	Attributes []keyValue `json:"attributes"`
}

type scopeSpan struct {
	Scope scope  `json:"scope,omitempty"`
	Spans []span `json:"spans"`
}

type scope struct {
	Name    string `json:"name,omitempty"`
	Version string `json:"version,omitempty"`
}

type span struct {
	TraceID           string     `json:"traceId"`
	SpanID            string     `json:"spanId"`
	ParentSpanID      string     `json:"parentSpanId,omitempty"`
	Name              string     `json:"name"`
	Kind              int        `json:"kind"`
	StartTimeUnixNano string     `json:"startTimeUnixNano"`
	EndTimeUnixNano   string     `json:"endTimeUnixNano"`
	Attributes        []keyValue `json:"attributes,omitempty"`
	Status            *status    `json:"status,omitempty"`
}

type status struct {
	Code    int    `json:"code"`
	Message string `json:"message,omitempty"`
}

type keyValue struct {
	Key   string   `json:"key"`
	Value anyValue `json:"value"`
}

type anyValue struct {
	StringValue string `json:"stringValue,omitempty"`
	IntValue    string `json:"intValue,omitempty"`
	BoolValue   bool   `json:"boolValue,omitempty"`
}

type otlpLogsRequest struct {
	ResourceLogs []resourceLog `json:"resourceLogs"`
}

type resourceLog struct {
	Resource  resource   `json:"resource"`
	ScopeLogs []scopeLog `json:"scopeLogs"`
}

type scopeLog struct {
	LogRecords []logRecord `json:"logRecords"`
}

type logRecord struct {
	TimeUnixNano   string     `json:"timeUnixNano"`
	SeverityNumber int        `json:"severityNumber"`
	SeverityText   string     `json:"severityText"`
	Body           anyValue   `json:"body"`
	Attributes     []keyValue `json:"attributes,omitempty"`
	TraceID        string     `json:"traceId,omitempty"`
	SpanID         string     `json:"spanId,omitempty"`
}

type otlpMetricsRequest struct {
	ResourceMetrics []resourceMetric `json:"resourceMetrics"`
}

type resourceMetric struct {
	Resource     resource      `json:"resource"`
	ScopeMetrics []scopeMetric `json:"scopeMetrics"`
}

type scopeMetric struct {
	Metrics []metric `json:"metrics"`
}

type metric struct {
	Name  string       `json:"name"`
	Unit  string       `json:"unit,omitempty"`
	Gauge *gaugeMetric `json:"gauge,omitempty"`
	Sum   *sumMetric   `json:"sum,omitempty"`
}

type gaugeMetric struct {
	DataPoints []dataPoint `json:"dataPoints"`
}

type sumMetric struct {
	DataPoints             []dataPoint `json:"dataPoints"`
	AggregationTemporality int         `json:"aggregationTemporality"`
	IsMonotonic            bool        `json:"isMonotonic"`
}

type dataPoint struct {
	TimeUnixNano string     `json:"timeUnixNano"`
	AsDouble     float64    `json:"asDouble,omitempty"`
	AsInt        string     `json:"asInt,omitempty"`
	Attributes   []keyValue `json:"attributes,omitempty"`
}

// Helper to create a minimal valid traces payload
func createTracesPayload() otlpTracesRequest {
	return otlpTracesRequest{
		ResourceSpans: []resourceSpan{
			{
				Resource: resource{
					Attributes: []keyValue{
						{Key: "service.name", Value: anyValue{StringValue: "test-service"}},
					},
				},
				ScopeSpans: []scopeSpan{
					{
						Spans: []span{
							{
								TraceID:           "0102030405060708090a0b0c0d0e0f10",
								SpanID:            "0102030405060708",
								Name:              "test-span",
								Kind:              2, // SERVER
								StartTimeUnixNano: "1609459200000000000",
								EndTimeUnixNano:   "1609459200100000000",
								Status:            &status{Code: 1}, // OK
							},
						},
					},
				},
			},
		},
	}
}

// Helper to create a minimal valid logs payload
func createLogsPayload() otlpLogsRequest {
	return otlpLogsRequest{
		ResourceLogs: []resourceLog{
			{
				Resource: resource{
					Attributes: []keyValue{
						{Key: "service.name", Value: anyValue{StringValue: "test-service"}},
					},
				},
				ScopeLogs: []scopeLog{
					{
						LogRecords: []logRecord{
							{
								TimeUnixNano:   "1609459200000000000",
								SeverityNumber: 9, // INFO
								SeverityText:   "INFO",
								Body:           anyValue{StringValue: "Test log message"},
							},
						},
					},
				},
			},
		},
	}
}

// Helper to create a minimal valid metrics payload
func createMetricsPayload() otlpMetricsRequest {
	return otlpMetricsRequest{
		ResourceMetrics: []resourceMetric{
			{
				Resource: resource{
					Attributes: []keyValue{
						{Key: "service.name", Value: anyValue{StringValue: "test-service"}},
					},
				},
				ScopeMetrics: []scopeMetric{
					{
						Metrics: []metric{
							{
								Name: "test_gauge",
								Unit: "1",
								Gauge: &gaugeMetric{
									DataPoints: []dataPoint{
										{
											TimeUnixNano: "1609459200000000000",
											AsDouble:     42.0,
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}
}

// Helper to gzip-compress data
func gzipCompress(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	if _, err := w.Write(data); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func TestHandleTraces_ValidJSON(t *testing.T) {
	h, cleanup := setupTestHandlers(t)
	defer cleanup()

	payload := createTracesPayload()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("failed to marshal payload: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/traces", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.HandleTraces(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Verify response is empty JSON object (OTLP success)
	if rec.Body.String() != "{}" {
		t.Errorf("expected empty JSON response, got: %s", rec.Body.String())
	}

	// Verify Content-Type header
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("expected Content-Type application/json, got %s", ct)
	}
}

func TestHandleTraces_EmptyResourceSpans(t *testing.T) {
	h, cleanup := setupTestHandlers(t)
	defer cleanup()

	payload := otlpTracesRequest{ResourceSpans: []resourceSpan{}}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/v1/traces", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.HandleTraces(rec, req)

	// Empty but valid payload should succeed
	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200 for empty resourceSpans, got %d", rec.Code)
	}
}

func TestHandleTraces_InvalidJSON(t *testing.T) {
	h, cleanup := setupTestHandlers(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/v1/traces", bytes.NewReader([]byte("invalid json")))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.HandleTraces(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status 400 for invalid JSON, got %d", rec.Code)
	}
}

func TestHandleTraces_EmptyBody(t *testing.T) {
	h, cleanup := setupTestHandlers(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/v1/traces", bytes.NewReader([]byte{}))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.HandleTraces(rec, req)

	// Empty body should result in error
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status 400 for empty body, got %d", rec.Code)
	}
}

func TestHandleLogs_ValidJSON(t *testing.T) {
	h, cleanup := setupTestHandlers(t)
	defer cleanup()

	payload := createLogsPayload()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("failed to marshal payload: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/logs", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.HandleLogs(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleLogs_MultipleSeverities(t *testing.T) {
	h, cleanup := setupTestHandlers(t)
	defer cleanup()

	payload := otlpLogsRequest{
		ResourceLogs: []resourceLog{
			{
				Resource: resource{
					Attributes: []keyValue{
						{Key: "service.name", Value: anyValue{StringValue: "test-service"}},
					},
				},
				ScopeLogs: []scopeLog{
					{
						LogRecords: []logRecord{
							{TimeUnixNano: "1609459200000000000", SeverityNumber: 5, SeverityText: "DEBUG", Body: anyValue{StringValue: "debug"}},
							{TimeUnixNano: "1609459200000000001", SeverityNumber: 9, SeverityText: "INFO", Body: anyValue{StringValue: "info"}},
							{TimeUnixNano: "1609459200000000002", SeverityNumber: 13, SeverityText: "WARN", Body: anyValue{StringValue: "warn"}},
							{TimeUnixNano: "1609459200000000003", SeverityNumber: 17, SeverityText: "ERROR", Body: anyValue{StringValue: "error"}},
						},
					},
				},
			},
		},
	}

	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/v1/logs", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.HandleLogs(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleLogs_InvalidJSON(t *testing.T) {
	h, cleanup := setupTestHandlers(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/v1/logs", bytes.NewReader([]byte("{invalid")))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.HandleLogs(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status 400 for invalid JSON, got %d", rec.Code)
	}
}

func TestHandleMetrics_ValidJSON(t *testing.T) {
	h, cleanup := setupTestHandlers(t)
	defer cleanup()

	payload := createMetricsPayload()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("failed to marshal payload: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/metrics", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.HandleMetrics(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleMetrics_SumMetric(t *testing.T) {
	h, cleanup := setupTestHandlers(t)
	defer cleanup()

	payload := otlpMetricsRequest{
		ResourceMetrics: []resourceMetric{
			{
				Resource: resource{
					Attributes: []keyValue{
						{Key: "service.name", Value: anyValue{StringValue: "test-service"}},
					},
				},
				ScopeMetrics: []scopeMetric{
					{
						Metrics: []metric{
							{
								Name: "request_count",
								Sum: &sumMetric{
									DataPoints: []dataPoint{
										{
											TimeUnixNano: "1609459200000000000",
											AsDouble:     100.0,
										},
									},
									AggregationTemporality: 2, // CUMULATIVE
									IsMonotonic:            true,
								},
							},
						},
					},
				},
			},
		},
	}

	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/v1/metrics", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.HandleMetrics(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleMetrics_InvalidJSON(t *testing.T) {
	h, cleanup := setupTestHandlers(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/v1/metrics", bytes.NewReader([]byte("not valid json")))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.HandleMetrics(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status 400 for invalid JSON, got %d", rec.Code)
	}
}

func TestHandleTraces_AutoDetectJSON(t *testing.T) {
	h, cleanup := setupTestHandlers(t)
	defer cleanup()

	payload := createTracesPayload()
	body, _ := json.Marshal(payload)

	// No Content-Type header - should auto-detect JSON
	req := httptest.NewRequest(http.MethodPost, "/v1/traces", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	h.HandleTraces(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200 with auto-detected JSON, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleTraces_WithAttributes(t *testing.T) {
	h, cleanup := setupTestHandlers(t)
	defer cleanup()

	payload := otlpTracesRequest{
		ResourceSpans: []resourceSpan{
			{
				Resource: resource{
					Attributes: []keyValue{
						{Key: "service.name", Value: anyValue{StringValue: "test-service"}},
						{Key: "service.version", Value: anyValue{StringValue: "1.0.0"}},
						{Key: "deployment.environment", Value: anyValue{StringValue: "production"}},
					},
				},
				ScopeSpans: []scopeSpan{
					{
						Scope: scope{
							Name:    "test-scope",
							Version: "1.0.0",
						},
						Spans: []span{
							{
								TraceID:           "0102030405060708090a0b0c0d0e0f10",
								SpanID:            "0102030405060708",
								ParentSpanID:      "0807060504030201",
								Name:              "GET /api/users",
								Kind:              2, // SERVER
								StartTimeUnixNano: "1609459200000000000",
								EndTimeUnixNano:   "1609459200100000000",
								Attributes: []keyValue{
									{Key: "http.method", Value: anyValue{StringValue: "GET"}},
									{Key: "http.url", Value: anyValue{StringValue: "/api/users"}},
									{Key: "http.status_code", Value: anyValue{IntValue: "200"}},
								},
								Status: &status{Code: 1, Message: "success"},
							},
						},
					},
				},
			},
		},
	}

	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/v1/traces", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.HandleTraces(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleLogs_WithTraceContext(t *testing.T) {
	h, cleanup := setupTestHandlers(t)
	defer cleanup()

	payload := otlpLogsRequest{
		ResourceLogs: []resourceLog{
			{
				Resource: resource{
					Attributes: []keyValue{
						{Key: "service.name", Value: anyValue{StringValue: "test-service"}},
					},
				},
				ScopeLogs: []scopeLog{
					{
						LogRecords: []logRecord{
							{
								TimeUnixNano:   "1609459200000000000",
								SeverityNumber: 9,
								SeverityText:   "INFO",
								Body:           anyValue{StringValue: "Request processed"},
								TraceID:        "0102030405060708090a0b0c0d0e0f10",
								SpanID:         "0102030405060708",
								Attributes: []keyValue{
									{Key: "request.id", Value: anyValue{StringValue: "req-123"}},
								},
							},
						},
					},
				},
			},
		},
	}

	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/v1/logs", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.HandleLogs(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleMetrics_MultipleMetricTypes(t *testing.T) {
	h, cleanup := setupTestHandlers(t)
	defer cleanup()

	payload := otlpMetricsRequest{
		ResourceMetrics: []resourceMetric{
			{
				Resource: resource{
					Attributes: []keyValue{
						{Key: "service.name", Value: anyValue{StringValue: "test-service"}},
					},
				},
				ScopeMetrics: []scopeMetric{
					{
						Metrics: []metric{
							{
								Name: "cpu_usage",
								Unit: "%",
								Gauge: &gaugeMetric{
									DataPoints: []dataPoint{
										{TimeUnixNano: "1609459200000000000", AsDouble: 45.5},
									},
								},
							},
							{
								Name: "requests_total",
								Unit: "1",
								Sum: &sumMetric{
									DataPoints: []dataPoint{
										{TimeUnixNano: "1609459200000000000", AsDouble: 1000},
									},
									AggregationTemporality: 2,
									IsMonotonic:            true,
								},
							},
						},
					},
				},
			},
		},
	}

	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/v1/metrics", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.HandleMetrics(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleTraces_MultipleSpans(t *testing.T) {
	h, cleanup := setupTestHandlers(t)
	defer cleanup()

	payload := otlpTracesRequest{
		ResourceSpans: []resourceSpan{
			{
				Resource: resource{
					Attributes: []keyValue{
						{Key: "service.name", Value: anyValue{StringValue: "test-service"}},
					},
				},
				ScopeSpans: []scopeSpan{
					{
						Spans: []span{
							{
								TraceID:           "0102030405060708090a0b0c0d0e0f10",
								SpanID:            "0102030405060701",
								Name:              "parent-span",
								Kind:              2,
								StartTimeUnixNano: "1609459200000000000",
								EndTimeUnixNano:   "1609459200200000000",
							},
							{
								TraceID:           "0102030405060708090a0b0c0d0e0f10",
								SpanID:            "0102030405060702",
								ParentSpanID:      "0102030405060701",
								Name:              "child-span-1",
								Kind:              3, // CLIENT
								StartTimeUnixNano: "1609459200010000000",
								EndTimeUnixNano:   "1609459200050000000",
							},
							{
								TraceID:           "0102030405060708090a0b0c0d0e0f10",
								SpanID:            "0102030405060703",
								ParentSpanID:      "0102030405060701",
								Name:              "child-span-2",
								Kind:              3,
								StartTimeUnixNano: "1609459200060000000",
								EndTimeUnixNano:   "1609459200100000000",
							},
						},
					},
				},
			},
		},
	}

	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/v1/traces", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.HandleTraces(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleTraces_ErrorStatus(t *testing.T) {
	h, cleanup := setupTestHandlers(t)
	defer cleanup()

	payload := otlpTracesRequest{
		ResourceSpans: []resourceSpan{
			{
				Resource: resource{
					Attributes: []keyValue{
						{Key: "service.name", Value: anyValue{StringValue: "test-service"}},
					},
				},
				ScopeSpans: []scopeSpan{
					{
						Spans: []span{
							{
								TraceID:           "0102030405060708090a0b0c0d0e0f10",
								SpanID:            "0102030405060708",
								Name:              "failed-request",
								Kind:              2,
								StartTimeUnixNano: "1609459200000000000",
								EndTimeUnixNano:   "1609459200100000000",
								Status:            &status{Code: 2, Message: "Internal server error"}, // ERROR
							},
						},
					},
				},
			},
		},
	}

	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/v1/traces", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.HandleTraces(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}
}
