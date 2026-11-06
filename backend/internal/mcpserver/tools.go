package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/tobilg/ai-observer/internal/api"
	"github.com/tobilg/ai-observer/internal/redact"
	"github.com/tobilg/ai-observer/internal/storage"
	"github.com/tobilg/ai-observer/internal/traceview"
	"github.com/tobilg/ai-observer/internal/transcript"
)

// handlers implements the tools. Text they return is redacted again with
// the storage redactor, which covers telemetry stored before redaction was
// enabled.
type handlers struct {
	store    *storage.DuckDBStore
	redactor *redact.Redactor
	now      func() time.Time
}

func (h *handlers) text(s string) string { return h.redactor.String(s) }

// --- search_conversations ---

type searchConversationsIn struct {
	Query string `json:"query" jsonschema:"words to find in prompts, replies and tool calls: a record matches when it contains all of them, in any order (case-insensitive substrings, so retr also finds retry and retries; not semantic). Put a phrase in double quotes to match it exactly, e.g. \"not found\" timeout"`
	Tool  string `json:"tool,omitempty" jsonschema:"only sessions of this tool: claude-code, codex, gemini, opencode, copilot-chat, github-copilot"`
	From  string `json:"from,omitempty" jsonschema:"start of the range (default: 30 days before to); RFC 3339, date or age such as 7d"`
	To    string `json:"to,omitempty" jsonschema:"end of the range (default: now)"`
	Limit int    `json:"limit,omitempty" jsonschema:"maximum sessions (default 10, at most 50)"`
}

type sessionOut struct {
	SessionID string `json:"session_id"`
	Tool      string `json:"tool"`
	Model     string `json:"model,omitempty"`
	StartTime string `json:"start_time"`
	LastTime  string `json:"last_time"`
	Messages  int    `json:"messages"`
}

type sessionMatchOut struct {
	sessionOut
	Matches   int    `json:"matches"`
	MatchedAt string `json:"matched_at"`
	Snippet   string `json:"snippet,omitempty"`
}

type searchConversationsOut struct {
	// Terms shows how the query was split
	Terms    []string          `json:"terms"`
	From     string            `json:"from"`
	To       string            `json:"to"`
	Sessions []sessionMatchOut `json:"sessions"`
}

func sessionOf(s api.Session) sessionOut {
	return sessionOut{
		SessionID: s.SessionID, Tool: toolName(s.ServiceName), Model: s.Model,
		StartTime: formatTime(s.StartTime), LastTime: formatTime(s.LastTime), Messages: s.MessageCount,
	}
}

func (h *handlers) searchConversations(ctx context.Context, _ *mcp.CallToolRequest, in searchConversationsIn) (*mcp.CallToolResult, searchConversationsOut, error) {
	service, err := serviceFor(in.Tool)
	if err != nil {
		return nil, searchConversationsOut{}, err
	}
	from, to, err := timeRange(in.From, in.To, h.now(), 30*24*time.Hour)
	if err != nil {
		return nil, searchConversationsOut{}, err
	}
	terms, err := searchTerms(in.Query)
	if err != nil {
		return nil, searchConversationsOut{}, err
	}
	matches, err := h.store.SearchSessions(ctx, terms, service, from, to, limitArg(in.Limit, 10, 50))
	if err != nil {
		return nil, searchConversationsOut{}, err
	}
	out := searchConversationsOut{Terms: terms, From: formatTime(from), To: formatTime(to), Sessions: []sessionMatchOut{}}
	for _, m := range matches {
		out.Sessions = append(out.Sessions, sessionMatchOut{
			sessionOut: sessionOf(m.Session), Matches: m.Matches, MatchedAt: formatTime(m.MatchedAt), Snippet: h.text(m.Snippet),
		})
	}
	return nil, out, nil
}

// --- list_sessions ---

type listSessionsIn struct {
	Tool   string `json:"tool,omitempty" jsonschema:"only sessions of this tool: claude-code, codex, gemini, opencode, copilot-chat, github-copilot"`
	From   string `json:"from,omitempty" jsonschema:"start of the range (default: 7 days before to); sessions with activity in the range are listed"`
	To     string `json:"to,omitempty" jsonschema:"end of the range (default: now)"`
	Limit  int    `json:"limit,omitempty" jsonschema:"maximum sessions (default 20, at most 100)"`
	Cursor string `json:"cursor,omitempty" jsonschema:"next_cursor of the previous page"`
}

