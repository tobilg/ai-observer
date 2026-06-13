package importer

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/tobilg/ai-observer/internal/api"
	"github.com/tobilg/ai-observer/internal/pricing"
)

// ClaudeParser implements SessionParser for Claude Code JSONL files
type ClaudeParser struct {
	configPaths []string
	pricingMode pricing.PricingMode
}

// NewClaudeParser creates a new Claude Code parser
func NewClaudeParser() *ClaudeParser {
	return &ClaudeParser{
		configPaths: getClaudeConfigPaths(),
		pricingMode: pricing.PricingModeAuto,
	}
}

// SetPricingMode sets the cost calculation mode
func (p *ClaudeParser) SetPricingMode(mode pricing.PricingMode) {
	p.pricingMode = mode
}

// GetClaudeWatchPaths returns the list of paths to watch for Claude Code sessions
func GetClaudeWatchPaths() []string { return getClaudeConfigPaths() }

// getClaudeConfigPaths returns the list of paths to search for Claude Code sessions
func getClaudeConfigPaths() []string {
	var paths []string

	// Check environment variable override
	if envPath := os.Getenv("AI_OBSERVER_CLAUDE_PATH"); envPath != "" {
		for _, p := range strings.Split(envPath, ",") {
			p = strings.TrimSpace(p)
			if p != "" {
				paths = append(paths, p)
			}
		}
		return paths
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return paths
	}

	// New default (XDG): ~/.config/claude/projects/
	xdgPath := filepath.Join(homeDir, ".config", "claude", "projects")
	if _, err := os.Stat(xdgPath); err == nil {
		paths = append(paths, xdgPath)
	}

	// Old default (Legacy): ~/.claude/projects/
	legacyPath := filepath.Join(homeDir, ".claude", "projects")
	if _, err := os.Stat(legacyPath); err == nil {
		paths = append(paths, legacyPath)
	}

	return paths
}

// Source returns the source type
func (p *ClaudeParser) Source() SourceType {
	return SourceClaude
}

// FindSessionFiles returns all JSONL session files
func (p *ClaudeParser) FindSessionFiles(ctx context.Context) ([]string, error) {
	var files []string

	for _, basePath := range p.configPaths {
		err := filepath.Walk(basePath, func(path string, info os.FileInfo, err error) error {
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
			// Log but don't fail
			continue
		}
	}

	return files, nil
}

// ClaudeJSONLEntry represents a single line in Claude Code JSONL files
type ClaudeJSONLEntry struct {
	Type      string         `json:"type,omitempty"` // Root type: "assistant", "user", "queue-operation", etc.
	Timestamp string         `json:"timestamp"`
	SessionID string         `json:"sessionId,omitempty"`
	Version   string         `json:"version,omitempty"`
	Cwd       string         `json:"cwd,omitempty"`
	RequestID string         `json:"requestId,omitempty"`
	CostUSD   *float64       `json:"costUSD,omitempty"`
	Message   *ClaudeMessage `json:"message,omitempty"`
}

type ClaudeMessage struct {
	ID      string          `json:"id,omitempty"`
	Model   string          `json:"model,omitempty"`
	Role    string          `json:"role,omitempty"`
	Type    string          `json:"type,omitempty"`
	Content []ClaudeContent `json:"content,omitempty"`
	Usage   *ClaudeUsage    `json:"usage,omitempty"`
}

type ClaudeContent struct {
	Type      string `json:"type,omitempty"`        // "text", "tool_use", "tool_result"
	Text      string `json:"text,omitempty"`        // message text
	Name      string `json:"name,omitempty"`        // tool name (for tool_use)
	Input     any    `json:"input,omitempty"`       // tool input (for tool_use)
	Content   string `json:"content,omitempty"`     // tool result content (for tool_result)
	ToolUseID string `json:"tool_use_id,omitempty"` // reference to tool_use (for tool_result)
}

type ClaudeUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
}

