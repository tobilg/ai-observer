package handlers

import (
	"io"
	"net/http"

	"github.com/tobilg/ai-observer/internal/logger"
	"github.com/tobilg/ai-observer/internal/otlp"
	"github.com/tobilg/ai-observer/internal/websocket"
	colmetricspb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
)

// HandleMetrics handles POST /v1/metrics
func (h *Handlers) HandleMetrics(w http.ResponseWriter, r *http.Request) {
	log := logger.Logger()
	contentType := r.Header.Get("Content-Type")

	// Use format detection to handle Content-Type mismatches. The body is
	// hashed while it is read, to recognize retries.
	hasher := newBatchHasher("metrics")
	decoder, body, err := otlp.GetDecoderWithDetection(io.TeeReader(r.Body, hasher), contentType)
	if err != nil {
		writeDecodeError(w, "metrics", err)
		return
	}

	req, err := decoder.DecodeMetrics(body)
	if err != nil {
		log.Error("Failed to decode metrics", "error", err)
		writeDecodeError(w, "metrics", err)
		return
	}

	// Cumulative counters are stored as they are: metric series compute
	// their per-interval increases
	result := otlp.ConvertMetrics(req)
	allMetrics := append(result.Metrics, result.DerivedMetrics...)

	key := hasher.key()
	if !h.claimBatch(w, r, decoder, key, "metrics", &colmetricspb.ExportMetricsServiceResponse{}) {
		return
	}
	err = h.store.InsertMetrics(r.Context(), allMetrics)
	h.dedup.release(key, err == nil)
	if err != nil {
		writeStoreError(w, "metrics", err)
		return
	}

	// Broadcast to WebSocket clients
	if h.hub != nil && len(allMetrics) > 0 {
		h.hub.Broadcast(websocket.MetricsStored(allMetrics))
	}

	log.Debug("Received metrics",
		"received", len(result.Metrics),
		"derived", len(result.DerivedMetrics))

	// OTLP success response
	writeExportResponse(w, decoder, &colmetricspb.ExportMetricsServiceResponse{})
}
