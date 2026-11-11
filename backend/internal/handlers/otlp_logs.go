package handlers

import (
	"io"
	"net/http"

	"github.com/tobilg/ai-observer/internal/logger"
	"github.com/tobilg/ai-observer/internal/otlp"
)

// HandleLogs handles POST /v1/logs
func (h *Handlers) HandleLogs(w http.ResponseWriter, r *http.Request) {
	// Use format detection to handle Content-Type mismatches. The body is
	// hashed while it is read, to recognize retries.
	hasher := newBatchHasher("logs")
	decoder, body, err := otlp.GetDecoderWithDetection(io.TeeReader(r.Body, hasher), r.Header.Get("Content-Type"))
	if err != nil {
		logger.Error("Failed to detect logs format", "error", err)
		writeDecodeError(w, "logs", err)
		return
	}
	req, err := decoder.DecodeLogs(body)
	if err != nil {
		logger.Error("Failed to decode logs", "error", err)
		writeDecodeError(w, "logs", err)
		return
	}

	resp, err := h.IngestLogs(r.Context(), req, hasher.key())
	if err != nil {
		writeStoreError(w, "logs")
		return
	}
	writeExportResponse(w, decoder, resp)
}
