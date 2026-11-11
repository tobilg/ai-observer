package server

import (
	"context"
	"time"

	"github.com/tobilg/ai-observer/internal/diskspace"
	"github.com/tobilg/ai-observer/internal/logger"
	"github.com/tobilg/ai-observer/internal/storage"
)

// retentionInterval is how often telemetry beyond the retention period or
// the size limit is deleted
const retentionInterval = time.Hour

// startRetention logs the database's size and growth, then deletes
// telemetry older than the configured retention period
// (AI_OBSERVER_RETENTION_DAYS) and the oldest telemetry above the size limit
// (AI_OBSERVER_MAX_DB_SIZE) now and then every retentionInterval, until
// Shutdown
func (s *Server) startRetention() {
	s.logDatabaseGrowth(context.Background())

	days, limit := s.config.RetentionDays, s.config.MaxDBSize
	if days <= 0 && limit <= 0 {
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	s.stopRetention = func() {
		cancel()
		<-done
	}

	if days > 0 {
		logger.Info("Telemetry retention enabled", "days", days)
	}
	if limit > 0 {
		logger.Info("Database size limit enabled: the oldest days are deleted while the data exceeds it",
			"max_db_size", diskspace.FormatBytes(limit),
			"note", "the limit applies to the used blocks; the file keeps its size and new data reuses the freed blocks (ai-observer compact shrinks it)")
	}
	go func() {
		defer close(done)
		ticker := time.NewTicker(retentionInterval)
		defer ticker.Stop()
		var guard sizeGuard
		for {
			if days > 0 {
				s.applyRetention(ctx, time.Now().AddDate(0, 0, -days))
			}
			if limit > 0 {
				s.applySizeLimit(ctx, &guard, time.Now())
			}
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
	recordRetention(deleted)
	if deleted.LogCount+deleted.MetricCount+deleted.SpanCount > 0 {
		logger.Info("Deleted expired telemetry",
			"before", cutoff.Format(time.RFC3339),
			"logs", deleted.LogCount,
			"metrics", deleted.MetricCount,
			"spans", deleted.SpanCount)
		s.checkpointAfterDeletion(ctx)
	}
}

// checkpointAfterDeletion frees the blocks of deleted telemetry, so new data
// reuses them instead of growing the file (storage.DuckDBStore.Checkpoint)
func (s *Server) checkpointAfterDeletion(ctx context.Context) {
	if err := s.storage.Checkpoint(ctx); err != nil && ctx.Err() == nil {
		logger.Warn("Checkpoint after deleting telemetry failed", "error", err)
	}
}

// sizeLimitKeep is the telemetry the size limit never deletes: the most
// recent day
const sizeLimitKeep = 24 * time.Hour

// reclaimRows is the number of records of a deleted day from which its
// blocks are expected to show as free after a checkpoint
const reclaimRows = 10_000

// largeDatabaseBytes is the size from which a database that keeps all
// telemetry is pointed out at startup
const largeDatabaseBytes = 1 << 30
