package storage

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"strings"
	"time"

	"github.com/tobilg/ai-observer/internal/api"
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
	"ScopeAttributes", "LogAttributes", "SessionId", "SessionIds", "SourceFile", "MessageKey",
}

// insertLogsTx appends logs. sourceFile is the session file they were read
// from, or "" for OTLP telemetry (see replaceFileRecords). Their text is
// redacted (w.redact); session IDs are read before, the message key
// (message_keys.go) after.
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
			messageKey(log.ServiceName, log.Body, log.LogAttributes),
		}
	})
}

// QueryLogs returns a page of the logs in [from, to] matching the filters,
// newest first
func (s *DuckDBStore) QueryLogs(ctx context.Context, service, severity, traceID, search string, from, to time.Time, page Page) (*api.LogsResponse, error) {
	var where conditions
	where.add("Timestamp >= ?::TIMESTAMP AND Timestamp <= ?::TIMESTAMP", formatTimeForDB(from), formatTimeForDB(to))
	where.addService(service)
	if severity != "" {
		where.add("SeverityText = ?", severity)
	}
	if traceID != "" {
		where.add("TraceId = ?", traceID)
	}
	if search != "" {
		cond, args := searchCondition([]string{"Body", "ScopeName", "SeverityText"}, []string{"LogAttributes"}, search)
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
			ScopeAttributes, LogAttributes, SessionId
		FROM otel_logs` + pageWhere.clause("WHERE") + `
		ORDER BY Timestamp DESC, rowid DESC
		LIMIT ? OFFSET ?`

	rows, err := s.db.QueryContext(ctx, query, append(pageWhere.args, limit+1, offset)...)
	if err != nil {
		return nil, fmt.Errorf("querying logs: %w", err)
	}
	defer rows.Close()

	logs := []api.LogRecord{} // clients read a list
	var rowIDs []int64
	for rows.Next() {
		var log api.LogRecord
		var rowID int64
		var traceIDNull, spanIDNull, severityText, body, resourceSchemaURL sql.NullString
		var scopeSchemaURL, scopeName, scopeVersion, sessionID sql.NullString
		var resourceAttrs, scopeAttrs, logAttrs interface{}

		if err := rows.Scan(
			&rowID, &log.Timestamp, &traceIDNull, &spanIDNull, &log.TraceFlags, &severityText,
			&log.SeverityNumber, &log.ServiceName, &body, &resourceSchemaURL,
			&resourceAttrs, &scopeSchemaURL, &scopeName, &scopeVersion,
			&scopeAttrs, &logAttrs, &sessionID,
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
		log.SessionID = sessionID.String
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

// attributeValueExpr is the SQL expression for the first non-empty value of
// keys in the JSON attributes column
func attributeValueExpr(column string, keys []string) string {
	values := make([]string, len(keys))
	for i, key := range keys {
		values[i] = fmt.Sprintf(`NULLIF(json_extract_string(%s, '$."%s"'), '')`, column, key)
	}
	return "COALESCE(" + strings.Join(values, ", ") + ")"
}
