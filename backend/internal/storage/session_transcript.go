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
	TraceID     string
	SpanID      string
	Body        string
	Attrs       map[string]string
}

func (s *DuckDBStore) querySessionLogsLocked(ctx context.Context, sessionID string) ([]sessionLogRow, error) {
	query := fmt.Sprintf(`
		SELECT
			Timestamp,
			ServiceName,
			TraceId,
			SpanId,
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

func (s *DuckDBStore) querySessionSpansLocked(ctx context.Context, sessionID string) ([]api.Span, error) {
	query := fmt.Sprintf(`
		WITH matching_traces AS (
			SELECT DISTINCT ServiceName, TraceId
			FROM otel_traces
			WHERE json_valid(SpanAttributes) AND %s
		)
		SELECT
			t.Timestamp, t.TraceId, t.SpanId, t.ParentSpanId, t.TraceState,
			t.SpanName, t.SpanKind, t.ServiceName, t.ResourceAttributes,
			t.ScopeName, t.ScopeVersion, t.SpanAttributes, t.Duration,
			t.StatusCode, t.StatusMessage
		FROM otel_traces t
		JOIN matching_traces m ON t.ServiceName = m.ServiceName AND t.TraceId = m.TraceId
		ORDER BY t.Timestamp ASC
	`, sessionIDMatchSQL("SpanAttributes"))

	spans, err := s.scanSpans(ctx, query, sessionIDMatchArgs(sessionID)...)
	if err != nil {
		return nil, err
	}
	return selectSessionSpans(spans, sessionID), nil
}

type sessionSpanKey struct {
	ServiceName string
	TraceID     string
	SpanID      string
}

func spanKey(span api.Span) sessionSpanKey {
	return sessionSpanKey{span.ServiceName, span.TraceID, span.SpanID}
}

func parentSpanKey(span api.Span) sessionSpanKey {
	return sessionSpanKey{span.ServiceName, span.TraceID, span.ParentSpanID}
}

// A Copilot agent can bridge an internal conversation ID and a VS Code chat ID.
// Recover descendants without IDs, but do not pull in unrelated siblings or
// descendants that explicitly identify a different session.
func selectSessionSpans(spans []api.Span, sessionID string) []api.Span {
	aliases := make(map[string]map[string]bool)
	selected := make(map[sessionSpanKey]bool)
	children := make(map[sessionSpanKey][]api.Span)
	var queue []api.Span
	for _, span := range spans {
		children[parentSpanKey(span)] = append(children[parentSpanKey(span)], span)
		for _, key := range sessionIDAttributeKeys {
			if id := span.SpanAttributes[key]; id == "" || id != sessionID {
				continue
			}
			if aliases[span.ServiceName] == nil {
				aliases[span.ServiceName] = make(map[string]bool)
			}
			for _, aliasKey := range sessionIDAttributeKeys {
				if id := span.SpanAttributes[aliasKey]; id != "" {
					aliases[span.ServiceName][id] = true
				}
			}
			selected[spanKey(span)] = true
			queue = append(queue, span)
			break
		}
	}
	for i := 0; i < len(queue); i++ {
		for _, child := range children[spanKey(queue[i])] {
			if selected[spanKey(child)] {
				continue
			}
			hasID, matches := false, false
			for _, key := range sessionIDAttributeKeys {
				if id := child.SpanAttributes[key]; id != "" {
					hasID = true
					matches = matches || aliases[child.ServiceName][id]
				}
			}
			if hasID && !matches {
				continue
			}
			selected[spanKey(child)] = true
			queue = append(queue, child)
		}
	}
	var result []api.Span
	for _, span := range spans {
		if selected[spanKey(span)] {
			result = append(result, span)
			delete(selected, spanKey(span)) // Deduplicate telemetry by span identity.
		}
	}
	return result
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

type spanTranscriptMessage struct {
	Source  sessionSpanKey
	Message api.TranscriptMessage
}

func transcriptMessagesFromSpans(spans []api.Span, logs []sessionLogRow) []spanTranscriptMessage {
	byID := make(map[sessionSpanKey]api.Span, len(spans))
	generated := make(map[sessionSpanKey][]api.TranscriptMessage, len(spans))
	for _, span := range spans {
		byID[spanKey(span)] = span
		generated[spanKey(span)] = transcriptMessagesFromSpan(span)
	}
	logMessages := make(map[sessionSpanKey][]api.TranscriptMessage)
	for _, row := range logs {
		if row.TraceID != "" && row.SpanID != "" {
			key := sessionSpanKey{row.ServiceName, row.TraceID, row.SpanID}
			logMessages[key] = append(logMessages[key], transcriptMessagesFromLogs([]sessionLogRow{row})...)
		}
	}
	// Agent usage aggregates its child calls. Track child replies and usage so
	// parent output is a fallback, without duplicating replies or token counts.
	descendantOutputs := make(map[sessionSpanKey]map[string]bool)
	descendantUsage := make(map[sessionSpanKey]bool)
	for _, span := range spans {
		coverage := append([]api.TranscriptMessage(nil), generated[spanKey(span)]...)
		coverage = append(coverage, logMessages[spanKey(span)]...)
		for _, msg := range coverage {
			if msg.Role != "assistant" {
				continue
			}
			seen := map[sessionSpanKey]bool{spanKey(span): true}
			for parent := parentSpanKey(span); parent.SpanID != "" && !seen[parent]; {
				seen[parent] = true
				if descendantOutputs[parent] == nil {
					descendantOutputs[parent] = make(map[string]bool)
				}
				if msg.Content != "" {
					descendantOutputs[parent][msg.Content] = true
				}
				if msg.InputTokens != 0 || msg.OutputTokens != 0 || msg.CacheRead != 0 || msg.CacheWrite != 0 {
					descendantUsage[parent] = true
				}
				ancestor, ok := byID[parent]
				if !ok {
					break
				}
				parent = parentSpanKey(ancestor)
			}
		}
	}
	var messages []spanTranscriptMessage
	for _, span := range spans {
		for _, msg := range generated[spanKey(span)] {
			if spanOperation(span) == "invoke_agent" && msg.Role == "assistant" {
				if descendantOutputs[spanKey(span)][msg.Content] {
					continue
				}
				if descendantUsage[spanKey(span)] {
					msg.InputTokens, msg.OutputTokens, msg.CacheRead, msg.CacheWrite = 0, 0, 0, 0
				}
			}
			messages = append(messages, spanTranscriptMessage{Source: spanKey(span), Message: msg})
		}
	}
	return messages
}

func spanOperation(span api.Span) string {
	return strings.ToLower(strings.TrimSpace(span.SpanAttributes["gen_ai.operation.name"]))
}

func spanEndTime(span api.Span) time.Time {
	if span.Duration > 0 {
		return span.Timestamp.Add(time.Duration(span.Duration))
	}
	return span.Timestamp
}

func transcriptMessagesFromSpan(span api.Span) []api.TranscriptMessage {
	attrs := span.SpanAttributes
	if attrs == nil {
		return nil
	}

	op := spanOperation(span)
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
		var messages []api.TranscriptMessage
		if content != "" {
			messages = append(messages, api.TranscriptMessage{
				Timestamp: span.Timestamp, Role: "user", Content: content,
				Model: model, DurationMs: durationMs,
			})
		}
		if output := extractGenAIMessageText(attrs["gen_ai.output.messages"], "assistant"); output != "" {
			messages = append(messages, api.TranscriptMessage{
				Timestamp: spanEndTime(span), Role: "assistant", Content: output,
				Model: model, DurationMs: durationMs,
				InputTokens:  parseIntAttr(attrs, "gen_ai.usage.input_tokens", "input_tokens"),
				OutputTokens: parseIntAttr(attrs, "gen_ai.usage.output_tokens", "output_tokens"),
				CacheRead:    parseIntAttr(attrs, "gen_ai.usage.cache_read.input_tokens"),
				CacheWrite:   parseIntAttr(attrs, "gen_ai.usage.cache_creation.input_tokens"),
			})
		}
		return messages

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
				Timestamp:  spanEndTime(span),
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

// Only correlate messages from different signals when they identify the same
// span and content. Equal text in distinct log entries is not a duplicate.
func mergeTranscriptMessages(rows []sessionLogRow, spans []spanTranscriptMessage) []api.TranscriptMessage {
	merged := transcriptMessagesFromLogs(rows)
	if len(spans) == 0 {
		if merged == nil {
			return []api.TranscriptMessage{}
		}
		return merged // Preserve imported message indices.
	}
	type messageKey struct {
		Source                                         sessionSpanKey
		Role, Content, ToolName, ToolInput, ToolOutput string
	}
	keyFor := func(source sessionSpanKey, msg api.TranscriptMessage) messageKey {
		return messageKey{source, msg.Role, msg.Content, msg.ToolName, msg.ToolInput, msg.ToolOutput}
	}
	logMessages := make(map[messageKey]int)
	index := 0
	for _, row := range rows {
		msgs := transcriptMessagesFromLogs([]sessionLogRow{row})
		if len(msgs) == 0 {
			continue
		}
		if row.TraceID != "" && row.SpanID != "" && row.Attrs["event.name"] != "transcript.message" {
			source := sessionSpanKey{row.ServiceName, row.TraceID, row.SpanID}
			logMessages[keyFor(source, msgs[0])] = index
		}
		index++
	}
	for _, span := range spans {
		if i, ok := logMessages[keyFor(span.Source, span.Message)]; ok {
			merged[i] = enrichTranscriptMessage(merged[i], span.Message)
			continue
		}
		merged = append(merged, span.Message)
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

func enrichTranscriptMessage(log, span api.TranscriptMessage) api.TranscriptMessage {
	if log.Model == "" {
		log.Model = span.Model
	}
	if log.InputTokens == 0 {
		log.InputTokens = span.InputTokens
	}
	if log.OutputTokens == 0 {
		log.OutputTokens = span.OutputTokens
	}
	if log.CacheRead == 0 {
		log.CacheRead = span.CacheRead
	}
	if log.CacheWrite == 0 {
		log.CacheWrite = span.CacheWrite
	}
	if log.CostUSD == 0 {
		log.CostUSD = span.CostUSD
	}
	if log.DurationMs == 0 {
		log.DurationMs = span.DurationMs
	}
	if log.Success == nil {
		log.Success = span.Success
	}
	if log.OutputSize == 0 {
		log.OutputSize = span.OutputSize
	}
	return log
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
