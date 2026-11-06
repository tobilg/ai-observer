package importer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tobilg/ai-observer/internal/api"
	"github.com/tobilg/ai-observer/internal/pricing"
)

// CodexParser implements SessionParser for Codex CLI JSONL files
type CodexParser struct {
	sessionsPath string
}

// NewCodexParser creates a new Codex CLI parser
func NewCodexParser() *CodexParser {
	return &CodexParser{
		sessionsPath: getCodexSessionsPath(),
	}
}

// GetCodexWatchPath returns the path to watch for Codex CLI sessions
func GetCodexWatchPath() string { return getCodexSessionsPath() }

// getCodexSessionsPath returns the path to Codex CLI sessions
func getCodexSessionsPath() string {
	// Check environment variable override
	if envPath := os.Getenv("AI_OBSERVER_CODEX_PATH"); envPath != "" {
		return envPath
	}

	// Check CODEX_HOME
	if codexHome := os.Getenv("CODEX_HOME"); codexHome != "" {
		return filepath.Join(codexHome, "sessions")
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

	return filepath.Join(homeDir, ".codex", "sessions")
}

// Source returns the source type
func (p *CodexParser) Source() SourceType {
	return SourceCodex
}

// FindSessionFiles returns all JSONL session files
func (p *CodexParser) FindSessionFiles(ctx context.Context) ([]string, error) {
	if p.sessionsPath == "" {
		return nil, nil
	}

	var files []string

	err := filepath.Walk(p.sessionsPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip errors, continue walking
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if !info.IsDir() && strings.HasSuffix(path, ".jsonl") {
			files = append(files, path)
		}
		return nil
	})

	if err != nil && err != context.Canceled {
		return nil, fmt.Errorf("walking sessions directory: %w", err)
	}

	return files, nil
}

