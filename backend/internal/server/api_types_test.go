package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/tobilg/ai-observer/internal/api"
	"github.com/tobilg/ai-observer/internal/apigen"
	"github.com/tobilg/ai-observer/internal/apigen/spec"
	"github.com/tobilg/ai-observer/internal/frontend"
)

// newSpecServer is a server with every optional endpoint enabled (MCP,
// admin API)
func newSpecServer(t *testing.T) *Server {
	t.Helper()
	cfg := getTestConfig(t)
	cfg.MCPEnabled = true
	cfg.AdminAPI = true
	cfg.BackupDir = t.TempDir()
	s, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.storage.Close() })
	return s
}

// The route table of the API description (docs/openapi.json) lists exactly
// the routes the routers register
func TestRouteTableMatchesRouter(t *testing.T) {
	s := newSpecServer(t)
	registered := map[string]bool{}
	for name, router := range map[string]chi.Router{spec.ServerAPI: s.apiRouter, spec.ServerOTLP: s.otlpRouter} {
		err := chi.Walk(router, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
			registered[name+" "+method+" "+route] = true
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	// The dashboard's catch-all is only registered when its files are built
	_, spaErr := frontend.NewSPAHandler()

	documented := map[string]bool{}
	var missing []string
	for _, r := range spec.Routes().Routes {
		servers := r.Servers
		if len(servers) == 0 {
			servers = []string{spec.ServerAPI}
		}
		for _, server := range servers {
			if r.Method == "*" {
				// A handler of every method: all registered methods of the path
				found := false
				for key := range registered {
					if strings.HasPrefix(key, server+" ") && strings.HasSuffix(key, " "+r.Path) {
						documented[key], found = true, true
					}
				}
				if !found && !(r.Path == "/*" && spaErr != nil) {
					missing = append(missing, server+" * "+r.Path)
				}
				continue
			}
			key := server + " " + r.Method + " " + r.Path
			documented[key] = true
			if !registered[key] && !(r.Path == "/*" && spaErr != nil) {
				missing = append(missing, key)
			}
		}
	}
	var undocumented []string
	for key := range registered {
		if !documented[key] {
			undocumented = append(undocumented, key)
		}
	}
	slices.Sort(missing)
	slices.Sort(undocumented)
	if len(undocumented) > 0 {
		t.Errorf("routes missing from the route table (internal/apigen/spec/routes.go):\n%s", strings.Join(undocumented, "\n"))
	}
	if len(missing) > 0 {
		t.Errorf("routes in the route table that are not registered:\n%s", strings.Join(missing, "\n"))
	}
}

// apiCheck sends requests to a server and checks their responses against
// the generated types
type apiCheck struct {
	t     *testing.T
	s     *Server
	model *apigen.Model
}

// do sends a request and checks that it answers status with a body of the
// type named want (empty: no check of the body)
func (c apiCheck) do(method, target string, body any, status int, want string) []byte {
	c.t.Helper()
	var reader *bytes.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			c.t.Fatal(err)
		}
		reader = bytes.NewReader(data)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, target, reader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := serve(c.s.apiRouter, req)
	if rec.Code != status {
		c.t.Errorf("%s %s: status %d, want %d: %s", method, target, rec.Code, status, rec.Body.String())
		return nil
	}
	if want != "" {
		for _, problem := range c.model.Validate(want, rec.Body.Bytes()) {
			c.t.Errorf("%s %s: %s", method, target, problem)
		}
	}
	return rec.Body.Bytes()
}

func (c apiCheck) get(target, want string) []byte {
	c.t.Helper()
	return c.do(http.MethodGet, target, nil, http.StatusOK, want)
}

func decode[T any](t *testing.T, data []byte) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatalf("decoding %s: %v", data, err)
	}
	return v
}