type listSessionsOut struct {
	From       string       `json:"from"`
	To         string       `json:"to"`
	Total      int          `json:"total"`
	Sessions   []sessionOut `json:"sessions"`
	NextCursor string       `json:"next_cursor,omitempty"`
}

func (h *handlers) listSessions(ctx context.Context, _ *mcp.CallToolRequest, in listSessionsIn) (*mcp.CallToolResult, listSessionsOut, error) {
	service, err := serviceFor(in.Tool)
	if err != nil {
		return nil, listSessionsOut{}, err
	}
	from, to, err := timeRange(in.From, in.To, h.now(), 7*24*time.Hour)
	if err != nil {
		return nil, listSessionsOut{}, err
	}
	resp, err := h.store.QuerySessions(ctx, service, from, to, storage.Page{Limit: limitArg(in.Limit, 20, 100), Cursor: in.Cursor})
	if err != nil {
		return nil, listSessionsOut{}, pageError(err)
	}
	out := listSessionsOut{From: formatTime(from), To: formatTime(to), Total: resp.Total, Sessions: []sessionOut{}, NextCursor: resp.NextCursor}
	for _, s := range resp.Sessions {
		out.Sessions = append(out.Sessions, sessionOf(s))
	}
	return nil, out, nil
}

// --- get_transcript ---

type getTranscriptIn struct {
	SessionID string   `json:"session_id" jsonschema:"session ID from list_sessions or search_conversations"`
	Roles     []string `json:"roles,omitempty" jsonschema:"only these roles: user, assistant, tool_use, tool_result, system (default: all)"`
	Limit     int      `json:"limit,omitempty" jsonschema:"maximum messages (default 50, at most 200)"`
	Cursor    string   `json:"cursor,omitempty" jsonschema:"next_cursor of the previous page"`
	MaxChars  int      `json:"max_chars,omitempty" jsonschema:"maximum characters per text field (default 2000, at most 20000); longer text is cut"`
}

type messageOut struct {
	Index        int     `json:"index"`
	Time         string  `json:"time"`
	Role         string  `json:"role"`
	Content      string  `json:"content,omitempty"`
	Model        string  `json:"model,omitempty"`
	ToolName     string  `json:"tool_name,omitempty"`
	ToolInput    string  `json:"tool_input,omitempty"`
	ToolOutput   string  `json:"tool_output,omitempty"`
	InputTokens  int     `json:"input_tokens,omitempty"`
	OutputTokens int     `json:"output_tokens,omitempty"`
	CostUSD      float64 `json:"cost_usd,omitempty"`
	Success      *bool   `json:"success,omitempty"`
	Truncated    bool    `json:"truncated,omitempty"`
}

type getTranscriptOut struct {
	SessionID  string       `json:"session_id"`
	Tool       string       `json:"tool"`
	StartTime  string       `json:"start_time"`
	LastTime   string       `json:"last_time"`
	Total      int          `json:"total_messages"`
	Messages   []messageOut `json:"messages"`
	NextCursor string       `json:"next_cursor,omitempty"`
}

func (h *handlers) getTranscript(ctx context.Context, _ *mcp.CallToolRequest, in getTranscriptIn) (*mcp.CallToolResult, getTranscriptOut, error) {
	resp, err := transcript.Load(ctx, h.store, in.SessionID)
	if errors.Is(err, transcript.ErrSessionNotFound) {
		return nil, getTranscriptOut{}, fmt.Errorf("no session %q: find session IDs with list_sessions or search_conversations", in.SessionID)
	}
	if err != nil {
		return nil, getTranscriptOut{}, err
	}

	roles := make(map[string]bool, len(in.Roles))
	for _, r := range in.Roles {
		roles[r] = true
	}
	var messages []api.TranscriptMessage
	for _, m := range resp.Messages {
		if len(roles) == 0 || roles[m.Role] {
			messages = append(messages, m)
		}
	}

	offset, err := offsetCursor(in.Cursor)
	if err != nil {
		return nil, getTranscriptOut{}, err
	}
	limit := limitArg(in.Limit, 50, 200)
	maxChars := limitArg(in.MaxChars, 2000, 20000)
	out := getTranscriptOut{
		SessionID: resp.SessionID, Tool: toolName(resp.ServiceName),
		StartTime: formatTime(resp.StartTime), LastTime: formatTime(resp.LastTime),
		Total: len(messages), Messages: []messageOut{},
	}
	for i := offset; i < len(messages) && i < offset+limit; i++ {
		out.Messages = append(out.Messages, h.message(messages[i], maxChars))
	}
	if offset+limit < len(messages) {
		out.NextCursor = strconv.Itoa(offset + limit)
	}
	return nil, out, nil
}

