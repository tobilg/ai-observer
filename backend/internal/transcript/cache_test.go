package transcript

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tobilg/ai-observer/internal/api"
	"github.com/tobilg/ai-observer/internal/storage"
)

// generationStore is a GenerationStore whose generation tests set; the cache
// tests replace the loader, so its reads are not used
type generationStore struct {
	generation atomic.Uint64
}

func (s *generationStore) SessionLogs(context.Context, string) ([]storage.SessionLog, error) {
	return nil, nil
}
func (s *generationStore) SessionSpans(context.Context, string) ([]api.Span, error) { return nil, nil }
func (s *generationStore) WriteGeneration() uint64                                  { return s.generation.Load() }

// testCache returns a cache whose loader counts builds and returns a
// transcript of content (by session ID) of size bytes
func testCache(entries int, bytes int64, ttl time.Duration, size map[string]int) (*Cache, *generationStore, *atomic.Int32) {
	store := &generationStore{}
	c := NewCacheWithLimits(store, entries, bytes, ttl)
	var builds atomic.Int32
	c.load = func(ctx context.Context, sessionID string) (*api.TranscriptResponse, error) {
		builds.Add(1)
		return &api.TranscriptResponse{SessionID: sessionID, Messages: []api.TranscriptMessage{
			{Content: strings.Repeat("x", size[sessionID])},
		}}, nil
	}
	return c, store, &builds
}

func TestCacheReusesCurrentTranscripts(t *testing.T) {
	c, store, builds := testCache(4, 1<<20, time.Minute, nil)
	ctx := context.Background()
	first, err := c.Load(ctx, "a")
	if err != nil {
		t.Fatal(err)
	}
	if again, _ := c.Load(ctx, "a"); again != first || builds.Load() != 1 {
		t.Fatalf("second load built again (%d builds)", builds.Load())
	}
	// Two sessions do not evict each other
	if _, err := c.Load(ctx, "b"); err != nil {
		t.Fatal(err)
	}
	if again, _ := c.Load(ctx, "a"); again != first || builds.Load() != 2 {
		t.Fatalf("a was evicted by b (%d builds)", builds.Load())
	}
	// Written telemetry makes every entry stale
	store.generation.Add(1)
	if fresh, _ := c.Load(ctx, "a"); fresh == first || builds.Load() != 3 {
		t.Fatalf("stale transcript reused (%d builds)", builds.Load())
	}
}

func TestCacheEvictsLeastRecentlyUsed(t *testing.T) {
	ctx := context.Background()
	t.Run("by count", func(t *testing.T) {
		c, _, builds := testCache(2, 1<<20, time.Minute, nil)
		for _, id := range []string{"a", "b", "a", "c"} {
			if _, err := c.Load(ctx, id); err != nil {
				t.Fatal(err)
			}
		}
		// a was used after b, so b was evicted for c
		if n, _ := c.Len(); n != 2 {
			t.Fatalf("entries = %d, want 2", n)
		}
		before := builds.Load()
		c.Load(ctx, "a")
		if builds.Load() != before {
			t.Error("a was evicted")
		}
		c.Load(ctx, "b")
		if builds.Load() != before+1 {
			t.Error("b was not evicted")
		}
	})
	t.Run("by size", func(t *testing.T) {
		size := map[string]int{"small": 1000, "large": 10_000, "huge": 100_000}
		c, _, _ := testCache(10, 20_000, time.Minute, size)
		c.Load(ctx, "small")
		c.Load(ctx, "large")
		if n, _ := c.Len(); n != 2 {
			t.Fatalf("entries = %d, want 2", n)
		}
		// Larger than the bound: kept alone, as the last used
		c.Load(ctx, "huge")
		if n, bytes := c.Len(); n != 1 || bytes < 100_000 {
			t.Fatalf("entries = %d (%d bytes), want only the huge one", n, bytes)
		}
	})
}

func TestCacheExpiresUnusedEntries(t *testing.T) {
	c, _, _ := testCache(4, 1<<20, 30*time.Millisecond, nil)
	if _, err := c.Load(context.Background(), "a"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		if n, bytes := c.Len(); n == 0 && bytes == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("entry was not released after its TTL")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestCacheSharesConcurrentBuilds(t *testing.T) {
	c, _, builds := testCache(4, 1<<20, time.Minute, nil)
	release := make(chan struct{})
	started := make(chan struct{}, 10)
	inner := c.load
	c.load = func(ctx context.Context, sessionID string) (*api.TranscriptResponse, error) {
		started <- struct{}{}
		select {
		case <-release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		return inner(ctx, sessionID)
	}

	// The first caller gives up; the others still get the transcript
	cancelled, cancel := context.WithCancel(context.Background())
	firstErr := make(chan error, 1)
	go func() {
		_, err := c.Load(cancelled, "a")
		firstErr <- err
	}()
	<-started

	var wg sync.WaitGroup
	results := make([]*api.TranscriptResponse, 5)
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, err := c.Load(context.Background(), "a")
			if err != nil {
				t.Error(err)
			}
			results[i] = resp
		}()
	}
	// Wait until all have joined the build
	deadline := time.Now().Add(2 * time.Second)
	for {
		c.mu.Lock()
		waiting := 0
		for _, b := range c.builds {
			waiting = b.waiters
		}
		c.mu.Unlock()
		if waiting == 6 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("waiters = %d, want 6", waiting)
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	if err := <-firstErr; !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled caller: err = %v", err)
	}
	close(release)
	wg.Wait()
	if builds.Load() != 1 {
		t.Errorf("builds = %d, want 1", builds.Load())
	}
	for _, r := range results {
		if r == nil || r != results[0] {
			t.Fatal("callers did not share one transcript")
		}
	}
}

func TestCacheCancelsBuildWithoutCallers(t *testing.T) {
	c, _, _ := testCache(4, 1<<20, time.Minute, nil)
	buildDone := make(chan error, 1)
	c.load = func(ctx context.Context, sessionID string) (*api.TranscriptResponse, error) {
		<-ctx.Done()
		buildDone <- ctx.Err()
		return nil, ctx.Err()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := c.Load(ctx, "a"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v", err)
	}
	select {
	case err := <-buildDone:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("build ended with %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("build kept running after its only caller left")
	}
	if n, _ := c.Len(); n != 0 {
		t.Errorf("a failed build was cached")
	}
}