// The responses of the API match the generated TypeScript types: lists are
// never null (also on an empty database), required fields are present,
// enums hold their values, and no field is undeclared
func TestAPIResponsesMatchTypes(t *testing.T) {
	model, err := apigen.Build(spec.REST(), ".")
	if err != nil {
		t.Fatal(err)
	}
	s := newSpecServer(t)
	c := apiCheck{t: t, s: s, model: model}
	now := time.Now().UTC().Truncate(time.Second)
	rangeQuery := "from=" + url.QueryEscape(now.Add(-2*time.Hour).Format(time.RFC3339)) + "&to=" + url.QueryEscape(now.Add(time.Minute).Format(time.RFC3339))

	lists := func() {
		c.get("/api/traces", "TracesResponse")
		c.get("/api/traces?"+rangeQuery+"&search=x&background=true", "TracesResponse")
		c.get("/api/traces/recent?"+rangeQuery, "TracesResponse")
		c.get("/api/metrics?"+rangeQuery, "MetricsResponse")
		c.get("/api/metrics/names", "MetricNamesResponse")
		c.get("/api/metrics/names?service=nothing", "MetricNamesResponse")
		c.get("/api/metrics/breakdown-values?name=claude_code.token.usage&attribute=type", "BreakdownValuesResponse")
		c.get("/api/metrics/breakdown-values?name=nothing&attribute=type", "BreakdownValuesResponse")
		c.get("/api/metrics/attribute-keys?name=claude_code.token.usage", "MetricAttributeKeysResponse")
		c.get("/api/metrics/attribute-keys?name=nothing", "MetricAttributeKeysResponse")
		for _, q := range []string{"name=claude_code.token.usage&interval=600", "name=claude_code.token.usage&aggregate=true",
			"name=claude_code.token.usage&groupBy=model&tz=Europe/Berlin", "name=gemini_cli.api.request.latency&stat=p95&interval=600",
			"name=gauge.value&interval=600", "name=nothing"} {
			c.get("/api/metrics/series?"+rangeQuery+"&"+q, "TimeSeriesResponse")
		}
		c.do(http.MethodPost, "/api/metrics/batch-series", map[string]any{
			"from": now.Add(-2 * time.Hour).Format(time.RFC3339), "to": now.Format(time.RFC3339), "interval": 600,
			"queries": []map[string]any{
				{"id": "a", "name": "claude_code.token.usage"},
				{"id": "b", "name": "claude_code.token.usage", "aggregate": true, "groupBy": []string{"model"}},
				{"id": "c", "name": "gemini_cli.api.request.latency", "stat": "nope"},
				{"id": "d", "name": "nothing", "merge": true},
			},
		}, http.StatusOK, "BatchMetricSeriesResponse")
		c.get("/api/logs?"+rangeQuery, "LogsResponse")
		c.get("/api/logs?"+rangeQuery+"&search=nothing-matches", "LogsResponse")
		c.get("/api/logs/levels", "LogLevelCounts")
		c.get("/api/logs/levels?"+rangeQuery, "LogLevelCounts")
		c.get("/api/sessions?"+rangeQuery, "SessionsResponse")
		c.get("/api/sessions?"+rangeQuery+"&search=hello", "SessionsResponse")
		c.get("/api/sessions?"+rangeQuery+"&search=nothing-matches", "SessionsResponse")
		c.get("/api/services", "ServicesResponse")
		c.get("/api/providers", "ProvidersResponse")
		c.get("/api/stats", "StatsResponse")
		c.get("/api/stats?"+rangeQuery, "StatsResponse")
		c.get("/api/status", "StatusResponse")
		c.get("/api/internal/metrics", "InternalMetricsResponse")
	}

	// An empty database
	lists()
	c.get("/api/dashboards", "DashboardsResponse")
	c.do(http.MethodGet, "/api/sessions/none/transcript", nil, http.StatusNotFound, "ErrorResponse")
	c.do(http.MethodGet, "/api/traces/none?kind=otel_trace", nil, http.StatusNotFound, "ErrorResponse")
	c.do(http.MethodGet, "/api/traces?limit=abc", nil, http.StatusBadRequest, "ErrorResponse")

	// Dashboards and widgets
	def := decode[api.DashboardWithWidgets](t, c.get("/api/dashboards/default", "DashboardWithWidgets"))
	c.get("/api/dashboards/"+def.ID, "DashboardWithWidgets")
	created := decode[api.Dashboard](t, c.do(http.MethodPost, "/api/dashboards", api.CreateDashboardRequest{Name: "Empty"}, http.StatusCreated, "Dashboard"))
	c.get("/api/dashboards/"+created.ID, "DashboardWithWidgets")
	c.get("/api/dashboards", "DashboardsResponse")
	c.do(http.MethodPut, "/api/dashboards/"+created.ID, api.UpdateDashboardRequest{Description: "d"}, http.StatusOK, "Dashboard")
	widget := decode[api.DashboardWidget](t, c.do(http.MethodPost, "/api/dashboards/"+created.ID+"/widgets",
		api.CreateWidgetRequest{WidgetType: "stats_traces", Title: "Traces", ColSpan: 1, RowSpan: 1}, http.StatusCreated, "DashboardWidget"))
	stacked := false
	c.do(http.MethodPut, "/api/dashboards/"+created.ID+"/widgets/"+widget.ID,
		api.UpdateWidgetRequest{Title: "T", Config: &api.WidgetConfig{MetricName: "m", ChartStacked: &stacked, Stat: "p95"}}, http.StatusOK, "DashboardWidget")
	c.do(http.MethodPut, "/api/dashboards/"+created.ID+"/widgets/positions",
		api.UpdateWidgetPositionsRequest{Positions: []api.WidgetPosition{{ID: widget.ID, GridColumn: 2, GridRow: 1}}}, http.StatusOK, "OKResponse")
	c.do(http.MethodPut, "/api/dashboards/"+created.ID+"/default", nil, http.StatusOK, "OKResponse")
	c.get("/api/dashboards/default", "DashboardWithWidgets")
	c.do(http.MethodDelete, "/api/dashboards/"+created.ID+"/widgets/"+widget.ID, nil, http.StatusNoContent, "")
	c.do(http.MethodDelete, "/api/dashboards/"+created.ID, nil, http.StatusNoContent, "")

	// Health and admin API
	c.get("/health", "HealthResponse")
	c.get("/ready", "ReadyResponse")
	c.do(http.MethodPost, "/api/admin/checkpoint", nil, http.StatusOK, "AdminCheckpointResponse")
	c.do(http.MethodPost, "/api/admin/delete", api.AdminDeleteRequest{From: now.Add(-48 * time.Hour), To: now.Add(-47 * time.Hour), DryRun: true},
		http.StatusOK, "AdminDeleteResponse")
	c.do(http.MethodPost, "/api/admin/delete", api.AdminDeleteRequest{From: now.Add(-48 * time.Hour), To: now.Add(-47 * time.Hour), Scope: "logs"},
		http.StatusOK, "AdminDeleteResponse")
	c.do(http.MethodPost, "/api/admin/backup", api.AdminBackupRequest{Name: "b.duckdb"}, http.StatusOK, "AdminBackupResponse")
	c.do(http.MethodPost, "/api/admin/export", api.AdminExportRequest{Name: "export"}, http.StatusOK, "AdminExportResponse")

	// With data: a Claude Code session with a trace, metrics of several types
	seedTelemetry(t, s, now)
	lists()
	traces := decode[api.TracesResponse](t, c.get("/api/traces?"+rangeQuery, "TracesResponse"))
	if len(traces.Traces) == 0 {
		t.Fatal("no traces listed")
	}
	tr := traces.Traces[0]
	c.get(fmt.Sprintf("/api/traces/%s?kind=%s", tr.ID, tr.Kind), "SpansResponse")
	c.get(fmt.Sprintf("/api/traces/%s/spans?kind=%s&limit=1", tr.ID, tr.Kind), "SpansResponse")
	// A page after the last span
	c.get(fmt.Sprintf("/api/traces/%s/spans?kind=%s&offset=100", tr.ID, tr.Kind), "SpansResponse")
	sessions := decode[api.SessionsResponse](t, c.get("/api/sessions?"+rangeQuery, "SessionsResponse"))
	if len(sessions.Sessions) == 0 {
		t.Fatal("no sessions listed")
	}
	id := url.PathEscape(sessions.Sessions[0].SessionID)
	c.get("/api/sessions/"+id+"/transcript", "TranscriptResponse")
	c.get("/api/sessions/"+id+"/transcript?limit=1&search=hello", "TranscriptResponse")
	c.get("/api/sessions/"+id+"/transcript?search=nothing-matches", "TranscriptResponse")
	c.do(http.MethodPost, "/api/admin/delete", api.AdminDeleteRequest{From: now.Add(-3 * time.Hour), To: now.Add(time.Minute), Service: "codex", DryRun: true},
		http.StatusOK, "AdminDeleteResponse")
}

