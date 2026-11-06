// Package transcript assembles session transcripts from the logs and spans
// that storage reads for a session. The tools record conversations
// differently (imported transcript messages, OTLP events, GenAI span
// content); this package turns them into one ordered list of messages.
package transcript

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/tobilg/ai-observer/internal/api"
	"github.com/tobilg/ai-observer/internal/attrval"
	"github.com/tobilg/ai-observer/internal/storage"
	"github.com/tobilg/ai-observer/internal/tools"
)

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
		for _, key := range tools.SessionIDKeys {
			if id := span.SpanAttributes[key]; id == "" || id != sessionID {
				continue
			}
			if aliases[span.ServiceName] == nil {
				aliases[span.ServiceName] = make(map[string]bool)
			}
			for _, aliasKey := range tools.SessionIDKeys {
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
			for _, key := range tools.SessionIDKeys {
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

func transcriptMessagesFromLogs(rows []storage.SessionLog) []api.TranscriptMessage {
	var messages []api.TranscriptMessage
	index := 0
	for _, row := range rows {
		eventName := row.Attrs["event.name"]
		if eventName == "" && strings.Contains(row.Body, ".") && !strings.Contains(row.Body, " ") {
			eventName = row.Body
		}

		role := tools.TranscriptRole(eventName)
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
			ToolName:     getToolName(row.Attrs),
			ToolInput:    getToolInput(row.Attrs),
			ToolOutput:   getToolOutput(row.Attrs),
			InputTokens:  attrval.Int(row.Attrs, "input_tokens", "inputTokens", "gen_ai.usage.input_tokens", "llm.token_count.prompt"),
			OutputTokens: attrval.Int(row.Attrs, "output_tokens", "outputTokens", "gen_ai.usage.output_tokens", "llm.token_count.completion"),
			CacheRead:    attrval.Int(row.Attrs, "cache_read_input_tokens", "cacheRead", "gen_ai.usage.cache_read.input_tokens"),
			CacheWrite:   attrval.Int(row.Attrs, "cache_creation_input_tokens", "cacheWrite", "gen_ai.usage.cache_creation.input_tokens"),
			CostUSD:      attrval.Float(row.Attrs, "cost_usd", "costUsd", "llm.cost"),
			DurationMs:   attrval.Int(row.Attrs, "duration_ms", "durationMs"),
			Success:      attrval.Bool(row.Attrs, "success", "tool_success"),
			OutputSize:   attrval.Int(row.Attrs, "tool_result_size_bytes", "outputSize"),
		})
		index++
	}
	return messages
}

type spanTranscriptMessage struct {
	Source  sessionSpanKey
	Message api.TranscriptMessage
}

func transcriptMessagesFromSpans(spans []api.Span, logs []storage.SessionLog) []spanTranscriptMessage {
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
			logMessages[key] = append(logMessages[key], transcriptMessagesFromLogs([]storage.SessionLog{row})...)
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
				InputTokens:  attrval.Int(attrs, "gen_ai.usage.input_tokens", "input_tokens"),
				OutputTokens: attrval.Int(attrs, "gen_ai.usage.output_tokens", "output_tokens"),
				CacheRead:    attrval.Int(attrs, "gen_ai.usage.cache_read.input_tokens"),
				CacheWrite:   attrval.Int(attrs, "gen_ai.usage.cache_creation.input_tokens"),
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
			InputTokens:  attrval.Int(attrs, "gen_ai.usage.input_tokens", "input_tokens"),
			OutputTokens: attrval.Int(attrs, "gen_ai.usage.output_tokens", "output_tokens"),
			CacheRead:    attrval.Int(attrs, "gen_ai.usage.cache_read.input_tokens"),
			CacheWrite:   attrval.Int(attrs, "gen_ai.usage.cache_creation.input_tokens"),
			DurationMs:   durationMs,
		}
		if content == "" && msg.InputTokens == 0 && msg.OutputTokens == 0 && msg.CacheRead == 0 && msg.CacheWrite == 0 {
			return nil
		}
		return []api.TranscriptMessage{msg}

	case "execute_tool":
		toolName := getToolName(attrs)
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
			success = attrval.Bool(attrs, "success")
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
func mergeTranscriptMessages(rows []storage.SessionLog, spans []spanTranscriptMessage) []api.TranscriptMessage {
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
		msgs := transcriptMessagesFromLogs([]storage.SessionLog{row})
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

// ErrSessionNotFound is returned when no telemetry exists for a session ID
var ErrSessionNotFound = errors.New("session not found")

// Store reads the telemetry of a session (implemented by storage.DuckDBStore)
type Store interface {
	SessionLogs(ctx context.Context, sessionID string) ([]storage.SessionLog, error)
	SessionSpans(ctx context.Context, sessionID string) ([]api.Span, error)
}

// Load returns the transcript of a session from all of its logs and spans.
// It is not limited to a time range: a session's spans can continue long
// after its last log (Copilot sessions are listed from session-start logs,
// but the conversation lives on GenAI spans), and a session can span more
// than the time range it was listed for.
func Load(ctx context.Context, store Store, sessionID string) (*api.TranscriptResponse, error) {
	logRows, err := store.SessionLogs(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	var spans []api.Span
	if needsSpans(logRows) {
		traceSpans, err := store.SessionSpans(ctx, sessionID)
		if err != nil {
			return nil, err
		}
		spans = selectSessionSpans(traceSpans, sessionID)
	}
	if len(logRows) == 0 && len(spans) == 0 {
		return nil, fmt.Errorf("%w: %s", ErrSessionNotFound, sessionID)
	}
	return Build(sessionID, logRows, spans), nil
}

// needsSpans reports whether spans can add to a transcript. Imported
// transcripts already contain the conversation: their records, indices and
// bounds are kept even when live telemetry also exists.
func needsSpans(logRows []storage.SessionLog) bool {
	for _, row := range logRows {
		if row.Attrs["event.name"] == "transcript.message" {
			return false
		}
	}
	return true
}

// Build assembles the transcript of a session from its logs and the spans
// selected for it
func Build(sessionID string, logRows []storage.SessionLog, spans []api.Span) *api.TranscriptResponse {
	messages := mergeTranscriptMessages(logRows, transcriptMessagesFromSpans(spans, logRows))

	serviceName, startTime, lastTime := sessionTranscriptBounds(logRows, spans)
	return &api.TranscriptResponse{
		SessionID:   sessionID,
		ServiceName: serviceName,
		StartTime:   startTime,
		LastTime:    lastTime,
		Messages:    messages,
	}
}

func sessionTranscriptBounds(logRows []storage.SessionLog, spans []api.Span) (string, time.Time, time.Time) {
	var serviceName string
	var startTime, lastTime time.Time

	for _, row := range logRows {
		if serviceName == "" {
			serviceName = row.ServiceName
		}
		if startTime.IsZero() || row.Timestamp.Before(startTime) {
			startTime = row.Timestamp
		}
		if lastTime.IsZero() || row.Timestamp.After(lastTime) {
			lastTime = row.Timestamp
		}
	}
	for _, span := range spans {
		if serviceName == "" {
			serviceName = span.ServiceName
		}
		if startTime.IsZero() || span.Timestamp.Before(startTime) {
			startTime = span.Timestamp
		}
		end := spanEndTime(span)
		if lastTime.IsZero() || end.After(lastTime) {
			lastTime = end
		}
	}
	return serviceName, startTime, lastTime
}

func getModelName(attrs map[string]string) string { return attrval.String(attrs, tools.ModelKeys...) }

func getToolName(attrs map[string]string) string { return attrval.String(attrs, tools.ToolNameKeys...) }

func getToolInput(attrs map[string]string) string {
	return attrval.String(attrs, tools.ToolInputKeys...)
}

func getToolOutput(attrs map[string]string) string {
	return attrval.String(attrs, tools.ToolOutputKeys...)
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

	case "gen_ai.client.inference.operation.details", "copilot_chat.agent.turn":
		if output := extractGenAIMessageText(attrs["gen_ai.output.messages"], "assistant"); output != "" {
			return output
		}
		if output, ok := attrs["gen_ai.output.messages"]; ok && output != "" {
			return output
		}
		if input := extractGenAIMessageText(attrs["gen_ai.input.messages"], "user"); input != "" {
			return input
		}
		return body

	case "copilot_chat.tool.call":
		if result, ok := attrs["gen_ai.tool.call.result"]; ok && result != "" {
			return result
		}
		return body

	default:
		// For other events, use the body as-is
		return body
	}
}
