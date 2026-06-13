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

	if err := insertLogsTx(ctx, tx, logs); err != nil {
		return err
	}

	return tx.Commit()
}

func insertLogsTx(ctx context.Context, tx *sql.Tx, logs []api.LogRecord) error {
	if len(logs) == 0 {
		return nil
	}

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

	return nil
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

// QuerySessions returns sessions with transcript messages from all services
// Supports: Claude Code (transcript.message), Gemini CLI (session.id), Codex CLI (conversation.id)
func (s *DuckDBStore) QuerySessions(ctx context.Context, service string, from, to time.Time, limit, offset int) (*api.SessionsResponse, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	fromStr := formatTimeForDB(from)
	toStr := formatTimeForDB(to)

	// Build the query to group by session ID across all services
	// Different tools use different session identifiers:
	// - Claude Code (imported): session.id with event.name = 'transcript.message'
	// - Claude Code (OTLP): session.id in logAttributes
	// - Gemini CLI: session.id in logAttributes
	// - Codex CLI: conversation.id in logAttributes
	// Note: Keys contain dots, use JSONPath with escaped quotes: $."key.name"
	query := `
		SELECT
			COALESCE(
				json_extract_string(LogAttributes, '$."session.id"'),
				json_extract_string(LogAttributes, '$."conversation.id"')
			) as session_id,
			ServiceName,
			MIN(Timestamp) as start_time,
			MAX(Timestamp) as last_time,
			COUNT(*) as message_count,
			MAX(json_extract_string(LogAttributes, '$.model')) as model
		FROM otel_logs
		WHERE Timestamp >= ?::TIMESTAMP AND Timestamp <= ?::TIMESTAMP
		  AND json_valid(LogAttributes)
		  AND (
			json_extract_string(LogAttributes, '$."session.id"') IS NOT NULL
			OR json_extract_string(LogAttributes, '$."conversation.id"') IS NOT NULL
		  )
	`
	args := []interface{}{fromStr, toStr}

	if service != "" {
		query += " AND ServiceName = ?"
		args = append(args, service)
	}

	query += `
		GROUP BY session_id, ServiceName
		HAVING session_id IS NOT NULL
		ORDER BY last_time DESC
	`

	// Get total count first
	countQuery := `
		SELECT COUNT(DISTINCT COALESCE(
			json_extract_string(LogAttributes, '$."session.id"'),
			json_extract_string(LogAttributes, '$."conversation.id"')
		))
		FROM otel_logs
		WHERE Timestamp >= ?::TIMESTAMP AND Timestamp <= ?::TIMESTAMP
		  AND json_valid(LogAttributes)
		  AND (
			json_extract_string(LogAttributes, '$."session.id"') IS NOT NULL
			OR json_extract_string(LogAttributes, '$."conversation.id"') IS NOT NULL
		  )
	`
	countArgs := []interface{}{fromStr, toStr}
	if service != "" {
		countQuery += " AND ServiceName = ?"
		countArgs = append(countArgs, service)
	}

	var total int
	if err := s.db.QueryRowContext(ctx, countQuery, countArgs...).Scan(&total); err != nil {
		return nil, fmt.Errorf("counting sessions: %w", err)
	}

	query += fmt.Sprintf(" LIMIT %d OFFSET %d", limit, offset)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying sessions: %w", err)
	}
	defer rows.Close()

	var sessions []api.Session
	for rows.Next() {
		var session api.Session
		var sessionID, model sql.NullString

		if err := rows.Scan(
			&sessionID,
			&session.ServiceName,
			&session.StartTime,
			&session.LastTime,
			&session.MessageCount,
			&model,
		); err != nil {
			return nil, fmt.Errorf("scanning session: %w", err)
		}

		session.SessionID = sessionID.String
		session.Model = model.String

		sessions = append(sessions, session)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating sessions: %w", err)
	}

	return &api.SessionsResponse{
		Sessions: sessions,
		Total:    total,
		HasMore:  offset+len(sessions) < total,
	}, nil
}

