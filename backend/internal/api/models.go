package api

import (
	"encoding/json"
	"math"
	"strconv"
	"time"
)

// Kinds of trace views (TraceOverview.Kind, the kind parameter of GET
// /api/traces/{id})
const (
	// An OpenTelemetry trace: the spans with one trace ID
	TraceKindOTelTrace = "otel_trace"
	// The spans of one operation of an older Codex session trace: a
	// first-level span and its descendants (detail views only)
	TraceKindCodexOperation = "codex_operation"
)

// Levels of Codex traces (TraceOverview.GroupLevel)
const (
	// A trace of a Codex turn (root turn/start)
	TraceGroupLevelCodexTurn = "codex_turn"
	// A Codex operation (TraceKindCodexOperation)
	TraceGroupLevelCodexOperation = "codex_operation"
)

// Statuses of traces (TraceOverview.Status, storage.TraceStatus)
const (
	// A span failed
	TraceStatusError = "ERROR"
	// No span failed and one succeeded
	TraceStatusOK = "OK"
	// No span has a status
	TraceStatusUnset = "UNSET"
)

// Types of metric points (MetricDataPoint.MetricType), as OTLP names them
const (
	MetricTypeGauge                = "gauge"
	MetricTypeSum                  = "sum"
	MetricTypeHistogram            = "histogram"
	MetricTypeExponentialHistogram = "exponential_histogram"
	MetricTypeSummary              = "summary"
)

// TraceOverview describes a trace (or a Codex operation) for lists and
// detail views: its root span and all of its spans
type TraceOverview struct {
	// ID identifies the trace view: the trace ID, or the root span ID of a
	// Codex operation
	ID          string `json:"id"`
	Kind        string `json:"kind"`
	TraceID     string `json:"traceId"`
	RootSpanID  string `json:"rootSpanId"`
	RootSpan    string `json:"rootSpan"`
	ServiceName string `json:"serviceName"`
	GroupLevel  string `json:"groupLevel,omitempty"`
	// StartTime is the start of the earliest span
	StartTime time.Time `json:"startTime"`
	// Duration from the start of the earliest span to the end of the
	// latest, in nanoseconds
	Duration  int64  `json:"duration"`
	SpanCount int    `json:"spanCount"`
	Status    string `json:"status"`
	// SessionID is the session of the trace's spans, when those that have
	// one have the same (their SessionId: the first of tools.SessionIDKeys)
	SessionID string `json:"sessionId,omitempty"`
}

// Span is a span (Duration in nanoseconds)
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
	// SessionID is the stored session of the span (read from storage;
	// ignored when spans are stored, which take it from SpanAttributes)
	SessionID string `json:"sessionId,omitempty"`
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

// LogRecord is a log record
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
	// SessionID is the stored session of the log (read from storage;
	// ignored when logs are stored, which take it from LogAttributes)
	SessionID string `json:"sessionId,omitempty"`
}

// MetricDataPoint is a metric point. Which value fields are set depends on
// MetricType: Value for gauges and sums; Count, Sum, Min, Max and buckets
// for histograms; quantiles for summaries.
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

// TracesResponse is a page of traces, the most recent first
type TracesResponse struct {
	Traces []TraceOverview `json:"traces"`
	// Total counts the traces matching the query, HasMore whether pages
	// follow this one
	Total   int  `json:"total"`
	HasMore bool `json:"hasMore"`
	// NextCursor selects the next page (cursor parameter) when HasMore
	NextCursor string `json:"nextCursor,omitempty"`
}

// SpansResponse is a page of the spans of a trace view, oldest first
type SpansResponse struct {
	Spans []Span `json:"spans"`
	// Trace describes all spans of the trace view, not only this page's
	Trace *TraceOverview `json:"trace,omitempty"`
	// Total counts all spans of the trace view
	Total   int  `json:"total"`
	HasMore bool `json:"hasMore"`
	// NextCursor selects the next page (cursor parameter) when HasMore
	NextCursor string `json:"nextCursor,omitempty"`
}

// LogsResponse is a page of log records, the newest first
type LogsResponse struct {
	Logs    []LogRecord `json:"logs"`
	Total   int         `json:"total"`
	HasMore bool        `json:"hasMore"`
	// NextCursor selects the next page (cursor parameter) when HasMore
	NextCursor string `json:"nextCursor,omitempty"`
}