// seedTelemetry stores a Claude Code session (prompt, API request, a trace
// with an event and a link) and metric points of each type
func seedTelemetry(t *testing.T, s *Server, now time.Time) {
	t.Helper()
	at := func(minutes int) time.Time { return now.Add(time.Duration(minutes) * time.Minute) }
	session := map[string]string{"session.id": "sess-1"}
	with := func(base map[string]string, kv ...string) map[string]string {
		m := map[string]string{}
		for k, v := range base {
			m[k] = v
		}
		for i := 0; i+1 < len(kv); i += 2 {
			m[kv[i]] = kv[i+1]
		}
		return m
	}
	logs := []api.LogRecord{
		{Timestamp: at(-30), ServiceName: "claude-code", SeverityText: "INFO", Body: "claude_code.user_prompt",
			LogAttributes: with(session, "event.name", "user_prompt", "prompt", "hello there")},
		{Timestamp: at(-29), ServiceName: "claude-code", SeverityText: "INFO", Body: "claude_code.api_request", TraceID: "0102030405060708090a0b0c0d0e0f10",
			LogAttributes: with(session, "event.name", "api_request", "model", "claude-sonnet-5-5", "cost_usd", "0.25", "input_tokens", "100", "output_tokens", "20")},
		{Timestamp: at(-28), ServiceName: "claude-code", SeverityText: "ERROR", Body: "claude_code.tool_result",
			LogAttributes: with(session, "event.name", "tool_result", "tool_name", "Bash", "success", "false")},
	}
	spans := []api.Span{
		{Timestamp: at(-29), TraceID: "0102030405060708090a0b0c0d0e0f10", SpanID: "0102030405060708", SpanName: "claude_code.interaction",
			ServiceName: "claude-code", Duration: int64(time.Second), StatusCode: "OK", SpanAttributes: session,
			Events: []api.SpanEvent{{Timestamp: at(-29), Name: "start", Attributes: map[string]string{"k": "v"}}}},
		{Timestamp: at(-29), TraceID: "0102030405060708090a0b0c0d0e0f10", SpanID: "1112131415161718", ParentSpanID: "0102030405060708",
			SpanName: "claude_code.llm_request", ServiceName: "claude-code", Duration: int64(time.Millisecond), StatusCode: "ERROR",
			StatusMessage: "failed", SpanAttributes: with(session, "model", "claude-sonnet-5-5"),
			Links: []api.SpanLink{{TraceID: "a102030405060708090a0b0c0d0e0f10", SpanID: "a102030405060708"}}},
	}
	delta, cumulative := int32(1), int32(2)
	monotonic := true
	value := func(v float64) *float64 { return &v }
	count, zero := uint64(3), uint64(0)
	scale, offset := int32(1), int32(0)
	metrics := []api.MetricDataPoint{
		{Timestamp: at(-29), ServiceName: "claude-code", MetricName: "claude_code.token.usage", MetricType: api.MetricTypeSum,
			Attributes: map[string]string{"type": "input", "model": "claude-sonnet-5-5"}, Value: value(100), AggregationTemporality: &delta, IsMonotonic: &monotonic},
		{Timestamp: at(-29), ServiceName: "claude-code", MetricName: "claude_code.cost.usage", MetricType: api.MetricTypeSum,
			Attributes: map[string]string{"model": "claude-sonnet-5-5"}, Value: value(0.25), AggregationTemporality: &cumulative, IsMonotonic: &monotonic},
		{Timestamp: at(-90), ServiceName: "gemini_cli", MetricName: "gauge.value", MetricType: api.MetricTypeGauge, Value: value(7)},
		{Timestamp: at(-20), ServiceName: "gemini_cli", MetricName: "gemini_cli.api.request.latency", MetricType: api.MetricTypeHistogram,
			AggregationTemporality: &delta, Count: &count, Sum: value(300), Min: value(50), Max: value(200),
			ExplicitBounds: []float64{100, 1000}, BucketCounts: []uint64{1, 2, 0}},
		{Timestamp: at(-20), ServiceName: "gemini_cli", MetricName: "exp.histogram", MetricType: api.MetricTypeExponentialHistogram,
			AggregationTemporality: &delta, Count: &count, Sum: value(3), Scale: &scale, ZeroCount: &zero,
			PositiveOffset: &offset, PositiveBucketCounts: []uint64{1, 2}},
		{Timestamp: at(-20), ServiceName: "gemini_cli", MetricName: "summary.value", MetricType: api.MetricTypeSummary,
			Count: &count, Sum: value(3), QuantileValues: []float64{1, 2}, QuantileQuantiles: []float64{0.5, 0.99}},
	}
	if err := s.storage.InsertTelemetry(context.Background(), logs, metrics, spans); err != nil {
		t.Fatal(err)
	}
}

