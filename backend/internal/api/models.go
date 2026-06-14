package api

import "time"

const (
	TraceKindOTelTrace      = "otel_trace"
	TraceKindCodexOperation = "codex_operation"

	TraceGroupLevelCodexTurn      = "codex_turn"
	TraceGroupLevelCodexOperation = "codex_operation"
)

// Trace represents a trace or derived trace-like overview for listing
type TraceOverview struct {
	ID          string    `json:"id"`
	Kind        string    `json:"kind"`
	TraceID     string    `json:"traceId"`
	RootSpanID  string    `json:"rootSpanId"`
	RootSpan    string    `json:"rootSpan"`
	ServiceName string    `json:"serviceName"`
	GroupLevel  string    `json:"groupLevel,omitempty"`
	StartTime   time.Time `json:"startTime"`
	Duration    int64     `json:"duration"`
	SpanCount   int       `json:"spanCount"`
	Status      string    `json:"status"`
}

// Span represents a single span
type Span struct {
	Timestamp          time.Time         `json:"timestamp"`
	TraceID            string            `json:"traceId"`
	SpanID             string            `json:"spanId"`
	ParentSpanID       string            `json:"parentSpanId,omitempty"`
	TraceState         string            `json:"traceState,omitempty"`
	SpanName           string            `json:"spanName"`
	SpanKind           string            `json:"spanKind,omitempty"`
	ServiceName        string            `json:"serviceName"`
	ResourceAttributes map[string]string `json:"resourceAttributes,omitempty"`
	ScopeName          string            `json:"scopeName,omitempty"`
	ScopeVersion       string            `json:"scopeVersion,omitempty"`
	SpanAttributes     map[string]string `json:"spanAttributes,omitempty"`
	Duration           int64             `json:"duration"`
	StatusCode         string            `json:"statusCode,omitempty"`
	StatusMessage      string            `json:"statusMessage,omitempty"`
	Events             []SpanEvent       `json:"events,omitempty"`
	Links              []SpanLink        `json:"links,omitempty"`
}

type SpanEvent struct {
	Timestamp  time.Time         `json:"timestamp"`
	Name       string            `json:"name"`
	Attributes map[string]string `json:"attributes,omitempty"`
}

type SpanLink struct {
	TraceID    string            `json:"traceId"`
	SpanID     string            `json:"spanId"`
	TraceState string            `json:"traceState,omitempty"`
	Attributes map[string]string `json:"attributes,omitempty"`
}

// Log represents a log record
type LogRecord struct {
	Timestamp          time.Time         `json:"timestamp"`
	TraceID            string            `json:"traceId,omitempty"`
	SpanID             string            `json:"spanId,omitempty"`
	TraceFlags         uint32            `json:"traceFlags,omitempty"`
	SeverityText       string            `json:"severityText,omitempty"`
	SeverityNumber     int32             `json:"severityNumber,omitempty"`
	ServiceName        string            `json:"serviceName"`
	Body               string            `json:"body,omitempty"`
	ResourceSchemaURL  string            `json:"resourceSchemaUrl,omitempty"`
	ResourceAttributes map[string]string `json:"resourceAttributes,omitempty"`
	ScopeSchemaURL     string            `json:"scopeSchemaUrl,omitempty"`
	ScopeName          string            `json:"scopeName,omitempty"`
	ScopeVersion       string            `json:"scopeVersion,omitempty"`
	ScopeAttributes    map[string]string `json:"scopeAttributes,omitempty"`
	LogAttributes      map[string]string `json:"logAttributes,omitempty"`
}

// Metric represents a metric data point
type MetricDataPoint struct {
	Timestamp              time.Time         `json:"timestamp"`
	ServiceName            string            `json:"serviceName"`
	MetricName             string            `json:"metricName"`
	MetricDescription      string            `json:"metricDescription,omitempty"`
	MetricUnit             string            `json:"metricUnit,omitempty"`
	ResourceAttributes     map[string]string `json:"resourceAttributes,omitempty"`
	ScopeName              string            `json:"scopeName,omitempty"`
	ScopeVersion           string            `json:"scopeVersion,omitempty"`
	Attributes             map[string]string `json:"attributes,omitempty"`
	MetricType             string            `json:"metricType"`
	Value                  *float64          `json:"value,omitempty"`
	AggregationTemporality *int32            `json:"aggregationTemporality,omitempty"`
	IsMonotonic            *bool             `json:"isMonotonic,omitempty"`
	Count                  *uint64           `json:"count,omitempty"`
	Sum                    *float64          `json:"sum,omitempty"`
	BucketCounts           []uint64          `json:"bucketCounts,omitempty"`
	ExplicitBounds         []float64         `json:"explicitBounds,omitempty"`
	Scale                  *int32            `json:"scale,omitempty"`
	ZeroCount              *uint64           `json:"zeroCount,omitempty"`
	PositiveOffset         *int32            `json:"positiveOffset,omitempty"`
	PositiveBucketCounts   []uint64          `json:"positiveBucketCounts,omitempty"`
	NegativeOffset         *int32            `json:"negativeOffset,omitempty"`
	NegativeBucketCounts   []uint64          `json:"negativeBucketCounts,omitempty"`
	QuantileValues         []float64         `json:"quantileValues,omitempty"`
	QuantileQuantiles      []float64         `json:"quantileQuantiles,omitempty"`
	Min                    *float64          `json:"min,omitempty"`
	Max                    *float64          `json:"max,omitempty"`
}

