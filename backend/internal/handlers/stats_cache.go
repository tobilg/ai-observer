package handlers

import (
	"context"
	"sync"
	"time"

	"github.com/tobilg/ai-observer/internal/api"
)

const (
	// statsCacheTTL bounds how often /api/stats scans the telemetry tables:
	// the dashboard polls it and also refreshes it after every burst of new
	// data
	statsCacheTTL = 5 * time.Second
	// statsMaxStale bounds how long after expiry cached stats are still
	// served while they are refreshed; older ones are waited for
	statsMaxStale = time.Minute
	// statsLoadTimeout bounds a load, which runs for all callers and not
	// under any one request's timeout
	statsLoadTimeout = time.Minute
)

// statsCache holds the last stats response (of all telemetry, for
// /api/stats and /api/status). Concurrent callers share one load, which no
// caller's cancellation or timeout ends: a caller that gives up returns
// its context's error while the load goes on and fills the cache for the
// next request. While a refresh runs, stats that expired less than
// statsMaxStale ago are served. Its zero value is ready to use.
type statsCache struct {
	mu      sync.Mutex
	stats   *api.StatsResponse
	expires time.Time
	loading *statsLoad // the load in progress, nil when none
}

// statsLoad is a load in progress; done is closed when it has ended
type statsLoad struct {
	done  chan struct{}
	stats *api.StatsResponse
	err   error
}

// get returns the cached stats, loading them when they have expired
func (c *statsCache) get(ctx context.Context, load func(context.Context) (*api.StatsResponse, error)) (*api.StatsResponse, error) {
	c.mu.Lock()
	now := time.Now()
	if c.stats != nil && now.Before(c.expires) {
		stats := c.stats
		c.mu.Unlock()
		return stats, nil
	}
	l := c.loading
	if l == nil {
		l = &statsLoad{done: make(chan struct{})}
		c.loading = l
		go c.run(context.WithoutCancel(ctx), load, l)
	}
	if c.stats != nil && now.Before(c.expires.Add(statsMaxStale)) {
		stats := c.stats
		c.mu.Unlock()
		return stats, nil
	}
	c.mu.Unlock()

	select {
	case <-l.done:
		return l.stats, l.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// run loads the stats for every caller waiting for l
func (c *statsCache) run(ctx context.Context, load func(context.Context) (*api.StatsResponse, error), l *statsLoad) {
	ctx, cancel := context.WithTimeout(ctx, statsLoadTimeout)
	defer cancel()
	stats, err := load(ctx)

	c.mu.Lock()
	defer c.mu.Unlock()
	l.stats, l.err = stats, err
	if err == nil {
		c.stats = stats
		c.expires = time.Now().Add(statsCacheTTL)
	}
	c.loading = nil
	close(l.done)
}