func (h *handlers) message(m api.TranscriptMessage, maxChars int) messageOut {
	out := messageOut{
		Index: m.Index, Time: formatTime(m.Timestamp), Role: m.Role, Model: m.Model, ToolName: m.ToolName,
		InputTokens: m.InputTokens, OutputTokens: m.OutputTokens, CostUSD: m.CostUSD, Success: m.Success,
	}
	var cut [3]bool
	out.Content, cut[0] = truncate(h.text(m.Content), maxChars)
	out.ToolInput, cut[1] = truncate(h.text(m.ToolInput), maxChars)
	out.ToolOutput, cut[2] = truncate(h.text(m.ToolOutput), maxChars)
	out.Truncated = cut[0] || cut[1] || cut[2]
	return out
}

// offsetCursor reads the cursor of an in-memory page: the offset of its
// first item
func offsetCursor(cursor string) (int, error) {
	if cursor == "" {
		return 0, nil
	}
	offset, err := strconv.Atoi(cursor)
	if err != nil || offset < 0 {
		return 0, fmt.Errorf("invalid cursor %q: pass the next_cursor of the previous page", cursor)
	}
	return offset, nil
}

// pageError explains cursors the list queries reject
func pageError(err error) error {
	if errors.Is(err, storage.ErrInvalidCursor) {
		return fmt.Errorf("invalid cursor: pass the next_cursor of the previous page with the same arguments")
	}
	return err
}

// --- search_logs ---

type searchLogsIn struct {
	Query    string `json:"query,omitempty" jsonschema:"text to find in log bodies and attributes (case-insensitive substring)"`
	Tool     string `json:"tool,omitempty" jsonschema:"only logs of this tool"`
	Severity string `json:"severity,omitempty" jsonschema:"only this severity, such as ERROR or WARN"`
	TraceID  string `json:"trace_id,omitempty" jsonschema:"only logs of this trace"`
	From     string `json:"from,omitempty" jsonschema:"start of the range (default: 24 hours before to)"`
	To       string `json:"to,omitempty" jsonschema:"end of the range (default: now)"`
	Limit    int    `json:"limit,omitempty" jsonschema:"maximum logs (default 20, at most 100)"`
	Cursor   string `json:"cursor,omitempty" jsonschema:"next_cursor of the previous page"`
}

type logOut struct {
	Time       string            `json:"time"`
	Tool       string            `json:"tool"`
	Severity   string            `json:"severity,omitempty"`
	Body       string            `json:"body,omitempty"`
	Attributes map[string]string `json:"attributes,omitempty"`
	TraceID    string            `json:"trace_id,omitempty"`
	SpanID     string            `json:"span_id,omitempty"`
}

type searchLogsOut struct {
	From       string   `json:"from"`
	To         string   `json:"to"`
	Total      int      `json:"total"`
	Logs       []logOut `json:"logs"`
	NextCursor string   `json:"next_cursor,omitempty"`
}

// maxAttributeChars bounds attribute values in log and span results
const maxAttributeChars = 500

func (h *handlers) attributes(attrs map[string]string, maxChars int) map[string]string {
	if len(attrs) == 0 {
		return nil
	}
	out := make(map[string]string, len(attrs))
	for k, v := range attrs {
		out[k], _ = truncate(h.text(v), maxChars)
	}
	return out
}

