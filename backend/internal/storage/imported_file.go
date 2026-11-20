package storage

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/tobilg/ai-observer/internal/api"
)

// FileRecords selects the stored records of one session file: all of them,
// or those within From and To when a bound is set
type FileRecords struct {
	From, To *time.Time
	// Legacy also selects the imported records stored before records were
	// linked to their files (migration 3) that equal the new records of the
	// file, so files imported by earlier versions are replaced too
	Legacy bool
}

// InsertImportedFile atomically inserts the telemetry parsed from one session
// file and records the file's import state. The records carry the file's path
// in SourceFile. With replace, the file's stored records it selects are
// deleted first, so re-importing a file that changed replaces its records
// instead of adding them again. If any step fails nothing is written, so a
// later import retries the whole file.
func (s *DuckDBStore) InsertImportedFile(ctx context.Context, logs []api.LogRecord, metrics []api.MetricDataPoint, spans []api.Span, state *ImportState, replace *FileRecords) error {
	return s.inWriteTx(ctx, func(w *writeTx) error {
		return insertImportedFileTx(ctx, w, logs, metrics, spans, state, replace)
	})
}

// ImportTx is a write transaction shared by a purge and the import of the
// files that rebuild the purged range, so a failure leaves all data as it was
type ImportTx struct {
	w *writeTx
}

// InImportTx runs fn in one write transaction, committed if fn returns nil
func (s *DuckDBStore) InImportTx(ctx context.Context, fn func(tx *ImportTx) error) error {
	return s.inWriteTx(ctx, func(w *writeTx) error {
		return fn(&ImportTx{w: w})
	})
}

// InsertImportedFile is DuckDBStore.InsertImportedFile within the transaction
func (t *ImportTx) InsertImportedFile(ctx context.Context, logs []api.LogRecord, metrics []api.MetricDataPoint, spans []api.Span, state *ImportState, replace *FileRecords) error {
	return insertImportedFileTx(ctx, t.w, logs, metrics, spans, state, replace)
}

// DeleteAllInRange is DuckDBStore.DeleteAllInRange within the transaction
func (t *ImportTx) DeleteAllInRange(ctx context.Context, from, to time.Time, service string) (*DeleteSummary, error) {
	return deleteAllInRange(ctx, t.w, from, to, service)
}

func insertImportedFileTx(ctx context.Context, w *writeTx, logs []api.LogRecord, metrics []api.MetricDataPoint, spans []api.Span, state *ImportState, replace *FileRecords) error {
	if state == nil {
		return fmt.Errorf("import state is nil")
	}
	if replace != nil {
		if err := deleteFileRecords(ctx, w.tx, state.FilePath, *replace); err != nil {
			return err
		}
		if replace.Legacy {
			if err := deleteLegacyRecords(ctx, w.tx, logs, metrics); err != nil {
				return err
			}
		}
	}
	if err := insertLogsTx(ctx, w, logs, state.FilePath); err != nil {
		return fmt.Errorf("inserting logs: %w", err)
	}
	if err := insertMetricsTx(ctx, w, metrics, state.FilePath); err != nil {
		return fmt.Errorf("inserting metrics: %w", err)
	}
	if err := insertSpansTx(ctx, w, spans, state.FilePath); err != nil {
		return fmt.Errorf("inserting spans: %w", err)
	}
	// Count requests OTLP recorded as well only once (usage_overlap.go)
	if _, err := reconcileUsage(ctx, w, logs, metrics, true); err != nil {
		return err
	}
	return setImportState(ctx, w.tx, state)
}

