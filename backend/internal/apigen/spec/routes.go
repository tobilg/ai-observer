package spec

import (
	"net/http"

	"github.com/tobilg/ai-observer/internal/api"
	"github.com/tobilg/ai-observer/internal/apigen"
)

// Servers of the routes (apigen.Route.Servers)
const (
	ServerAPI  = "api"
	ServerOTLP = "otlp"
)

// Parameters shared by several routes
var (
	paramService = apigen.Param{Name: "service", Description: "Service name, or a tool name (such as codex) for all of its services"}
	paramFrom    = apigen.Param{Name: "from", Description: "Start of the range (RFC 3339); default: 24 hours before to"}
	paramTo      = apigen.Param{Name: "to", Description: "End of the range (RFC 3339); default: now"}
	paramFromOpt = apigen.Param{Name: "from", Description: "Start of the range (RFC 3339); default: open"}
	paramToOpt   = apigen.Param{Name: "to", Description: "End of the range (RFC 3339); default: open"}
	paramCursor  = apigen.Param{Name: "cursor", Description: "nextCursor of the previous page"}
	paramOffset  = apigen.Param{Name: "offset", Type: "integer", Min: apigen.Int64(0), Max: apigen.Int64(10000),
		Description: "Rows to skip (deprecated: page with cursor)"}
	paramSearch = apigen.Param{Name: "search", Description: `Words and "quoted phrases" a record must all contain (case-insensitive)`}
)

func limit(def, max int64) apigen.Param {
	return apigen.Param{Name: "limit", Type: "integer", Default: def, Min: apigen.Int64(1), Max: apigen.Int64(max),
		Description: "Page size; larger values are reduced to the maximum"}
}

// listParams are the paging parameters of the list endpoints
func listParams(extra ...apigen.Param) []apigen.Param {
	return append(extra, paramFrom, paramTo, limit(50, 1000), paramCursor, paramOffset)
}

// Responses shared by the routes
var (
	errDefault    = apigen.Response{Description: "Error: 400 for invalid parameters, 401 without the token (AI_OBSERVER_AUTH_TOKEN), 429 after failed tokens, 500", Body: api.ErrorResponse{}}
	errBadRequest = apigen.Response{Status: http.StatusBadRequest, Description: "Invalid parameters, named in the message", Body: api.ErrorResponse{}}
	errNotFound   = apigen.Response{Status: http.StatusNotFound, Body: api.ErrorResponse{}}
	errTimeout    = apigen.Response{Status: http.StatusGatewayTimeout, Description: "The query took longer than the request timeout (5 seconds); not worth retrying", Body: api.ErrorResponse{}}
	errAdminOff   = apigen.Response{Status: http.StatusNotFound, Description: "The admin API is disabled (AI_OBSERVER_ADMIN_API)", Body: api.ErrorResponse{}}
	errAdminBusy  = apigen.Response{Status: http.StatusConflict, Description: "Another admin operation runs, or the name exists", Body: api.ErrorResponse{}}
)

func ok(body any) apigen.Response {
	return apigen.Response{Status: http.StatusOK, Description: "OK", Body: body}
}

// query is the responses of a query endpoint
func query(body any) []apigen.Response {
	return []apigen.Response{ok(body), errBadRequest, errTimeout, errDefault}
}

func get(path, tag, summary string, params []apigen.Param, responses ...apigen.Response) apigen.Route {
	return apigen.Route{Method: http.MethodGet, Path: path, Tag: tag, Summary: summary, Query: params, Responses: responses}
}

