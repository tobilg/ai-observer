package handlers

import (
	"net/http"

	"github.com/tobilg/ai-observer/internal/api"
	"github.com/tobilg/ai-observer/internal/selfmetrics"
)

// InternalMetrics handles GET /api/internal/metrics: AI Observer's own
// metrics (internal/selfmetrics) as JSON
func (h *Handlers) InternalMetrics(w http.ResponseWriter, r *http.Request) {
	api.WriteJSON(w, http.StatusOK, selfmetrics.Default.Snapshot())
}

// PrometheusMetrics handles GET /metrics: the same metrics in the
// Prometheus text exposition format
func (h *Handlers) PrometheusMetrics(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	selfmetrics.Default.WritePrometheus(w)
}