// MetricsResponse is a page of metric points, the newest first
type MetricsResponse struct {
	Metrics []MetricDataPoint `json:"metrics"`
	Total   int               `json:"total"`
	HasMore bool              `json:"hasMore"`
	// NextCursor selects the next page (cursor parameter) when HasMore
	NextCursor string `json:"nextCursor,omitempty"`
}

// TimeSeries is a series of [timestamp (Unix ms), value] points; the
// timestamp is 0 for the single point of an aggregate. A value is NaN, sent
// as JSON null, where a bucket has no value: histogram statistics other than
// sum and count, and gauges, in buckets without points.
type TimeSeries struct {
	Name       string            `json:"name"`
	Labels     map[string]string `json:"labels,omitempty"`
	DataPoints [][2]float64      `json:"datapoints"` // [timestamp, value]
}

// timeSeriesJSON is TimeSeries without its JSON methods
type timeSeriesJSON struct {
	Name       string            `json:"name"`
	Labels     map[string]string `json:"labels,omitempty"`
	DataPoints [][2]*float64     `json:"datapoints"`
}

// MarshalJSON writes NaN values as null (encoding/json rejects NaN)
func (s TimeSeries) MarshalJSON() ([]byte, error) {
	head, err := json.Marshal(timeSeriesJSON{Name: s.Name, Labels: s.Labels})
	if err != nil {
		return nil, err
	}
	// {"name":...,"datapoints":null} without its "null}", then the points
	head = head[:len(head)-len("null}")]
	buf := make([]byte, 0, len(head)+len(s.DataPoints)*24+2)
	buf = append(buf, head...)
	buf = append(buf, '[')
	for i, dp := range s.DataPoints {
		if i > 0 {
			buf = append(buf, ',')
		}
		buf = append(buf, '[')
		buf = appendJSONFloat(buf, dp[0])
		buf = append(buf, ',')
		buf = appendJSONFloat(buf, dp[1])
		buf = append(buf, ']')
	}
	return append(buf, "]}"...), nil
}

// appendJSONFloat formats f like encoding/json, and NaN and infinities as null
func appendJSONFloat(b []byte, f float64) []byte {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return append(b, "null"...)
	}
	format := byte('f')
	if abs := math.Abs(f); abs != 0 && (abs < 1e-6 || abs >= 1e21) {
		format = 'e'
	}
	b = strconv.AppendFloat(b, f, format, -1, 64)
	if format == 'e' {
		// e-07 becomes e-7, as encoding/json writes it
		if n := len(b); n >= 4 && b[n-4] == 'e' && b[n-3] == '-' && b[n-2] == '0' {
			b[n-2] = b[n-1]
			b = b[:n-1]
		}
	}
	return b
}

// UnmarshalJSON reads null values as NaN
func (s *TimeSeries) UnmarshalJSON(data []byte) error {
	var in timeSeriesJSON
	if err := json.Unmarshal(data, &in); err != nil {
		return err
	}
	s.Name, s.Labels = in.Name, in.Labels
	s.DataPoints = make([][2]float64, len(in.DataPoints))
	for i, dp := range in.DataPoints {
		s.DataPoints[i] = [2]float64{0, math.NaN()}
		if dp[0] != nil {
			s.DataPoints[i][0] = *dp[0]
		}
		if dp[1] != nil {
			s.DataPoints[i][1] = *dp[1]
		}
	}
	return nil
}

// TimeSeriesResponse holds the series of a metric (GET /api/metrics/series)
type TimeSeriesResponse struct {
	Series []TimeSeries `json:"series"`
	// MetricType is the type of the points read (sum, gauge, histogram,
	// exponential_histogram, summary); empty without points in the range
	MetricType string `json:"metricType,omitempty"`
	// Stat is the statistic of histogram series (MetricQuery.Stat, avg when
	// the query named none)
	Stat string `json:"stat,omitempty"`
	// Interval is the bucket size in seconds: the requested one, or a
	// coarser one when the range would have too many buckets; 0 for
	// aggregates
	Interval int64 `json:"interval,omitempty"`
	// TimeZone is the IANA zone the buckets are aligned to
	TimeZone string `json:"timeZone,omitempty"`
}

// MetricAttributeKeysResponse lists the attribute keys of a metric's points
type MetricAttributeKeysResponse struct {
	Keys []string `json:"keys"`
}

// Batch metric series request/response types

