package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/tobilg/ai-observer/internal/api"
	"github.com/tobilg/ai-observer/internal/otlp"
)

// migration9CodexServices are the services of Codex 0.167 and later whose
// codex.sse_event logs earlier versions stored instead of deriving usage
// from them. A literal copy of the provider table at the time, so that the
// released migration does not change when the table does.
var migration9CodexServices = []any{"codex-app-server", "codex-exec-server"}

// migration9SSEEvents selects those logs
const migration9SSEEvents = `ServiceName IN (?, ?)
	AND json_extract_string(LogAttributes, '$."event.name"') = 'codex.sse_event'`

// deriveCodexServerUsage derives the token and cost metrics of the
// codex.sse_event logs that earlier versions stored for Codex's app and exec
// servers, as ingestion does now, and deletes the logs, which ingestion
// does not store
func deriveCodexServerUsage(ctx context.Context, w *writeTx) error {
	rows, err := w.tx.QueryContext(ctx, `
		SELECT Timestamp, ServiceName, ResourceAttributes, LogAttributes, COALESCE(TraceId, ''), COALESCE(SpanId, '')
		FROM otel_logs
		WHERE `+migration9SSEEvents, migration9CodexServices...)
	if err != nil {
		return fmt.Errorf("reading Codex SSE events: %w", err)
	}
	var metrics []api.MetricDataPoint
	for rows.Next() {
		var ts time.Time
		var service, traceID, spanID string
		var resourceAttrs, logAttrs any
		if err := rows.Scan(&ts, &service, &resourceAttrs, &logAttrs, &traceID, &spanID); err != nil {
			rows.Close()
			return fmt.Errorf("scanning Codex SSE event: %w", err)
		}
		metrics = append(metrics, otlp.ExtractCodexMetrics(scanJSONToMap(logAttrs), ts, service, scanJSONToMap(resourceAttrs), traceID, spanID)...)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("reading Codex SSE events: %w", err)
	}
	rows.Close()

	if _, err := w.tx.ExecContext(ctx, `DELETE FROM otel_logs WHERE `+migration9SSEEvents, migration9CodexServices...); err != nil {
		return fmt.Errorf("deleting Codex SSE events: %w", err)
	}
	return insertMetricsTx(ctx, w, metrics, "")
}
