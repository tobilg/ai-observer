package storage

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"slices"
	"time"

	"github.com/tobilg/ai-observer/internal/api"
	"github.com/tobilg/ai-observer/internal/tools"
)

// sessionIDColumns returns the SessionId and SessionIds column values for the
// attributes of a log or span: the first session ID in key priority order
// (tools.SessionIDKeys), and all distinct IDs, since some tools (Copilot)
// carry several aliases for one session. Both are NULL when the record
// belongs to no session.
func sessionIDColumns(attrs map[string]string) (driver.Value, driver.Value) {
	var ids []string
	for _, key := range tools.SessionIDKeys {
		if id := attrs[key]; id != "" && !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil, nil
	}
	return ids[0], ids
}

// SessionLog is a log record of a session, as read for its transcript
type SessionLog struct {
	Timestamp   time.Time
	ServiceName string
	TraceID     string
	SpanID      string
	Body        string
	Attrs       map[string]string
}

// SessionLogs returns the logs of a session, oldest first
func (s *DuckDBStore) SessionLogs(ctx context.Context, sessionID string) ([]SessionLog, error) {
	query := `
		SELECT
			Timestamp,
			ServiceName,
			TraceId,
			SpanId,
			Body,
			LogAttributes
		FROM otel_logs
		WHERE list_contains(SessionIds, ?)
		ORDER BY Timestamp ASC
	`

	rows, err := s.db.QueryContext(ctx, query, sessionID)
	if err != nil {
		return nil, fmt.Errorf("querying transcript: %w", err)
	}
	defer rows.Close()

	var result []SessionLog
	for rows.Next() {
		var row SessionLog
		var body, traceID, spanID sql.NullString
		var logAttrs interface{}
		if err := rows.Scan(&row.Timestamp, &row.ServiceName, &traceID, &spanID, &body, &logAttrs); err != nil {
			return nil, fmt.Errorf("scanning transcript message: %w", err)
		}
		row.Body = body.String
		row.TraceID = traceID.String
		row.SpanID = spanID.String
		row.Attrs = scanJSONToMap(logAttrs)
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating transcript: %w", err)
	}
	return result, nil
}

// SessionSpans returns all spans of the traces that have a span of the
// session, oldest first. Spans of those traces without the session ID are
// included: the transcript decides which of them belong to the session.
func (s *DuckDBStore) SessionSpans(ctx context.Context, sessionID string) ([]api.Span, error) {
	query := `
		WITH matching_traces AS (
			SELECT DISTINCT ServiceName, TraceId
			FROM otel_traces
			WHERE list_contains(SessionIds, ?)
		)
		SELECT
			t.Timestamp, t.TraceId, t.SpanId, t.ParentSpanId, t.TraceState,
			t.SpanName, t.SpanKind, t.ServiceName, t.ResourceAttributes,
			t.ScopeName, t.ScopeVersion, t.SpanAttributes, t.Duration,
			t.StatusCode, t.StatusMessage
		FROM otel_traces t
		JOIN matching_traces m ON t.ServiceName = m.ServiceName AND t.TraceId = m.TraceId
		ORDER BY t.Timestamp ASC
	`
	return s.scanSpans(ctx, query, sessionID)
}
