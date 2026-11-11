package handlers

import (
	"io"
	"net/http"

	"github.com/tobilg/ai-observer/internal/logger"
	"github.com/tobilg/ai-observer/internal/otlp"
)

// HandleMetrics handles POST /v1/metrics
func (h *Handlers) HandleMetrics(w http.ResponseWriter, r *http.Request) {
	// Use format detection to handle Content-Type mismatches. The body is
	// hashed while it is read, to recognize retries.
	hasher := newBatchHasher("metrics")
	decoder, body, err := otlp.GetDecoderWithDetection(io.TeeReader(r.Body, hasher), r.Header.Get("Content-Type"))
	if err != nil {
		writeDecodeError(w, "metrics", err)
		return
	}
	req, err := decoder.DecodeMetrics(body)
	if err != nil {
		logger.Error("Failed to decode metrics", "error", err)
		writeDecodeError(w, "metrics", err)
		return
	}

	resp, err := h.IngestMetrics(r.Context(), req, hasher.key())
	if err != nil {
		writeStoreError(w, "metrics")
		return
	}
	writeExportResponse(w, decoder, resp)
}
