package storage

import (
	"context"
	"fmt"

	"github.com/tobilg/ai-observer/internal/api"
)

// InsertWatcherBatch atomically inserts watcher-derived telemetry and advances
// the per-file watcher state. If any insert fails, the offset is not advanced.
// The records carry the file's path in SourceFile. With replace, all stored
// records of the file are deleted first (the file was rewritten and is read
// again from the start), and for a file whose records are not all linked to it
// (state.SourceTracked unset) the older imported records equal to the new ones.
func (s *DuckDBStore) InsertWatcherBatch(ctx context.Context, logs []api.LogRecord, metrics []api.MetricDataPoint, spans []api.Span, state *ImportState, replace bool) error {
	if state == nil {
		return fmt.Errorf("watch import state is nil")
	}

	return s.inWriteTx(ctx, func(w *writeTx) error {
		if replace {
			if err := deleteFileRecords(ctx, w.tx, state.FilePath, FileRecords{}); err != nil {
				return err
			}
			if !state.SourceTracked {
				if err := deleteLegacyRecords(ctx, w.tx, logs, metrics); err != nil {
					return err
				}
			}
		}
		if err := insertLogsTx(ctx, w, logs, state.FilePath); err != nil {
			return fmt.Errorf("inserting watcher logs: %w", err)
		}
		if err := insertMetricsTx(ctx, w, metrics, state.FilePath); err != nil {
			return fmt.Errorf("inserting watcher metrics: %w", err)
		}
		if err := insertSpansTx(ctx, w, spans, state.FilePath); err != nil {
			return fmt.Errorf("inserting watcher spans: %w", err)
		}
		return setWatchFields(ctx, w.tx, state.Source, state.FilePath, state.ByteOffset, state.MessageCount, state.ParserState)
	})
}
