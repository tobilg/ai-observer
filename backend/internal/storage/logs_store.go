package storage

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/tobilg/ai-observer/internal/api"
)

func (s *DuckDBStore) InsertLogs(ctx context.Context, logs []api.LogRecord) error {
	if len(logs) == 0 {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO otel_logs (
			Timestamp, TraceId, SpanId, TraceFlags, SeverityText,
			SeverityNumber, ServiceName, Body, ResourceSchemaUrl,
			ResourceAttributes, ScopeSchemaUrl, ScopeName, ScopeVersion,
			ScopeAttributes, LogAttributes
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("preparing statement: %w", err)
	}
	defer stmt.Close()

	for _, log := range logs {
		_, err := stmt.ExecContext(ctx,
			log.Timestamp,
			nullString(log.TraceID),
			nullString(log.SpanID),
			log.TraceFlags,
			nullString(log.SeverityText),
			log.SeverityNumber,
			log.ServiceName,
			nullString(log.Body),
			nullString(log.ResourceSchemaURL),
			mapToString(log.ResourceAttributes),
			nullString(log.ScopeSchemaURL),
			nullString(log.ScopeName),
			nullString(log.ScopeVersion),
			mapToString(log.ScopeAttributes),
			mapToString(log.LogAttributes),
		)
		if err != nil {
			return fmt.Errorf("inserting log: %w", err)
		}
	}

	return tx.Commit()
}

func (s *DuckDBStore) QueryLogs(ctx context.Context, service, severity, traceID, search string, from, to time.Time, limit, offset int) (*api.LogsResponse, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Format times as strings to avoid timezone issues with DuckDB's TIMESTAMP type
	fromStr := formatTimeForDB(from)
	toStr := formatTimeForDB(to)

	query := `
		SELECT
			Timestamp, TraceId, SpanId, TraceFlags, SeverityText,
			SeverityNumber, ServiceName, Body, ResourceSchemaUrl,
			ResourceAttributes, ScopeSchemaUrl, ScopeName, ScopeVersion,
			ScopeAttributes, LogAttributes
		FROM otel_logs
		WHERE Timestamp >= ?::TIMESTAMP AND Timestamp <= ?::TIMESTAMP
	`
	args := []interface{}{fromStr, toStr}

	if service != "" {
		query += " AND ServiceName = ?"
		args = append(args, service)
	}

	if severity != "" {
		query += " AND SeverityText = ?"
		args = append(args, severity)
	}

	if traceID != "" {
		query += " AND TraceId = ?"
		args = append(args, traceID)
	}

	if search != "" {
		query += " AND (Body ILIKE ? OR ScopeName ILIKE ? OR SeverityText ILIKE ? OR CAST(LogAttributes AS VARCHAR) ILIKE ?)"
		pattern := "%" + search + "%"
		args = append(args, pattern, pattern, pattern, pattern)
	}

	// Get total count
	countQuery := "SELECT COUNT(*) FROM otel_logs WHERE Timestamp >= ?::TIMESTAMP AND Timestamp <= ?::TIMESTAMP"
	countArgs := []interface{}{fromStr, toStr}
	if service != "" {
		countQuery += " AND ServiceName = ?"
		countArgs = append(countArgs, service)
	}
	if severity != "" {
		countQuery += " AND SeverityText = ?"
		countArgs = append(countArgs, severity)
	}
	if traceID != "" {
		countQuery += " AND TraceId = ?"
		countArgs = append(countArgs, traceID)
	}
	if search != "" {
		countQuery += " AND (Body ILIKE ? OR ScopeName ILIKE ? OR SeverityText ILIKE ? OR CAST(LogAttributes AS VARCHAR) ILIKE ?)"
		pattern := "%" + search + "%"
		countArgs = append(countArgs, pattern, pattern, pattern, pattern)
	}

	var total int
	if err := s.db.QueryRowContext(ctx, countQuery, countArgs...).Scan(&total); err != nil {
		return nil, fmt.Errorf("counting logs: %w", err)
	}

	query += fmt.Sprintf(" ORDER BY Timestamp DESC LIMIT %d OFFSET %d", limit, offset)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying logs: %w", err)
	}
	defer rows.Close()

	var logs []api.LogRecord
	for rows.Next() {
		var log api.LogRecord
		var traceIDNull, spanIDNull, severityText, body, resourceSchemaURL sql.NullString
		var scopeSchemaURL, scopeName, scopeVersion sql.NullString
		var resourceAttrs, scopeAttrs, logAttrs interface{}

		if err := rows.Scan(
			&log.Timestamp, &traceIDNull, &spanIDNull, &log.TraceFlags, &severityText,
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
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating logs: %w", err)
	}

	return &api.LogsResponse{
		Logs:    logs,
		Total:   total,
		HasMore: offset+len(logs) < total,
	}, nil
}

func (s *DuckDBStore) GetLogLevels(ctx context.Context) (map[string]int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := `
		SELECT SeverityText, COUNT(*) as count
		FROM otel_logs
		WHERE SeverityText IS NOT NULL
		GROUP BY SeverityText
		ORDER BY count DESC
	`

	rows, err := s.db.QueryContext(ctx, query)
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
