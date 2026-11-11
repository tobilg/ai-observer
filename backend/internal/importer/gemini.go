package importer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tobilg/ai-observer/internal/api"
	"github.com/tobilg/ai-observer/internal/pricing"
)

// GeminiParser implements SessionParser for Gemini CLI JSON files
type GeminiParser struct {
	geminiPath string
}

// NewGeminiParser creates a new Gemini CLI parser
func NewGeminiParser() *GeminiParser {
	return &GeminiParser{
		geminiPath: getGeminiPath(),
	}
}

// GetGeminiWatchPath returns the path to watch for Gemini CLI sessions
func GetGeminiWatchPath() string { return getGeminiPath() }

// getGeminiPath returns the path to Gemini CLI data
func getGeminiPath() string {
	// Check environment variable override
	if envPath := os.Getenv("AI_OBSERVER_GEMINI_PATH"); envPath != "" {
		return envPath
	}

	// Check GEMINI_HOME
	if geminiHome := os.Getenv("GEMINI_HOME"); geminiHome != "" {
		return filepath.Join(geminiHome, "tmp")
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

	return filepath.Join(homeDir, ".gemini", "tmp")
}

// Source returns the source type
func (p *GeminiParser) Source() SourceType {
	return SourceGemini
}

// FindSessionFiles returns all JSON session files
func (p *GeminiParser) FindSessionFiles(ctx context.Context) ([]string, error) {
	if p.geminiPath == "" {
		return nil, nil
	}

	var files []string

	// Gemini stores sessions in: ~/.gemini/tmp/{project_hash}/chats/session-*.json
	err := filepath.Walk(p.geminiPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip errors, continue walking
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}

		if !info.IsDir() && strings.HasPrefix(filepath.Base(path), "session-") && strings.HasSuffix(path, ".json") {
			files = append(files, path)
		}
		return nil
	})

	if err != nil && err != context.Canceled {
		return nil, fmt.Errorf("walking gemini directory: %w", err)
	}

	return files, nil
}

// GeminiSession represents a Gemini CLI session file
type GeminiSession struct {
	SessionID   string          `json:"sessionId"`
	ProjectHash string          `json:"projectHash"`
	StartTime   string          `json:"startTime"`
	LastUpdated string          `json:"lastUpdated"`
	Messages    []GeminiMessage `json:"messages"`
	Summary     string          `json:"summary,omitempty"`
}

// GeminiMessage represents a message in the session
type GeminiMessage struct {
	ID        string           `json:"id"`
	Timestamp string           `json:"timestamp"`
	Type      string           `json:"type"` // "user", "gemini", "info", "error", "warning"
	Content   string           `json:"content,omitempty"`
	Tokens    *GeminiTokens    `json:"tokens,omitempty"`
	Model     string           `json:"model,omitempty"`
	ToolCalls []GeminiToolCall `json:"toolCalls,omitempty"`
}

