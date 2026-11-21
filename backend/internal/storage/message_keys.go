package storage

import (
	"context"
	"database/sql/driver"
	"fmt"
	"time"

	"github.com/tobilg/ai-observer/internal/logger"
	"github.com/tobilg/ai-observer/internal/msgmerge"
	"github.com/tobilg/ai-observer/internal/tools"
)

// Every log stores its message key (otel_logs.MessageKey, migration 9):
// what the transcript merge rules compare of it (msgmerge.Key), or
// msgmerge.NotMessage() for logs that become no transcript message. Session
// lists merge a session's records by these keys (fillSessionExtents), as
// transcripts merge them, without reading the logs' attributes. A key is
// derived from the stored log (after redaction) by the code of one version
// (msgmerge.KeyVersion): keys that are NULL (restored records, logs stored
// before migration 9) or of another version are computed from the
// attributes by readers, and refilled in the background by servers
// (StartMessageKeyRefill) and in restore's transaction (fillMessageKeysTx).

// messageKey returns the key a log stores
func messageKey(service, body string, attrs map[string]string) driver.Value {
	key, ok := msgmerge.KeyOf(service, body, attrs)
	if !ok {
		return msgmerge.NotMessage()
	}
	return key.Encode()
}

// messageKeyFillBatch bounds the logs whose attributes one step of
// fillMessageKeysWhere holds at once
const messageKeyFillBatch = 20000

// staleMessageKey selects the logs whose key is NULL or of another version
// (args: staleMessageKeyArgs)
const staleMessageKey = `(MessageKey IS NULL OR (MessageKey <> ? AND NOT starts_with(MessageKey, ?)))`

func staleMessageKeyArgs() []any { return []any{msgmerge.NotMessage(), msgmerge.KeyPrefix()} }

// fillMessageKeysTx sets the message key of every log whose key is NULL or
// of another version, in the caller's transaction (restore, whose rows have
// no committed row IDs yet)
func fillMessageKeysTx(ctx context.Context, w *writeTx) error {
	_, err := fillMessageKeysWhere(ctx, w, "true", nil)
	return err
}

// fillMessageKeysWhere sets the message key of the stale logs matching cond
// (a condition on otel_logs with args) and returns how many logs it set
func fillMessageKeysWhere(ctx context.Context, w *writeTx, cond string, args []any) (int64, error) {
	where := staleMessageKey + ` AND (` + cond + `)`
	whereArgs := append(staleMessageKeyArgs(), args...)
	// Logs of no session and logs whose event is no conversation event
	// (a superset of msgmerge.LogMessage's) are no messages, without
	// reading them in Go
	event := `COALESCE(NULLIF(json_extract_string(LogAttributes, '$."event.name"'), ''), Body)`
	res, err := w.tx.ExecContext(ctx, `UPDATE otel_logs SET MessageKey = ?
		WHERE `+where+` AND (SessionId IS NULL OR NOT `+sqlIn(event, tools.TranscriptEvents())+` OR `+event+` IS NULL)`,
		append([]any{msgmerge.NotMessage()}, whereArgs...)...)
	if err != nil {
		return 0, fmt.Errorf("marking logs that are no messages: %w", err)
	}
	filled, _ := res.RowsAffected()
	created := false
	for {
		rows, err := w.tx.QueryContext(ctx, `SELECT rowid, ServiceName, Body, LogAttributes FROM otel_logs
			WHERE `+where+` LIMIT ?`, append(whereArgs, messageKeyFillBatch)...)
		if err != nil {
			return filled, fmt.Errorf("reading logs without message keys: %w", err)
		}
		type fill struct {
			rid int64
			key driver.Value
		}
		var fills []fill
		for rows.Next() {
			var rid int64
			var service string
			var body *string
			var attrs any
			if err := rows.Scan(&rid, &service, &body, &attrs); err != nil {
				rows.Close()
				return filled, fmt.Errorf("scanning log: %w", err)
			}
			text := ""
			if body != nil {
				text = *body
			}
			fills = append(fills, fill{rid, messageKey(service, text, scanJSONToMap(attrs))})
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return filled, fmt.Errorf("iterating logs: %w", err)
		}
		rows.Close()
		if len(fills) == 0 {
			break
		}
		if !created {
			if _, err := w.tx.ExecContext(ctx, `CREATE OR REPLACE TABLE message_key_fill (rid BIGINT, k VARCHAR)`); err != nil {
				return filled, fmt.Errorf("creating message key table: %w", err)
			}
			created = true
		}
		if err := w.appendRows(ctx, "message_key_fill", []string{"rid", "k"}, len(fills), func(i int) []driver.Value {
			return []driver.Value{fills[i].rid, fills[i].key}
		}); err != nil {
			return filled, err
		}
		if _, err := w.tx.ExecContext(ctx, `UPDATE otel_logs SET MessageKey = f.k FROM message_key_fill f WHERE otel_logs.rowid = f.rid`); err != nil {
			return filled, fmt.Errorf("storing message keys: %w", err)
		}
		if _, err := w.tx.ExecContext(ctx, `DELETE FROM message_key_fill`); err != nil {
			return filled, fmt.Errorf("clearing message key table: %w", err)
		}
		filled += int64(len(fills))
		if len(fills) < messageKeyFillBatch {
			break
		}
	}
	if created {
		if _, err := w.tx.ExecContext(ctx, `DROP TABLE message_key_fill`); err != nil {
			return filled, fmt.Errorf("dropping message key table: %w", err)
		}
	}
	return filled, nil
}

