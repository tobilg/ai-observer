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

// SessionLogs returns the logs of a session, oldest first, logs of the same
// time in the order they were stored, as session lists merge them
// (mergeSessionMessages)
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
		ORDER BY Timestamp ASC, rowid ASC
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

// SessionSpans returns the spans a session's transcript is built from,
// oldest first: of the traces that have a span of the session, all spans of
// those with GenAI spans (gen_ai.operation.name, the only spans that become
// messages; the transcript decides which of them belong to the session, also
// by their parents), and of the others only the spans carrying the session
// ID, which give the session's extent. Codex records up to 100,000 spans a
// turn, none of them GenAI spans: reading them all made transcripts of long
// sessions time out.
func (s *DuckDBStore) SessionSpans(ctx context.Context, sessionID string) ([]api.Span, error) {
	query := `
		WITH matching_traces AS (
			SELECT DISTINCT ServiceName, TraceId
			FROM otel_traces
			WHERE list_contains(SessionIds, ?)
		),
		genai_traces AS (
			SELECT DISTINCT t.ServiceName, t.TraceId
			FROM otel_traces t
			JOIN matching_traces m ON t.ServiceName = m.ServiceName AND t.TraceId = m.TraceId
			WHERE json_exists(t.SpanAttributes, '$."gen_ai.operation.name"')
		)
		SELECT
			t.Timestamp, t.TraceId, t.SpanId, t.ParentSpanId, t.TraceState,
			t.SpanName, t.SpanKind, t.ServiceName, t.ResourceAttributes,
			t.ScopeName, t.ScopeVersion, t.SpanAttributes, t.Duration,
			t.StatusCode, t.StatusMessage, t.SessionId
		FROM otel_traces t
		JOIN matching_traces m ON t.ServiceName = m.ServiceName AND t.TraceId = m.TraceId
		LEFT JOIN genai_traces g ON t.ServiceName = g.ServiceName AND t.TraceId = g.TraceId
		WHERE g.TraceId IS NOT NULL OR list_contains(t.SessionIds, ?)
		ORDER BY t.Timestamp ASC
	`
	return s.scanSpans(ctx, query, sessionID, sessionID)
}
