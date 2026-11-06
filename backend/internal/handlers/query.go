package handlers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/tobilg/ai-observer/internal/api"
	"github.com/tobilg/ai-observer/internal/logger"
	"github.com/tobilg/ai-observer/internal/storage"
	"github.com/tobilg/ai-observer/internal/tools"
	"github.com/tobilg/ai-observer/internal/traceview"
	"github.com/tobilg/ai-observer/internal/transcript"
	"github.com/tobilg/ai-observer/internal/websocket"
)

// QueryTraces handles GET /api/traces
func (h *Handlers) QueryTraces(w http.ResponseWriter, r *http.Request) {
	service := r.URL.Query().Get("service")
	search := r.URL.Query().Get("search")
	from, to, ok := requestTimeRange(w, r)
	if !ok {
		return
	}
	resp, err := h.store.QueryTraces(r.Context(), service, search, from, to, requestPage(r))
	if err != nil {
		writeQueryError(w, r, "failed to query traces", err)
		return
	}

	api.WriteJSON(w, http.StatusOK, resp)
}

// GetTrace handles GET /api/traces/{id} and /api/traces/{id}/spans
func (h *Handlers) GetTrace(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "traceId")
	if id == "" {
		api.WriteError(w, http.StatusBadRequest, "trace id is required")
		return
	}

	kind := r.URL.Query().Get("kind")
	if kind == "" {
		api.WriteError(w, http.StatusBadRequest, "kind is required")
		return
	}
	if kind != api.TraceKindOTelTrace && kind != api.TraceKindCodexOperation {
		api.WriteError(w, http.StatusBadRequest, "unsupported trace kind")
		return
	}

	spans, err := traceview.Load(r.Context(), h.store, id, kind)
	if err != nil {
		writeQueryError(w, r, "failed to load trace", err)
		return
	}

	if len(spans) == 0 {
		api.WriteError(w, http.StatusNotFound, "trace not found")
		return
	}

	api.WriteJSON(w, http.StatusOK, api.SpansResponse{Spans: spans})
}

// QueryRecentTraces handles GET /api/traces/recent
func (h *Handlers) QueryRecentTraces(w http.ResponseWriter, r *http.Request) {
	limit, _ := parsePagination(r)
	if limit > 100 {
		limit = 100
	}
	from, to, ok := requestTimeRange(w, r)
	if !ok {
		return
	}

	resp, err := h.store.GetRecentTraces(r.Context(), limit, from, to)
	if err != nil {
		writeQueryError(w, r, "failed to query recent traces", err)
		return
	}

	api.WriteJSON(w, http.StatusOK, resp)
}