// UnmarshalJSON reads a message. Gemini CLI declares its content as a
// PartListUnion (packages/core/src/services/chatRecordingService.ts,
// BaseMessageRecord.content): a string, a Part, or a list of strings and
// Parts. Version 0.24 records strings only, but its readers accept all of
// them (partListUnionToString). Content is the text of the parts, joined
// like partListUnionToString joins them, without thought parts (recorded in
// the message's thoughts); function calls and responses among the parts
// become tool calls unless the message lists them already.
func (m *GeminiMessage) UnmarshalJSON(data []byte) error {
	type plain GeminiMessage
	var aux struct {
		plain
		Content json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	parts, err := geminiParts(aux.Content)
	if err != nil {
		return fmt.Errorf("message content: %w", err)
	}
	*m = GeminiMessage(aux.plain)
	m.Content = geminiPartsText(parts)
	for _, part := range parts {
		if fc := part.FunctionCall; fc != nil && m.toolCall(fc.ID, fc.Name, false) == nil {
			m.ToolCalls = append(m.ToolCalls, GeminiToolCall{ID: fc.ID, Name: fc.Name, Args: fc.Args})
		}
		if fr := part.FunctionResponse; fr != nil {
			result := GeminiToolResults{{FunctionResponse: fr}}
			if call := m.toolCall(fr.ID, fr.Name, true); call != nil {
				if len(call.Result) == 0 {
					call.Result = result
				}
				continue
			}
			m.ToolCalls = append(m.ToolCalls, GeminiToolCall{ID: fr.ID, Name: fr.Name, Result: result})
		}
	}
	return nil
}

// toolCall returns the message's tool call of a call ID, or without one,
// the first of the name (one without result, when forResult)
func (m *GeminiMessage) toolCall(id, name string, forResult bool) *GeminiToolCall {
	for i := range m.ToolCalls {
		call := &m.ToolCalls[i]
		if id != "" && call.ID == id {
			return call
		}
		if id == "" && call.Name == name && (!forResult || len(call.Result) == 0) {
			return call
		}
	}
	return nil
}

// geminiPart is a Part of Gemini's API as Gemini CLI records it
type geminiPart struct {
	Text             *string                 `json:"text,omitempty"`
	Thought          bool                    `json:"thought,omitempty"`
	FunctionCall     *geminiFunctionCall     `json:"functionCall,omitempty"`
	FunctionResponse *GeminiFunctionResponse `json:"functionResponse,omitempty"`
}

type geminiFunctionCall struct {
	ID   string          `json:"id,omitempty"`
	Name string          `json:"name"`
	Args json.RawMessage `json:"args,omitempty"`
}

// geminiParts decodes a PartListUnion: null, a string, a Part, or a list of
// strings and Parts (a string is a text part)
func geminiParts(raw json.RawMessage) ([]geminiPart, error) {
	raw = json.RawMessage(strings.TrimSpace(string(raw)))
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var items []json.RawMessage
	if raw[0] == '[' {
		if err := json.Unmarshal(raw, &items); err != nil {
			return nil, err
		}
	} else {
		items = []json.RawMessage{raw}
	}
	parts := make([]geminiPart, 0, len(items))
	for _, item := range items {
		var part geminiPart
		var text string
		if err := json.Unmarshal(item, &text); err == nil {
			part.Text = &text
		} else if err := json.Unmarshal(item, &part); err != nil {
			return nil, err
		}
		parts = append(parts, part)
	}
	return parts, nil
}

// geminiPartsText joins the text of parts, without thoughts
func geminiPartsText(parts []geminiPart) string {
	var b strings.Builder
	for _, part := range parts {
		if part.Text != nil && !part.Thought {
			b.WriteString(*part.Text)
		}
	}
	return b.String()
}

// GeminiToolCall represents a tool call in a message
type GeminiToolCall struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Args        json.RawMessage   `json:"args,omitempty"`
	Result      GeminiToolResults `json:"result,omitempty"`
	Status      string            `json:"status,omitempty"`
	Timestamp   string            `json:"timestamp,omitempty"`
	DisplayName string            `json:"displayName,omitempty"`
}

// output returns the tool's output: the first function response's, else
// the text of the result; false without a result
func (c GeminiToolCall) output() (string, bool) {
	for _, r := range c.Result {
		if r.FunctionResponse != nil {
			if r.FunctionResponse.Response == nil {
				return "", true
			}
			return r.FunctionResponse.Response.Output, true
		}
	}
	var b strings.Builder
	for _, r := range c.Result {
		b.WriteString(r.Text)
	}
	return b.String(), b.Len() > 0
}

// GeminiToolResults is a tool call's result, a PartListUnion
// (ToolCallRecord.result): Gemini CLI 0.24 records a list of function
// response parts, or null
type GeminiToolResults []GeminiToolResult

