package handlers

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/tobilg/ai-observer/internal/api"
	"github.com/tobilg/ai-observer/internal/otlp"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// writeExportResponse writes the OTLP response in the encoding of the
// request, as the OTLP/HTTP spec requires: a binary Export*ServiceResponse for
// protobuf requests and its JSON form otherwise ("{}" for a full success, the
// partial_success otherwise).
func writeExportResponse(w http.ResponseWriter, decoder otlp.Decoder, resp proto.Message) {
	contentType := "application/json"
	marshal := protojson.Marshal
	if _, ok := decoder.(*otlp.ProtoDecoder); ok {
		contentType = "application/x-protobuf"
		marshal = proto.Marshal
	}
	body, err := marshal(resp)
	if err != nil {
		api.WriteInternalError(w, "failed to encode response", err)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(http.StatusOK)
	w.Write(body)
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

// writeStoreError reports that received telemetry could not be stored (a
// *StoreError, logged by the Ingest functions). OTLP exporters retry 503
// (unlike 500, which makes them drop the batch), and nothing of the request
// was stored, so the retry duplicates nothing.
func writeStoreError(w http.ResponseWriter, signal string) {
	w.Header().Set("Retry-After", "5")
	api.WriteError(w, http.StatusServiceUnavailable, "failed to store "+signal)
}
