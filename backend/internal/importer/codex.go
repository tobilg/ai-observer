package importer

import (
	"bufio"
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

// codexJSONLEntry represents a single line in Codex CLI JSONL files
type codexJSONLEntry struct {
	Timestamp string          `json:"timestamp"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
}

// codexSessionMeta represents session metadata
type codexSessionMeta struct {
	ID            string `json:"id"`
	Timestamp     string `json:"timestamp"`
	Cwd           string `json:"cwd"`
	Originator    string `json:"originator"`
	CliVersion    string `json:"cli_version"`
	ModelProvider string `json:"model_provider"`
	Model         string `json:"model"`
}

// codexEventMsg represents an event message payload
// The actual JSON structure has "info" at the same level as "type", not nested in another "payload"
type codexEventMsg struct {
	Type string           `json:"type"`
	Info *codexTokenInfo  `json:"info"` // Present for token_count events
}

// codexTokenInfo contains token usage information
type codexTokenInfo struct {
	TotalTokenUsage *codexTokenCount `json:"total_token_usage"`
}

// codexTokenCount represents token count data
type codexTokenCount struct {
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

	scanner := bufio.NewScanner(file)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	var sessionMeta *codexSessionMeta
	var currentModel string
	var lastTokenCount *codexTokenCount

	for scanner.Scan() {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}

		var entry codexJSONLEntry
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

		// Update time range
		if result.FirstTime.IsZero() || ts.Before(result.FirstTime) {
			result.FirstTime = ts
		}
		if ts.After(result.LastTime) {
			result.LastTime = ts
		}

		switch entry.Type {
		case "session_meta":
			var meta codexSessionMeta
			if err := json.Unmarshal(entry.Payload, &meta); err == nil {
				sessionMeta = &meta
				if meta.ID != "" {
					result.SessionID = meta.ID
				}
				if meta.Model != "" {
					currentModel = meta.Model
				}

				// Create session start log
				logRecord := api.LogRecord{
					Timestamp:      ts,
					ServiceName:    SourceCodex.ServiceName(),
					SeverityText:   "INFO",
					SeverityNumber: 9,
					Body:           "conversation_starts",
					LogAttributes: map[string]string{
						"event.name":      "codex.conversation_starts",
						"session.id":      meta.ID,
						"model":           meta.Model,
						"model_provider":  meta.ModelProvider,
						"cli_version":     meta.CliVersion,
						"import_source":   "local_jsonl",
					},
				}
				if meta.Cwd != "" {
					logRecord.LogAttributes["cwd"] = meta.Cwd
				}
				result.Logs = append(result.Logs, logRecord)
				result.RecordCount++
			}

		case "event_msg":
			var eventMsg codexEventMsg
			if err := json.Unmarshal(entry.Payload, &eventMsg); err != nil {
				continue
			}

			switch eventMsg.Type {
			case "token_count":
				// Info is at the same level as type in the event_msg payload
				if eventMsg.Info != nil && eventMsg.Info.TotalTokenUsage != nil {
					tokenCount := eventMsg.Info.TotalTokenUsage

					// Handle alternative field name for cached tokens
					cachedTokens := tokenCount.CacheReadInputTokens
					if cachedTokens == 0 {
						cachedTokens = tokenCount.CachedInputTokens
					}

					// Calculate delta from last token count (Codex reports cumulative)
					var deltaInput, deltaOutput, deltaCacheCreation, deltaCacheRead, deltaReasoning, deltaTool int

					if lastTokenCount == nil {
						deltaInput = tokenCount.InputTokens
						deltaOutput = tokenCount.OutputTokens
						deltaCacheCreation = tokenCount.CacheCreationInputTokens
						deltaCacheRead = cachedTokens
						deltaReasoning = tokenCount.ReasoningTokens
						deltaTool = tokenCount.ToolTokens
					} else {
						lastCached := lastTokenCount.CacheReadInputTokens
						if lastCached == 0 {
							lastCached = lastTokenCount.CachedInputTokens
						}
						deltaInput = tokenCount.InputTokens - lastTokenCount.InputTokens
						deltaOutput = tokenCount.OutputTokens - lastTokenCount.OutputTokens
						deltaCacheCreation = tokenCount.CacheCreationInputTokens - lastTokenCount.CacheCreationInputTokens
						deltaCacheRead = cachedTokens - lastCached
						deltaReasoning = tokenCount.ReasoningTokens - lastTokenCount.ReasoningTokens
						deltaTool = tokenCount.ToolTokens - lastTokenCount.ToolTokens
					}

					// Create metrics for non-zero deltas
					if deltaInput > 0 {
						result.Metrics = append(result.Metrics, createCodexTokenMetric(ts, currentModel, "input", float64(deltaInput)))
					}
					if deltaOutput > 0 {
						result.Metrics = append(result.Metrics, createCodexTokenMetric(ts, currentModel, "output", float64(deltaOutput)))
					}
					if deltaCacheCreation > 0 {
						result.Metrics = append(result.Metrics, createCodexTokenMetric(ts, currentModel, "cache_creation", float64(deltaCacheCreation)))
					}
					if deltaCacheRead > 0 {
						result.Metrics = append(result.Metrics, createCodexTokenMetric(ts, currentModel, "cache_read", float64(deltaCacheRead)))
					}
					if deltaReasoning > 0 {
						result.Metrics = append(result.Metrics, createCodexTokenMetric(ts, currentModel, "reasoning", float64(deltaReasoning)))
					}
					if deltaTool > 0 {
						result.Metrics = append(result.Metrics, createCodexTokenMetric(ts, currentModel, "tool", float64(deltaTool)))
					}

					// Calculate and add cost metric
					// Note: cache_read is used for cost calculation (cache_creation tokens are billed at input rate)
					cost := pricing.CalculateCodexCost(currentModel, int64(deltaInput), int64(deltaCacheRead), int64(deltaOutput))
					if cost != nil && *cost > 0 {
						result.Metrics = append(result.Metrics, createCodexCostMetric(ts, currentModel, *cost))
					}

					lastTokenCount = tokenCount
					result.RecordCount++
				}

			case "user_message", "agent_message":
				// Create log record for messages
				logRecord := api.LogRecord{
					Timestamp:      ts,
					ServiceName:    SourceCodex.ServiceName(),
					SeverityText:   "INFO",
					SeverityNumber: 9,
					Body:           eventMsg.Type,
					LogAttributes: map[string]string{
						"event.name":    "codex." + eventMsg.Type,
						"import_source": "local_jsonl",
					},
				}
				if sessionMeta != nil {
					logRecord.LogAttributes["session.id"] = sessionMeta.ID
				}
				result.Logs = append(result.Logs, logRecord)
				result.RecordCount++
			}

		case "turn_context":
			// Extract model from turn context if available
			var turnCtx struct {
				Model string `json:"model"`
			}
			if err := json.Unmarshal(entry.Payload, &turnCtx); err == nil {
				if turnCtx.Model != "" {
					currentModel = turnCtx.Model
				}
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading file: %w", err)
	}

	return result, nil
}

// createCodexTokenMetric creates a token usage metric for Codex
func createCodexTokenMetric(ts time.Time, model, tokenType string, value float64) api.MetricDataPoint {
	return api.MetricDataPoint{
		Timestamp:   ts,
		ServiceName: SourceCodex.ServiceName(),
		MetricName:  "codex_cli_rs.token.usage",
		MetricType:  "sum",
		Value:       &value,
		Attributes: map[string]string{
			"type":          tokenType,
			"model":         model,
			"import_source": "local_jsonl",
		},
	}
}

// createCodexCostMetric creates a cost usage metric for Codex
func createCodexCostMetric(ts time.Time, model string, cost float64) api.MetricDataPoint {
	return api.MetricDataPoint{
		Timestamp:   ts,
		ServiceName: SourceCodex.ServiceName(),
		MetricName:  "codex_cli_rs.cost.usage",
		MetricType:  "sum",
		Value:       &cost,
		Attributes: map[string]string{
			"model":         model,
			"import_source": "local_jsonl",
		},
	}
}