// ParseFile parses a Claude Code JSONL file
func (p *ClaudeParser) ParseFile(ctx context.Context, path string) (*ImportResult, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening file: %w", err)
	}
	defer file.Close()

	result := &ImportResult{
		FilePath: path,
	}

	// Extract session ID from filename
	filename := filepath.Base(path)
	result.SessionID = strings.TrimSuffix(filename, ".jsonl")

	scanner := bufio.NewScanner(file)
	// Increase buffer size for long lines
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	lineNum := 0
	messageIndex := 0                     // Track message order for transcripts
	seenRequests := make(map[string]bool) // For deduplication of metrics

	for scanner.Scan() {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		lineNum++
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}

		var entry ClaudeJSONLEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			// Skip malformed lines
			continue
		}

		// Skip entries without message
		if entry.Message == nil {
			continue
		}

		// Only process user and assistant entries for transcripts
		if entry.Type != "user" && entry.Type != "assistant" {
			continue
		}

		// Parse timestamp
		ts, err := time.Parse(time.RFC3339Nano, entry.Timestamp)
		if err != nil {
			ts, err = time.Parse(time.RFC3339, entry.Timestamp)
			if err != nil {
				continue // Skip entries with invalid timestamp
			}
		}

		// Update time range
		if result.FirstTime.IsZero() || ts.Before(result.FirstTime) {
			result.FirstTime = ts
		}
		if ts.After(result.LastTime) {
			result.LastTime = ts
		}

		// Use session ID from entry if available
		sessionID := entry.SessionID
		if sessionID == "" {
			sessionID = result.SessionID
		}
		if sessionID != "" && result.SessionID == "" {
			result.SessionID = sessionID
		}

		// Create transcript log records from message content
		transcriptLogs := p.CreateTranscriptLogs(entry, ts, sessionID, &messageIndex)
		result.Logs = append(result.Logs, transcriptLogs...)

		// For assistant entries with usage data, also create metrics
		if entry.Type == "assistant" && entry.Message.Usage != nil {
			// Deduplication using messageId:requestId for metrics only
			dedupKey := fmt.Sprintf("%s:%s", entry.Message.ID, entry.RequestID)
			if entry.Message.ID != "" && entry.RequestID != "" {
				if seenRequests[dedupKey] {
					continue
				}
				seenRequests[dedupKey] = true
			}

			// Create api_request log record (existing behavior)
			logRecord := api.LogRecord{
				Timestamp:      ts,
				ServiceName:    SourceClaude.ServiceName(),
				SeverityText:   "INFO",
				SeverityNumber: 9,
				Body:           "api_request",
				LogAttributes: map[string]string{
					"event.name":    "claude_code.api_request",
					"session.id":    sessionID,
					"model":         entry.Message.Model,
					"import_source": "local_jsonl",
				},
			}
			if entry.Cwd != "" {
				logRecord.LogAttributes["cwd"] = entry.Cwd
			}
			if entry.RequestID != "" {
				logRecord.LogAttributes["request_id"] = entry.RequestID
			}
			result.Logs = append(result.Logs, logRecord)

			// Create metrics
			usage := entry.Message.Usage
			model := entry.Message.Model

			// Token usage metrics (creates both regular and user-facing variants)
			if usage.InputTokens > 0 {
				result.Metrics = append(result.Metrics, CreateTokenMetrics(ts, model, "input", float64(usage.InputTokens))...)
			}
			if usage.OutputTokens > 0 {
				result.Metrics = append(result.Metrics, CreateTokenMetrics(ts, model, "output", float64(usage.OutputTokens))...)
			}
			if usage.CacheCreationInputTokens > 0 {
				result.Metrics = append(result.Metrics, CreateTokenMetrics(ts, model, "cacheCreation", float64(usage.CacheCreationInputTokens))...)
			}
			if usage.CacheReadInputTokens > 0 {
				result.Metrics = append(result.Metrics, CreateTokenMetrics(ts, model, "cacheRead", float64(usage.CacheReadInputTokens))...)
			}

			// Cost metrics using pricing mode (creates both regular and user-facing variants)
			tokenUsage := pricing.ClaudeTokenUsage{
				InputTokens:              int64(usage.InputTokens),
				OutputTokens:             int64(usage.OutputTokens),
				CacheCreationInputTokens: int64(usage.CacheCreationInputTokens),
				CacheReadInputTokens:     int64(usage.CacheReadInputTokens),
			}
			cost := pricing.GetClaudeCostWithMode(p.pricingMode, model, tokenUsage, entry.CostUSD)
			if cost > 0 {
				result.Metrics = append(result.Metrics, CreateCostMetrics(ts, model, cost)...)
			}
		}

		result.RecordCount++
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading file: %w", err)
	}

	return result, nil
}

