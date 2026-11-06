package storage

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"strings"
	"time"

	"github.com/tobilg/ai-observer/internal/api"
	"github.com/tobilg/ai-observer/internal/tools"
)

func (s *DuckDBStore) InsertLogs(ctx context.Context, logs []api.LogRecord) error {
	if len(logs) == 0 {
		return nil
	}

	return s.inWriteTx(ctx, func(w *writeTx) error {
		return insertLogsTx(ctx, w, logs, "")
	})
}

var logColumns = []string{
	"Timestamp", "TraceId", "SpanId", "TraceFlags", "SeverityText",
	"SeverityNumber", "ServiceName", "Body", "ResourceSchemaUrl",
	"ResourceAttributes", "ScopeSchemaUrl", "ScopeName", "ScopeVersion",
	"ScopeAttributes", "LogAttributes", "SessionId", "SessionIds", "SourceFile",
}

// insertLogsTx appends logs. sourceFile is the session file they were read
// from, or "" for OTLP telemetry (see replaceFileRecords). Their text is
// redacted (w.redact); session IDs are read before.
func insertLogsTx(ctx context.Context, w *writeTx, logs []api.LogRecord, sourceFile string) error {
	return w.appendRows(ctx, "otel_logs", logColumns, len(logs), func(i int) []driver.Value {
		log := logs[i]
		sessionID, sessionIDs := sessionIDColumns(log.LogAttributes)
		log.Body = w.redact.String(log.Body)
		log.ResourceAttributes = w.redactShared(log.ResourceAttributes)
		log.ScopeAttributes = w.redactShared(log.ScopeAttributes)
		log.LogAttributes = w.redact.Map(log.LogAttributes)
		return []driver.Value{
			log.Timestamp,
			nullableString(log.TraceID),
			nullableString(log.SpanID),
			log.TraceFlags,
			nullableString(log.SeverityText),
			log.SeverityNumber,
			log.ServiceName,
			nullableString(log.Body),
			nullableString(log.ResourceSchemaURL),
			rawJSON(mapToString(log.ResourceAttributes)),
			nullableString(log.ScopeSchemaURL),
			nullableString(log.ScopeName),
			nullableString(log.ScopeVersion),
			rawJSON(mapToString(log.ScopeAttributes)),
			rawJSON(mapToString(log.LogAttributes)),
			sessionID,
			sessionIDs,
			nullableString(sourceFile),
		}
	})
}

// QueryLogs returns a page of the logs in [from, to] matching the filters,
// newest first
func (s *DuckDBStore) QueryLogs(ctx context.Context, service, severity, traceID, search string, from, to time.Time, page Page) (*api.LogsResponse, error) {
	var where conditions
	where.add("Timestamp >= ?::TIMESTAMP AND Timestamp <= ?::TIMESTAMP", formatTimeForDB(from), formatTimeForDB(to))
	if service != "" {
		where.add("ServiceName = ?", service)
	}
	if severity != "" {
		where.add("SeverityText = ?", severity)
	}
	if traceID != "" {
		where.add("TraceId = ?", traceID)
	}
	if search != "" {
		cond, args := containsCondition([]string{"Body", "ScopeName", "SeverityText", "CAST(LogAttributes AS VARCHAR)"}, search)
		where.add(cond, args...)
	}

	limit, offset, err := pageLimit(page)
	if err != nil {
		return nil, err
	}

	var total int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM otel_logs"+where.clause("WHERE"), where.args...).Scan(&total); err != nil {
		return nil, fmt.Errorf("counting logs: %w", err)
	}

	pageWhere := where
	if page.Cursor != "" {
		c, err := decodeCursor(page.Cursor, 0)
		if err != nil {
			return nil, err
		}
		cond, args := newestFirstAfter("Timestamp", []string{"rowid"}, c.Time, []any{c.RowID})
		pageWhere = where.and(cond, args...)
	}
	query := `
		SELECT
			rowid, Timestamp, TraceId, SpanId, TraceFlags, SeverityText,
			SeverityNumber, ServiceName, Body, ResourceSchemaUrl,
			ResourceAttributes, ScopeSchemaUrl, ScopeName, ScopeVersion,
			ScopeAttributes, LogAttributes
		FROM otel_logs` + pageWhere.clause("WHERE") + `
		ORDER BY Timestamp DESC, rowid DESC
		LIMIT ? OFFSET ?`

	rows, err := s.db.QueryContext(ctx, query, append(pageWhere.args, limit+1, offset)...)
	if err != nil {
		return nil, fmt.Errorf("querying logs: %w", err)
	}
	defer rows.Close()

	var logs []api.LogRecord
	var rowIDs []int64
	for rows.Next() {
		var log api.LogRecord
		var rowID int64
		var traceIDNull, spanIDNull, severityText, body, resourceSchemaURL sql.NullString
		var scopeSchemaURL, scopeName, scopeVersion sql.NullString
		var resourceAttrs, scopeAttrs, logAttrs interface{}

		if err := rows.Scan(
			&rowID, &log.Timestamp, &traceIDNull, &spanIDNull, &log.TraceFlags, &severityText,
			&log.SeverityNumber, &log.ServiceName, &body, &resourceSchemaURL,
			&resourceAttrs, &scopeSchemaURL, &scopeName, &scopeVersion,
			&scopeAttrs, &logAttrs,
		); err != nil {
			return nil, fmt.Errorf("scanning log: %w", err)
		}

		log.TraceID = traceIDNull.String
		log.SpanID = spanIDNull.String
		log.SeverityText = severityText.String
		log.Body = body.String
		log.ResourceSchemaURL = resourceSchemaURL.String
		log.ScopeSchemaURL = scopeSchemaURL.String
		log.ScopeName = scopeName.String
		log.ScopeVersion = scopeVersion.String
		log.ResourceAttributes = scanJSONToMap(resourceAttrs)
		log.ScopeAttributes = scanJSONToMap(scopeAttrs)
		log.LogAttributes = scanJSONToMap(logAttrs)

		logs = append(logs, log)
		rowIDs = append(rowIDs, rowID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating logs: %w", err)
	}

	resp := &api.LogsResponse{Logs: logs, Total: total}
	if len(logs) > limit {
		resp.Logs = logs[:limit]
		resp.HasMore = true
		last := limit - 1
		resp.NextCursor = encodeCursor(pageCursor{Time: formatTimeForDB(logs[last].Timestamp), RowID: rowIDs[last]})
	}
	return resp, nil
}

