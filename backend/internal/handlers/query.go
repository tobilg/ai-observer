package handlers

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/tobilg/ai-observer/internal/api"
	"github.com/tobilg/ai-observer/internal/storage"
	"github.com/tobilg/ai-observer/internal/tools"
	"github.com/tobilg/ai-observer/internal/traceview"
	"github.com/tobilg/ai-observer/internal/transcript"
)

// QueryTraces handles GET /api/traces. Background traces of Codex's app and
// exec servers (storage.TraceFilter.Background) are listed with
// background=true only.
func (h *Handlers) QueryTraces(w http.ResponseWriter, r *http.Request) {
	p := newParams(r)
	filter := storage.TraceFilter{
		Service:    p.Str("service"),
		Search:     p.Str("search"),
		Background: p.Bool("background", false),
	}
	from, to := p.TimeRange()
	page := p.Page()
	if !p.Valid(w) {
		return
	}
	resp, err := h.store.QueryTraces(r.Context(), filter, from, to, page)
	if err != nil {
		writeQueryError(w, r, "failed to query traces", err)
		return
	}

	api.WriteJSON(w, http.StatusOK, resp)
}

// GetTrace handles GET /api/traces/{id} and /api/traces/{id}/spans: a page
// of the trace's spans (limit, cursor) and the overview of all of them
func (h *Handlers) GetTrace(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "traceId")
	if id == "" {
		api.WriteError(w, http.StatusBadRequest, "trace id is required")
		return
	}

	p := newParams(r)
	var kind string
	if p.Required("kind") != "" {
		kind = p.Enum("kind", "", api.TraceKindOTelTrace, api.TraceKindCodexOperation)
	}
	// A page of the trace's spans: 1,000 unless asked for fewer
	page := p.PageOf(traceSpansPageSize, traceSpansPageSize)
	if !p.Valid(w) {
		return
	}
	resp, err := traceview.Load(r.Context(), h.store, id, kind, page)
	if err != nil {
		writeQueryError(w, r, "failed to load trace", err)
		return
	}
	if resp == nil {
		api.WriteError(w, http.StatusNotFound, "trace not found")
		return
	}
	resp.Spans = api.List(resp.Spans)
	api.WriteJSON(w, http.StatusOK, resp)
}

// traceSpansPageSize is the default number of spans of a trace detail page
const traceSpansPageSize = 1000

// maxRecentTraces bounds the limit of GET /api/traces/recent
const maxRecentTraces = 100

// QueryRecentTraces handles GET /api/traces/recent (limit, from, to; no
// paging)
func (h *Handlers) QueryRecentTraces(w http.ResponseWriter, r *http.Request) {
	p := newParams(r)
	limit := p.Limit("limit", defaultPageSize, maxRecentTraces)
	from, to := p.TimeRange()
	if !p.Valid(w) {
		return
	}

	resp, err := h.store.GetRecentTraces(r.Context(), limit, from, to)
	if err != nil {
		writeQueryError(w, r, "failed to query recent traces", err)
		return
	}

	api.WriteJSON(w, http.StatusOK, resp)
}

// QueryMetrics handles GET /api/metrics: a page of metric points (service,
// name, type, from, to, limit, cursor)
func (h *Handlers) QueryMetrics(w http.ResponseWriter, r *http.Request) {
	p := newParams(r)
	service := p.Str("service")
	metricName := p.Str("name")
	metricType := p.Enum("type", "", api.MetricTypeGauge, api.MetricTypeSum, api.MetricTypeHistogram, api.MetricTypeExponentialHistogram, api.MetricTypeSummary)
	from, to := p.TimeRange()
	page := p.Page()
	if !p.Valid(w) {
		return
	}
	resp, err := h.store.QueryMetrics(r.Context(), service, metricName, metricType, from, to, page)
	if err != nil {
		writeQueryError(w, r, "failed to query metrics", err)
		return
	}
	api.WriteJSON(w, http.StatusOK, resp)
}