// Routes describes the HTTP endpoints of both servers. The server's
// TestRouteTableMatchesRouter fails when it differs from the registered
// routes.
func Routes() apigen.Spec {
	traceKind := apigen.Param{Name: "kind", Enum: "TraceKind", Required: true, Description: "The kind of the trace view (TraceOverview.kind)"}
	traceDetail := func(path, summary string) apigen.Route {
		r := get(path, "traces", summary,
			[]apigen.Param{traceKind, limit(1000, 1000), paramCursor, paramOffset},
			append(query(api.SpansResponse{}), errNotFound)...)
		r.Description = "A page of the spans of a trace view, oldest first, with the overview of all of its spans."
		r.PathParams = map[string]string{"traceId": "Trace ID, or the root span ID of a Codex operation"}
		return r
	}
	adminPost := func(path, summary string, body, resp any) apigen.Route {
		return apigen.Route{Method: http.MethodPost, Path: path, Tag: "admin", Summary: summary, Body: body,
			Description: "Only with AI_OBSERVER_ADMIN_API=true; one admin operation at a time.",
			Responses:   []apigen.Response{ok(resp), errBadRequest, errAdminOff, errAdminBusy, errDefault}}
	}
	dashboardID := map[string]string{"id": "Dashboard ID"}
	widgetIDs := map[string]string{"id": "Dashboard ID", "widgetId": "Widget ID"}
	noContent := apigen.Response{Status: http.StatusNoContent, Description: "Deleted"}

	routes := []apigen.Route{
		// Traces
		get("/api/traces", "traces", "A page of traces, the most recent first",
			listParams(paramService, paramSearch,
				apigen.Param{Name: "background", Type: "boolean", Default: false, Description: "Include the background traces of Codex's app and exec servers"}),
			query(api.TracesResponse{})...),
		get("/api/traces/recent", "traces", "The most recent traces (no paging)",
			[]apigen.Param{limit(50, 100), paramFrom, paramTo}, query(api.TracesResponse{})...),
		traceDetail("/api/traces/{traceId}", "A page of a trace's spans"),
		traceDetail("/api/traces/{traceId}/spans", "A page of a trace's spans (same as /api/traces/{traceId})"),

		// Metrics
		get("/api/metrics", "metrics", "A page of metric points, the newest first",
			listParams(paramService, apigen.Param{Name: "name", Description: "Metric name"},
				apigen.Param{Name: "type", Enum: "MetricType"}),
			query(api.MetricsResponse{})...),
		get("/api/metrics/names", "metrics", "The names of stored metrics",
			[]apigen.Param{paramService}, query(api.MetricNamesResponse{})...),
		get("/api/metrics/breakdown-values", "metrics", "The values of a metric's attribute",
			[]apigen.Param{{Name: "name", Required: true}, {Name: "attribute", Required: true}, paramService},
			query(api.BreakdownValuesResponse{})...),
		get("/api/metrics/attribute-keys", "metrics", "The attribute keys of a metric's points, which series can be grouped and filtered by",
			[]apigen.Param{{Name: "name", Required: true}, paramService},
			query(api.MetricAttributeKeysResponse{})...),
		func() apigen.Route {
			r := get("/api/metrics/series", "metrics", "A metric as time series, or totals",
				[]apigen.Param{
					{Name: "name", Required: true}, paramService, paramFrom, paramTo,
					{Name: "interval", Type: "integer", Default: 60, Min: apigen.Int64(1), Max: apigen.Int64(2678400), Description: "Bucket size in seconds (coarser when the range would have more than ~2,000 buckets)"},
					{Name: "aggregate", Type: "boolean", Default: false, Description: "One total per series over the range"},
					{Name: "groupBy", Description: "Up to two attribute keys, comma-separated (or repeated): series per service and their values"},
					{Name: "stat", Enum: "HistogramStat", Description: "Statistic of a histogram metric (default avg)"},
					{Name: "merge", Type: "boolean", Default: false, Description: "One series of all matching points"},
					{Name: "tz", Description: "IANA time zone the buckets follow (default UTC)"},
				},
				query(api.TimeSeriesResponse{})...)
			r.Description = "attr.<key>=<value> parameters keep only points whose attribute has the value (at most 10). Cumulative counters are turned into per-interval increases."
			return r
		}(),
		{Method: http.MethodPost, Path: "/api/metrics/batch-series", Tag: "metrics", Summary: "Several metric series over one range (a read sent as POST)",
			Description: "Up to 50 queries; a query that fails has success false, its error and a code, without affecting the others.",
			Body:        api.BatchMetricSeriesRequest{}, BodyRequired: true,
			Responses: query(api.BatchMetricSeriesResponse{})},

		// Logs
		get("/api/logs", "logs", "A page of log records, the newest first",
			listParams(paramService, apigen.Param{Name: "severity", Description: "Severity text, such as ERROR"},
				apigen.Param{Name: "traceId"}, apigen.Param{Name: "search", Description: "Text the body, scope, severity or attributes contain"}),
			query(api.LogsResponse{})...),
		get("/api/logs/levels", "logs", "The number of log records per severity",
			[]apigen.Param{paramFromOpt, paramToOpt}, query(api.LogLevelCounts{})...),

		// Sessions
		get("/api/sessions", "sessions", "A page of sessions, the most recently active first",
			listParams(paramService, apigen.Param{Name: "search", Description: `Words and "quoted phrases" (at most 10): sessions with a record containing all of them, newest match first, with a snippet`}),
			query(api.SessionsResponse{})...),
		func() apigen.Route {
			r := get("/api/sessions/{sessionId}/transcript", "sessions", "A page of a session's transcript, oldest first",
				[]apigen.Param{limit(500, 1000), paramCursor, paramSearch},
				append(query(api.TranscriptResponse{}), errNotFound)...)
			r.PathParams = map[string]string{"sessionId": "Session ID (or one of its aliases)"}
			r.Description = "With search, search.matches lists the positions of all matching messages of the session."
			return r
		}(),

		// Services, stats, status
		get("/api/services", "status", "The services that sent telemetry", nil, query(api.ServicesResponse{})...),
		get("/api/providers", "status", "The supported tools", nil, ok(api.ProvidersResponse{}), errDefault),
		get("/api/stats", "status", "Counts of the stored telemetry (all of it without from and to)",
			[]apigen.Param{paramFromOpt, paramToOpt}, query(api.StatsResponse{})...),
		get("/api/status", "status", "The running server: version, mode, database, settings, pricing, imports",
			nil, ok(api.StatusResponse{}), errDefault),
		get("/api/internal/metrics", "status", "AI Observer's own metrics (JSON)", nil, ok(api.InternalMetricsResponse{}), errDefault),

		// Admin API
		adminPost("/api/admin/delete", "Delete telemetry in a range (or count it: dryRun)", api.AdminDeleteRequest{}, api.AdminDeleteResponse{}),
		adminPost("/api/admin/export", "Export telemetry to Parquet files in the backup directory", api.AdminExportRequest{}, api.AdminExportResponse{}),
		adminPost("/api/admin/backup", "Write a backup of the database to the backup directory", api.AdminBackupRequest{}, api.AdminBackupResponse{}),
		adminPost("/api/admin/checkpoint", "Write the write-ahead log into the database file", nil, api.AdminCheckpointResponse{}),

		// Dashboards
		get("/api/dashboards", "dashboards", "The dashboards (without widgets)", nil, ok(api.DashboardsResponse{}), errDefault),
		{Method: http.MethodPost, Path: "/api/dashboards", Tag: "dashboards", Summary: "Create a dashboard",
			Body: api.CreateDashboardRequest{}, BodyRequired: true,
			Responses: []apigen.Response{{Status: http.StatusCreated, Description: "Created", Body: api.Dashboard{}}, errBadRequest, errDefault}},
		get("/api/dashboards/default", "dashboards", "The default dashboard with its widgets (created when there is none)",
			nil, ok(api.DashboardWithWidgets{}), errDefault),
		func() apigen.Route {
			r := get("/api/dashboards/{id}", "dashboards", "A dashboard with its widgets", nil, ok(api.DashboardWithWidgets{}), errNotFound, errDefault)
			r.PathParams = dashboardID
			return r
		}(),
		{Method: http.MethodPut, Path: "/api/dashboards/{id}", Tag: "dashboards", Summary: "Rename a dashboard", PathParams: dashboardID,
			Body: api.UpdateDashboardRequest{}, BodyRequired: true,
			Responses: []apigen.Response{ok(api.Dashboard{}), errBadRequest, errNotFound, errDefault}},
		{Method: http.MethodDelete, Path: "/api/dashboards/{id}", Tag: "dashboards", Summary: "Delete a dashboard", PathParams: dashboardID,
			Responses: []apigen.Response{noContent, errDefault}},
		{Method: http.MethodPut, Path: "/api/dashboards/{id}/default", Tag: "dashboards", Summary: "Make a dashboard the default", PathParams: dashboardID,
			Responses: []apigen.Response{ok(api.OKResponse{}), errNotFound, errDefault}},
		{Method: http.MethodPost, Path: "/api/dashboards/{id}/widgets", Tag: "dashboards", Summary: "Add a widget", PathParams: dashboardID,
			Body: api.CreateWidgetRequest{}, BodyRequired: true,
			Responses: []apigen.Response{{Status: http.StatusCreated, Description: "Created", Body: api.DashboardWidget{}}, errBadRequest, errNotFound, errDefault}},
		{Method: http.MethodPut, Path: "/api/dashboards/{id}/widgets/positions", Tag: "dashboards", Summary: "Move widgets", PathParams: dashboardID,
			Body: api.UpdateWidgetPositionsRequest{}, BodyRequired: true,
			Responses: []apigen.Response{ok(api.OKResponse{}), errBadRequest, errNotFound, errDefault}},
		{Method: http.MethodPut, Path: "/api/dashboards/{id}/widgets/{widgetId}", Tag: "dashboards", Summary: "Change a widget (the fields the body sets)", PathParams: widgetIDs,
			Body: api.UpdateWidgetRequest{}, BodyRequired: true,
			Responses: []apigen.Response{ok(api.DashboardWidget{}), errBadRequest, errNotFound, errDefault}},
		{Method: http.MethodDelete, Path: "/api/dashboards/{id}/widgets/{widgetId}", Tag: "dashboards", Summary: "Delete a widget", PathParams: widgetIDs,
			Responses: []apigen.Response{noContent, errNotFound, errDefault}},

		// Real time, health, Prometheus, MCP, dashboard
		{Method: http.MethodGet, Path: "/ws", Tag: "realtime", Summary: "WebSocket of real-time notifications",
			Description: "Each text frame is a JSON WebSocketMessage announcing stored records (count, services, timestamps), without their content.",
			Responses:   []apigen.Response{{Status: http.StatusSwitchingProtocols, Description: "WebSocket; messages are WebSocketMessage"}, errDefault}},
		{Method: http.MethodGet, Path: "/health", Servers: []string{ServerAPI, ServerOTLP}, Tag: "status", Summary: "Liveness: always ok while the server runs",
			Responses: []apigen.Response{ok(api.HealthResponse{})}},
		{Method: http.MethodGet, Path: "/ready", Servers: []string{ServerAPI, ServerOTLP}, Tag: "status", Summary: "Readiness: whether the database answers",
			Responses: []apigen.Response{ok(api.ReadyResponse{}), {Status: http.StatusServiceUnavailable, Description: "The database does not answer", Body: api.ReadyResponse{}}}},
		{Method: http.MethodGet, Path: "/metrics", Tag: "status", Summary: "AI Observer's own metrics in the Prometheus text format",
			Responses: []apigen.Response{{Status: http.StatusOK, Media: "text/plain; version=0.0.4", Description: "Prometheus text exposition"}, errDefault}},
		{Method: "*", Methods: []string{http.MethodPost}, Path: "/mcp", Tag: "mcp", Summary: "MCP endpoint (Streamable HTTP, JSON responses)",
			Description: "Enabled with AI_OBSERVER_MCP=true or --mcp. Legacy MCP Apps clients receive a session ID for capability negotiation. GET answers 405.",
			BodyMedia:   []string{"application/json"},
			Responses:   []apigen.Response{{Status: http.StatusOK, Media: "application/json", Description: "JSON-RPC response"}, errNotFound, errDefault}},
		{Method: http.MethodDelete, Path: "/mcp", Tag: "mcp", Summary: "Close a legacy MCP Apps session",
			Description: "Supply the Mcp-Session-Id returned during initialization. Sessionless requests receive 405.",
			Responses:   []apigen.Response{{Status: http.StatusNoContent, Description: "Session closed"}, errNotFound, errDefault}},
		{Method: "*", Path: "/*", Hidden: true, Summary: "The dashboard (static files)"},

		// OTLP/HTTP
		otlpRoute("/v1/traces", "Receive spans (OTLP ExportTraceServiceRequest)"),
		otlpRoute("/v1/metrics", "Receive metric points (OTLP ExportMetricsServiceRequest)"),
		otlpRoute("/v1/logs", "Receive log records (OTLP ExportLogsServiceRequest)"),
	}
	return apigen.Spec{
		Title:   "AI Observer API",
		Version: "1",
		Description: "The REST API of AI Observer's dashboard (API port, default 8080) and its OTLP/HTTP ingestion (OTLP port, default 4318). " +
			"With AI_OBSERVER_AUTH_TOKEN set, requests need Authorization: Bearer <token>. Generated from the Go types and route table " +
			"(backend/internal/apigen/spec); do not edit.",
		Servers: []apigen.Server{
			{Name: ServerAPI, URL: "http://localhost:8080", Description: "API, dashboard and WebSocket"},
			{Name: ServerOTLP, URL: "http://localhost:4318", Description: "OTLP/HTTP ingestion"},
		},
		Routes: routes,
	}
}

