package server

import (
	"context"
	"time"

	"github.com/tobilg/ai-observer/internal/logger"
	"github.com/tobilg/ai-observer/internal/storage"
)

// retentionInterval is how often telemetry beyond the retention period is deleted
const retentionInterval = time.Hour

// startRetention deletes telemetry older than the configured retention period
// now and then every retentionInterval, until Shutdown
func (s *Server) startRetention() {
	days := s.config.RetentionDays
	if days <= 0 {
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	s.stopRetention = func() {
		cancel()
		<-done
	}

	logger.Info("Telemetry retention enabled", "days", days)
	go func() {
		defer close(done)
		ticker := time.NewTicker(retentionInterval)
		defer ticker.Stop()
		for {
			s.applyRetention(ctx, time.Now().AddDate(0, 0, -days))
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

// retentionChunk is the span of telemetry deleted per transaction. Each
// deletion holds the write lock, so a large backlog (retention enabled on a
// big database) is deleted a day at a time instead of blocking ingestion
// until all of it is gone.
const retentionChunk = 24 * time.Hour

// applyRetention deletes the logs, metrics and spans of all services stored
// before cutoff
func (s *Server) applyRetention(ctx context.Context, cutoff time.Time) {
	oldest, ok, err := s.storage.OldestTimestamp(ctx)
	if err != nil {
		if ctx.Err() == nil {
			logger.Error("Deleting expired telemetry failed", "error", err)
		}
		return
	}
	if !ok || !oldest.Before(cutoff) {
		return
	}

	var deleted storage.DeleteSummary
	for end := oldest.Add(retentionChunk); ; end = end.Add(retentionChunk) {
		if end.After(cutoff) {
			end = cutoff
		}
		chunk, err := s.storage.DeleteAllInRange(ctx, time.Time{}, end, "")
		if err != nil {
			if ctx.Err() == nil {
				logger.Error("Deleting expired telemetry failed", "error", err)
			}
			return
		}
		deleted.LogCount += chunk.LogCount
		deleted.MetricCount += chunk.MetricCount
		deleted.SpanCount += chunk.SpanCount
		if !end.Before(cutoff) {
			break
		}
	}
	if deleted.LogCount+deleted.MetricCount+deleted.SpanCount > 0 {
		logger.Info("Deleted expired telemetry",
			"before", cutoff.Format(time.RFC3339),
			"logs", deleted.LogCount,
			"metrics", deleted.MetricCount,
			"spans", deleted.SpanCount)
	}
}
