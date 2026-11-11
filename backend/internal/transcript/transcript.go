// Package transcript assembles session transcripts from the logs and spans
// that storage reads for a session. The tools record conversations
// differently (imported transcript messages, OTLP events, GenAI span
// content); this package turns them into one ordered list of messages.
package transcript

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/tobilg/ai-observer/internal/api"
	"github.com/tobilg/ai-observer/internal/attrval"
	"github.com/tobilg/ai-observer/internal/msgmerge"
	"github.com/tobilg/ai-observer/internal/redact"
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

// transcriptMessagesFromLogs returns the transcript messages of logs, as
// recorded (mergeTranscriptMessages merges the records of one message)
func transcriptMessagesFromLogs(rows []storage.SessionLog) []api.TranscriptMessage {
	records := logRecords(rows)
	messages := make([]api.TranscriptMessage, len(records))
	for i, r := range records {
		messages[i] = r.msg
	}
	return messages
}

// logMessage returns the transcript message of a log and its event name,
// or false when the log is no message. Its index is set by the caller.
func logMessage(row storage.SessionLog) (api.TranscriptMessage, string, bool) {
	return msgmerge.LogMessage(row.Timestamp, row.Body, row.Attrs)
}

type spanTranscriptMessage struct {
	Source  sessionSpanKey
	Message api.TranscriptMessage
	// ToolCallID is the ID of the tool call a tool message of the span
	// belongs to, if the span names one (tools.ToolCallIDKeys)
	ToolCallID string
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
			messages = append(messages, spanTranscriptMessage{Source: spanKey(span), Message: msg, ToolCallID: toolCallID(span.SpanAttributes, msg.Role)})
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

func extractGenAIMessageText(raw, role string) string { return msgmerge.GenAIMessageText(raw, role) }

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

// redactingStore is a Store that redacts telemetry before storing it
// (storage.DuckDBStore): transcript titles are redacted with its redactor,
// as session lists redact them
type redactingStore interface {
	Redactor() *redact.Redactor
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
	resp := Build(sessionID, logRows, spans)
	if r, ok := store.(redactingStore); ok {
		resp.Title = title(resp.Messages, r.Redactor().String)
	}
	return resp, nil
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
// selected for it. Its title is not redacted (Load redacts it).
func Build(sessionID string, logRows []storage.SessionLog, spans []api.Span) *api.TranscriptResponse {
	messages := mergeTranscriptMessages(logRows, transcriptMessagesFromSpans(spans, logRows))

	serviceName, startTime, lastTime := sessionTranscriptBounds(logRows, spans)
	return &api.TranscriptResponse{
		SessionID:   sessionID,
		ServiceName: serviceName,
		Title:       title(messages, nil),
		StartTime:   startTime,
		LastTime:    lastTime,
		Messages:    messages,
		Totals:      totals(messages),
		Total:       len(messages),
	}
}

// title is the title of a transcript: of its first user message that gives
// one (storage.SessionTitle), as session lists show it
func title(messages []api.TranscriptMessage, redact func(string) string) string {
	for _, m := range messages {
		if m.Role != tools.RoleUser {
			continue
		}
		if t := storage.SessionTitle(m.Content, redact); t != "" {
			return t
		}
	}
	return ""
}

// totals sums the usage of messages and counts them by role
func totals(messages []api.TranscriptMessage) *api.TranscriptTotals {
	var t api.TranscriptTotals
	for _, m := range messages {
		t.CostUSD += m.CostUSD
		t.InputTokens += int64(m.InputTokens)
		t.OutputTokens += int64(m.OutputTokens)
		t.CacheRead += int64(m.CacheRead)
		t.CacheWrite += int64(m.CacheWrite)
		switch m.Role {
		case tools.RoleUser:
			t.UserMessages++
		case tools.RoleAssistant:
			t.AssistantMessages++
		case tools.RoleToolUse:
			t.ToolCalls++
		case tools.RoleToolResult:
			if m.Success != nil && !*m.Success {
				t.ToolErrors++
			}
		}
	}
	return &t
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

func getModelName(attrs map[string]string) string { return msgmerge.ModelName(attrs) }

func getToolName(attrs map[string]string) string { return msgmerge.ToolName(attrs) }

func getToolInput(attrs map[string]string) string { return msgmerge.ToolInput(attrs) }

func getToolOutput(attrs map[string]string) string { return msgmerge.ToolOutput(attrs) }
