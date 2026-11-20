package transcript

import (
	"container/list"
	"context"
	"sync"
	"time"
	"unsafe"

	"github.com/tobilg/ai-observer/internal/api"
)

// Cache defaults. The transcript of a session of 7,665 messages is about
// 17 MB (approxSize; 13 MB of heap): the byte bound keeps about seven of
// them, the count bound eight smaller ones.
const (
	DefaultCacheEntries = 8
	DefaultCacheBytes   = 128 << 20
	DefaultCacheTTL     = time.Minute
)

// GenerationStore is a Store that tells when its telemetry changed
// (storage.DuckDBStore.WriteGeneration)
type GenerationStore interface {
	Store
	WriteGeneration() uint64
}

// Cache keeps the transcripts that were loaded last. A transcript is
// assembled whole from the session's records for every page of it (0.6 to
// 0.9 s for a session of 7,665 messages), so paging through a session, in the
// dashboard or with the MCP tool get_transcript, would otherwise repeat that
// work per page. The REST API and the MCP server share one Cache.
//
// Invariants:
//   - A cached transcript is never older than the stored data: an entry is
//     reused only while no telemetry was written since it was built
//     (WriteGeneration), and a load in progress is joined only by callers
//     that saw the same generation.
//   - Large transcripts are not held after their readers left: an entry is
//     released TTL after it was last used, and the least recently used
//     entries are evicted beyond maxEntries or maxBytes (approximate). The
//     entry used last is always kept, however large.
//   - Concurrent loads of a session share one build. The build ends when it
//     is done or when all its callers have gone (their contexts ended), so a
//     cancelled caller does not fail it for the others.
//
// Callers must not change the transcripts it returns.
type Cache struct {
	store      GenerationStore
	maxEntries int
	maxBytes   int64
	ttl        time.Duration
	// load builds a transcript (Load); replaced in tests
	load func(ctx context.Context, sessionID string) (*api.TranscriptResponse, error)
	now  func() time.Time

	mu      sync.Mutex
	entries map[string]*cacheEntry // by session ID
	lru     *list.List             // of *cacheEntry, most recently used first
	bytes   int64
	builds  map[buildKey]*build
}

type cacheEntry struct {
	sessionID  string
	generation uint64
	resp       *api.TranscriptResponse
	size       int64
	lastUsed   time.Time
	elem       *list.Element
	timer      *time.Timer
}

type buildKey struct {
	sessionID  string
	generation uint64
}

// build is a load in progress that callers wait for
type build struct {
	key     buildKey
	done    chan struct{}
	resp    *api.TranscriptResponse
	err     error
	waiters int
	cancel  context.CancelFunc
}

// NewCache returns a cache of the transcripts of store's sessions with the
// default bounds
func NewCache(store GenerationStore) *Cache {
	return NewCacheWithLimits(store, DefaultCacheEntries, DefaultCacheBytes, DefaultCacheTTL)
}

// NewCacheWithLimits returns a cache holding at most maxEntries transcripts
// of about maxBytes in all, each for ttl after its last use
func NewCacheWithLimits(store GenerationStore, maxEntries int, maxBytes int64, ttl time.Duration) *Cache {
	return &Cache{
		store:      store,
		maxEntries: max(maxEntries, 1),
		maxBytes:   maxBytes,
		ttl:        ttl,
		load: func(ctx context.Context, sessionID string) (*api.TranscriptResponse, error) {
			return Load(ctx, store, sessionID)
		},
		now:     time.Now,
		entries: make(map[string]*cacheEntry),
		lru:     list.New(),
		builds:  make(map[buildKey]*build),
	}
}

// Load returns the whole transcript of a session (see the package function
// Load), from the cache when it is current
func (c *Cache) Load(ctx context.Context, sessionID string) (*api.TranscriptResponse, error) {
	// Read before loading: a write during the load makes the entry stale,
	// which only costs the next request a reload
	generation := c.store.WriteGeneration()
	key := buildKey{sessionID, generation}

	c.mu.Lock()
	if e := c.entries[sessionID]; e != nil && e.generation == generation {
		e.lastUsed = c.now()
		c.lru.MoveToFront(e.elem)
		c.mu.Unlock()
		return e.resp, nil
	}
	b := c.builds[key]
	if b == nil {
		// Not tied to the first caller's context: it ends when all callers
		// have gone (leave)
		buildCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
		b = &build{key: key, done: make(chan struct{}), cancel: cancel}
		c.builds[key] = b
		go c.run(buildCtx, key, b)
	}
	b.waiters++
	c.mu.Unlock()

	select {
	case <-b.done:
		c.leave(b)
		return b.resp, b.err
	case <-ctx.Done():
		c.leave(b)
		return nil, ctx.Err()
	}
}

// run builds a transcript and caches it
func (c *Cache) run(ctx context.Context, key buildKey, b *build) {
	resp, err := c.load(ctx, key.sessionID)
	b.cancel()

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.builds[key] == b {
		delete(c.builds, key)
	}
	b.resp, b.err = resp, err
	close(b.done)
	if err != nil {
		return
	}
	// An entry built for a newer generation meanwhile is kept
	if old := c.entries[key.sessionID]; old != nil {
		if old.generation > key.generation {
			return
		}
		c.removeLocked(old)
	}
	e := &cacheEntry{sessionID: key.sessionID, generation: key.generation, resp: resp, size: approxSize(resp), lastUsed: c.now()}
	e.elem = c.lru.PushFront(e)
	c.entries[key.sessionID] = e
	c.bytes += e.size
	for c.lru.Len() > 1 && (c.lru.Len() > c.maxEntries || c.bytes > c.maxBytes) {
		c.removeLocked(c.lru.Back().Value.(*cacheEntry))
	}
	c.expireLater(e, c.ttl)
}

// leave ends a caller's wait for b. A build nobody waits for is cancelled,
// and later callers start a new one.
func (c *Cache) leave(b *build) {
	c.mu.Lock()
	defer c.mu.Unlock()
	b.waiters--
	if b.waiters == 0 {
		b.cancel()
		if c.builds[b.key] == b {
			delete(c.builds, b.key)
		}
	}
}

// expireLater removes e once it has not been used for the TTL
func (c *Cache) expireLater(e *cacheEntry, after time.Duration) {
	e.timer = time.AfterFunc(after, func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		if c.entries[e.sessionID] != e {
			return // evicted or replaced
		}
		if rest := c.ttl - c.now().Sub(e.lastUsed); rest > 0 {
			c.expireLater(e, rest)
			return
		}
		c.removeLocked(e)
	})
}

func (c *Cache) removeLocked(e *cacheEntry) {
	if c.entries[e.sessionID] == e {
		delete(c.entries, e.sessionID)
	}
	e.timer.Stop()
	c.lru.Remove(e.elem)
	c.bytes -= e.size
}

// Len returns the number of cached transcripts and their approximate size
func (c *Cache) Len() (entries int, bytes int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lru.Len(), c.bytes
}

// messageOverhead approximates the memory of a message besides its strings
var messageOverhead = int64(unsafe.Sizeof(api.TranscriptMessage{}))

// approxSize approximates the memory a transcript holds: its strings and
// message structs
func approxSize(resp *api.TranscriptResponse) int64 {
	size := int64(len(resp.SessionID) + len(resp.ServiceName) + len(resp.Title))
	for i := range resp.Messages {
		m := &resp.Messages[i]
		size += messageOverhead + int64(len(m.Role)+len(m.Content)+len(m.Model)+len(m.ToolName)+len(m.ToolInput)+len(m.ToolOutput))
	}
	return size
}
