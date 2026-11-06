package handlers

import (
	"io"
	"net/http"

	"github.com/tobilg/ai-observer/internal/logger"
	"github.com/tobilg/ai-observer/internal/otlp"
	"github.com/tobilg/ai-observer/internal/websocket"
	collogspb "go.opentelemetry.io/proto/otlp/collector/logs/v1"
)

// HandleLogs handles POST /v1/logs
func (h *Handlers) HandleLogs(w http.ResponseWriter, r *http.Request) {
	log := logger.Logger()
	contentType := r.Header.Get("Content-Type")

	// Use format detection to handle Content-Type mismatches. The body is
	// hashed while it is read, to recognize retries.
	hasher := newBatchHasher("logs")
	decoder, body, err := otlp.GetDecoderWithDetection(io.TeeReader(r.Body, hasher), contentType)
	if err != nil {
		log.Error("Failed to detect logs format", "error", err)
		writeDecodeError(w, "logs", err)
		return
	}

	req, err := decoder.DecodeLogs(body)
	if err != nil {
		log.Error("Failed to decode logs", "error", err)
		writeDecodeError(w, "logs", err)
		return
	}

	result := otlp.ConvertLogs(req)

	key := hasher.key()
	if !h.claimBatch(w, r, decoder, key, "logs", &collogspb.ExportLogsServiceResponse{}) {
		return
	}

	// Store logs and the metrics derived from them (Codex token usage and
	// cost) together, so a failure leaves nothing for the exporter's retry to
	// duplicate
	err = h.store.InsertTelemetry(r.Context(), result.Logs, result.DerivedMetrics, nil)
	h.dedup.release(key, err == nil)
	if err != nil {
		writeStoreError(w, "logs", err)
		return
	}

	if h.hub != nil && len(result.DerivedMetrics) > 0 {
		h.hub.Broadcast(websocket.MetricsStored(result.DerivedMetrics))
	}
	if h.hub != nil && len(result.Logs) > 0 {
		h.hub.Broadcast(websocket.LogsStored(result.Logs))
	}

	log.Debug("Received log records", "count", len(result.Logs), "derived_metrics", len(result.DerivedMetrics))

	// OTLP success response
	writeExportResponse(w, decoder, &collogspb.ExportLogsServiceResponse{})
}