// GetLogLevels counts all stored logs by severity
func (s *DuckDBStore) GetLogLevels(ctx context.Context) (map[string]int64, error) {
	return s.GetLogLevelsInRange(ctx, TimeRange{})
}

// GetLogLevelsInRange counts the logs in r by severity
func (s *DuckDBStore) GetLogLevelsInRange(ctx context.Context, r TimeRange) (map[string]int64, error) {
	filter, args := r.filter("Timestamp")
	query := `
		SELECT SeverityText, COUNT(*) as count
		FROM otel_logs
		WHERE SeverityText IS NOT NULL` + filter + `
		GROUP BY SeverityText
		ORDER BY count DESC
	`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying log levels: %w", err)
	}
	defer rows.Close()

	levels := make(map[string]int64)
	for rows.Next() {
		var level string
		var count int64
		if err := rows.Scan(&level, &count); err != nil {
			return nil, fmt.Errorf("scanning log level: %w", err)
		}
		levels[level] = count
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating log levels: %w", err)
	}

	return levels, nil
}

// sessionModelExpr is the model a log record names (tools.ModelKeys). A
// session shows the model of its latest record that names one.
var sessionModelExpr = attributeValueExpr("LogAttributes", tools.ModelKeys)

// attributeValueExpr is the SQL expression for the first non-empty value of
// keys in the JSON attributes column
func attributeValueExpr(column string, keys []string) string {
	values := make([]string, len(keys))
	for i, key := range keys {
		values[i] = fmt.Sprintf(`NULLIF(json_extract_string(%s, '$."%s"'), '')`, column, key)
	}
	return "COALESCE(" + strings.Join(values, ", ") + ")"
}