// Query response types
type TracesResponse struct {
	Traces  []TraceOverview `json:"traces"`
	Total   int             `json:"total"`
	HasMore bool            `json:"hasMore"`
}

type SpansResponse struct {
	Spans []Span `json:"spans"`
}

type LogsResponse struct {
	Logs    []LogRecord `json:"logs"`
	Total   int         `json:"total"`
	HasMore bool        `json:"hasMore"`
}

type MetricsResponse struct {
	Metrics []MetricDataPoint `json:"metrics"`
	Total   int               `json:"total"`
	HasMore bool              `json:"hasMore"`
}

type TimeSeries struct {
	Name       string            `json:"name"`
	Labels     map[string]string `json:"labels,omitempty"`
	DataPoints [][2]float64      `json:"datapoints"` // [timestamp, value]
}

type TimeSeriesResponse struct {
	Series []TimeSeries `json:"series"`
}

// Batch metric series request/response types

// BatchMetricSeriesRequest represents a batch query for multiple metric series
type BatchMetricSeriesRequest struct {
	From     string        `json:"from"`
	To       string        `json:"to"`
	Interval int64         `json:"interval,omitempty"` // Interval in seconds
	Queries  []MetricQuery `json:"queries"`
}

// MetricQuery represents a single query within a batch request
type MetricQuery struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Service   string `json:"service,omitempty"`
	Aggregate bool   `json:"aggregate,omitempty"`
}

// BatchMetricSeriesResponse contains results for all queried metrics
type BatchMetricSeriesResponse struct {
	Results []MetricQueryResult `json:"results"`
}

// MetricQueryResult contains the result for a single metric query
type MetricQueryResult struct {
	ID      string       `json:"id"`
	Success bool         `json:"success"`
	Error   string       `json:"error,omitempty"`
	Series  []TimeSeries `json:"series,omitempty"`
}

type StatsResponse struct {
	TraceCount          int64    `json:"traceCount"`
	RawTraceCount       int64    `json:"rawTraceCount"`
	CodexOperationCount int64    `json:"codexOperationCount"`
	SpanCount           int64    `json:"spanCount"`
	LogCount            int64    `json:"logCount"`
	MetricCount         int64    `json:"metricCount"`
	ServiceCount        int      `json:"serviceCount"`
	Services            []string `json:"services"`
	ErrorRate           float64  `json:"errorRate"`
}

type ServicesResponse struct {
	Services []string `json:"services"`
}

type MetricNamesResponse struct {
	Names []string `json:"names"`
}

type BreakdownValuesResponse struct {
	Values []string `json:"values"`
}

// Session represents a conversation session summary
type Session struct {
	SessionID    string    `json:"sessionId"`
	ServiceName  string    `json:"serviceName"`
	StartTime    time.Time `json:"startTime"`
	LastTime     time.Time `json:"lastTime"`
	MessageCount int       `json:"messageCount"`
	Model        string    `json:"model,omitempty"`
}

// SessionsResponse for listing sessions
type SessionsResponse struct {
	Sessions []Session `json:"sessions"`
	Total    int       `json:"total"`
	HasMore  bool      `json:"hasMore"`
}

// TranscriptMessage represents a single message in a transcript
type TranscriptMessage struct {
	Timestamp    time.Time `json:"timestamp"`
	Role         string    `json:"role"`
	Content      string    `json:"content"`
	Index        int       `json:"index"`
	Model        string    `json:"model,omitempty"`
	ToolName     string    `json:"toolName,omitempty"`
	ToolInput    string    `json:"toolInput,omitempty"`
	ToolOutput   string    `json:"toolOutput,omitempty"`   // Tool execution output (from imports)
	InputTokens  int       `json:"inputTokens,omitempty"`  // Input token count
	OutputTokens int       `json:"outputTokens,omitempty"` // Output token count
	CacheRead    int       `json:"cacheRead,omitempty"`    // Cache read tokens
	CacheWrite   int       `json:"cacheWrite,omitempty"`   // Cache write tokens
	CostUSD      float64   `json:"costUsd,omitempty"`      // Cost in USD
	DurationMs   int       `json:"durationMs,omitempty"`   // Duration in milliseconds
	Success      *bool     `json:"success,omitempty"`      // Tool execution success (pointer to distinguish false from unset)
	OutputSize   int       `json:"outputSize,omitempty"`   // Tool output size in bytes
}

// TranscriptResponse contains the full transcript for a session
type TranscriptResponse struct {
	SessionID   string              `json:"sessionId"`
	ServiceName string              `json:"serviceName"`
	StartTime   time.Time           `json:"startTime"`
	LastTime    time.Time           `json:"lastTime"`
	Messages    []TranscriptMessage `json:"messages"`
}
