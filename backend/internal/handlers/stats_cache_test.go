package handlers

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tobilg/ai-observer/internal/api"
)

func TestStatsCacheSharesOneLoad(t *testing.T) {
	var c statsCache
	var loads atomic.Int32
	release := make(chan struct{})
	load := func(ctx context.Context) (*api.StatsResponse, error) {
		loads.Add(1)
		select {
		case <-release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		return &api.StatsResponse{LogCount: 7}, nil
	}

	// A caller that gives up does not end the load for the others
	cancelled, cancel := context.WithCancel(context.Background())
	firstErr := make(chan error, 1)
	go func() {
		_, err := c.get(cancelled, load)
		firstErr <- err
	}()
	waitFor(t, func() bool { return loads.Load() == 1 })
	cancel()
	if err := <-firstErr; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled caller: err = %v", err)
	}

	var wg sync.WaitGroup
	for range 5 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			stats, err := c.get(context.Background(), load)
			if err != nil || stats.LogCount != 7 {
				t.Errorf("stats = %+v, err = %v", stats, err)
			}
		}()
	}
	time.Sleep(20 * time.Millisecond)
	close(release)
	wg.Wait()
	if n := loads.Load(); n != 1 {
		t.Errorf("loads = %d, want 1 shared load", n)
	}
	// Cached now
	if stats, _ := c.get(context.Background(), load); stats.LogCount != 7 || loads.Load() != 1 {
		t.Error("stats were not cached")
	}
}

func TestStatsCacheServesStaleWhileRefreshing(t *testing.T) {
	var c statsCache
	c.stats = &api.StatsResponse{LogCount: 1}
	c.expires = time.Now().Add(-time.Second)

	release := make(chan struct{})
	load := func(context.Context) (*api.StatsResponse, error) {
		<-release
		return &api.StatsResponse{LogCount: 2}, nil
	}
	start := time.Now()
	stats, err := c.get(context.Background(), load)
	if err != nil || stats.LogCount != 1 {
		t.Fatalf("stats = %+v, err = %v; want the stale ones", stats, err)
	}
	if time.Since(start) > time.Second {
		t.Error("waited for the refresh")
	}
	close(release)
	waitFor(t, func() bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		return c.stats.LogCount == 2
	})
}

func TestStatsCacheReportsLoadErrors(t *testing.T) {
	var c statsCache
	boom := errors.New("boom")
	if _, err := c.get(context.Background(), func(context.Context) (*api.StatsResponse, error) { return nil, boom }); !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
	// A failed load is not cached: the next call loads again
	stats, err := c.get(context.Background(), func(context.Context) (*api.StatsResponse, error) {
		return &api.StatsResponse{SpanCount: 3}, nil
	})
	if err != nil || stats.SpanCount != 3 {
		t.Fatalf("stats = %+v, err = %v", stats, err)
	}
}

// waitFor polls cond for up to two seconds
func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not met in time")
		}
		time.Sleep(time.Millisecond)
	}
}
