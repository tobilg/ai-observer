package handlers

import (
	"net/http"

	"github.com/tobilg/ai-observer/internal/api"
	"github.com/tobilg/ai-observer/internal/logger"
	"github.com/tobilg/ai-observer/internal/otlp"
	"github.com/tobilg/ai-observer/internal/storage"
	"github.com/tobilg/ai-observer/internal/websocket"
)

type Handlers struct {
	store *storage.DuckDBStore
	hub   *websocket.Hub
}

func New(store *storage.DuckDBStore, hub *websocket.Hub) *Handlers {
	return &Handlers{
		store: store,
		hub:   hub,
	}
}

// HandleTraces handles POST /v1/traces
func (h *Handlers) HandleTraces(w http.ResponseWriter, r *http.Request) {
	log := logger.Logger()
	contentType := r.Header.Get("Content-Type")

	// Use format detection to handle Content-Type mismatches
	decoder, body, _, err := otlp.GetDecoderWithDetection(r.Body, contentType)
	if err != nil {
		api.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	req, err := decoder.DecodeTraces(body)
	if err != nil {
		log.Error("Failed to decode traces", "error", err)
		api.WriteError(w, http.StatusBadRequest, "failed to decode traces: "+err.Error())
		return
	}

	spans := otlp.ConvertTraces(req)
	derivedMetrics := otlp.DeriveCopilotMetricsFromSpans(spans)

	// Store spans as-is - Codex CLI spans are handled at query time
	if err := h.store.InsertSpans(r.Context(), spans); err != nil {
		log.Error("Failed to store traces", "error", err)
		api.WriteError(w, http.StatusInternalServerError, "failed to store traces")
		return
	}

	if len(derivedMetrics) > 0 {
		if err := h.store.InsertMetrics(r.Context(), derivedMetrics); err != nil {
			log.Warn("Failed to store derived metrics from traces", "error", err)
		}
	}

	// Broadcast to WebSocket clients
	if h.hub != nil && len(spans) > 0 {
		h.hub.Broadcast(websocket.NewTracesMessage(spans))
	}
	if h.hub != nil && len(derivedMetrics) > 0 {
		h.hub.Broadcast(websocket.NewMetricsMessage(derivedMetrics))
	}

	log.Debug("Received spans", "count", len(spans), "derived_metrics", len(derivedMetrics))

	// OTLP success response
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("{}"))
}