func (h *handlers) searchLogs(ctx context.Context, _ *mcp.CallToolRequest, in searchLogsIn) (*mcp.CallToolResult, searchLogsOut, error) {
	service, err := serviceFor(in.Tool)
	if err != nil {
		return nil, searchLogsOut{}, err
	}
	from, to, err := timeRange(in.From, in.To, h.now(), 24*time.Hour)
	if err != nil {
		return nil, searchLogsOut{}, err
	}
	resp, err := h.store.QueryLogs(ctx, service, in.Severity, in.TraceID, in.Query, from, to,
		storage.Page{Limit: limitArg(in.Limit, 20, 100), Cursor: in.Cursor})
	if err != nil {
		return nil, searchLogsOut{}, pageError(err)
	}
	out := searchLogsOut{From: formatTime(from), To: formatTime(to), Total: resp.Total, Logs: []logOut{}, NextCursor: resp.NextCursor}
	for _, l := range resp.Logs {
		body, _ := truncate(h.text(l.Body), 2000)
		out.Logs = append(out.Logs, logOut{
			Time: formatTime(l.Timestamp), Tool: toolName(l.ServiceName), Severity: l.SeverityText, Body: body,
			Attributes: h.attributes(l.LogAttributes, maxAttributeChars), TraceID: l.TraceID, SpanID: l.SpanID,
		})
	}
	return nil, out, nil
}

// --- list_traces / get_trace ---

type listTracesIn struct {
	Query  string `json:"query,omitempty" jsonschema:"text to find in span names and attributes (case-insensitive substring)"`
	Tool   string `json:"tool,omitempty" jsonschema:"only traces of this tool"`
	From   string `json:"from,omitempty" jsonschema:"start of the range (default: 24 hours before to)"`
	To     string `json:"to,omitempty" jsonschema:"end of the range (default: now)"`
	Limit  int    `json:"limit,omitempty" jsonschema:"maximum traces (default 20, at most 100)"`
	Cursor string `json:"cursor,omitempty" jsonschema:"next_cursor of the previous page"`
}

type traceOut struct {
	ID         string  `json:"id"`
	Kind       string  `json:"kind"`
	Tool       string  `json:"tool"`
	RootSpan   string  `json:"root_span"`
	StartTime  string  `json:"start_time"`
	DurationMs float64 `json:"duration_ms"`
	Spans      int     `json:"spans"`
	Status     string  `json:"status"`
}

type listTracesOut struct {
	From       string     `json:"from"`
	To         string     `json:"to"`
	Total      int        `json:"total"`
	Traces     []traceOut `json:"traces"`
	NextCursor string     `json:"next_cursor,omitempty"`
}

func (h *handlers) listTraces(ctx context.Context, _ *mcp.CallToolRequest, in listTracesIn) (*mcp.CallToolResult, listTracesOut, error) {
	service, err := serviceFor(in.Tool)
	if err != nil {
		return nil, listTracesOut{}, err
	}
	from, to, err := timeRange(in.From, in.To, h.now(), 24*time.Hour)
	if err != nil {
		return nil, listTracesOut{}, err
	}
	resp, err := h.store.QueryTraces(ctx, service, in.Query, from, to, storage.Page{Limit: limitArg(in.Limit, 20, 100), Cursor: in.Cursor})
	if err != nil {
		return nil, listTracesOut{}, pageError(err)
	}
	out := listTracesOut{From: formatTime(from), To: formatTime(to), Total: resp.Total, Traces: []traceOut{}, NextCursor: resp.NextCursor}
	for _, tr := range resp.Traces {
		out.Traces = append(out.Traces, traceOut{
			ID: tr.ID, Kind: tr.Kind, Tool: toolName(tr.ServiceName), RootSpan: h.text(tr.RootSpan),
			StartTime: formatTime(tr.StartTime), DurationMs: float64(tr.Duration) / 1e6, Spans: tr.SpanCount, Status: tr.Status,
		})
	}
	return nil, out, nil
}

type getTraceIn struct {
	ID       string `json:"id" jsonschema:"trace ID from list_traces (or a root span ID)"`
	Kind     string `json:"kind,omitempty" jsonschema:"otel_trace (default) or codex_operation, as list_traces returns it"`
	MaxSpans int    `json:"max_spans,omitempty" jsonschema:"maximum spans (default 200, at most 1000)"`
	MaxChars int    `json:"max_chars,omitempty" jsonschema:"maximum characters per attribute value (default 300, at most 5000)"`
}

