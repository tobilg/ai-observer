package importer

import (
	"context"
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
	ID        string            `json:"id"`
	Timestamp string            `json:"timestamp"`
	Type      string            `json:"type"` // "user", "gemini", "info", "error", "warning"
	Content   string            `json:"content,omitempty"`
	Tokens    *GeminiTokens     `json:"tokens,omitempty"`
	Model     string            `json:"model,omitempty"`
	ToolCalls []GeminiToolCall  `json:"toolCalls,omitempty"`
}

// GeminiToolCall represents a tool call in a message
type GeminiToolCall struct {
	ID          string              `json:"id"`
	Name        string              `json:"name"`
	Args        json.RawMessage     `json:"args,omitempty"`
	Result      []GeminiToolResult  `json:"result,omitempty"`
	Status      string              `json:"status,omitempty"`
	Timestamp   string              `json:"timestamp,omitempty"`
	DisplayName string              `json:"displayName,omitempty"`
}

// GeminiToolResult represents the result of a tool call
type GeminiToolResult struct {
	FunctionResponse *GeminiFunctionResponse `json:"functionResponse,omitempty"`
}

// GeminiFunctionResponse represents the function response
type GeminiFunctionResponse struct {
	ID       string                    `json:"id"`
	Name     string                    `json:"name"`
	Response *GeminiFunctionResponseData `json:"response,omitempty"`
}