// messageKeyRefillChunk is the row ID range RefillMessageKeys fills in one
// write transaction: ingestion waits for at most one chunk. Measured on
// 42k logs (OTLP and imported Claude Code/Codex files): 10,000 took 0.76 s
// with 203 ms per transaction at most, 5,000 0.77 s and 124 ms, 2,500
// 0.8-0.9 s and 69-91 ms. A variable for tests.
var messageKeyRefillChunk int64 = 5000

// MessageKeyRefill describes a run of RefillMessageKeys
type MessageKeyRefill struct {
	Logs     int64         // keys set
	Chunks   int           // write transactions
	Duration time.Duration // whole run
	Longest  time.Duration // longest write transaction, lock wait included
}

// RefillMessageKeys sets the message keys of logs stored by another version
// (or restored or migrated without keys), in chunks of row IDs, one write
// transaction each, so writes of other callers wait for one chunk at most.
// Readers compute such keys from the attributes, so results are the same
// before, during and after the refill, only slower before. Logs stored
// meanwhile get their keys on insert.
func (s *DuckDBStore) RefillMessageKeys(ctx context.Context) (MessageKeyRefill, error) {
	start := time.Now()
	var r MessageKeyRefill
	var lo, hi *int64
	if err := s.db.QueryRowContext(ctx, `SELECT min(rowid), max(rowid) FROM otel_logs WHERE `+staleMessageKey,
		staleMessageKeyArgs()...).Scan(&lo, &hi); err != nil {
		return r, fmt.Errorf("finding logs without message keys: %w", err)
	}
	if lo == nil || hi == nil {
		return r, nil
	}
	for from := *lo; from <= *hi; from += messageKeyRefillChunk {
		if err := ctx.Err(); err != nil {
			return r, err
		}
		chunkStart := time.Now()
		var n int64
		if err := s.inWriteTx(ctx, func(w *writeTx) error {
			var err error
			n, err = fillMessageKeysWhere(ctx, w, `rowid >= ? AND rowid < ?`, []any{from, from + messageKeyRefillChunk})
			return err
		}); err != nil {
			return r, err
		}
		r.Logs += n
		r.Chunks++
		r.Longest = max(r.Longest, time.Since(chunkStart))
	}
	r.Duration = time.Since(start)
	return r, nil
}

// StartMessageKeyRefill runs RefillMessageKeys in the background (servers
// call it once they serve); Close stops it and waits for it
func (s *DuckDBStore) StartMessageKeyRefill() {
	s.bgMu.Lock()
	defer s.bgMu.Unlock()
	if s.bgCancel != nil || s.closed {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	s.bgCancel, s.bgDone = cancel, done
	go func() {
		defer close(done)
		r, err := s.RefillMessageKeys(ctx)
		switch {
		case err != nil && ctx.Err() == nil:
			logger.Warn("Filling message keys failed; session lists compute them meanwhile", "error", err)
		case err == nil && r.Logs > 0:
			logger.Info("Filled message keys of stored logs", "logs", r.Logs, "transactions", r.Chunks,
				"duration", r.Duration.Round(time.Millisecond), "longest_transaction", r.Longest.Round(time.Millisecond))
		}
	}()
}

// stopBackground stops StartMessageKeyRefill's work and waits for it
func (s *DuckDBStore) stopBackground() {
	s.bgMu.Lock()
	s.closed = true
	cancel, done := s.bgCancel, s.bgDone
	s.bgMu.Unlock()
	if cancel != nil {
		cancel()
		<-done
	}
}
