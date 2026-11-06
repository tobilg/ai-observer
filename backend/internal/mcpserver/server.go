// Package mcpserver serves AI Observer's data to agents over the Model
// Context Protocol (MCP): the endpoint /mcp on the API port, enabled with
// AI_OBSERVER_MCP=true or --mcp. Its tools read stored sessions, transcripts,
// logs, traces, metrics and usage; none of them changes data.
package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/tobilg/ai-observer/internal/logger"
	"github.com/tobilg/ai-observer/internal/storage"
	"github.com/tobilg/ai-observer/internal/transcript"
)

// Path is where the API router serves MCP
const Path = "/mcp"

const instructions = `AI Observer stores the telemetry of AI coding tools (Claude Code, OpenAI Codex CLI, Gemini CLI, GitHub Copilot, OpenCode): conversations (sessions with their transcripts), logs, traces, metrics, token usage and costs.

Typical use:
- Find earlier work: search_conversations (records containing all of the query's words, in any order; "quoted phrases" match exactly), then get_transcript with a session_id.
- Browse recent sessions: list_sessions.
- Costs and tokens: usage_summary (group by tool, model, day or repository).
- Errors and events: search_logs; requests and tool calls as spans: list_traces, get_trace.
- Other metrics: list_metric_names, then query_metrics.

Times accept RFC 3339, dates (UTC) or ages relative to now such as 30m, 24h, 7d, 2w. Lists are paged: pass next_cursor as cursor for the next page. Long text is cut (max_chars); secrets in stored text are replaced by [REDACTED:...] markers.`

// New returns the MCP server with AI Observer's tools
func New(store *storage.DuckDBStore, version string) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "ai-observer", Title: "AI Observer", Version: version},
		&mcp.ServerOptions{Instructions: instructions})
	h := &handlers{store: store, redactor: store.Redactor(), now: time.Now}

	readOnly := func(name, title, description string) *mcp.Tool {
		closed := false
		return &mcp.Tool{Name: name, Title: title, Description: description,
			Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true, OpenWorldHint: &closed}}
	}
	mcp.AddTool(server, readOnly("search_conversations", "Search conversations",
		"Find sessions with a prompt, reply or tool call that contains all words of the query, in any order (case-insensitive substrings; \"quoted phrases\" match exactly), newest match first, with a snippet around the match. Use get_transcript to read a session."),
		h.searchConversations)
	mcp.AddTool(server, readOnly("list_sessions", "List sessions",
		"List the sessions (conversations) of the AI coding tools with activity in a time range, most recently active first, with tool, model, start, last activity and message count."),
		h.listSessions)
	mcp.AddTool(server, readOnly("get_transcript", "Get transcript",
		"Read the transcript of a session: user prompts, assistant replies, tool calls and results in order, paged, with long text cut to max_chars."),
		h.getTranscript)
	mcp.AddTool(server, readOnly("usage_summary", "Usage summary",
		"Token usage and cost in USD over a time range, grouped by tool, model, day or repository."),
		h.usageSummary)
	mcp.AddTool(server, readOnly("search_logs", "Search logs",
		"Search the stored log records (events such as prompts, API requests, tool results and errors) by text, tool, severity or trace, newest first."),
		h.searchLogs)
	mcp.AddTool(server, readOnly("list_traces", "List traces",
		"List traces (requests, agent turns and tool calls recorded as spans) in a time range, newest first, optionally matching a text."),
		h.listTraces)
	mcp.AddTool(server, readOnly("get_trace", "Get trace",
		"Read the spans of a trace as a tree (depth first) with durations, status and attributes."),
		h.getTrace)
	mcp.AddTool(server, readOnly("list_metric_names", "List metric names",
		"List the names of the stored metrics, optionally of one tool."),
		h.listMetricNames)
	mcp.AddTool(server, readOnly("query_metrics", "Query metrics",
		"Read a metric as time series (buckets) or totals over a range, per series of service, type and model. Cumulative counters are turned into increases."),
		h.queryMetrics)

	server.AddResourceTemplate(&mcp.ResourceTemplate{
		URITemplate: transcriptURIPrefix + "{session_id}/transcript",
		Name:        "session-transcript",
		Title:       "Session transcript",
		Description: "The transcript of a session as Markdown",
		MIMEType:    "text/markdown",
	}, h.readTranscriptResource)
	return server
}

// NewHandler returns the HTTP handler of the MCP endpoint (streamable HTTP).
// It is stateless with JSON responses: the tools need no session and no
// server-sent events. The API router's Host and Origin checks and token
// authentication apply before it, so the SDK's own localhost check, which
// would reject the host names allowed with AI_OBSERVER_ALLOWED_HOSTS, is off.
func NewHandler(store *storage.DuckDBStore, version string) http.Handler {
	server := New(store, version)
	return mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{
		Stateless:                  true,
		JSONResponse:               true,
		DisableLocalhostProtection: true,
		Logger:                     logger.Logger(),
	})
}

const transcriptURIPrefix = "ai-observer://sessions/"

func (h *handlers) readTranscriptResource(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	uri := req.Params.URI
	rest, ok := strings.CutPrefix(uri, transcriptURIPrefix)
	escapedID, ok2 := strings.CutSuffix(rest, "/transcript")
	if !ok || !ok2 || escapedID == "" {
		return nil, mcp.ResourceNotFoundError(uri)
	}
	sessionID, err := url.PathUnescape(escapedID)
	if err != nil {
		return nil, mcp.ResourceNotFoundError(uri)
	}
	resp, err := transcript.Load(ctx, h.store, sessionID)
	if errors.Is(err, transcript.ErrSessionNotFound) {
		return nil, mcp.ResourceNotFoundError(uri)
	}
	if err != nil {
		return nil, err
	}

	var b strings.Builder
	fmt.Fprintf(&b, "# Session %s (%s)\n\n%s to %s, %d messages\n", resp.SessionID, toolName(resp.ServiceName),
		formatTime(resp.StartTime), formatTime(resp.LastTime), len(resp.Messages))
	for _, m := range resp.Messages {
		msg := h.message(m, 4000)
		fmt.Fprintf(&b, "\n## %s, %s", msg.Role, msg.Time)
		if msg.ToolName != "" {
			fmt.Fprintf(&b, " (%s)", msg.ToolName)
		}
		b.WriteString("\n\n")
		for _, text := range []string{msg.Content, msg.ToolInput, msg.ToolOutput} {
			if text != "" {
				b.WriteString(text)
				b.WriteString("\n\n")
			}
		}
	}
	return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{URI: uri, MIMEType: "text/markdown", Text: b.String()}}}, nil
}