// QueryMetrics handles GET /api/metrics
func (h *Handlers) QueryMetrics(w http.ResponseWriter, r *http.Request) {
	service := r.URL.Query().Get("service")
	metricName := r.URL.Query().Get("name")
	metricType := r.URL.Query().Get("type")
	from, to, ok := requestTimeRange(w, r)
	if !ok {
		return
	}
	resp, err := h.store.QueryMetrics(r.Context(), service, metricName, metricType, from, to, requestPage(r))
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
	metricName := r.URL.Query().Get("name")
	if metricName == "" {
		api.WriteError(w, http.StatusBadRequest, "name parameter is required")
		return
	}

	attribute := r.URL.Query().Get("attribute")
	if attribute == "" {
		api.WriteError(w, http.StatusBadRequest, "attribute parameter is required")
		return
	}

	service := r.URL.Query().Get("service")

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

// QueryMetricSeries handles GET /api/metrics/series
func (h *Handlers) QueryMetricSeries(w http.ResponseWriter, r *http.Request) {
	metricName := r.URL.Query().Get("name")
	if metricName == "" {
		api.WriteError(w, http.StatusBadRequest, "name parameter is required")
		return
	}

	service := r.URL.Query().Get("service")
	intervalStr := r.URL.Query().Get("interval")
	var intervalSeconds int64 = 60 // default 1 minute
	if intervalStr != "" {
		if parsed, err := strconv.ParseInt(intervalStr, 10, 64); err == nil && parsed > 0 {
			intervalSeconds = parsed
		}
	}
	aggregate := r.URL.Query().Get("aggregate") == "true"
	from, to, ok := requestTimeRange(w, r)
	if !ok {
		return
	}

	resp, err := h.store.QueryMetricSeries(r.Context(), metricName, service, from, to, intervalSeconds, aggregate)
	if err != nil {
		writeQueryError(w, r, "failed to query metric series", err)
		return
	}

	api.WriteJSON(w, http.StatusOK, resp)
}

// QueryBatchMetricSeries handles POST /api/metrics/batch-series
func (h *Handlers) QueryBatchMetricSeries(w http.ResponseWriter, r *http.Request) {
	var req api.BatchMetricSeriesRequest
	if !decodeBody(w, r, &req) {
		return
	}

	// Validate request
	if len(req.Queries) == 0 {
		api.WriteError(w, http.StatusBadRequest, "queries array is required and must not be empty")
		return
	}

	if len(req.Queries) > 50 {
		api.WriteError(w, http.StatusBadRequest, "maximum 50 queries per batch")
		return
	}

	// Validate each query has required fields
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

	// Parse time range from request body
	from, to, err := parseTimeRange(req.From, req.To)
	if err != nil {
		api.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Default to 60 seconds if not specified
	intervalSeconds := req.Interval
	if intervalSeconds <= 0 {
		intervalSeconds = 60
	}

	resp := h.store.QueryBatchMetricSeries(r.Context(), req.Queries, from, to, intervalSeconds)
	api.WriteJSON(w, http.StatusOK, resp)
}

// QueryLogs handles GET /api/logs
func (h *Handlers) QueryLogs(w http.ResponseWriter, r *http.Request) {
	service := r.URL.Query().Get("service")
	severity := r.URL.Query().Get("severity")
	traceID := r.URL.Query().Get("traceId")
	search := r.URL.Query().Get("search")
	from, to, ok := requestTimeRange(w, r)
	if !ok {
		return
	}
	resp, err := h.store.QueryLogs(r.Context(), service, severity, traceID, search, from, to, requestPage(r))
	if err != nil {
		writeQueryError(w, r, "failed to query logs", err)
		return
	}

	api.WriteJSON(w, http.StatusOK, resp)
}

// GetLogLevels handles GET /api/logs/levels (optional from/to)
func (h *Handlers) GetLogLevels(w http.ResponseWriter, r *http.Request) {
	timeRange, ok := optionalTimeRange(w, r)
	if !ok {
		return
	}
	levels, err := h.store.GetLogLevelsInRange(r.Context(), timeRange)
	if err != nil {
		writeQueryError(w, r, "failed to load log levels", err)
		return
	}

	api.WriteJSON(w, http.StatusOK, levels)
}

// QuerySessions handles GET /api/sessions
func (h *Handlers) QuerySessions(w http.ResponseWriter, r *http.Request) {
	service := r.URL.Query().Get("service")
	from, to, ok := requestTimeRange(w, r)
	if !ok {
		return
	}
	resp, err := h.store.QuerySessions(r.Context(), service, from, to, requestPage(r))
	if err != nil {
		writeQueryError(w, r, "failed to query sessions", err)
		return
	}

	api.WriteJSON(w, http.StatusOK, resp)
}

// GetSessionTranscript handles GET /api/sessions/{sessionId}/transcript
func (h *Handlers) GetSessionTranscript(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "sessionId")
	if sessionID == "" {
		api.WriteError(w, http.StatusBadRequest, "sessionId is required")
		return
	}

	resp, err := transcript.Load(r.Context(), h.store, sessionID)
	if err != nil {
		if errors.Is(err, transcript.ErrSessionNotFound) {
			api.WriteError(w, http.StatusNotFound, "session not found")
			return
		}
		writeQueryError(w, r, "failed to load session transcript", err)
		return
	}

	api.WriteJSON(w, http.StatusOK, resp)
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
	}
	api.WriteJSON(w, http.StatusOK, resp)
}

// statsCacheTTL bounds how often /api/stats scans the telemetry tables: the
// dashboard polls it and also refreshes it after every burst of new data
const statsCacheTTL = 5 * time.Second

// statsCache holds the last stats response. Its mutex is held during the
// query, so concurrent requests share one scan, but not while responding.
type statsCache struct {
	mu      sync.Mutex
	stats   *api.StatsResponse
	expires time.Time
}

// get returns the cached stats, refreshing them when they have expired
func (c *statsCache) get(ctx context.Context, load func(context.Context) (*api.StatsResponse, error)) (*api.StatsResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.stats == nil || time.Now().After(c.expires) {
		stats, err := load(ctx)
		if err != nil {
			return nil, err
		}
		c.stats = stats
		c.expires = time.Now().Add(statsCacheTTL)
	}
	return c.stats, nil
}