// QuerySessions returns a page of the sessions with logs in [from, to], the
// most recently active in the range first. Each session shows its whole
// extent: start, last time, message count and latest model over all of its
// logs, also those outside the range.
// Supports: Claude Code (transcript.message), Gemini CLI (session.id), Codex CLI (conversation.id),
// Copilot (session.id / gen_ai.conversation.id / copilot_chat.session_id), OpenCode (session.id)
func (s *DuckDBStore) QuerySessions(ctx context.Context, service string, from, to time.Time, page Page) (*api.SessionsResponse, error) {
	// SessionId is extracted from the tool-specific attributes (session.id,
	// conversation.id, gen_ai.conversation.id, ...) when a log is stored
	var where conditions
	where.add("Timestamp >= ?::TIMESTAMP AND Timestamp <= ?::TIMESTAMP", formatTimeForDB(from), formatTimeForDB(to))
	where.add("SessionId IS NOT NULL")
	if service != "" {
		where.add("ServiceName = ?", service)
	}

	limit, offset, err := pageLimit(page)
	if err != nil {
		return nil, err
	}
	var after conditions
	if page.Cursor != "" {
		c, err := decodeCursor(page.Cursor, 2)
		if err != nil {
			return nil, err
		}
		cond, args := newestFirstAfter("last_active", []string{"SessionId", "ServiceName"}, c.Time, []any{c.Keys[0], c.Keys[1]})
		after.add(cond, args...)
	}
	// The total is counted over the grouped sessions before the cursor
	// condition, in the same scan as the page
	query := `
		WITH active AS (
			SELECT SessionId, ServiceName, MAX(Timestamp) AS last_active
			FROM otel_logs` + where.clause("WHERE") + `
			GROUP BY SessionId, ServiceName
		), counted AS (
			SELECT *, COUNT(*) OVER () AS total FROM active
		)
		SELECT SessionId, ServiceName, last_active, total
		FROM counted` + after.clause("WHERE") + `
		ORDER BY last_active DESC, SessionId DESC, ServiceName DESC
		LIMIT ? OFFSET ?`
	rows, err := s.db.QueryContext(ctx, query, concatArgs(where.args, after.args, []any{limit + 1, offset})...)
	if err != nil {
		return nil, fmt.Errorf("querying sessions: %w", err)
	}
	defer rows.Close()

	var sessions []api.Session
	var lastActive []time.Time
	total := -1
	for rows.Next() {
		var session api.Session
		var active time.Time
		if err := rows.Scan(&session.SessionID, &session.ServiceName, &active, &total); err != nil {
			return nil, fmt.Errorf("scanning session: %w", err)
		}
		// Replaced by the session's whole extent below
		session.StartTime, session.LastTime = active, active
		sessions = append(sessions, session)
		lastActive = append(lastActive, active)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating sessions: %w", err)
	}

	if total < 0 {
		// An empty page has no row to carry the total
		if page.Cursor == "" && offset == 0 {
			total = 0
		} else {
			countQuery := "SELECT COUNT(*) FROM (SELECT 1 FROM otel_logs" + where.clause("WHERE") + " GROUP BY SessionId, ServiceName)"
			if err := s.db.QueryRowContext(ctx, countQuery, where.args...).Scan(&total); err != nil {
				return nil, fmt.Errorf("counting sessions: %w", err)
			}
		}
	}

	resp := &api.SessionsResponse{Sessions: sessions, Total: total}
	if len(sessions) > limit {
		resp.Sessions = sessions[:limit]
		resp.HasMore = true
		last := sessions[limit-1]
		resp.NextCursor = encodeCursor(pageCursor{
			Time: formatTimeForDB(lastActive[limit-1]),
			Keys: []string{last.SessionID, last.ServiceName},
		})
	}
	if err := s.fillSessionExtents(ctx, resp.Sessions); err != nil {
		return nil, err
	}
	return resp, nil
}

// fillSessionExtents sets the start, last time, message count and latest
// model of sessions from all of their logs
func (s *DuckDBStore) fillSessionExtents(ctx context.Context, sessions []api.Session) error {
	if len(sessions) == 0 {
		return nil
	}
	type sessionKey struct{ id, service string }
	index := make(map[sessionKey]int, len(sessions))
	ids := make(map[string]bool, len(sessions))
	var placeholders []string
	var args []any
	for i, session := range sessions {
		index[sessionKey{session.SessionID, session.ServiceName}] = i
		if !ids[session.SessionID] {
			ids[session.SessionID] = true
			placeholders = append(placeholders, "?")
			args = append(args, session.SessionID)
		}
	}
	query := `
		SELECT
			SessionId,
			ServiceName,
			MIN(Timestamp),
			MAX(Timestamp),
			COUNT(*),
			arg_max(` + sessionModelExpr + `, Timestamp) FILTER (WHERE ` + sessionModelExpr + ` IS NOT NULL)
		FROM otel_logs
		WHERE SessionId IN (` + strings.Join(placeholders, ", ") + `)
		GROUP BY SessionId, ServiceName`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("querying session extents: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var key sessionKey
		var start, last time.Time
		var count int
		var model sql.NullString
		if err := rows.Scan(&key.id, &key.service, &start, &last, &count, &model); err != nil {
			return fmt.Errorf("scanning session extent: %w", err)
		}
		i, ok := index[key]
		if !ok {
			continue // the same session ID from a service not on the page
		}
		sessions[i].StartTime, sessions[i].LastTime = start, last
		sessions[i].MessageCount = count
		sessions[i].Model = model.String
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterating session extents: %w", err)
	}
	return nil
}
