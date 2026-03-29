package server

import (
	"github.com/go-chi/chi/v5"
	"github.com/tobilg/ai-observer/internal/frontend"
	"github.com/tobilg/ai-observer/internal/handlers"
	"github.com/tobilg/ai-observer/internal/logger"
)

func (s *Server) setupRoutes(h *handlers.Handlers) error {
	// OTLP ingestion endpoints (port 4318)
	s.otlpRouter.Route("/v1", func(r chi.Router) {
		r.Post("/traces", h.HandleTraces)
		r.Post("/metrics", h.HandleMetrics)
		r.Post("/logs", h.HandleLogs)
	})
	s.otlpRouter.Get("/health", h.Health)

	// Query API for frontend (port 8080)
	s.apiRouter.Route("/api", func(r chi.Router) {
		// Traces
		r.Get("/traces", h.QueryTraces)
		r.Get("/traces/recent", h.QueryRecentTraces)
		r.Get("/traces/{traceId}", h.GetTrace)
		r.Get("/traces/{traceId}/spans", h.GetTraceSpans)

		// Metrics
		r.Get("/metrics", h.QueryMetrics)
		r.Get("/metrics/names", h.ListMetricNames)
		r.Get("/metrics/breakdown-values", h.GetBreakdownValues)
		r.Get("/metrics/series", h.QueryMetricSeries)
		r.Post("/metrics/batch-series", h.QueryBatchMetricSeries)

		// Logs
		r.Get("/logs", h.QueryLogs)
		r.Get("/logs/levels", h.GetLogLevels)

		// Sessions
		r.Get("/sessions", h.QuerySessions)
		r.Get("/sessions/{sessionId}/transcript", h.GetSessionTranscript)

		// Services
		r.Get("/services", h.ListServices)

		// Stats
		r.Get("/stats", h.GetStats)

		// Dashboards
		r.Get("/dashboards", h.ListDashboards)
		r.Post("/dashboards", h.CreateDashboard)
		r.Get("/dashboards/default", h.GetDefaultDashboard)
		r.Get("/dashboards/{id}", h.GetDashboard)
		r.Put("/dashboards/{id}", h.UpdateDashboard)
		r.Delete("/dashboards/{id}", h.DeleteDashboard)
		r.Put("/dashboards/{id}/default", h.SetDefaultDashboard)
		r.Post("/dashboards/{id}/widgets", h.CreateWidget)
		r.Put("/dashboards/{id}/widgets/positions", h.UpdateWidgetPositions)
		r.Put("/dashboards/{id}/widgets/{widgetId}", h.UpdateWidget)
		r.Delete("/dashboards/{id}/widgets/{widgetId}", h.DeleteWidget)
	})

	// WebSocket for real-time updates (port 8080)
	s.apiRouter.Get("/ws", h.HandleWebSocket)

	// Health check (port 8080)
	s.apiRouter.Get("/health", h.Health)

	// Serve embedded frontend (catch-all, must be last)
	spaHandler, err := frontend.NewSPAHandler()
	if err != nil {
		logger.Warn("Failed to create SPA handler, frontend may not be available", "error", err)
		// Continue without frontend - API will still work
	} else {
		s.apiRouter.Handle("/*", spaHandler)
	}

	return nil
}