// GetStats handles GET /api/stats. Without from/to it counts all telemetry
// and is cached; a time range is answered directly, which is cheap because
// only the range's row groups are read.
func (h *Handlers) GetStats(w http.ResponseWriter, r *http.Request) {
	timeRange, ok := optionalTimeRange(w, r)
	if !ok {
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

// HandleWebSocket handles GET /ws
func (h *Handlers) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	websocket.ServeWs(h.hub, w, r)
}

// Health handles GET /health
func (h *Handlers) Health(w http.ResponseWriter, r *http.Request) {
	api.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// writeQueryError reports a failed query. A query stopped by the request
// timeout is a 504, which clients must not retry: the same query would time
// out again. Nothing is written when the client has gone away.
func writeQueryError(w http.ResponseWriter, r *http.Request, message string, err error) {
	switch {
	case errors.Is(err, storage.ErrInvalidCursor):
		api.WriteError(w, http.StatusBadRequest, "invalid cursor: pass the nextCursor of the previous page")
	case errors.Is(err, storage.ErrOffsetTooLarge):
		api.WriteError(w, http.StatusBadRequest, err.Error()+": page with cursor (the nextCursor of the previous page) instead")
	case errors.Is(r.Context().Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded):
		logger.Warn("Query timed out", "path", r.URL.Path, "error", err)
		api.WriteError(w, http.StatusGatewayTimeout, message+": query timed out")
	case errors.Is(r.Context().Err(), context.Canceled):
		logger.Debug("Query canceled by client", "path", r.URL.Path)
	default:
		api.WriteInternalError(w, message, err)
	}
}

// Helper functions
// parseTimeRange parses from and to (RFC 3339). to defaults to now and from
// to 24 hours before to. Invalid values and from after to are errors, rather
// than silently showing a different range.
func parseTimeRange(fromStr, toStr string) (from, to time.Time, err error) {
	to = time.Now()
	if toStr != "" {
		if to, err = time.Parse(time.RFC3339, toStr); err != nil {
			return from, to, fmt.Errorf("invalid to %q: expected an RFC 3339 time", toStr)
		}
	}
	from = to.Add(-24 * time.Hour)
	if fromStr != "" {
		if from, err = time.Parse(time.RFC3339, fromStr); err != nil {
			return from, to, fmt.Errorf("invalid from %q: expected an RFC 3339 time", fromStr)
		}
	}
	if from.After(to) {
		return from, to, fmt.Errorf("from must not be after to")
	}
	return from, to, nil
}

// requestTimeRange parses the from and to query parameters, writing a 400
// response and returning false when they are invalid
func requestTimeRange(w http.ResponseWriter, r *http.Request) (from, to time.Time, ok bool) {
	from, to, err := parseTimeRange(r.URL.Query().Get("from"), r.URL.Query().Get("to"))
	if err != nil {
		api.WriteError(w, http.StatusBadRequest, err.Error())
		return from, to, false
	}
	return from, to, true
}

// optionalTimeRange parses the from and to query parameters (RFC 3339) of a
// request that may leave either side open. It writes a 400 response and
// returns false when they are invalid.
func optionalTimeRange(w http.ResponseWriter, r *http.Request) (storage.TimeRange, bool) {
	var tr storage.TimeRange
	for _, p := range []struct {
		name string
		dst  *time.Time
	}{{"from", &tr.From}, {"to", &tr.To}} {
		value := r.URL.Query().Get(p.name)
		if value == "" {
			continue
		}
		t, err := time.Parse(time.RFC3339, value)
		if err != nil {
			api.WriteError(w, http.StatusBadRequest, fmt.Sprintf("invalid %s %q: expected an RFC 3339 time", p.name, value))
			return tr, false
		}
		*p.dst = t
	}
	if !tr.From.IsZero() && !tr.To.IsZero() && tr.From.After(tr.To) {
		api.WriteError(w, http.StatusBadRequest, "from must not be after to")
		return tr, false
	}
	return tr, true
}

// requestPage returns the page a list request asks for: limit, and cursor
// (from the previous page's nextCursor) or the deprecated offset
func requestPage(r *http.Request) storage.Page {
	limit, offset := parsePagination(r)
	return storage.Page{Limit: limit, Offset: offset, Cursor: r.URL.Query().Get("cursor")}
}

func parsePagination(r *http.Request) (limit, offset int) {
	limit = 50
	offset = 0

	if l := r.URL.Query().Get("limit"); l != "" {
		// Storage caps the page size
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 {
			limit = parsed
		}
	}

	if o := r.URL.Query().Get("offset"); o != "" {
		if parsed, err := strconv.Atoi(o); err == nil && parsed >= 0 {
			offset = parsed
		}
	}

	return limit, offset
}
