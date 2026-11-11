package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/tobilg/ai-observer/internal/api"
	"github.com/tobilg/ai-observer/internal/logger"
	"github.com/tobilg/ai-observer/internal/storage"
	"github.com/tobilg/ai-observer/internal/websocket"
)

// Handlers serves the OTLP/HTTP endpoints and the API
type Handlers struct {
	store Store
	hub   *websocket.Hub
	stats statsCache
	// dedup recognizes retried log and metric exports (spans are
	// deduplicated by their IDs when stored)
	dedup *batchDedup
	// transcripts keeps the transcript requested last (loadTranscript)
	transcripts transcriptCache
	// serverInfo describes the running server for GET /api/status (nil
	// until SetServerInfo)
	serverInfo func() ServerInfo
}

// New returns the handlers over store (a *storage.DuckDBStore)
func New(store Store, hub *websocket.Hub) *Handlers {
	return &Handlers{
		store: store,
		hub:   hub,
		dedup: newBatchDedup(),
	}
}

// decodeBody decodes a JSON request body into v. It answers 413 for a body
// over the API limit and 400 for an invalid one, and then returns false.
func decodeBody(w http.ResponseWriter, r *http.Request, v any) bool {
	err := json.NewDecoder(r.Body).Decode(v)
	if err == nil {
		return true
	}
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		api.WriteError(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("request body exceeds the %d byte limit", tooLarge.Limit))
	} else {
		api.WriteError(w, http.StatusBadRequest, "invalid request body")
	}
	return false
}

// HandleWebSocket handles GET /ws
func (h *Handlers) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	websocket.ServeWs(h.hub, w, r)
}

// writeQueryError reports a failed query. A query stopped by the request
// timeout is a 504, which clients must not retry: the same query would time
// out again. Nothing is written when the client has gone away.
func writeQueryError(w http.ResponseWriter, r *http.Request, message string, err error) {
	switch {
	case errors.Is(err, storage.ErrInvalidCursor):
		api.WriteError(w, http.StatusBadRequest, "invalid cursor: pass the nextCursor of the previous page")
	case errors.Is(err, storage.ErrOffsetTooLarge):
		api.WriteError(w, http.StatusBadRequest, err.Error()+": page with cursor (the nextCursor of the previous page) instead")
	case errors.Is(r.Context().Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded):
		logger.Warn("Query timed out", "path", r.URL.Path, "error", err)
		api.WriteError(w, http.StatusGatewayTimeout, message+": query timed out")
	case errors.Is(r.Context().Err(), context.Canceled):
		logger.Debug("Query canceled by client", "path", r.URL.Path)
	default:
		api.WriteInternalError(w, message, err)
	}
}
