package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/tobilg/ai-observer/internal/api"
	"github.com/tobilg/ai-observer/internal/logger"
)

// readyTimeout bounds the database check of GET /ready
const readyTimeout = 2 * time.Second

// Health handles GET /health: liveness, always 200 while the server answers
// (it checks nothing, so it stays cheap for frequent probes)
func (h *Handlers) Health(w http.ResponseWriter, r *http.Request) {
	api.WriteJSON(w, http.StatusOK, api.HealthResponse{Status: api.ReadyOK})
}

// Ready handles GET /ready: readiness, 200 when the database answers a query
// within readyTimeout, else 503
func (h *Handlers) Ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), readyTimeout)
	defer cancel()
	if err := h.store.Ping(ctx); err != nil {
		logger.Warn("Readiness check failed", "error", err)
		api.WriteJSON(w, http.StatusServiceUnavailable, api.ReadyResponse{
			Status: api.ReadyUnavailable, Database: api.ReadyUnavailable, Error: "the database does not answer",
		})
		return
	}
	api.WriteJSON(w, http.StatusOK, api.ReadyResponse{Status: api.ReadyOK, Database: api.ReadyOK})
}
