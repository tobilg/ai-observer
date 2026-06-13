package storage

import (
	"context"
	"fmt"

	"github.com/tobilg/ai-observer/internal/api"
)

// InsertWatcherBatch atomically inserts watcher-derived telemetry and advances
// the per-file watcher state. If any insert fails, the offset is not advanced.
func (s *DuckDBStore) InsertWatcherBatch(ctx context.Context, logs []api.LogRecord, metrics []api.MetricDataPoint, spans []api.Span, state *ImportState) error {
	if state == nil {
		return fmt.Errorf("watch import state is nil")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("beginning watcher transaction: %w", err)
	}
	defer tx.Rollback()

	if err := insertLogsTx(ctx, tx, logs); err != nil {
		return fmt.Errorf("inserting watcher logs: %w", err)
	}
	if err := insertMetricsTx(ctx, tx, metrics); err != nil {
		return fmt.Errorf("inserting watcher metrics: %w", err)
	}
	if err := insertSpansTx(ctx, tx, spans); err != nil {
		return fmt.Errorf("inserting watcher spans: %w", err)
	}
	if err := setWatchFields(ctx, tx, state.Source, state.FilePath, state.ByteOffset, state.MessageCount, state.ParserState); err != nil {
		return err
	}

	return tx.Commit()
}