func (r *GeminiToolResults) UnmarshalJSON(data []byte) error {
	parts, err := geminiParts(data)
	if err != nil {
		return fmt.Errorf("tool call result: %w", err)
	}
	*r = nil
	for _, part := range parts {
		result := GeminiToolResult{FunctionResponse: part.FunctionResponse}
		if part.Text != nil && !part.Thought {
			result.Text = *part.Text
		}
		*r = append(*r, result)
	}
	return nil
}

// GeminiToolResult represents a part of the result of a tool call
type GeminiToolResult struct {
	FunctionResponse *GeminiFunctionResponse `json:"functionResponse,omitempty"`
	Text             string                  `json:"text,omitempty"`
}

// GeminiFunctionResponse represents the function response
type GeminiFunctionResponse struct {
	ID       string                      `json:"id"`
	Name     string                      `json:"name"`
	Response *GeminiFunctionResponseData `json:"response,omitempty"`
}

// GeminiFunctionResponseData contains the actual response data
type GeminiFunctionResponseData struct {
	Output string `json:"output,omitempty"`
}

// UnmarshalJSON reads a function response, a free-form object: Gemini CLI
// sends {output} for results and {error} for failures; Output is that text,
// else the response's JSON
func (d *GeminiFunctionResponseData) UnmarshalJSON(data []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil || fields == nil {
		d.Output = strings.TrimSpace(string(data))
		return nil
	}
	*d = GeminiFunctionResponseData{}
	for _, key := range []string{"output", "error"} {
		if raw, ok := fields[key]; ok {
			var text string
			if err := json.Unmarshal(raw, &text); err == nil {
				d.Output = text
			} else {
				d.Output = string(raw)
			}
			return nil
		}
	}
	if len(fields) > 0 {
		d.Output = string(data)
	}
	return nil
}

// GeminiTokens represents token counts for a message
type GeminiTokens struct {
	Input    int `json:"input"`
	Output   int `json:"output"`
	Cached   int `json:"cached"`
	Thoughts int `json:"thoughts,omitempty"`
	Tool     int `json:"tool,omitempty"`
	Total    int `json:"total"`
}

// ParseFile parses a Gemini CLI JSON file
func (p *GeminiParser) ParseFile(ctx context.Context, path string) (*ImportResult, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading file: %w", err)
	}

	var session GeminiSession
	if err := json.Unmarshal(data, &session); err != nil {
		return nil, fmt.Errorf("parsing JSON: %w", err)
	}

	hash := sha256.Sum256(data)
	result := &ImportResult{
		FilePath:     path,
		SessionID:    session.SessionID,
		FileHash:     hex.EncodeToString(hash[:]),
		ParsedBytes:  int64(len(data)),
		MessageCount: len(session.Messages),
	}
	// Watch continues from here, including what Gemini CLI adds to the
	// last message later
	watchState, err := NewGeminiWatchState(ctx, &session)
	if err != nil {
		return nil, err
	}
	stateJSON, err := json.Marshal(watchState)
	if err != nil {
		return nil, fmt.Errorf("encoding watch state: %w", err)
	}
	result.ParserState = string(stateJSON)

	// Parse start time
	if session.StartTime != "" {
		if ts, err := ParseGeminiTime(session.StartTime); err == nil {
			result.FirstTime = ts
		}
	}

	// Parse last updated time
	if session.LastUpdated != "" {
		if ts, err := ParseGeminiTime(session.LastUpdated); err == nil {
			result.LastTime = ts
		}
	}

	// Process messages
	records, err := ConvertGeminiMessages(ctx, &session, 0, "local_jsonl")
	if err != nil {
		return nil, err
	}
	result.Logs = records.Logs
	result.Metrics = records.Metrics
	result.RecordCount = len(records.Logs)
	if !records.FirstTime.IsZero() && (result.FirstTime.IsZero() || records.FirstTime.Before(result.FirstTime)) {
		result.FirstTime = records.FirstTime
	}
	if records.LastTime.After(result.LastTime) {
		result.LastTime = records.LastTime
	}

	return result, nil
}

