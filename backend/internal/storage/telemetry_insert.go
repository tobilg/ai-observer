package storage

import (
	"context"
	"fmt"

	"github.com/tobilg/ai-observer/internal/api"
)

// InsertTelemetry inserts received telemetry in one transaction, so either
// all of it is stored or none
func (s *DuckDBStore) InsertTelemetry(ctx context.Context, logs []api.LogRecord, metrics []api.MetricDataPoint, spans []api.Span) error {
	if len(logs) == 0 && len(metrics) == 0 && len(spans) == 0 {
		return nil
	}
	return s.inWriteTx(ctx, func(w *writeTx) error {
		if err := insertLogsTx(ctx, w, logs, ""); err != nil {
			return fmt.Errorf("inserting logs: %w", err)
		}
		if err := insertMetricsTx(ctx, w, metrics, ""); err != nil {
			return fmt.Errorf("inserting metrics: %w", err)
		}
		if err := insertSpansTx(ctx, w, spans, ""); err != nil {
			return fmt.Errorf("inserting spans: %w", err)
		}
		// Count requests a session file recorded as well only once
		// (usage_overlap.go)
		_, err := reconcileUsage(ctx, w, logs, metrics, false)
		return err
	})
}

// InsertTraces inserts received spans and the metrics derive returns for
// them in one transaction. Spans that are already stored, such as those of a
// batch an exporter sent again, are skipped and get no derived metrics, and
// the tools' noise spans are left out (dropNoiseSpans) unless the store
// keeps all spans. It returns the stored spans and metrics.
func (s *DuckDBStore) InsertTraces(ctx context.Context, spans []api.Span, derive func([]api.Span) []api.MetricDataPoint) ([]api.Span, []api.MetricDataPoint, error) {
	var stored []api.Span
	var derived []api.MetricDataPoint
	err := s.inWriteTx(ctx, func(w *writeTx) error {
		var err error
		if !s.storeAllSpans {
			if spans, err = dropNoiseSpans(ctx, w.tx, spans); err != nil {
				return err
			}
		}
		if stored, err = insertNewSpansTx(ctx, w, spans, ""); err != nil {
			return fmt.Errorf("inserting spans: %w", err)
		}
		derived = derive(stored)
		if err := insertMetricsTx(ctx, w, derived, ""); err != nil {
			return fmt.Errorf("inserting metrics: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return stored, derived, nil
}