// ListMetricNames handles GET /api/metrics/names
func (h *Handlers) ListMetricNames(w http.ResponseWriter, r *http.Request) {
	service := r.URL.Query().Get("service")

	names, err := h.store.GetMetricNames(r.Context(), service)
	if err != nil {
		writeQueryError(w, r, "failed to list metric names", err)
		return
	}

	api.WriteJSON(w, http.StatusOK, api.MetricNamesResponse{Names: names})
}

// GetBreakdownValues handles GET /api/metrics/breakdown-values
func (h *Handlers) GetBreakdownValues(w http.ResponseWriter, r *http.Request) {
	p := newParams(r)
	metricName := p.Required("name")
	attribute := p.Required("attribute")
	service := p.Str("service")
	if !p.Valid(w) {
		return
	}

	values, err := h.store.GetBreakdownValues(r.Context(), metricName, attribute, service)
	if err != nil {
		if errors.Is(err, storage.ErrInvalidAttributeKey) {
			api.WriteError(w, http.StatusBadRequest, "invalid attribute parameter")
			return
		}
		writeQueryError(w, r, "failed to load breakdown values", err)
		return
	}

	api.WriteJSON(w, http.StatusOK, api.BreakdownValuesResponse{Values: values})
}

// GetMetricAttributeKeys handles GET /api/metrics/attribute-keys (name,
// service): the attribute keys of a metric's points, which series can be
// grouped and filtered by
func (h *Handlers) GetMetricAttributeKeys(w http.ResponseWriter, r *http.Request) {
	p := newParams(r)
	metricName := p.Required("name")
	service := p.Str("service")
	if !p.Valid(w) {
		return
	}
	keys, err := h.store.GetMetricAttributeKeys(r.Context(), metricName, service)
	if err != nil {
		writeQueryError(w, r, "failed to load attribute keys", err)
		return
	}
	api.WriteJSON(w, http.StatusOK, api.MetricAttributeKeysResponse{Keys: keys})
}

// QueryMetricSeries handles GET /api/metrics/series: name (required),
// service, from, to, interval (bucket seconds, 1 to 31 days, default 60),
// aggregate, groupBy (attribute keys, comma-separated or repeated, at most
// 2), attr.<key>=<value> (exact attribute filters), stat (histograms: avg,
// sum, count, min, max, p50, p90, p95, p99), merge (one series of all
// points) and tz (IANA zone the buckets follow, default UTC). Invalid values
// are a 400 naming them.
func (h *Handlers) QueryMetricSeries(w http.ResponseWriter, r *http.Request) {
	p := newParams(r)
	q := api.MetricQuery{
		Name:      p.Required("name"),
		Service:   p.Str("service"),
		Aggregate: p.Bool("aggregate", false),
		GroupBy:   groupByParam(p),
		Filters:   attributeFilterParams(p),
		Stat:      p.Str("stat"),
		Merge:     p.Bool("merge", false),
	}
	interval := p.Int64("interval", storage.DefaultSeriesInterval, 1, storage.MaxSeriesInterval)
	from, to := p.TimeRange()
	zone := p.Str("tz")
	if !p.Valid(w) {
		return
	}

	resp, err := h.store.QuerySeries(r.Context(), q, storage.SeriesRange{From: from, To: to, Interval: interval, TimeZone: zone})
	if err != nil {
		var invalid *storage.MetricQueryError
		if errors.As(err, &invalid) {
			api.WriteError(w, http.StatusBadRequest, invalid.Message)
			return
		}
		writeQueryError(w, r, "failed to query metric series", err)
		return
	}

	api.WriteJSON(w, http.StatusOK, resp)
}

// groupByParam reads the attribute keys of groupBy (comma-separated or
// repeated)
func groupByParam(p *params) []string {
	var keys []string
	for _, v := range p.q["groupBy"] {
		for _, key := range strings.Split(v, ",") {
			if key = strings.TrimSpace(key); key != "" {
				keys = append(keys, key)
			}
		}
	}
	return keys
}

// attributeFilterParams reads the attr.<key>=<value> filters
func attributeFilterParams(p *params) map[string]string {
	var filters map[string]string
	for name, values := range p.q {
		key, ok := strings.CutPrefix(name, "attr.")
		if !ok {
			continue
		}
		if len(values) > 1 {
			p.Fail(fmt.Sprintf("%s is given %d times: filters take one value", name, len(values)))
			continue
		}
		if filters == nil {
			filters = make(map[string]string)
		}
		filters[key] = values[0]
	}
	return filters
}