// GeminiWatchState is what watch keeps of a Gemini CLI session file
// (import_state.parser_state); imports record it too
type GeminiWatchState struct {
	MessageCount int `json:"messageCount"`
	// Last describes what was stored of the last message read: Gemini CLI
	// rewrites its session file and adds to its last message later (the
	// tool calls of a response once they ran, the response's tokens when
	// they arrive after its text). nil when unknown (the start of watching
	// without importing, or imports of earlier versions, which recorded only
	// the message count): its records are then taken as complete.
	Last *GeminiLastMessage `json:"last,omitempty"`
}

// GeminiLastMessage is what was stored of a session file's last message
type GeminiLastMessage struct {
	// Logs is the number of its logs (the message and its tool calls'
	// use and result)
	Logs int `json:"logs"`
	// Tokens tells whether its usage was stored
	Tokens bool `json:"tokens"`
}

// NewGeminiWatchState returns the watch state after all messages of session
// were stored
func NewGeminiWatchState(ctx context.Context, session *GeminiSession) (GeminiWatchState, error) {
	state := GeminiWatchState{MessageCount: len(session.Messages)}
	if len(session.Messages) == 0 {
		return state, nil
	}
	last, err := GeminiMessageRecords(ctx, session, len(session.Messages)-1, "")
	if err != nil {
		return state, err
	}
	state.Last = &GeminiLastMessage{Logs: len(last.Logs), Tokens: len(last.Metrics) > 0}
	return state, nil
}

// GeminiMessageRecords returns the records of message i of session, as
// import and watch store them (indexed by the records of the messages before)
func GeminiMessageRecords(ctx context.Context, session *GeminiSession, i int, importSource string) (*GeminiRecords, error) {
	head := *session
	head.Messages = session.Messages[:i+1]
	return ConvertGeminiMessages(ctx, &head, i, importSource)
}

// GeminiRecords are the records converted from Gemini session messages
type GeminiRecords struct {
	Logs    []api.LogRecord
	Metrics []api.MetricDataPoint
	// FirstTime and LastTime are the earliest and latest converted message
	FirstTime, LastTime time.Time
}

// ConvertGeminiMessages converts the messages of session from index start on.
// Import and watch both use it, so they store the same records; importSource
// marks the logs ("local_jsonl" for imports, "file_watcher" for watch).
// Transcript indexes count the records of all messages, so they do not depend
// on whether a session was imported at once or watched message by message.
func ConvertGeminiMessages(ctx context.Context, session *GeminiSession, start int, importSource string) (*GeminiRecords, error) {
	records := &GeminiRecords{}
	logIndex := 0
	for i, msg := range session.Messages {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		ts, err := ParseGeminiTime(msg.Timestamp)
		if err != nil {
			continue
		}
		logs := geminiMessageLogs(session, msg, ts, logIndex, importSource)
		logIndex += len(logs)
		if i < start {
			continue
		}

		if records.FirstTime.IsZero() || ts.Before(records.FirstTime) {
			records.FirstTime = ts
		}
		if ts.After(records.LastTime) {
			records.LastTime = ts
		}
		records.Logs = append(records.Logs, logs...)
		if msg.Type == "gemini" && msg.Tokens != nil {
			records.Metrics = append(records.Metrics, geminiMessageMetrics(ts, session.SessionID, msg.Model, msg.Tokens)...)
		}
	}
	return records, nil
}

