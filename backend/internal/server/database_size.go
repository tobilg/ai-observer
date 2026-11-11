package server

import (
	"context"
	"time"

	"github.com/tobilg/ai-observer/internal/diskspace"
	"github.com/tobilg/ai-observer/internal/logger"
	"github.com/tobilg/ai-observer/internal/storage"
)

// sizeGuard remembers a deletion of the size limit whose space did not show
// as free: DuckDB frees the blocks of deleted rows at a checkpoint after
// the deletion (one the transactions that may still read them do not
// prevent), and only whole row groups (122,880 rows)
type sizeGuard struct {
	pending bool
	// generation is the database's write generation after that deletion
	generation uint64
}

// sizeLimitStore is the storage the size limit works on
type sizeLimitStore interface {
	Checkpoint(ctx context.Context) error
	DatabaseSize(ctx context.Context) (storage.DatabaseSize, error)
	OldestTimestamp(ctx context.Context) (time.Time, bool, error)
	DeleteAllInRange(ctx context.Context, from, to time.Time, service string) (*storage.DeleteSummary, error)
	WriteGeneration() uint64
}

// applySizeLimit deletes the oldest telemetry above AI_OBSERVER_MAX_DB_SIZE
// (applySizeLimit)
func (s *Server) applySizeLimit(ctx context.Context, guard *sizeGuard, now time.Time) {
	applySizeLimit(ctx, s.storage, s.config.MaxDBSize, guard, now)
}

// applySizeLimit deletes the oldest telemetry, a day at a time, while the
// database's used blocks exceed limit. Deleting does not shrink the file,
// so the limit applies to the used blocks, measured after a checkpoint; the
// free blocks are reused by new data. The last day of telemetry is kept
// even above the limit. When a deletion's space does not show as free, no
// more is deleted until new data has been written (and with it a later
// checkpoint), so a stale measurement never deletes further days.
func applySizeLimit(ctx context.Context, store sizeLimitStore, limit int64, guard *sizeGuard, now time.Time) {
	if guard.pending {
		if store.WriteGeneration() == guard.generation {
			return
		}
		guard.pending = false
	}
	if err := store.Checkpoint(ctx); err != nil {
		if ctx.Err() == nil {
			logger.Warn("Checking the database size limit failed", "error", err)
		}
		return
	}
	size, err := store.DatabaseSize(ctx)
	if err != nil || size.UsedBytes() <= limit {
		return
	}

	var deleted storage.DeleteSummary
	before := size.UsedBytes()
	defer func() {
		if deleted.LogCount+deleted.MetricCount+deleted.SpanCount == 0 {
			return
		}
		recordRetention(deleted)
		logger.Info("Deleted the oldest telemetry above the database size limit",
			"max_db_size", diskspace.FormatBytes(limit),
			"used_before", diskspace.FormatBytes(before),
			"used_after", diskspace.FormatBytes(size.UsedBytes()),
			"logs", deleted.LogCount, "metrics", deleted.MetricCount, "spans", deleted.SpanCount)
	}()
	for size.UsedBytes() > limit {
		oldest, ok, err := store.OldestTimestamp(ctx)
		if err != nil || !ok {
			return
		}
		keepFrom := now.Add(-sizeLimitKeep)
		if !oldest.Before(keepFrom) {
			logger.Warn("The database exceeds AI_OBSERVER_MAX_DB_SIZE with only the last day of telemetry left, which is kept",
				"max_db_size", diskspace.FormatBytes(limit), "used", diskspace.FormatBytes(size.UsedBytes()))
			return
		}
		end := oldest.Add(retentionChunk)
		if end.After(keepFrom) {
			end = keepFrom
		}
		chunk, err := store.DeleteAllInRange(ctx, time.Time{}, end, "")
		if err != nil {
			if ctx.Err() == nil {
				logger.Error("Deleting telemetry above the database size limit failed", "error", err)
			}
			return
		}
		deleted.LogCount += chunk.LogCount
		deleted.MetricCount += chunk.MetricCount
		deleted.SpanCount += chunk.SpanCount
		if err := store.Checkpoint(ctx); err != nil {
			return
		}
		after, err := store.DatabaseSize(ctx)
		if err != nil {
			return
		}
		rows := chunk.LogCount + chunk.MetricCount + chunk.SpanCount
		if after.UsedBytes() >= size.UsedBytes() && rows >= reclaimRows {
			// Not freed yet: wait for a later checkpoint rather than
			// deleting more on a stale measurement
			guard.pending, guard.generation = true, store.WriteGeneration()
			size = after
			return
		}
		size = after
	}
}

// logDatabaseGrowth logs the database's size, free space and growth, with
// hints when it grows without bound or holds much free space
func (s *Server) logDatabaseGrowth(ctx context.Context) {
	size, err := s.storage.DatabaseSize(ctx)
	if err != nil || size.Path == "" {
		return
	}
	oldest, newest, err := s.storage.TelemetryBounds(ctx)
	if err != nil {
		return
	}
	total := size.FileBytes + size.WALBytes
	growth := storage.GrowthPerDay(size.UsedBytes(), oldest, newest)
	attrs := []any{
		"path", size.Path,
		"size", diskspace.FormatBytes(total),
		"used", diskspace.FormatBytes(size.UsedBytes()),
		"free", diskspace.FormatBytes(size.FreeBytes()),
	}
	if !oldest.IsZero() {
		attrs = append(attrs, "oldest_record", oldest.Format(time.RFC3339))
	}
	if growth > 0 {
		attrs = append(attrs, "growth_per_day", diskspace.FormatBytes(growth))
	}
	logger.Info("Database", attrs...)

	if s.config.RetentionDays <= 0 && s.config.MaxDBSize <= 0 && total >= largeDatabaseBytes {
		logger.Warn("The database keeps all telemetry and grows without bound",
			"size", diskspace.FormatBytes(total),
			"hint", "set AI_OBSERVER_RETENTION_DAYS (such as 30) or AI_OBSERVER_MAX_DB_SIZE (such as 20GB) to delete the oldest telemetry")
	}
	if size.CompactionWorthwhile() {
		logger.Info("Free space inside the database file",
			"free", diskspace.FormatBytes(size.FreeBytes()),
			"hint", "new data reuses it; to return it to the disk, stop the server and run `ai-observer compact`")
	}
}