type spanOut struct {
	SpanID        string            `json:"span_id"`
	ParentSpanID  string            `json:"parent_span_id,omitempty"`
	Depth         int               `json:"depth"`
	Name          string            `json:"name"`
	Tool          string            `json:"tool"`
	StartTime     string            `json:"start_time"`
	DurationMs    float64           `json:"duration_ms"`
	Status        string            `json:"status,omitempty"`
	StatusMessage string            `json:"status_message,omitempty"`
	Attributes    map[string]string `json:"attributes,omitempty"`
}

type getTraceOut struct {
	ID        string    `json:"id"`
	Kind      string    `json:"kind"`
	Total     int       `json:"total_spans"`
	Spans     []spanOut `json:"spans"`
	Truncated bool      `json:"truncated,omitempty"`
}

func (h *handlers) getTrace(ctx context.Context, _ *mcp.CallToolRequest, in getTraceIn) (*mcp.CallToolResult, getTraceOut, error) {
	kind := in.Kind
	if kind == "" {
		kind = api.TraceKindOTelTrace
	}
	spans, err := traceview.Load(ctx, h.store, in.ID, kind)
	if errors.Is(err, traceview.ErrUnsupportedKind) {
		return nil, getTraceOut{}, fmt.Errorf("unknown kind %q: use %s or %s", kind, api.TraceKindOTelTrace, api.TraceKindCodexOperation)
	}
	if err != nil {
		return nil, getTraceOut{}, err
	}
	if len(spans) == 0 {
		return nil, getTraceOut{}, fmt.Errorf("no trace %q of kind %s: find traces with list_traces", in.ID, kind)
	}

	maxSpans := limitArg(in.MaxSpans, 200, 1000)
	maxChars := limitArg(in.MaxChars, 300, 5000)
	out := getTraceOut{ID: in.ID, Kind: kind, Total: len(spans), Spans: []spanOut{}}
	for _, s := range treeOrder(spans) {
		if len(out.Spans) == maxSpans {
			out.Truncated = true
			break
		}
		out.Spans = append(out.Spans, spanOut{
			SpanID: s.span.SpanID, ParentSpanID: s.span.ParentSpanID, Depth: s.depth, Name: h.text(s.span.SpanName),
			Tool: toolName(s.span.ServiceName), StartTime: formatTime(s.span.Timestamp), DurationMs: float64(s.span.Duration) / 1e6,
			Status: s.span.StatusCode, StatusMessage: h.text(s.span.StatusMessage), Attributes: h.attributes(s.span.SpanAttributes, maxChars),
		})
	}
	return nil, out, nil
}

type treeSpan struct {
	span  api.Span
	depth int
}

// treeOrder orders spans depth first, each parent before its children, both
// oldest first. Spans whose parent is not in the trace are roots.
func treeOrder(spans []api.Span) []treeSpan {
	byID := make(map[string]bool, len(spans))
	for _, s := range spans {
		byID[s.SpanID] = true
	}
	children := make(map[string][]api.Span)
	var roots []api.Span
	for _, s := range spans {
		if s.ParentSpanID != "" && byID[s.ParentSpanID] && s.ParentSpanID != s.SpanID {
			children[s.ParentSpanID] = append(children[s.ParentSpanID], s)
		} else {
			roots = append(roots, s)
		}
	}
	byTime := func(list []api.Span) {
		sort.SliceStable(list, func(i, j int) bool { return list[i].Timestamp.Before(list[j].Timestamp) })
	}
	byTime(roots)
	var out []treeSpan
	visited := make(map[string]bool, len(spans))
	var visit func(s api.Span, depth int)
	visit = func(s api.Span, depth int) {
		if visited[s.SpanID] {
			return
		}
		visited[s.SpanID] = true
		out = append(out, treeSpan{s, depth})
		kids := children[s.SpanID]
		byTime(kids)
		for _, c := range kids {
			visit(c, depth+1)
		}
	}
	for _, r := range roots {
		visit(r, 0)
	}
	return out
}

// --- list_metric_names / query_metrics ---