// geminiMessageLogs returns the transcript logs of one message: the message
// itself, and a tool use and tool result entry per tool call
func geminiMessageLogs(session *GeminiSession, msg GeminiMessage, ts time.Time, index int, importSource string) []api.LogRecord {
	attrs := map[string]string{
		"event.name":    "transcript.message",
		"session.id":    session.SessionID,
		"message.id":    msg.ID,
		"message.index": fmt.Sprintf("%d", index),
		"message.role":  MapGeminiTypeToRole(msg.Type),
		"import_source": importSource,
	}
	if msg.Model != "" {
		attrs["model"] = msg.Model
	}
	if session.ProjectHash != "" {
		attrs["project_hash"] = session.ProjectHash
	}
	// Token counts for the transcript display
	if msg.Tokens != nil {
		if msg.Tokens.Input > 0 {
			attrs["input_tokens"] = fmt.Sprintf("%d", msg.Tokens.Input)
		}
		if msg.Tokens.Output > 0 {
			attrs["output_tokens"] = fmt.Sprintf("%d", msg.Tokens.Output)
		}
		if msg.Tokens.Cached > 0 {
			attrs["cache_read_input_tokens"] = fmt.Sprintf("%d", msg.Tokens.Cached)
		}
		// The response's other token counts, named as its OTLP
		// api_response event names them: with the above, they identify the
		// response the event records (internal/msgmerge)
		attrs["thoughts_token_count"] = fmt.Sprintf("%d", msg.Tokens.Thoughts)
		attrs["tool_token_count"] = fmt.Sprintf("%d", msg.Tokens.Tool)
	}
	logs := []api.LogRecord{{
		Timestamp:      ts,
		ServiceName:    SourceGemini.ServiceName(),
		SeverityText:   MapGeminiSeverity(msg.Type),
		SeverityNumber: MapGeminiSeverityNumber(msg.Type),
		Body:           msg.Content,
		LogAttributes:  attrs,
	}}

	for _, toolCall := range msg.ToolCalls {
		toolTs := ts // Use message timestamp as fallback
		if toolCall.Timestamp != "" {
			if parsed, err := ParseGeminiTime(toolCall.Timestamp); err == nil {
				toolTs = parsed
			}
		}

		toolUseAttrs := map[string]string{
			"event.name":    "transcript.message",
			"session.id":    session.SessionID,
			"message.index": fmt.Sprintf("%d", index+len(logs)),
			"message.role":  "tool_use",
			"tool.name":     toolCall.Name,
			"import_source": importSource,
		}
		if toolCall.ID != "" {
			toolUseAttrs["tool.call_id"] = toolCall.ID
		}
		if len(toolCall.Args) > 0 {
			toolUseAttrs["tool.input"] = string(toolCall.Args)
		}
		logs = append(logs, api.LogRecord{
			Timestamp:      toolTs,
			ServiceName:    SourceGemini.ServiceName(),
			SeverityText:   "INFO",
			SeverityNumber: 9,
			Body:           fmt.Sprintf("Tool call: %s", toolCall.Name),
			LogAttributes:  toolUseAttrs,
		})

		toolOutput, ok := toolCall.output()
		if !ok {
			continue
		}
		toolResultAttrs := map[string]string{
			"event.name":    "transcript.message",
			"session.id":    session.SessionID,
			"message.index": fmt.Sprintf("%d", index+len(logs)),
			"message.role":  "tool_result",
			"tool.name":     toolCall.Name,
			"import_source": importSource,
		}
		if toolCall.ID != "" {
			toolResultAttrs["tool.call_id"] = toolCall.ID
		}
		if toolCall.Status != "" {
			toolResultAttrs["success"] = fmt.Sprintf("%v", toolCall.Status == "success")
		}
		if toolOutput != "" {
			toolResultAttrs["tool.output"] = toolOutput
		}
		logs = append(logs, api.LogRecord{
			Timestamp:      toolTs,
			ServiceName:    SourceGemini.ServiceName(),
			SeverityText:   "INFO",
			SeverityNumber: 9,
			Body:           toolOutput,
			LogAttributes:  toolResultAttrs,
		})
	}
	return logs
}