// BatchMetricSeriesRequest represents a batch query for multiple metric series
type BatchMetricSeriesRequest struct {
	From     string        `json:"from"`
	To       string        `json:"to"`
	Interval int64         `json:"interval,omitempty"` // Interval in seconds
	Queries  []MetricQuery `json:"queries"`
	// TimeZone is the IANA zone buckets are aligned to (default UTC)
	TimeZone string `json:"timeZone,omitempty"`
}

// MetricQuery represents a single query within a batch request
type MetricQuery struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Service   string `json:"service,omitempty"`
	Aggregate bool   `json:"aggregate,omitempty"`
	// GroupBy names up to two point attributes: series are grouped by
	// service and their values (labelled by the keys) instead of by type
	GroupBy []string `json:"groupBy,omitempty"`
	// Filters keep only points whose attributes have these exact values
	Filters map[string]string `json:"filters,omitempty"`
	// Stat is the statistic of a histogram metric: avg (default), sum,
	// count, min, max, p50, p90, p95 or p99
	Stat string `json:"stat,omitempty"`
	// Merge makes one series of all matching points, without a service
	// label (for a single value of an average or percentile); it takes no
	// GroupBy
	Merge bool `json:"merge,omitempty"`
}

// Codes of failed metric queries (MetricQueryResult.Code)
const (
	MetricErrorInvalidQuery    = "invalid_query"
	MetricErrorInvalidGroupBy  = "invalid_group_by"
	MetricErrorInvalidFilter   = "invalid_filter"
	MetricErrorInvalidStat     = "invalid_stat"
	MetricErrorInvalidTimeZone = "invalid_time_zone"
	MetricErrorTooManySeries   = "too_many_series"
	MetricErrorTimeout         = "timeout"
	MetricErrorFailed          = "query_failed"
)

// BatchMetricSeriesResponse contains results for all queried metrics
type BatchMetricSeriesResponse struct {
	Results []MetricQueryResult `json:"results"`
}

// MetricQueryResult contains the result for a single metric query
type MetricQueryResult struct {
	ID      string `json:"id"`
	Success bool   `json:"success"`
	// Error says why a query failed, and Code is its kind (MetricError*)
	Error  string       `json:"error,omitempty"`
	Code   string       `json:"code,omitempty"`
	Series []TimeSeries `json:"series,omitempty"`
	// MetricType and Stat as in TimeSeriesResponse
	MetricType string `json:"metricType,omitempty"`
	Stat       string `json:"stat,omitempty"`
}

// StatsResponse counts the stored telemetry (GET /api/stats): all of it, or
// that of a time range
type StatsResponse struct {
	TraceCount   int64 `json:"traceCount"`
	SpanCount    int64 `json:"spanCount"`
	LogCount     int64 `json:"logCount"`
	MetricCount  int64 `json:"metricCount"`
	ServiceCount int   `json:"serviceCount"`
	// Services lists all services that sent telemetry, whatever the range
	Services []string `json:"services"`
	// ErrorRate is the percentage of spans with an error status
	ErrorRate float64 `json:"errorRate"`
}

// ServicesResponse lists the services that sent telemetry
type ServicesResponse struct {
	Services []string `json:"services"`
}

// Provider describes a supported AI coding tool (tools.Provider)
type Provider struct {
	Tool         string `json:"tool"`
	ServiceName  string `json:"serviceName"`
	DisplayName  string `json:"displayName"`
	SessionFiles bool   `json:"sessionFiles"`
	// OtherServices are further service names the tool sends
	OtherServices []ProviderService `json:"otherServices,omitempty"`
}

// ProviderService is a further service name of a tool (tools.Service)
type ProviderService struct {
	ServiceName string `json:"serviceName"`
	DisplayName string `json:"displayName"`
}

// ProvidersResponse lists the supported tools (GET /api/providers)
type ProvidersResponse struct {
	Providers []Provider `json:"providers"`
}

// MetricNamesResponse lists the names of stored metrics
type MetricNamesResponse struct {
	Names []string `json:"names"`
}

