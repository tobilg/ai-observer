package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/tobilg/ai-observer/internal/api"
)

var sessionIDAttributeKeys = []string{
	"session.id",
	"conversation.id",
	"gen_ai.conversation.id",
	"copilot_chat.session_id",
	"copilot_chat.chat_session_id",
}

func sessionIDJSONPath(key string) string {
	return `$."` + key + `"`
}

func sessionIDCoalesceSQL(column string) string {
	parts := make([]string, 0, len(sessionIDAttributeKeys))
	for _, key := range sessionIDAttributeKeys {
		parts = append(parts, fmt.Sprintf("json_extract_string(%s, '%s')", column, sessionIDJSONPath(key)))
	}
	return "COALESCE(" + strings.Join(parts, ", ") + ")"
}

func sessionIDPresentSQL(column string) string {
	parts := make([]string, 0, len(sessionIDAttributeKeys))
	for _, key := range sessionIDAttributeKeys {
		parts = append(parts, fmt.Sprintf("json_extract_string(%s, '%s') IS NOT NULL", column, sessionIDJSONPath(key)))
	}
	return "(" + strings.Join(parts, " OR ") + ")"
}

func sessionIDMatchSQL(column string) string {
	parts := make([]string, 0, len(sessionIDAttributeKeys))
	for _, key := range sessionIDAttributeKeys {
		parts = append(parts, fmt.Sprintf("json_extract_string(%s, '%s') = ?", column, sessionIDJSONPath(key)))
	}
	return "(" + strings.Join(parts, " OR ") + ")"
}

func sessionIDMatchArgs(sessionID string) []interface{} {
	args := make([]interface{}, len(sessionIDAttributeKeys))
	for i := range sessionIDAttributeKeys {
		args[i] = sessionID
	}
	return args
}

type sessionLogRow struct {
	Timestamp   time.Time
	ServiceName string
	Body        string
	Attrs       map[string]string
}

func (s *DuckDBStore) querySessionLogsLocked(ctx context.Context, sessionID string) ([]sessionLogRow, error) {
	query := fmt.Sprintf(`
		SELECT
			Timestamp,
			ServiceName,
			Body,
			LogAttributes
		FROM otel_logs
		WHERE json_valid(LogAttributes)
		  AND %s
		ORDER BY Timestamp ASC
	`, sessionIDMatchSQL("LogAttributes"))

	rows, err := s.db.QueryContext(ctx, query, sessionIDMatchArgs(sessionID)...)
	if err != nil {
		return nil, fmt.Errorf("querying transcript: %w", err)
	}
	defer rows.Close()

	var result []sessionLogRow
	for rows.Next() {
		var row sessionLogRow
		var body sql.NullString
		var logAttrs interface{}
		if err := rows.Scan(&row.Timestamp, &row.ServiceName, &body, &logAttrs); err != nil {
			return nil, fmt.Errorf("scanning transcript message: %w", err)
		}
		row.Body = body.String
		row.Attrs = scanJSONToMap(logAttrs)
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating transcript: %w", err)
	}
	return result, nil
}

func (s *DuckDBStore) querySessionSpansLocked(ctx context.Context, sessionID string) ([]api.Span, error) {
	query := fmt.Sprintf(`
		SELECT
			Timestamp, TraceId, SpanId, ParentSpanId, TraceState,
			SpanName, SpanKind, ServiceName, ResourceAttributes,
			ScopeName, ScopeVersion, SpanAttributes, Duration,
			StatusCode, StatusMessage
		FROM otel_traces
		WHERE json_valid(SpanAttributes)
		  AND %s
		ORDER BY Timestamp ASC
	`, sessionIDMatchSQL("SpanAttributes"))

	return s.scanSpans(ctx, query, sessionIDMatchArgs(sessionID)...)
}

func transcriptMessagesFromLogs(rows []sessionLogRow) []api.TranscriptMessage {
	var messages []api.TranscriptMessage
	index := 0
	for _, row := range rows {
		eventName := row.Attrs["event.name"]
		if eventName == "" && strings.Contains(row.Body, ".") && !strings.Contains(row.Body, " ") {
			eventName = row.Body
		}

		role := mapEventToRole(eventName, row.ServiceName)
		if eventName == "transcript.message" {
			role = row.Attrs["message.role"]
		}
		if role == "" {
			continue
		}

		if idxStr, ok := row.Attrs["message.index"]; ok {
			fmt.Sscanf(idxStr, "%d", &index)
		}

		messages = append(messages, api.TranscriptMessage{
			Timestamp:    row.Timestamp,
			Role:         role,
			Content:      extractMessageContent(eventName, row.Attrs, row.Body),
			Index:        index,
			Model:        getModelName(row.Attrs),
			ToolName:     getToolName(row.Attrs, eventName),
			ToolInput:    getToolInput(row.Attrs),
			ToolOutput:   getToolOutput(row.Attrs),
			InputTokens:  parseIntAttr(row.Attrs, "input_tokens", "inputTokens", "gen_ai.usage.input_tokens", "llm.token_count.prompt"),
			OutputTokens: parseIntAttr(row.Attrs, "output_tokens", "outputTokens", "gen_ai.usage.output_tokens", "llm.token_count.completion"),
			CacheRead:    parseIntAttr(row.Attrs, "cache_read_input_tokens", "cacheRead", "gen_ai.usage.cache_read.input_tokens"),
			CacheWrite:   parseIntAttr(row.Attrs, "cache_creation_input_tokens", "cacheWrite", "gen_ai.usage.cache_creation.input_tokens"),
			CostUSD:      parseFloatAttr(row.Attrs, "cost_usd", "costUsd", "llm.cost"),
			DurationMs:   parseIntAttr(row.Attrs, "duration_ms", "durationMs"),
			Success:      parseBoolAttr(row.Attrs, "success", "tool_success"),
			OutputSize:   parseIntAttr(row.Attrs, "tool_result_size_bytes", "outputSize"),
		})
		index++
	}
	return messages
}

