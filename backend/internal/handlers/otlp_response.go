package handlers

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/tobilg/ai-observer/internal/api"
	"github.com/tobilg/ai-observer/internal/logger"
	"github.com/tobilg/ai-observer/internal/otlp"
	"google.golang.org/protobuf/proto"
)

// writeExportResponse writes the OTLP success response in the encoding of the
// request, as the OTLP/HTTP spec requires: a binary Export*ServiceResponse for
// protobuf requests and its JSON form ("{}" for an empty response) otherwise.
func writeExportResponse(w http.ResponseWriter, decoder otlp.Decoder, resp proto.Message) {
	if _, ok := decoder.(*otlp.ProtoDecoder); ok {
		body, err := proto.Marshal(resp)
		if err != nil {
			api.WriteInternalError(w, "failed to encode response", err)
			return
		}
		w.Header().Set("Content-Type", "application/x-protobuf")
		w.WriteHeader(http.StatusOK)
		w.Write(body)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("{}"))
}

// writeDecodeError rejects a request whose body could not be read or decoded.
// A body over the payload limit is a 413, which exporters do not retry.
func writeDecodeError(w http.ResponseWriter, signal string, err error) {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		api.WriteError(w, http.StatusRequestEntityTooLarge,
			fmt.Sprintf("payload exceeds the %d byte limit", tooLarge.Limit))
		return
	}
	api.WriteError(w, http.StatusBadRequest, "failed to decode "+signal+": "+err.Error())
}

// claimBatch reports whether the request's batch must be stored. For a retry
// of a batch stored before it writes the success response, since the batch is
// stored, and returns false. When it returns true, the caller must release
// the claim with the outcome of storing the batch.
func (h *Handlers) claimBatch(w http.ResponseWriter, r *http.Request, decoder otlp.Decoder, key batchKey, signal string, resp proto.Message) bool {
	claimed, err := h.dedup.claim(r.Context(), key)
	if err != nil {
		// The client went away while the same batch was being stored
		writeStoreError(w, signal, err)
		return false
	}
	if !claimed {
		logger.Debug("Skipping a batch that was stored before (exporter retry)", "signal", signal)
		writeExportResponse(w, decoder, resp)
		return false
	}
	return true
}

// writeStoreError reports that received telemetry could not be stored. OTLP
// exporters retry 503 (unlike 500, which makes them drop the batch), and
// nothing of the request was stored, so the retry duplicates nothing.
func writeStoreError(w http.ResponseWriter, signal string, err error) {
	logger.Error("Failed to store "+signal, "error", err)
	w.Header().Set("Retry-After", "5")
	api.WriteError(w, http.StatusServiceUnavailable, "failed to store "+signal)
}
