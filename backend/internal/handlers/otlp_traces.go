package handlers

import (
	"net/http"

	"github.com/tobilg/ai-observer/internal/logger"
	"github.com/tobilg/ai-observer/internal/otlp"
)

// HandleTraces handles POST /v1/traces
func (h *Handlers) HandleTraces(w http.ResponseWriter, r *http.Request) {
	// Use format detection to handle Content-Type mismatches
	decoder, body, err := otlp.GetDecoderWithDetection(r.Body, r.Header.Get("Content-Type"))
	if err != nil {
		writeDecodeError(w, "traces", err)
		return
	}
	req, err := decoder.DecodeTraces(body)
	if err != nil {
		logger.Error("Failed to decode traces", "error", err)
		writeDecodeError(w, "traces", err)
		return
	}

	resp, err := h.IngestTraces(r.Context(), req)
	if err != nil {
		writeStoreError(w, "traces")
		return
	}
	writeExportResponse(w, decoder, resp)
}