// GetSessionTranscript returns all logs for a session, mapping events to transcript roles
// Supports: Claude Code, Gemini CLI, Codex CLI
func (s *DuckDBStore) GetSessionTranscript(ctx context.Context, sessionID string) (*api.TranscriptResponse, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Query for logs matching either session.id or conversation.id
	// Note: Keys contain dots, use JSONPath with escaped quotes: $."key.name"
	// Use json_valid() guard to skip rows with malformed JSON in LogAttributes
	query := `
		SELECT
			Timestamp,
			ServiceName,
			Body,
			LogAttributes
		FROM otel_logs
		WHERE json_valid(LogAttributes)
		  AND (
			json_extract_string(LogAttributes, '$."session.id"') = ?
			OR json_extract_string(LogAttributes, '$."conversation.id"') = ?
		)
		ORDER BY Timestamp ASC
	`

	rows, err := s.db.QueryContext(ctx, query, sessionID, sessionID)
	if err != nil {
		return nil, fmt.Errorf("querying transcript: %w", err)
	}
	defer rows.Close()

	var messages []api.TranscriptMessage
	var serviceName string
	var startTime, lastTime time.Time
	isFirst := true
	index := 0

	for rows.Next() {
		var timestamp time.Time
		var svc string
		var body sql.NullString
		var logAttrs interface{}

		if err := rows.Scan(&timestamp, &svc, &body, &logAttrs); err != nil {
			return nil, fmt.Errorf("scanning transcript message: %w", err)
		}

		attrs := scanJSONToMap(logAttrs)

		if isFirst {
			serviceName = svc
			startTime = timestamp
			isFirst = false
		}
		lastTime = timestamp

		// Map event types to roles based on service
		eventName := attrs["event.name"]
		role := mapEventToRole(eventName, svc)

		// For transcript.message events, use the message.role attribute
		if eventName == "transcript.message" {
			role = attrs["message.role"]
		}

		// Skip events that don't map to transcript roles
		if role == "" {
			continue
		}

		// Get index from attributes if available (Claude Code imported)
		if idxStr, ok := attrs["message.index"]; ok {
			fmt.Sscanf(idxStr, "%d", &index)
		}

		// Extract actual content based on event type
		content := extractMessageContent(eventName, attrs, body.String)

		msg := api.TranscriptMessage{
			Timestamp:    timestamp,
			Role:         role,
			Content:      content,
			Index:        index,
			Model:        attrs["model"],
			ToolName:     getToolName(attrs, eventName),
			ToolInput:    getToolInput(attrs),
			ToolOutput:   getToolOutput(attrs),
			InputTokens:  parseIntAttr(attrs, "input_tokens", "inputTokens"),
			OutputTokens: parseIntAttr(attrs, "output_tokens", "outputTokens"),
			CacheRead:    parseIntAttr(attrs, "cache_read_input_tokens", "cacheRead"),
			CacheWrite:   parseIntAttr(attrs, "cache_creation_input_tokens", "cacheWrite"),
			CostUSD:      parseFloatAttr(attrs, "cost_usd", "costUsd"),
			DurationMs:   parseIntAttr(attrs, "duration_ms", "durationMs"),
			Success:      parseBoolAttr(attrs, "success", "tool_success"),
			OutputSize:   parseIntAttr(attrs, "tool_result_size_bytes", "outputSize"),
		}

		messages = append(messages, msg)
		index++
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating transcript: %w", err)
	}

	if len(messages) == 0 {
		return nil, fmt.Errorf("session not found: %s", sessionID)
	}

	return &api.TranscriptResponse{
		SessionID:   sessionID,
		ServiceName: serviceName,
		StartTime:   startTime,
		LastTime:    lastTime,
		Messages:    messages,
	}, nil
}

// mapEventToRole converts event names to transcript roles
func mapEventToRole(eventName, serviceName string) string {
	switch eventName {
	// Claude Code (imported transcripts)
	case "transcript.message":
		return "" // Role is in message.role attribute, handled separately

	// Claude Code (OTLP)
	case "user_prompt", "codex.user_prompt":
		return "user"
	case "api_request", "api_response", "codex.api_request":
		return "assistant"
	case "tool_result", "codex.tool_result":
		return "tool_result"
	case "tool_decision", "codex.tool_decision":
		return "tool_use"

	// Gemini CLI
	case "gemini_cli.user_prompt":
		return "user"
	case "gemini_cli.api_request", "gemini_cli.api_response":
		return "assistant"
	case "gemini_cli.tool_call":
		return "tool_use"

	default:
		return ""
	}
}

// getToolName extracts tool name from attributes
func getToolName(attrs map[string]string, eventName string) string {
	if name, ok := attrs["tool.name"]; ok {
		return name
	}
	if name, ok := attrs["tool_name"]; ok {
		return name
	}
	if name, ok := attrs["function_name"]; ok {
		return name
	}
	return ""
}

// getToolInput extracts tool input from attributes
func getToolInput(attrs map[string]string) string {
	if input, ok := attrs["tool.input"]; ok {
		return input
	}
	if input, ok := attrs["tool_parameters"]; ok {
		return input
	}
	// Codex CLI uses "arguments"
	if input, ok := attrs["arguments"]; ok {
		return input
	}
	return ""
}

// getToolOutput extracts tool output from attributes (for imported data and Codex OTLP)
func getToolOutput(attrs map[string]string) string {
	if output, ok := attrs["tool.output"]; ok {
		return output
	}
	if output, ok := attrs["tool_result"]; ok {
		return output
	}
	// Codex CLI OTLP sends actual output in "output" attribute
	if output, ok := attrs["output"]; ok {
		return output
	}
	return ""
}

// parseIntAttr parses an integer from a string attribute
func parseIntAttr(attrs map[string]string, keys ...string) int {
	for _, key := range keys {
		if val, ok := attrs[key]; ok && val != "" {
			var result int
			fmt.Sscanf(val, "%d", &result)
			return result
		}
	}
	return 0
}

// parseFloatAttr parses a float from a string attribute
func parseFloatAttr(attrs map[string]string, keys ...string) float64 {
	for _, key := range keys {
		if val, ok := attrs[key]; ok && val != "" {
			var result float64
			fmt.Sscanf(val, "%f", &result)
			return result
		}
	}
	return 0
}

// parseBoolAttr parses a boolean from a string attribute
func parseBoolAttr(attrs map[string]string, keys ...string) *bool {
	for _, key := range keys {
		if val, ok := attrs[key]; ok {
			result := val == "true" || val == "1"
			return &result
		}
	}
	return nil
}

// extractMessageContent extracts actual content from logAttributes based on event type
func extractMessageContent(eventName string, attrs map[string]string, body string) string {
	switch eventName {
	case "user_prompt", "codex.user_prompt", "gemini_cli.user_prompt":
		// User prompts have the actual text in the 'prompt' attribute
		if prompt, ok := attrs["prompt"]; ok && prompt != "" {
			return prompt
		}
		return body

	case "transcript.message":
		// Imported transcripts already have content in body
		return body

	case "codex.tool_result":
		// Codex tool results have output in attributes
		if output, ok := attrs["output"]; ok && output != "" {
			return output
		}
		return body

	default:
		// For other events, use the body as-is
		return body
	}
}