func transcriptMessagesFromSpans(spans []api.Span) []api.TranscriptMessage {
	var messages []api.TranscriptMessage
	for _, span := range spans {
		messages = append(messages, transcriptMessagesFromSpan(span)...)
	}
	return messages
}

func transcriptMessagesFromSpan(span api.Span) []api.TranscriptMessage {
	attrs := span.SpanAttributes
	if attrs == nil {
		return nil
	}

	op := strings.ToLower(strings.TrimSpace(attrs["gen_ai.operation.name"]))
	model := getModelName(attrs)
	durationMs := int(span.Duration / int64(time.Millisecond))
	if durationMs < 0 {
		durationMs = 0
	}

	switch op {
	case "invoke_agent":
		content := firstNonEmpty(
			extractGenAIMessageText(attrs["gen_ai.input.messages"], "user"),
			attrs["copilot_chat.user_request"],
		)
		if content == "" {
			return nil
		}
		return []api.TranscriptMessage{{
			Timestamp:  span.Timestamp,
			Role:       "user",
			Content:    content,
			Model:      model,
			DurationMs: durationMs,
		}}

	case "chat":
		content := extractGenAIMessageText(attrs["gen_ai.output.messages"], "assistant")
		msg := api.TranscriptMessage{
			Timestamp:    span.Timestamp,
			Role:         "assistant",
			Content:      content,
			Model:        model,
			InputTokens:  parseIntAttr(attrs, "gen_ai.usage.input_tokens", "input_tokens"),
			OutputTokens: parseIntAttr(attrs, "gen_ai.usage.output_tokens", "output_tokens"),
			CacheRead:    parseIntAttr(attrs, "gen_ai.usage.cache_read.input_tokens"),
			CacheWrite:   parseIntAttr(attrs, "gen_ai.usage.cache_creation.input_tokens"),
			DurationMs:   durationMs,
		}
		if content == "" && msg.InputTokens == 0 && msg.OutputTokens == 0 && msg.CacheRead == 0 && msg.CacheWrite == 0 {
			return nil
		}
		return []api.TranscriptMessage{msg}

	case "execute_tool":
		toolName := getToolName(attrs, op)
		if toolName == "" {
			toolName = span.SpanName
		}
		toolInput := getToolInput(attrs)
		toolOutput := getToolOutput(attrs)
		var success *bool
		switch strings.ToUpper(span.StatusCode) {
		case "OK", "STATUS_CODE_OK":
			ok := true
			success = &ok
		case "ERROR", "STATUS_CODE_ERROR":
			ok := false
			success = &ok
		default:
			success = parseBoolAttr(attrs, "success")
		}

		messages := []api.TranscriptMessage{{
			Timestamp:  span.Timestamp,
			Role:       "tool_use",
			Content:    toolInput,
			Model:      model,
			ToolName:   toolName,
			ToolInput:  toolInput,
			DurationMs: durationMs,
			Success:    success,
		}}
		if toolOutput != "" {
			messages = append(messages, api.TranscriptMessage{
				Timestamp:  span.Timestamp.Add(time.Millisecond),
				Role:       "tool_result",
				Content:    toolOutput,
				Model:      model,
				ToolName:   toolName,
				ToolOutput: toolOutput,
				Success:    success,
				OutputSize: len(toolOutput),
			})
		}
		return messages
	}

	return nil
}

func mergeTranscriptMessages(groups ...[]api.TranscriptMessage) []api.TranscriptMessage {
	var merged []api.TranscriptMessage
	seen := make(map[string]struct{})
	for _, group := range groups {
		for _, msg := range group {
			key := transcriptDedupKey(msg)
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			merged = append(merged, msg)
		}
	}

	sort.SliceStable(merged, func(i, j int) bool {
		return merged[i].Timestamp.Before(merged[j].Timestamp)
	})
	for i := range merged {
		merged[i].Index = i
	}
	if merged == nil {
		return []api.TranscriptMessage{}
	}
	return merged
}

func transcriptDedupKey(msg api.TranscriptMessage) string {
	return strings.Join([]string{
		msg.Timestamp.UTC().Format(time.RFC3339Nano),
		msg.Role,
		msg.Content,
		msg.ToolName,
		msg.ToolInput,
		msg.ToolOutput,
	}, "\x1f")
}

func extractGenAIMessageText(raw, role string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}

	var messages []map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &messages); err != nil {
		var single map[string]interface{}
		if err := json.Unmarshal([]byte(raw), &single); err != nil {
			return ""
		}
		messages = []map[string]interface{}{single}
	}

	var parts []string
	for _, msg := range messages {
		msgRole, _ := msg["role"].(string)
		if role != "" && msgRole != "" && !strings.EqualFold(msgRole, role) {
			continue
		}
		if text := genAIContentText(msg["content"]); text != "" {
			parts = append(parts, text)
			continue
		}
		if text := genAIContentText(msg["parts"]); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n")
}

func genAIContentText(value interface{}) string {
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	case []interface{}:
		var parts []string
		for _, item := range v {
			if text := genAIContentText(item); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "\n")
	case map[string]interface{}:
		if text, ok := v["content"].(string); ok && strings.TrimSpace(text) != "" {
			return strings.TrimSpace(text)
		}
		if text, ok := v["text"].(string); ok && strings.TrimSpace(text) != "" {
			return strings.TrimSpace(text)
		}
		if inner, ok := v["parts"]; ok {
			return genAIContentText(inner)
		}
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