// BreakdownValuesResponse lists the values of a metric's attribute
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
	// Title is the session's first user prompt, on one line, cut to 200
	// characters and redacted (storage.SessionTitle)
	Title string `json:"title,omitempty"`
	// Repository and Branch attribute the session to a Git repository (the
	// latest of its logs that names them: imported and watched sessions)
	Repository string `json:"repository,omitempty"`
	Branch     string `json:"branch,omitempty"`
	// Usage summed over the session's logs that become transcript messages
	// and carry it (tools.CostKeys, InputTokenKeys, OutputTokenKeys); nil
	// when none does
	CostUSD      *float64 `json:"costUsd,omitempty"`
	InputTokens  *int64   `json:"inputTokens,omitempty"`
	OutputTokens *int64   `json:"outputTokens,omitempty"`
	// Search results only: the text around the terms in the newest matching
	// record (redacted), the number of matching records in the range, and
	// the time of the newest
	Snippet   string     `json:"snippet,omitempty"`
	Matches   int        `json:"matches,omitempty"`
	MatchedAt *time.Time `json:"matchedAt,omitempty"`
}

// SessionsResponse for listing sessions
type SessionsResponse struct {
	Sessions []Session `json:"sessions"`
	Total    int       `json:"total"`
	HasMore  bool      `json:"hasMore"`
	// NextCursor selects the next page (cursor parameter) when HasMore
	NextCursor string `json:"nextCursor,omitempty"`
}

// TranscriptMessage is a message of a transcript
type TranscriptMessage struct {
	Timestamp time.Time `json:"timestamp"`
	Role      string    `json:"role"`
	Content   string    `json:"content"`
	// Index is the message's number in its source (not its position in
	// the transcript)
	Index        int     `json:"index"`
	Model        string  `json:"model,omitempty"`
	ToolName     string  `json:"toolName,omitempty"`
	ToolInput    string  `json:"toolInput,omitempty"`
	ToolOutput   string  `json:"toolOutput,omitempty"`   // Tool execution output (from imports)
	InputTokens  int     `json:"inputTokens,omitempty"`  // Input token count
	OutputTokens int     `json:"outputTokens,omitempty"` // Output token count
	CacheRead    int     `json:"cacheRead,omitempty"`    // Cache read tokens
	CacheWrite   int     `json:"cacheWrite,omitempty"`   // Cache write tokens
	CostUSD      float64 `json:"costUsd,omitempty"`      // Cost in USD
	DurationMs   int     `json:"durationMs,omitempty"`   // Duration in milliseconds
	Success      *bool   `json:"success,omitempty"`      // Tool execution success (pointer to distinguish false from unset)
	OutputSize   int     `json:"outputSize,omitempty"`   // Tool output size in bytes
}

// TranscriptResponse contains the transcript of a session, or a page of its
// messages (transcript.Page): Total counts all messages of the session, and
// NextCursor requests the messages after this page when HasMore
type TranscriptResponse struct {
	SessionID   string `json:"sessionId"`
	ServiceName string `json:"serviceName"`
	// Title is the first user prompt of the transcript (see Session.Title)
	Title     string    `json:"title,omitempty"`
	StartTime time.Time `json:"startTime"`
	LastTime  time.Time `json:"lastTime"`
	// Messages are a page of the session's messages, oldest first
	Messages []TranscriptMessage `json:"messages"`
	// Totals sum all messages of the transcript, not only this page's
	Totals *TranscriptTotals `json:"totals,omitempty"`
	// Search lists the matching messages (only with the search parameter)
	Search *TranscriptSearch `json:"search,omitempty"`
	// Total counts all messages of the session
	Total   int  `json:"total"`
	HasMore bool `json:"hasMore"`
	// NextCursor selects the next page (cursor parameter) when HasMore
	NextCursor string `json:"nextCursor,omitempty"`
}

// TranscriptTotals are the totals of all messages of a transcript
type TranscriptTotals struct {
	CostUSD           float64 `json:"costUsd"`
	InputTokens       int64   `json:"inputTokens"`
	OutputTokens      int64   `json:"outputTokens"`
	CacheRead         int64   `json:"cacheRead"`
	CacheWrite        int64   `json:"cacheWrite"`
	UserMessages      int     `json:"userMessages"`
	AssistantMessages int     `json:"assistantMessages"`
	// ToolCalls counts the tool_use messages, ToolErrors the tool_result
	// messages with success false
	ToolCalls  int `json:"toolCalls"`
	ToolErrors int `json:"toolErrors"`
}

// TranscriptSearch lists the messages of a whole transcript that contain all
// search terms
type TranscriptSearch struct {
	// Terms are the words and "quoted phrases" read from the search
	// parameter
	Terms []string `json:"terms"`
	// Matches are the positions (0-based, ascending) of the matching
	// messages among all messages of the transcript: position p is on the
	// page that starts at the multiple of the page size below p
	Matches []int `json:"matches"`
}