func otlpRoute(path, summary string) apigen.Route {
	return apigen.Route{Method: http.MethodPost, Path: path, Servers: []string{ServerOTLP}, Tag: "otlp", Summary: summary,
		Description: "OTLP/HTTP as the OpenTelemetry protocol specifies it: protobuf or JSON (detected from the body), optionally gzip- or deflate-compressed, at most 10 MB. " +
			"Responses are the OTLP Export*ServiceResponse in the request's encoding, with partial_success for rejected records.",
		BodyMedia: []string{"application/x-protobuf", "application/json"},
		Responses: []apigen.Response{
			{Status: http.StatusOK, Media: "application/x-protobuf", Description: "Stored (an OTLP Export*ServiceResponse, JSON for JSON requests)"},
			{Status: http.StatusBadRequest, Description: "The body is not OTLP", Body: api.ErrorResponse{}},
			{Status: http.StatusRequestEntityTooLarge, Description: "The body exceeds 10 MB", Body: api.ErrorResponse{}},
			{Status: http.StatusUnsupportedMediaType, Description: "Unsupported Content-Encoding", Body: api.ErrorResponse{}},
			{Status: http.StatusServiceUnavailable, Description: "Storing failed; retry after Retry-After", Body: api.ErrorResponse{}},
			errDefault,
		},
	}
}
