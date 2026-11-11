package handlers

import (
	"context"
	"sync"

	"github.com/tobilg/ai-observer/internal/api"
	"github.com/tobilg/ai-observer/internal/transcript"
)

// transcriptCache holds the handlers' transcript.Cache: the server's, shared
// with the MCP endpoint (SetTranscriptCache), or one of their own. See
// transcript.Cache for its invariants: a cached transcript is never older
// than the stored data, is released a minute after its last use, and
// concurrent loads of a session share one build.
type transcriptCache struct {
	once  sync.Once
	cache *transcript.Cache
}

// SetTranscriptCache makes the handlers use cache, which the server shares
// with the MCP endpoint. It must be called before the handlers serve
// requests; later calls have no effect.
func (h *Handlers) SetTranscriptCache(cache *transcript.Cache) {
	h.transcripts.once.Do(func() { h.transcripts.cache = cache })
}

// loadTranscript returns the whole transcript of a session, from the cache
// when it is current. Callers must not change the result.
func (h *Handlers) loadTranscript(ctx context.Context, sessionID string) (*api.TranscriptResponse, error) {
	h.transcripts.once.Do(func() { h.transcripts.cache = transcript.NewCache(h.store) })
	return h.transcripts.cache.Load(ctx, sessionID)
}