// maxBatchQueries bounds the queries of a batch series request
const maxBatchQueries = 50

// QueryBatchMetricSeries handles POST /api/metrics/batch-series: up to 50
// queries (id and name required) over one range, interval and time zone
// (timeZone). A query that fails, such as one with an invalid groupBy,
// filter or stat, or one that timed out, has success false, its error and a
// code (api.MetricError*); the others are not affected.
func (h *Handlers) QueryBatchMetricSeries(w http.ResponseWriter, r *http.Request) {
	var req api.BatchMetricSeriesRequest
	if !decodeBody(w, r, &req) {
		return
	}

	if len(req.Queries) == 0 {
		api.WriteError(w, http.StatusBadRequest, "queries array is required and must not be empty")
		return
	}
	if len(req.Queries) > maxBatchQueries {
		api.WriteError(w, http.StatusBadRequest, fmt.Sprintf("maximum %d queries per batch", maxBatchQueries))
		return
	}
	// Results are matched to queries by id
	for i, q := range req.Queries {
		if q.ID == "" {
			api.WriteError(w, http.StatusBadRequest, fmt.Sprintf("query %d: id is required", i))
			return
		}
		if q.Name == "" {
			api.WriteError(w, http.StatusBadRequest, fmt.Sprintf("query %d: name is required", i))
			return
		}
	}

	from, to, err := parseTimeRange(req.From, req.To)
	if err != nil {
		api.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	// 0 (or omitted) is the default
	if req.Interval < 0 || req.Interval > storage.MaxSeriesInterval {
		api.WriteError(w, http.StatusBadRequest, fmt.Sprintf("invalid interval %d: expected seconds from 1 to %d", req.Interval, storage.MaxSeriesInterval))
		return
	}
	if err := h.store.ValidateTimeZone(req.TimeZone); err != nil {
		var invalid *storage.MetricQueryError
		if errors.As(err, &invalid) {
			api.WriteError(w, http.StatusBadRequest, "invalid timeZone: "+invalid.Message)
			return
		}
		writeQueryError(w, r, "failed to query metric series", err)
		return
	}

	resp := h.store.QuerySeriesBatch(r.Context(), req.Queries, storage.SeriesRange{From: from, To: to, Interval: req.Interval, TimeZone: req.TimeZone})
	api.WriteJSON(w, http.StatusOK, resp)
}

// QueryLogs handles GET /api/logs
func (h *Handlers) QueryLogs(w http.ResponseWriter, r *http.Request) {
	p := newParams(r)
	service := p.Str("service")
	severity := p.Str("severity")
	traceID := p.Str("traceId")
	search := p.Str("search")
	from, to := p.TimeRange()
	page := p.Page()
	if !p.Valid(w) {
		return
	}
	resp, err := h.store.QueryLogs(r.Context(), service, severity, traceID, search, from, to, page)
	if err != nil {
		writeQueryError(w, r, "failed to query logs", err)
		return
	}
	api.WriteJSON(w, http.StatusOK, resp)
}

// GetLogLevels handles GET /api/logs/levels (optional from/to)
func (h *Handlers) GetLogLevels(w http.ResponseWriter, r *http.Request) {
	p := newParams(r)
	timeRange := p.OptionalTimeRange()
	if !p.Valid(w) {
		return
	}
	levels, err := h.store.GetLogLevelsInRange(r.Context(), timeRange)
	if err != nil {
		writeQueryError(w, r, "failed to load log levels", err)
		return
	}

	api.WriteJSON(w, http.StatusOK, api.LogLevelCounts(levels))
}

// QuerySessions handles GET /api/sessions. With search (words and "quoted
// phrases", all of which a record must contain) it lists the sessions with
// matching records in the range, the most recently matched first, with a
// snippet of the newest match (storage.SearchSessionsPage).
func (h *Handlers) QuerySessions(w http.ResponseWriter, r *http.Request) {
	p := newParams(r)
	service := p.Str("service")
	from, to := p.TimeRange()
	page := p.Page()
	terms := p.SearchTerms()
	if !p.Valid(w) {
		return
	}
	var resp *api.SessionsResponse
	var err error
	if terms != nil {
		resp, err = h.store.SearchSessionsPage(r.Context(), terms, service, from, to, page)
	} else {
		resp, err = h.store.QuerySessions(r.Context(), service, from, to, page)
	}
	if err != nil {
		writeQueryError(w, r, "failed to query sessions", err)
		return
	}

	api.WriteJSON(w, http.StatusOK, resp)
}

// Messages of a transcript page: by default, and at most. Sessions have up
// to thousands of messages with tool output of several KB each.
const (
	transcriptPageSize    = 500
	maxTranscriptPageSize = 1000
)

// GetSessionTranscript handles GET /api/sessions/{sessionId}/transcript
// (limit, cursor, search): a page of the transcript's messages, with the
// totals of all of them, and with search the positions of all messages that
// contain the terms (transcript.Search), so a client can page to matches it
// has not loaded yet
func (h *Handlers) GetSessionTranscript(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "sessionId")
	if sessionID == "" {
		api.WriteError(w, http.StatusBadRequest, "sessionId is required")
		return
	}
	p := newParams(r)
	terms := p.SearchTerms()
	limit := p.Limit("limit", transcriptPageSize, maxTranscriptPageSize)
	cursor := p.Str("cursor")
	if !p.Valid(w) {
		return
	}

	resp, err := h.loadTranscript(r.Context(), sessionID)
	if err != nil {
		if errors.Is(err, transcript.ErrSessionNotFound) {
			api.WriteError(w, http.StatusNotFound, "session not found")
			return
		}
		writeQueryError(w, r, "failed to load session transcript", err)
		return
	}

	page, err := transcript.Page(resp, limit, cursor)
	if err != nil {
		api.WriteError(w, http.StatusBadRequest, "invalid cursor: pass the nextCursor of the previous page")
		return
	}
	if terms != nil {
		page.Search = &api.TranscriptSearch{Terms: terms, Matches: transcript.Search(resp, terms)}
	}

	api.WriteJSON(w, http.StatusOK, page)
}