// deleteFileRecords deletes the selected logs, metrics and spans read from
// the session file at path
func deleteFileRecords(ctx context.Context, execer execContexter, path string, sel FileRecords) error {
	// A rewritten file may give a response another timestamp. Release its
	// matches in exactly the portion that will be rebuilt.
	r := TimeRange{}
	if sel.From != nil {
		r.From = *sel.From
	}
	if sel.To != nil {
		r.To = *sel.To
	}
	bounds, args := r.filter("CodexMatch.recorded_at")
	if _, err := execer.ExecContext(ctx, "UPDATE otel_metrics SET CodexMatch = NULL WHERE CodexMatch.file_path = ?"+bounds, append([]any{path}, args...)...); err != nil {
		return fmt.Errorf("clearing Codex usage matches for %s: %w", path, err)
	}
	bounds, args = r.filter("Timestamp")
	args = append([]any{path}, args...)
	for _, table := range []string{"otel_logs", "otel_metrics", "otel_traces"} {
		if _, err := execer.ExecContext(ctx, "DELETE FROM "+table+" WHERE SourceFile = ?"+bounds, args...); err != nil {
			return fmt.Errorf("deleting records of %s from %s: %w", path, table, err)
		}
	}
	return nil
}

// legacyKeyBatch is the number of records matched per statement
const legacyKeyBatch = 250

// deleteLegacyRecords deletes the imported records without SourceFile, stored
// before records were linked to their files, that equal the given records of
// one file. Records are matched by time and service, logs also by session and
// event name, metrics by name, type and model; values are not compared, since
// costs may have been computed with other prices. Only imported records
// (import_source) are deleted, never live telemetry. Importers produce no spans.
func deleteLegacyRecords(ctx context.Context, execer execContexter, logs []api.LogRecord, metrics []api.MetricDataPoint) error {
	for start := 0; start < len(logs); start += legacyKeyBatch {
		batch := logs[start:min(start+legacyKeyBatch, len(logs))]
		args := make([]any, 0, 4*len(batch))
		for _, l := range batch {
			args = append(args, formatTimeForDB(l.Timestamp), l.ServiceName, l.LogAttributes["session.id"], l.LogAttributes["event.name"])
		}
		query := `DELETE FROM otel_logs USING (VALUES ` + legacyValues(len(batch), 4) + `) AS k(ts, service, session_id, event_name)
			WHERE otel_logs.SourceFile IS NULL
				AND json_extract_string(otel_logs.LogAttributes, '$.import_source') IS NOT NULL
				AND otel_logs.Timestamp = k.ts AND otel_logs.ServiceName = k.service
				AND COALESCE(json_extract_string(otel_logs.LogAttributes, '$."session.id"'), '') = k.session_id
				AND COALESCE(json_extract_string(otel_logs.LogAttributes, '$."event.name"'), '') = k.event_name`
		if _, err := execer.ExecContext(ctx, query, args...); err != nil {
			return fmt.Errorf("deleting logs of an earlier import: %w", err)
		}
	}
	for start := 0; start < len(metrics); start += legacyKeyBatch {
		batch := metrics[start:min(start+legacyKeyBatch, len(metrics))]
		args := make([]any, 0, 5*len(batch))
		for _, m := range batch {
			args = append(args, formatTimeForDB(m.Timestamp), m.ServiceName, m.MetricName, m.Attributes["type"], m.Attributes["model"])
		}
		query := `DELETE FROM otel_metrics USING (VALUES ` + legacyValues(len(batch), 5) + `) AS k(ts, service, name, token_type, model)
			WHERE otel_metrics.SourceFile IS NULL
				AND json_extract_string(otel_metrics.Attributes, '$.import_source') IS NOT NULL
				AND otel_metrics.Timestamp = k.ts AND otel_metrics.ServiceName = k.service
				AND otel_metrics.MetricName = k.name
				AND COALESCE(json_extract_string(otel_metrics.Attributes, '$.type'), '') = k.token_type
				AND COALESCE(json_extract_string(otel_metrics.Attributes, '$.model'), '') = k.model`
		if _, err := execer.ExecContext(ctx, query, args...); err != nil {
			return fmt.Errorf("deleting metrics of an earlier import: %w", err)
		}
	}
	return nil
}

// legacyValues returns rows placeholder rows of a VALUES list with cols
// columns, the first a timestamp
func legacyValues(rows, cols int) string {
	row := "(?::TIMESTAMP" + strings.Repeat(", ?", cols-1) + ")"
	return strings.TrimSuffix(strings.Repeat(row+", ", rows), ", ")
}
