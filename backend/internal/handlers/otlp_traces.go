package handlers

import (
	"net/http"

	"github.com/tobilg/ai-observer/internal/logger"
	"github.com/tobilg/ai-observer/internal/otlp"
	"github.com/tobilg/ai-observer/internal/storage"
	"github.com/tobilg/ai-observer/internal/websocket"
	coltracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
)

type Handlers struct {
	store *storage.DuckDBStore
	hub   *websocket.Hub
	stats statsCache
	// dedup recognizes retried log and metric exports (spans are
	// deduplicated by their IDs when stored)
	dedup *batchDedup
}

func New(store *storage.DuckDBStore, hub *websocket.Hub) *Handlers {
	return &Handlers{
		store: store,
		hub:   hub,
		dedup: newBatchDedup(),
	}
}

// HandleTraces handles POST /v1/traces
func (h *Handlers) HandleTraces(w http.ResponseWriter, r *http.Request) {
	log := logger.Logger()
	contentType := r.Header.Get("Content-Type")

	// Use format detection to handle Content-Type mismatches
	decoder, body, err := otlp.GetDecoderWithDetection(r.Body, contentType)
	if err != nil {
		writeDecodeError(w, "traces", err)
		return
	}

	req, err := decoder.DecodeTraces(body)
	if err != nil {
		log.Error("Failed to decode traces", "error", err)
		writeDecodeError(w, "traces", err)
		return
	}

	// Store new spans and the metrics derived from them (Copilot token usage
	// and cost) together; spans sent again get no second set of metrics.
	// Codex CLI spans are handled at query time.
	spans, derivedMetrics, err := h.store.InsertTraces(r.Context(), otlp.ConvertTraces(req), otlp.DeriveCopilotMetricsFromSpans)
	if err != nil {
		writeStoreError(w, "traces", err)
		return
	}

	// Broadcast to WebSocket clients
	if h.hub != nil && len(spans) > 0 {
		h.hub.Broadcast(websocket.SpansStored(spans))
	}
	if h.hub != nil && len(derivedMetrics) > 0 {
		h.hub.Broadcast(websocket.MetricsStored(derivedMetrics))
	}

	log.Debug("Received spans", "count", len(spans), "derived_metrics", len(derivedMetrics))

	// OTLP success response
	writeExportResponse(w, decoder, &coltracepb.ExportTraceServiceResponse{})
}