// ListServices handles GET /api/services
func (h *Handlers) ListServices(w http.ResponseWriter, r *http.Request) {
	services, err := h.store.GetServices(r.Context())
	if err != nil {
		writeQueryError(w, r, "failed to list services", err)
		return
	}

	api.WriteJSON(w, http.StatusOK, api.ServicesResponse{Services: services})
}

// ListProviders handles GET /api/providers: the supported tools, so the
// dashboard shows their names without keeping its own list
func (h *Handlers) ListProviders(w http.ResponseWriter, r *http.Request) {
	providers := tools.Providers()
	resp := api.ProvidersResponse{Providers: make([]api.Provider, len(providers))}
	for i, p := range providers {
		resp.Providers[i] = api.Provider{
			Tool:         string(p.Tool),
			ServiceName:  p.ServiceName,
			DisplayName:  p.DisplayName,
			SessionFiles: p.SessionFiles,
		}
		for _, s := range p.OtherServices {
			resp.Providers[i].OtherServices = append(resp.Providers[i].OtherServices,
				api.ProviderService{ServiceName: s.Name, DisplayName: s.DisplayName})
		}
	}
	api.WriteJSON(w, http.StatusOK, resp)
}

// GetStats handles GET /api/stats. Without from/to it counts all telemetry
// and is cached; a time range is answered directly, which is cheap because
// only the range's row groups are read.
func (h *Handlers) GetStats(w http.ResponseWriter, r *http.Request) {
	p := newParams(r)
	timeRange := p.OptionalTimeRange()
	if !p.Valid(w) {
		return
	}
	var stats *api.StatsResponse
	var err error
	if timeRange == (storage.TimeRange{}) {
		stats, err = h.stats.get(r.Context(), h.store.GetStats)
	} else {
		stats, err = h.store.GetStatsInRange(r.Context(), timeRange)
	}
	if err != nil {
		writeQueryError(w, r, "failed to load stats", err)
		return
	}
	api.WriteJSON(w, http.StatusOK, stats)
}