// GeminiFunctionResponseData contains the actual response data
type GeminiFunctionResponseData struct {
	Output string `json:"output,omitempty"`
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

	result := &ImportResult{
		FilePath:  path,
		SessionID: session.SessionID,
	}

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
	messageIndex := 0
	for _, msg := range session.Messages {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		// Parse message timestamp
		ts, err := ParseGeminiTime(msg.Timestamp)
		if err != nil {
			continue
		}

		// Update time range
		if result.FirstTime.IsZero() || ts.Before(result.FirstTime) {
			result.FirstTime = ts
		}
		if ts.After(result.LastTime) {
			result.LastTime = ts
		}

		// Create transcript log record with actual content
		logRecord := api.LogRecord{
			Timestamp:      ts,
			ServiceName:    SourceGemini.ServiceName(),
			SeverityText:   MapGeminiSeverity(msg.Type),
			SeverityNumber: MapGeminiSeverityNumber(msg.Type),
			Body:           msg.Content, // Actual message content!
			LogAttributes: map[string]string{
				"event.name":    "transcript.message",
				"session.id":    session.SessionID,
				"message.id":    msg.ID,
				"message.index": fmt.Sprintf("%d", messageIndex),
				"message.role":  MapGeminiTypeToRole(msg.Type),
				"import_source": "local_jsonl",
			},
		}
		if msg.Model != "" {
			logRecord.LogAttributes["model"] = msg.Model
		}
		if session.ProjectHash != "" {
			logRecord.LogAttributes["project_hash"] = session.ProjectHash
		}
		// Add token counts to attributes for transcript display
		if msg.Tokens != nil {
			if msg.Tokens.Input > 0 {
				logRecord.LogAttributes["input_tokens"] = fmt.Sprintf("%d", msg.Tokens.Input)
			}
			if msg.Tokens.Output > 0 {
				logRecord.LogAttributes["output_tokens"] = fmt.Sprintf("%d", msg.Tokens.Output)
			}
			if msg.Tokens.Cached > 0 {
				logRecord.LogAttributes["cache_read_input_tokens"] = fmt.Sprintf("%d", msg.Tokens.Cached)
			}
		}
		result.Logs = append(result.Logs, logRecord)
		result.RecordCount++
		messageIndex++

		// Create separate log entries for tool calls
		for _, toolCall := range msg.ToolCalls {
			toolTs := ts // Use message timestamp as fallback
			if toolCall.Timestamp != "" {
				if parsed, err := ParseGeminiTime(toolCall.Timestamp); err == nil {
					toolTs = parsed
				}
			}

			// Tool use entry
			toolUseAttrs := map[string]string{
				"event.name":    "transcript.message",
				"session.id":    session.SessionID,
				"message.index": fmt.Sprintf("%d", messageIndex),
				"message.role":  "tool_use",
				"tool.name":     toolCall.Name,
				"import_source": "local_jsonl",
			}
			if len(toolCall.Args) > 0 {
				toolUseAttrs["tool.input"] = string(toolCall.Args)
			}
			toolUseLog := api.LogRecord{
				Timestamp:      toolTs,
				ServiceName:    SourceGemini.ServiceName(),
				SeverityText:   "INFO",
				SeverityNumber: 9,
				Body:           fmt.Sprintf("Tool call: %s", toolCall.Name),
				LogAttributes:  toolUseAttrs,
			}
			result.Logs = append(result.Logs, toolUseLog)
			result.RecordCount++
			messageIndex++

			// Tool result entry (if we have a result)
			if len(toolCall.Result) > 0 && toolCall.Result[0].FunctionResponse != nil {
				fr := toolCall.Result[0].FunctionResponse
				toolOutput := ""
				if fr.Response != nil {
					toolOutput = fr.Response.Output
				}

				toolResultAttrs := map[string]string{
					"event.name":    "transcript.message",
					"session.id":    session.SessionID,
					"message.index": fmt.Sprintf("%d", messageIndex),
					"message.role":  "tool_result",
					"tool.name":     toolCall.Name,
					"import_source": "local_jsonl",
				}
				if toolCall.Status != "" {
					toolResultAttrs["success"] = fmt.Sprintf("%v", toolCall.Status == "success")
				}
				if toolOutput != "" {
					toolResultAttrs["tool.output"] = toolOutput
				}

				toolResultLog := api.LogRecord{
					Timestamp:      toolTs,
					ServiceName:    SourceGemini.ServiceName(),
					SeverityText:   "INFO",
					SeverityNumber: 9,
					Body:           toolOutput,
					LogAttributes:  toolResultAttrs,
				}
				result.Logs = append(result.Logs, toolResultLog)
				result.RecordCount++
				messageIndex++
			}
		}

		// Create metrics for gemini messages with tokens
		if msg.Type == "gemini" && msg.Tokens != nil {
			tokens := msg.Tokens
			model := msg.Model
			var totalCost float64

			if tokens.Input > 0 {
				result.Metrics = append(result.Metrics, CreateGeminiTokenMetric(ts, model, "input", float64(tokens.Input)))
				if cost := pricing.CalculateGeminiCostForTokenType(model, "input", int64(tokens.Input)); cost != nil {
					totalCost += *cost
				}
			}
			if tokens.Output > 0 {
				result.Metrics = append(result.Metrics, CreateGeminiTokenMetric(ts, model, "output", float64(tokens.Output)))
				if cost := pricing.CalculateGeminiCostForTokenType(model, "output", int64(tokens.Output)); cost != nil {
					totalCost += *cost
				}
			}
			if tokens.Cached > 0 {
				result.Metrics = append(result.Metrics, CreateGeminiTokenMetric(ts, model, "cached", float64(tokens.Cached)))
				if cost := pricing.CalculateGeminiCostForTokenType(model, "cache", int64(tokens.Cached)); cost != nil {
					totalCost += *cost
				}
			}
			if tokens.Thoughts > 0 {
				result.Metrics = append(result.Metrics, CreateGeminiTokenMetric(ts, model, "thoughts", float64(tokens.Thoughts)))
				if cost := pricing.CalculateGeminiCostForTokenType(model, "thought", int64(tokens.Thoughts)); cost != nil {
					totalCost += *cost
				}
			}
			if tokens.Tool > 0 {
				result.Metrics = append(result.Metrics, CreateGeminiTokenMetric(ts, model, "tool", float64(tokens.Tool)))
				// Tool tokens don't have direct cost
			}

			// Add cost metric if we calculated any cost
			if totalCost > 0 {
				result.Metrics = append(result.Metrics, CreateGeminiCostMetric(ts, model, totalCost))
			}
		}
	}

	return result, nil
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