// CodexJSONLEntry represents a single line in Codex CLI JSONL files
// Format: { "timestamp": "...", "type": "session_meta|response_item|event_msg|...", "payload": {...} }
type CodexJSONLEntry struct {
	Timestamp string          `json:"timestamp"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
}

// CodexResponseItem represents a ResponseItem in the rollout file
// Types: message, function_call, function_call_output, reasoning, local_shell_call, etc.
type CodexResponseItem struct {
	Type string `json:"type"`
	// For "message" type
	Role    string             `json:"role,omitempty"`
	Content []CodexContentItem `json:"content,omitempty"`
	// For "function_call" type
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
	CallID    string `json:"call_id,omitempty"`
	// For "function_call_output" type
	Output json.RawMessage `json:"output,omitempty"`
	// For "reasoning" type
	Summary []CodexReasoningSummary `json:"summary,omitempty"`
}

// CodexContentItem represents content within a message
type CodexContentItem struct {
	Type string `json:"type"` // "input_text", "output_text", "input_image"
	Text string `json:"text,omitempty"`
}

// CodexReasoningSummary represents reasoning summary
type CodexReasoningSummary struct {
	Type string `json:"type"` // "summary_text"
	Text string `json:"text,omitempty"`
}

// codexGitInfo contains git metadata from session_meta
type codexGitInfo struct {
	RepositoryURL string `json:"repository_url"`
	Branch        string `json:"branch"`
}

// CodexSessionMeta represents session metadata
type CodexSessionMeta struct {
	ID            string        `json:"id"`
	Timestamp     string        `json:"timestamp"`
	Cwd           string        `json:"cwd"`
	Originator    string        `json:"originator"`
	CliVersion    string        `json:"cli_version"`
	ModelProvider string        `json:"model_provider"`
	Model         string        `json:"model"`
	Git           *codexGitInfo `json:"git,omitempty"`
}

// CodexEventMsg represents an event message payload
// The actual JSON structure has "info" at the same level as "type", not nested in another "payload"
type CodexEventMsg struct {
	Type string          `json:"type"`
	Info *CodexTokenInfo `json:"info"` // Present for token_count events
}

// CodexTokenInfo contains token usage information
type CodexTokenInfo struct {
	TotalTokenUsage *CodexTokenCount `json:"total_token_usage"`
}

// CodexTokenCount represents token count data
type CodexTokenCount struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
	CachedInputTokens        int `json:"cached_input_tokens"` // Alternative field name
	ReasoningTokens          int `json:"reasoning_output_tokens"`
	ToolTokens               int `json:"tool_tokens"`
}

// ParseFile parses a Codex CLI JSONL file
func (p *CodexParser) ParseFile(ctx context.Context, path string) (*ImportResult, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening file: %w", err)
	}
	defer file.Close()

	result := &ImportResult{
		FilePath: path,
	}

	// Extract session ID from filename (format: rollout-YYYY-MM-DDThh-mm-ss-{id}.jsonl)
	filename := filepath.Base(path)
	result.SessionID = strings.TrimSuffix(filename, ".jsonl")

	codexState := CodexState{SessionID: result.SessionID}
	var attribution RepositoryMetadata

	// Hash exactly the content that is parsed
	hasher := sha256.New()
	for line, err := range jsonlLinesCounted(io.TeeReader(file, hasher), &result.ParsedBytes) {
		if err != nil {
			return nil, fmt.Errorf("reading file: %w", err)
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		var entry CodexJSONLEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}

		// Parse timestamp
		ts, err := time.Parse(time.RFC3339Nano, entry.Timestamp)
		if err != nil {
			ts, err = time.Parse(time.RFC3339, entry.Timestamp)
			if err != nil {
				continue
			}
		}

		attribution.ObserveCodex(entry)

		// Update time range
		if result.FirstTime.IsZero() || ts.Before(result.FirstTime) {
			result.FirstTime = ts
		}
		if ts.After(result.LastTime) {
			result.LastTime = ts
		}

		logs, metrics, records := ConvertCodexEntry(entry, ts, &codexState, "local_jsonl")
		result.Logs = append(result.Logs, logs...)
		result.Metrics = append(result.Metrics, metrics...)
		result.RecordCount += records
	}

	result.FileHash = hex.EncodeToString(hasher.Sum(nil))
	result.SessionID = codexState.SessionID
	result.MessageCount = codexState.MessageIndex

	attribution.Enrich(result.Metrics, result.Logs)
	return result, nil
}

// CodexState is the context a Codex session carries from entry to entry. The
// file watcher keeps it between runs as part of its parser state.
type CodexState struct {
	SessionID      string           `json:"sessionId"`
	CurrentModel   string           `json:"currentModel"`
	MessageIndex   int              `json:"messageIndex"`
	LastTokenCount *CodexTokenCount `json:"lastTokenCount"`
}

// ConvertCodexEntry converts one entry of a Codex session file into records,
// updating state. Import and watch both use it, so they store the same
// records; importSource marks them ("local_jsonl" for imports,
// "file_watcher" for watch). records counts the entries that were recorded.
func ConvertCodexEntry(entry CodexJSONLEntry, ts time.Time, state *CodexState, importSource string) (logs []api.LogRecord, metrics []api.MetricDataPoint, records int) {
	codexLog := func(body string, attrs map[string]string) api.LogRecord {
		attrs["import_source"] = importSource
		return api.LogRecord{
			Timestamp:      ts,
			ServiceName:    SourceCodex.ServiceName(),
			SeverityText:   "INFO",
			SeverityNumber: 9,
			Body:           body,
			LogAttributes:  attrs,
		}
	}
	// transcript adds a transcript message with the next message index
	transcript := func(body, role string, attrs map[string]string) {
		attrs["event.name"] = "transcript.message"
		attrs["session.id"] = state.SessionID
		attrs["message.index"] = fmt.Sprintf("%d", state.MessageIndex)
		attrs["message.role"] = role
		logs = append(logs, codexLog(body, attrs))
		records++
		state.MessageIndex++
	}

	switch entry.Type {
	case "session_meta":
		var meta CodexSessionMeta
		if err := json.Unmarshal(entry.Payload, &meta); err != nil {
			return
		}
		if meta.ID != "" {
			state.SessionID = meta.ID
		}
		if meta.Model != "" {
			state.CurrentModel = meta.Model
		}
		attrs := map[string]string{
			"event.name":     "codex.conversation_starts",
			"session.id":     meta.ID,
			"model":          meta.Model,
			"model_provider": meta.ModelProvider,
			"cli_version":    meta.CliVersion,
		}
		if meta.Cwd != "" {
			attrs["cwd"] = meta.Cwd
		}
		logs = append(logs, codexLog("conversation_starts", attrs))
		records++

	case "event_msg":
		var eventMsg CodexEventMsg
		if err := json.Unmarshal(entry.Payload, &eventMsg); err != nil {
			return
		}
		switch eventMsg.Type {
		case "token_count":
			// Info is at the same level as type in the event_msg payload
			if eventMsg.Info != nil && eventMsg.Info.TotalTokenUsage != nil {
				metrics = codexTokenMetrics(ts, state.CurrentModel, eventMsg.Info.TotalTokenUsage, state.LastTokenCount)
				state.LastTokenCount = eventMsg.Info.TotalTokenUsage
				records++
			}
		case "user_message", "agent_message":
			attrs := map[string]string{"event.name": "codex." + eventMsg.Type}
			if state.SessionID != "" {
				attrs["session.id"] = state.SessionID
			}
			logs = append(logs, codexLog(eventMsg.Type, attrs))
			records++
		}

	case "turn_context":
		// Extract model from turn context if available
		var turnCtx struct {
			Model string `json:"model"`
		}
		if err := json.Unmarshal(entry.Payload, &turnCtx); err == nil && turnCtx.Model != "" {
			state.CurrentModel = turnCtx.Model
		}

	case "response_item":
		// ResponseItem entries hold the transcript content
		var respItem CodexResponseItem
		if err := json.Unmarshal(entry.Payload, &respItem); err != nil {
			return
		}
		modelAttrs := func() map[string]string {
			attrs := map[string]string{}
			if state.CurrentModel != "" {
				attrs["model"] = state.CurrentModel
			}
			return attrs
		}

		switch respItem.Type {
		case "message":
			var textContent string
			for _, content := range respItem.Content {
				if content.Type == "input_text" || content.Type == "output_text" {
					if textContent != "" {
						textContent += "\n"
					}
					textContent += content.Text
				}
			}
			if textContent == "" {
				return
			}
			role := respItem.Role
			if role == "" {
				role = "assistant"
			}
			transcript(textContent, role, modelAttrs())

		case "function_call":
			attrs := map[string]string{"tool.name": respItem.Name}
			if respItem.Arguments != "" {
				attrs["tool.input"] = respItem.Arguments
			}
			if respItem.CallID != "" {
				attrs["tool.call_id"] = respItem.CallID
			}
			transcript(fmt.Sprintf("Tool call: %s", respItem.Name), "tool_use", attrs)

		case "function_call_output":
			// The output is a string or a JSON object
			var outputContent string
			if len(respItem.Output) > 0 {
				var strOutput string
				if err := json.Unmarshal(respItem.Output, &strOutput); err == nil {
					outputContent = strOutput
				} else {
					outputContent = string(respItem.Output)
				}
			}
			attrs := map[string]string{}
			if outputContent != "" {
				attrs["tool.output"] = outputContent
			}
			if respItem.CallID != "" {
				attrs["tool.call_id"] = respItem.CallID
			}
			transcript(outputContent, "tool_result", attrs)

		case "reasoning":
			var reasoningText string
			for _, summary := range respItem.Summary {
				if summary.Type == "summary_text" && summary.Text != "" {
					if reasoningText != "" {
						reasoningText += "\n"
					}
					reasoningText += summary.Text
				}
			}
			if reasoningText == "" {
				return
			}
			transcript(reasoningText, "assistant", modelAttrs()) // Reasoning is from the assistant
		}
	}
	return
}

// codexTokenMetrics returns the token and cost metrics of a token_count
// event. Codex reports running totals, so the metrics hold the increase over
// the previous event's totals (last, nil for the first event).
func codexTokenMetrics(ts time.Time, model string, total, last *CodexTokenCount) []api.MetricDataPoint {
	cached := func(c *CodexTokenCount) int {
		// Handle alternative field name for cached tokens
		if c.CacheReadInputTokens != 0 {
			return c.CacheReadInputTokens
		}
		return c.CachedInputTokens
	}
	if last == nil {
		last = &CodexTokenCount{}
	}
	deltaInput := total.InputTokens - last.InputTokens
	deltaCacheRead := cached(total) - cached(last)
	deltaOutput := total.OutputTokens - last.OutputTokens

	var metrics []api.MetricDataPoint
	for _, t := range []struct {
		tokenType string
		delta     int
	}{
		{"input", deltaInput},
		{"output", deltaOutput},
		{"cache_creation", total.CacheCreationInputTokens - last.CacheCreationInputTokens},
		{"cache_read", deltaCacheRead},
		{"reasoning", total.ReasoningTokens - last.ReasoningTokens},
		{"tool", total.ToolTokens - last.ToolTokens},
	} {
		if t.delta > 0 {
			metrics = append(metrics, CreateCodexTokenMetric(ts, model, t.tokenType, float64(t.delta)))
		}
	}

	// cache_read is used for cost calculation (cache_creation tokens are billed at input rate)
	cost := pricing.CalculateCodexCost(model, int64(deltaInput), int64(deltaCacheRead), int64(deltaOutput))
	if cost != nil && *cost > 0 {
		metrics = append(metrics, CreateCodexCostMetric(ts, model, *cost))
	}
	return metrics
}

// CreateCodexTokenMetric creates a token usage metric. Importers and watchers
// attach repository context to the completed batch.
func CreateCodexTokenMetric(ts time.Time, model, tokenType string, value float64) api.MetricDataPoint {
	attrs := map[string]string{
		"type":          tokenType,
		"model":         model,
		"import_source": "local_jsonl",
	}
	return api.MetricDataPoint{
		Timestamp:   ts,
		ServiceName: SourceCodex.ServiceName(),
		MetricName:  "codex_cli_rs.token.usage",
		MetricType:  "sum",
		Value:       &value,
		Attributes:  attrs,
	}
}

// CreateCodexCostMetric creates a cost metric. Importers and watchers attach
// repository context to the completed batch.
func CreateCodexCostMetric(ts time.Time, model string, cost float64) api.MetricDataPoint {
	attrs := map[string]string{
		"model":         model,
		"import_source": "local_jsonl",
	}
	return api.MetricDataPoint{
		Timestamp:   ts,
		ServiceName: SourceCodex.ServiceName(),
		MetricName:  "codex_cli_rs.cost.usage",
		MetricType:  "sum",
		Value:       &cost,
		Attributes:  attrs,
	}
}