// The structured outputs of the MCP tools the MCP Apps view draws match
// their generated types
func TestMCPOutputsMatchTypes(t *testing.T) {
	cfg, err := spec.MCP()
	if err != nil {
		t.Fatal(err)
	}
	model, err := apigen.Build(cfg, ".")
	if err != nil {
		t.Fatal(err)
	}
	s := newSpecServer(t)
	ts := httptest.NewServer(s.apiRouter)
	defer ts.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	cs, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: ts.URL + "/mcp"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()

	check := func(tool, want string, args map[string]any) {
		t.Helper()
		res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: tool, Arguments: args})
		if err != nil || res.IsError {
			t.Fatalf("%s %v: %+v, %v", tool, args, res, err)
		}
		data, err := json.Marshal(res.StructuredContent)
		if err != nil {
			t.Fatal(err)
		}
		for _, problem := range model.Validate(want, data) {
			t.Errorf("%s %v: %s", tool, args, problem)
		}
	}
	calls := func() {
		for _, group := range []string{"tool", "model", "day", "repository"} {
			check("usage_summary", "UsageSummary", map[string]any{"group_by": group})
		}
		check("list_sessions", "SessionsResult", map[string]any{})
	}
	calls()
	now := time.Now().UTC().Truncate(time.Second)
	seedTelemetry(t, s, now)
	calls()
	check("query_metrics", "MetricsResult", map[string]any{"name": "claude_code.token.usage", "from": "3h"})
	check("query_metrics", "MetricsResult", map[string]any{"name": "claude_code.token.usage", "aggregate": true, "group_by": []string{"model"}})
	check("query_metrics", "MetricsResult", map[string]any{"name": "gemini_cli.api.request.latency", "from": "3h", "interval": "10m", "stat": "p95"})
	check("query_metrics", "MetricsResult", map[string]any{"name": "gauge.value", "from": "3h", "interval": "10m"})
	check("get_trace", "TraceResult", map[string]any{"id": "0102030405060708090a0b0c0d0e0f10"})
	check("get_trace", "TraceResult", map[string]any{"id": "0102030405060708090a0b0c0d0e0f10", "max_spans": 1})
}