// CreateTranscriptLogs creates transcript log records from message content
func (p *ClaudeParser) CreateTranscriptLogs(entry ClaudeJSONLEntry, ts time.Time, sessionID string, messageIndex *int) []api.LogRecord {
	var logs []api.LogRecord

	if entry.Message == nil || len(entry.Message.Content) == 0 {
		return logs
	}

	// Determine base role from entry type
	baseRole := entry.Type // "user" or "assistant"

	for _, content := range entry.Message.Content {
		var body string
		var role string
		attrs := map[string]string{
			"event.name":     "transcript.message",
			"session.id":     sessionID,
			"message.index":  strconv.Itoa(*messageIndex),
			"message.role":   baseRole,
			"import_source":  "local_jsonl",
		}

		if entry.Message.Model != "" {
			attrs["model"] = entry.Message.Model
		}
		if entry.Message.ID != "" {
			attrs["message.id"] = entry.Message.ID
		}

		switch content.Type {
		case "text":
			body = content.Text
			role = baseRole
		case "tool_use":
			role = "tool_use"
			attrs["message.role"] = role
			attrs["tool.name"] = content.Name
			if content.Input != nil {
				if inputBytes, err := json.Marshal(content.Input); err == nil {
					attrs["tool.input"] = string(inputBytes)
				}
			}
			body = fmt.Sprintf("Tool call: %s", content.Name)
		case "tool_result":
			role = "tool_result"
			attrs["message.role"] = role
			if content.ToolUseID != "" {
				attrs["tool.use_id"] = content.ToolUseID
			}
			// Store tool output in attrs for consistent extraction
			if content.Content != "" {
				attrs["tool.output"] = content.Content
			}
			body = content.Content
		default:
			// Skip unknown content types
			continue
		}

		// Skip empty content
		if body == "" && content.Type == "text" {
			continue
		}

		logRecord := api.LogRecord{
			Timestamp:      ts,
			ServiceName:    SourceClaude.ServiceName(),
			SeverityText:   "INFO",
			SeverityNumber: 9,
			Body:           body,
			LogAttributes:  attrs,
		}
		logs = append(logs, logRecord)
		*messageIndex++
	}

	return logs
}

// Metric name constants for Claude Code imports
const (
	ClaudeTokenUsageMetric           = "claude_code.token.usage"
	ClaudeUserFacingTokenUsageMetric = "claude_code.token.usage_user_facing"
	ClaudeCostMetric                 = "claude_code.cost.usage"
	ClaudeUserFacingCostMetric       = "claude_code.cost.usage_user_facing"
)

// CreateTokenMetric creates a token usage metric with the specified name
func CreateTokenMetric(ts time.Time, metricName, model, tokenType string, value float64) api.MetricDataPoint {
	return api.MetricDataPoint{
		Timestamp:   ts,
		ServiceName: SourceClaude.ServiceName(),
		MetricName:  metricName,
		MetricType:  "sum",
		Value:       &value,
		Attributes: map[string]string{
			"type":          tokenType,
			"model":         model,
			"import_source": "local_jsonl",
		},
	}
}

// CreateTokenMetrics creates both regular and user-facing token usage metrics.
// JSONL data is already user-facing (only assistant messages with cache tokens),
// so both metrics have identical values for consistency with OTLP-derived metrics.
func CreateTokenMetrics(ts time.Time, model, tokenType string, value float64) []api.MetricDataPoint {
	return []api.MetricDataPoint{
		CreateTokenMetric(ts, ClaudeTokenUsageMetric, model, tokenType, value),
		CreateTokenMetric(ts, ClaudeUserFacingTokenUsageMetric, model, tokenType, value),
	}
}

// CreateCostMetric creates a cost metric with the specified name
func CreateCostMetric(ts time.Time, metricName, model string, value float64) api.MetricDataPoint {
	return api.MetricDataPoint{
		Timestamp:   ts,
		ServiceName: SourceClaude.ServiceName(),
		MetricName:  metricName,
		MetricType:  "sum",
		Value:       &value,
		Attributes: map[string]string{
			"model":         model,
			"import_source": "local_jsonl",
		},
	}
}

// CreateCostMetrics creates both regular and user-facing cost metrics.
// JSONL data is already user-facing (only assistant messages with cache tokens),
// so both metrics have identical values for consistency with OTLP-derived metrics.
func CreateCostMetrics(ts time.Time, model string, value float64) []api.MetricDataPoint {
	return []api.MetricDataPoint{
		CreateCostMetric(ts, ClaudeCostMetric, model, value),
		CreateCostMetric(ts, ClaudeUserFacingCostMetric, model, value),
	}
}