type listMetricNamesIn struct {
	Tool string `json:"tool,omitempty" jsonschema:"only metrics of this tool"`
}

type listMetricNamesOut struct {
	Names []string `json:"names"`
}

func (h *handlers) listMetricNames(ctx context.Context, _ *mcp.CallToolRequest, in listMetricNamesIn) (*mcp.CallToolResult, listMetricNamesOut, error) {
	service, err := serviceFor(in.Tool)
	if err != nil {
		return nil, listMetricNamesOut{}, err
	}
	names, err := h.store.GetMetricNames(ctx, service)
	if err != nil {
		return nil, listMetricNamesOut{}, err
	}
	if names == nil {
		names = []string{}
	}
	return nil, listMetricNamesOut{Names: names}, nil
}

type queryMetricsIn struct {
	Name      string `json:"name" jsonschema:"metric name from list_metric_names, such as claude_code.token.usage"`
	Tool      string `json:"tool,omitempty" jsonschema:"only points of this tool"`
	From      string `json:"from,omitempty" jsonschema:"start of the range (default: 24 hours before to)"`
	To        string `json:"to,omitempty" jsonschema:"end of the range (default: now)"`
	Aggregate bool   `json:"aggregate,omitempty" jsonschema:"one total per series over the whole range instead of time buckets"`
	Interval  string `json:"interval,omitempty" jsonschema:"bucket size such as 5m, 1h or 1d (default: about 50 buckets over the range)"`
}

type pointOut struct {
	Time  string  `json:"time,omitempty"`
	Value float64 `json:"value"`
}

type seriesOut struct {
	Labels map[string]string `json:"labels,omitempty"`
	Points []pointOut        `json:"points"`
}

type queryMetricsOut struct {
	Name     string      `json:"name"`
	From     string      `json:"from"`
	To       string      `json:"to"`
	Interval string      `json:"interval,omitempty"`
	Series   []seriesOut `json:"series"`
}

// maxMetricPoints bounds the points query_metrics returns
const maxMetricPoints = 2000

func (h *handlers) queryMetrics(ctx context.Context, _ *mcp.CallToolRequest, in queryMetricsIn) (*mcp.CallToolResult, queryMetricsOut, error) {
	service, err := serviceFor(in.Tool)
	if err != nil {
		return nil, queryMetricsOut{}, err
	}
	from, to, err := timeRange(in.From, in.To, h.now(), 24*time.Hour)
	if err != nil {
		return nil, queryMetricsOut{}, err
	}
	interval := to.Sub(from) / 50
	if in.Interval != "" {
		if interval, err = parseAge(in.Interval); err != nil || interval < time.Second {
			return nil, queryMetricsOut{}, fmt.Errorf("invalid interval %q: use 30m, 1h or 1d", in.Interval)
		}
	}
	interval = max(interval.Truncate(time.Second), time.Minute)

	resp, err := h.store.QueryMetricSeries(ctx, in.Name, service, from, to, int64(interval/time.Second), in.Aggregate)
	if err != nil {
		return nil, queryMetricsOut{}, err
	}
	out := queryMetricsOut{Name: in.Name, From: formatTime(from), To: formatTime(to), Series: []seriesOut{}}
	if !in.Aggregate {
		out.Interval = interval.String()
	}
	points := 0
	for _, s := range resp.Series {
		series := seriesOut{Labels: h.redactor.Map(s.Labels), Points: []pointOut{}}
		for _, dp := range s.DataPoints {
			p := pointOut{Value: dp[1]}
			if !in.Aggregate {
				p.Time = formatTime(time.UnixMilli(int64(dp[0])))
			}
			series.Points = append(series.Points, p)
		}
		points += len(series.Points)
		if points > maxMetricPoints {
			return nil, queryMetricsOut{}, fmt.Errorf("the result has more than %d points: use a larger interval, a shorter range or aggregate", maxMetricPoints)
		}
		out.Series = append(out.Series, series)
	}
	if len(out.Series) == 0 {
		if names, err := h.store.GetMetricNames(ctx, service); err == nil && !slices.Contains(names, in.Name) {
			return nil, queryMetricsOut{}, fmt.Errorf("no metric %q: find metric names with list_metric_names", in.Name)
		}
	}
	return nil, out, nil
}