// geminiMessageMetrics returns the token usage and cost metrics of one
// response. Token types are named like Gemini CLI's own telemetry (input,
// output, cache, thought, tool), so imported and live data form the same
// series; session.id is the session's, as on Gemini CLI's own points, so a
// response recorded over OTLP as well is counted once
// (storage/usage_overlap.go).
func geminiMessageMetrics(ts time.Time, sessionID, model string, tokens *GeminiTokens) []api.MetricDataPoint {
	var metrics []api.MetricDataPoint
	for _, t := range []struct {
		tokenType string
		count     int
	}{
		{pricing.GeminiTokenTypeInput, tokens.Input},
		{pricing.GeminiTokenTypeOutput, tokens.Output},
		{pricing.GeminiTokenTypeCache, tokens.Cached},
		{pricing.GeminiTokenTypeThought, tokens.Thoughts},
		{pricing.GeminiTokenTypeTool, tokens.Tool},
	} {
		if t.count > 0 {
			metrics = append(metrics, withSession(CreateGeminiTokenMetric(ts, model, t.tokenType, float64(t.count)), sessionID))
		}
	}

	cost := pricing.CalculateGeminiCost(model, int64(tokens.Input), int64(tokens.Cached), int64(tokens.Output), int64(tokens.Thoughts))
	if cost != nil && *cost > 0 {
		metrics = append(metrics, withSession(CreateGeminiCostMetric(ts, model, *cost), sessionID))
	}
	return metrics
}

// withSession returns point with the session.id attribute of sessionID
func withSession(point api.MetricDataPoint, sessionID string) api.MetricDataPoint {
	if sessionID != "" {
		point.Attributes["session.id"] = sessionID
	}
	return point
}

// ParseGeminiTime parses various time formats used by Gemini
func ParseGeminiTime(s string) (time.Time, error) {
	formats := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05.000Z",
		"2006-01-02T15:04:05Z",
	}

	for _, format := range formats {
		if t, err := time.Parse(format, s); err == nil {
			return t, nil
		}
	}

	return time.Time{}, fmt.Errorf("unable to parse time: %s", s)
}

// MapGeminiSeverity maps Gemini message type to OTLP severity
func MapGeminiSeverity(msgType string) string {
	switch msgType {
	case "error":
		return "ERROR"
	case "warning":
		return "WARN"
	case "info":
		return "INFO"
	default:
		return "INFO"
	}
}

// MapGeminiSeverityNumber maps Gemini message type to OTLP severity number
func MapGeminiSeverityNumber(msgType string) int32 {
	switch msgType {
	case "error":
		return 17 // ERROR
	case "warning":
		return 13 // WARN
	case "info":
		return 9 // INFO
	default:
		return 9
	}
}

// MapGeminiEventName maps Gemini message type to event name
func MapGeminiEventName(msgType string) string {
	switch msgType {
	case "gemini":
		return "api_response"
	case "user":
		return "user_prompt"
	case "error":
		return "api_error"
	case "warning":
		return "warning"
	case "info":
		return "info"
	default:
		return msgType
	}
}

// MapGeminiTypeToRole maps Gemini message type to transcript role
func MapGeminiTypeToRole(msgType string) string {
	switch msgType {
	case "user":
		return "user"
	case "gemini":
		return "assistant"
	default:
		return "assistant" // info, warning, error treated as assistant messages
	}
}

// CreateGeminiTokenMetric creates a token usage metric for Gemini
func CreateGeminiTokenMetric(ts time.Time, model, tokenType string, value float64) api.MetricDataPoint {
	return api.MetricDataPoint{
		Timestamp:   ts,
		ServiceName: SourceGemini.ServiceName(),
		MetricName:  "gemini_cli.token.usage",
		MetricType:  "sum",
		Value:       &value,
		Attributes: map[string]string{
			"type":          tokenType,
			"model":         model,
			"import_source": "local_jsonl",
		},
	}
}

// CreateGeminiCostMetric creates a cost usage metric for Gemini
func CreateGeminiCostMetric(ts time.Time, model string, cost float64) api.MetricDataPoint {
	return api.MetricDataPoint{
		Timestamp:   ts,
		ServiceName: SourceGemini.ServiceName(),
		MetricName:  "gemini_cli.cost.usage",
		MetricType:  "sum",
		Value:       &cost,
		Attributes: map[string]string{
			"model":         model,
			"import_source": "local_jsonl",
		},
	}
}
