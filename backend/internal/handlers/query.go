package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/tobilg/ai-observer/internal/api"
	"github.com/tobilg/ai-observer/internal/storage"
	"github.com/tobilg/ai-observer/internal/websocket"
)

// QueryTraces handles GET /api/traces
func (h *Handlers) QueryTraces(w http.ResponseWriter, r *http.Request) {
	service := r.URL.Query().Get("service")
	search := r.URL.Query().Get("search")
	from, to := parseTimeRange(r)
	limit, offset := parsePagination(r)

	resp, err := h.store.QueryTraces(r.Context(), service, search, from, to, limit, offset)
	if err != nil {
		api.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	api.WriteJSON(w, http.StatusOK, resp)
}

// GetTrace handles GET /api/traces/{id}
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

	spans, err := h.store.GetTraceSpans(r.Context(), id, kind)
	if err != nil {
		api.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if len(spans) == 0 {
		api.WriteError(w, http.StatusNotFound, "trace not found")
		return
	}

	api.WriteJSON(w, http.StatusOK, api.SpansResponse{Spans: spans})
}

// GetTraceSpans handles GET /api/traces/{id}/spans?kind={kind}
func (h *Handlers) GetTraceSpans(w http.ResponseWriter, r *http.Request) {
	h.GetTrace(w, r) // Same implementation
}

// QueryRecentTraces handles GET /api/traces/recent
func (h *Handlers) QueryRecentTraces(w http.ResponseWriter, r *http.Request) {
	limit, _ := parsePagination(r)
	if limit > 100 {
		limit = 100
	}
	from, to := parseTimeRange(r)

	resp, err := h.store.GetRecentTraces(r.Context(), limit, from, to)
	if err != nil {
		api.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	api.WriteJSON(w, http.StatusOK, resp)
}

// QueryMetrics handles GET /api/metrics
func (h *Handlers) QueryMetrics(w http.ResponseWriter, r *http.Request) {
	service := r.URL.Query().Get("service")
	metricName := r.URL.Query().Get("name")
	metricType := r.URL.Query().Get("type")
	from, to := parseTimeRange(r)
	limit, offset := parsePagination(r)

	resp, err := h.store.QueryMetrics(r.Context(), service, metricName, metricType, from, to, limit, offset)
	if err != nil {
		api.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	api.WriteJSON(w, http.StatusOK, resp)
}

// ListMetricNames handles GET /api/metrics/names
func (h *Handlers) ListMetricNames(w http.ResponseWriter, r *http.Request) {
	service := r.URL.Query().Get("service")

	names, err := h.store.GetMetricNames(r.Context(), service)
	if err != nil {
		api.WriteError(w, http.StatusInternalServerError, err.Error())
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
		api.WriteError(w, http.StatusInternalServerError, err.Error())
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
	from, to := parseTimeRange(r)

	resp, err := h.store.QueryMetricSeries(r.Context(), metricName, service, from, to, intervalSeconds, aggregate)
	if err != nil {
		api.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	api.WriteJSON(w, http.StatusOK, resp)
}

// QueryBatchMetricSeries handles POST /api/metrics/batch-series
func (h *Handlers) QueryBatchMetricSeries(w http.ResponseWriter, r *http.Request) {
	var req api.BatchMetricSeriesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		api.WriteError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
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
	from, to := parseTimeRangeFromStrings(req.From, req.To)

	// Default to 60 seconds if not specified
	intervalSeconds := req.Interval
	if intervalSeconds <= 0 {
		intervalSeconds = 60
	}

	resp := h.store.QueryBatchMetricSeries(r.Context(), req.Queries, from, to, intervalSeconds)
	api.WriteJSON(w, http.StatusOK, resp)
}

// parseTimeRangeFromStrings parses time range from string parameters
func parseTimeRangeFromStrings(fromStr, toStr string) (from, to time.Time) {
	// Default to last 24 hours
	to = time.Now()
	from = to.Add(-24 * time.Hour)

	if fromStr != "" {
		if parsed, err := time.Parse(time.RFC3339, fromStr); err == nil {
			from = parsed
		}
	}

	if toStr != "" {
		if parsed, err := time.Parse(time.RFC3339, toStr); err == nil {
			to = parsed
		}
	}

	return from, to
}

// QueryLogs handles GET /api/logs
func (h *Handlers) QueryLogs(w http.ResponseWriter, r *http.Request) {
	service := r.URL.Query().Get("service")
	severity := r.URL.Query().Get("severity")
	traceID := r.URL.Query().Get("traceId")
	search := r.URL.Query().Get("search")
	from, to := parseTimeRange(r)
	limit, offset := parsePagination(r)

	resp, err := h.store.QueryLogs(r.Context(), service, severity, traceID, search, from, to, limit, offset)
	if err != nil {
		api.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	api.WriteJSON(w, http.StatusOK, resp)
}

// GetLogLevels handles GET /api/logs/levels
func (h *Handlers) GetLogLevels(w http.ResponseWriter, r *http.Request) {
	levels, err := h.store.GetLogLevels(r.Context())
	if err != nil {
		api.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	api.WriteJSON(w, http.StatusOK, levels)
}

// QuerySessions handles GET /api/sessions
func (h *Handlers) QuerySessions(w http.ResponseWriter, r *http.Request) {
	service := r.URL.Query().Get("service")
	from, to := parseTimeRange(r)
	limit, offset := parsePagination(r)

	resp, err := h.store.QuerySessions(r.Context(), service, from, to, limit, offset)
	if err != nil {
		api.WriteError(w, http.StatusInternalServerError, err.Error())
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

	resp, err := h.store.GetSessionTranscript(r.Context(), sessionID)
	if err != nil {
		api.WriteError(w, http.StatusNotFound, err.Error())
		return
	}

	api.WriteJSON(w, http.StatusOK, resp)
}

// ListServices handles GET /api/services
func (h *Handlers) ListServices(w http.ResponseWriter, r *http.Request) {
	services, err := h.store.GetServices(r.Context())
	if err != nil {
		api.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	api.WriteJSON(w, http.StatusOK, api.ServicesResponse{Services: services})
}

// GetStats handles GET /api/stats
func (h *Handlers) GetStats(w http.ResponseWriter, r *http.Request) {
	stats, err := h.store.GetStats(r.Context())
	if err != nil {
		api.WriteError(w, http.StatusInternalServerError, err.Error())
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

// Helper functions
func parseTimeRange(r *http.Request) (from, to time.Time) {
	fromStr := r.URL.Query().Get("from")
	toStr := r.URL.Query().Get("to")

	// Default to last 24 hours
	to = time.Now()
	from = to.Add(-24 * time.Hour)

	if fromStr != "" {
		if parsed, err := time.Parse(time.RFC3339, fromStr); err == nil {
			from = parsed
		}
	}

	if toStr != "" {
		if parsed, err := time.Parse(time.RFC3339, toStr); err == nil {
			to = parsed
		}
	}

	return from, to
}

func parsePagination(r *http.Request) (limit, offset int) {
	limit = 50
	offset = 0

	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 {
			limit = parsed
			if limit > 1000 {
				limit = 1000
			}
		}
	}

	if o := r.URL.Query().Get("offset"); o != "" {
		if parsed, err := strconv.Atoi(o); err == nil && parsed >= 0 {
			offset = parsed
		}
	}

	return limit, offset
}
