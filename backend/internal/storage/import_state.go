package storage

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/tobilg/ai-observer/internal/api"
)

type execContexter interface {
	ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
}

// ImportState represents the import state for a single file
type ImportState struct {
	Source       string
	FilePath     string
	FileHash     string
	ImportedAt   time.Time
	RecordCount  int
	ByteOffset   int64
	MessageCount int
	ParserState  string
	// FileSize and FileModTimeNs describe the file the import parsed; while
	// they match, the file is unchanged and need not be hashed
	FileSize      int64
	FileModTimeNs int64
	// SourceTracked is set when all stored records of the file carry its
	// path in SourceFile, so a re-import can replace them. Records stored by
	// versions before migration 3 carry none.
	SourceTracked bool
	// Partial is set when an import stored only the records in a date range:
	// the next import reads the file again even if it is unchanged. When
	// records after the range were left out, ByteOffset and MessageCount are
	// 0, so watch reads the whole file again and replaces its records.
	Partial bool
}

const importStateColumns = `source, file_path, file_hash, imported_at, record_count, byte_offset,
	message_count, parser_state, file_size, file_mtime_ns, source_tracked, partial`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanImportState(row rowScanner) (*ImportState, error) {
	var state ImportState
	var parserState sql.NullString
	var size, mtime sql.NullInt64
	var tracked, partial sql.NullBool
	if err := row.Scan(
		&state.Source,
		&state.FilePath,
		&state.FileHash,
		&state.ImportedAt,
		&state.RecordCount,
		&state.ByteOffset,
		&state.MessageCount,
		&parserState,
		&size,
		&mtime,
		&tracked,
		&partial,
	); err != nil {
		return nil, err
	}
	state.ParserState = parserState.String
	state.FileSize = size.Int64
	state.FileModTimeNs = mtime.Int64
	state.SourceTracked = tracked.Bool
	state.Partial = partial.Bool
	return &state, nil
}

// GetImportState retrieves the import state for a specific file
func (s *DuckDBStore) GetImportState(ctx context.Context, source, filePath string) (*ImportState, error) {
	query := `SELECT ` + importStateColumns + ` FROM import_state WHERE source = ? AND file_path = ?`

	state, err := scanImportState(s.db.QueryRowContext(ctx, query, source, filePath))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("querying import state: %w", err)
	}
	return state, nil
}

// SetImportState creates or updates the import state for a file
func (s *DuckDBStore) SetImportState(ctx context.Context, state *ImportState) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return setImportState(ctx, s.db, state)
}

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
	return setImportState(ctx, w.tx, state)
}

// deleteFileRecords deletes the selected logs, metrics and spans read from
// the session file at path
func deleteFileRecords(ctx context.Context, execer execContexter, path string, sel FileRecords) error {
	for _, table := range []string{"otel_logs", "otel_metrics", "otel_traces"} {
		query := "DELETE FROM " + table + " WHERE SourceFile = ?"
		args := []any{path}
		if sel.From != nil {
			query += " AND Timestamp >= ?::TIMESTAMP"
			args = append(args, formatTimeForDB(*sel.From))
		}
		if sel.To != nil {
			query += " AND Timestamp <= ?::TIMESTAMP"
			args = append(args, formatTimeForDB(*sel.To))
		}
		if _, err := execer.ExecContext(ctx, query, args...); err != nil {
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
		return nil
	})
}

// InsertTraces inserts received spans and the metrics derive returns for
// them in one transaction. Spans that are already stored, such as those of a
// batch an exporter sent again, are skipped and get no derived metrics. It
// returns the stored spans and metrics.
func (s *DuckDBStore) InsertTraces(ctx context.Context, spans []api.Span, derive func([]api.Span) []api.MetricDataPoint) ([]api.Span, []api.MetricDataPoint, error) {
	var stored []api.Span
	var derived []api.MetricDataPoint
	err := s.inWriteTx(ctx, func(w *writeTx) error {
		var err error
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

func setImportState(ctx context.Context, execer execContexter, state *ImportState) error {
	// Use INSERT OR REPLACE for upsert behavior
	query := `INSERT OR REPLACE INTO import_state (` + importStateColumns + `)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	_, err := execer.ExecContext(ctx, query,
		state.Source,
		state.FilePath,
		state.FileHash,
		state.ImportedAt,
		state.RecordCount,
		state.ByteOffset,
		state.MessageCount,
		state.ParserState,
		state.FileSize,
		state.FileModTimeNs,
		state.SourceTracked,
		state.Partial,
	)
	if err != nil {
		return fmt.Errorf("setting import state: %w", err)
	}

	return nil
}

// SetWatchFields updates the watch-specific fields for a file, creating the row if needed
func (s *DuckDBStore) SetWatchFields(ctx context.Context, source, filePath string, byteOffset int64, messageCount int, parserState string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return setWatchFields(ctx, s.db, source, filePath, byteOffset, messageCount, parserState)
}

func setWatchFields(ctx context.Context, execer execContexter, source, filePath string, byteOffset int64, messageCount int, parserState string) error {
	// Try to update existing row first
	updateQuery := `
		UPDATE import_state
		SET byte_offset = ?, message_count = ?, parser_state = ?, imported_at = ?
		WHERE source = ? AND file_path = ?
	`
	now := time.Now()
	result, err := execer.ExecContext(ctx, updateQuery, byteOffset, messageCount, parserState, now, source, filePath)
	if err != nil {
		return fmt.Errorf("updating watch fields: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("checking rows affected: %w", err)
	}

	// If no existing row, insert a new one. Everything watch stores for the
	// file from now on carries its path, so the file is source-tracked.
	if rowsAffected == 0 {
		insertQuery := `
			INSERT INTO import_state (source, file_path, file_hash, imported_at, record_count, byte_offset, message_count, parser_state, source_tracked)
			VALUES (?, ?, '', ?, 0, ?, ?, ?, true)
		`
		_, err := execer.ExecContext(ctx, insertQuery, source, filePath, now, byteOffset, messageCount, parserState)
		if err != nil {
			return fmt.Errorf("inserting watch fields: %w", err)
		}
	}

	return nil
}
