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

// claudeJSONLEntry represents a single line in Claude Code JSONL files
type claudeJSONLEntry struct {
	Type      string         `json:"type,omitempty"` // Root type: "assistant", "user", "queue-operation", etc.
	Timestamp string         `json:"timestamp"`
	SessionID string         `json:"sessionId,omitempty"`
	Version   string         `json:"version,omitempty"`
	Cwd       string         `json:"cwd,omitempty"`
	RequestID string         `json:"requestId,omitempty"`
	CostUSD   *float64       `json:"costUSD,omitempty"`
	Message   *claudeMessage `json:"message,omitempty"`
}

type claudeMessage struct {
	ID    string       `json:"id,omitempty"`
	Model string       `json:"model,omitempty"`
	Role  string       `json:"role,omitempty"`
	Type  string       `json:"type,omitempty"`
	Usage *claudeUsage `json:"usage,omitempty"`
}

type claudeUsage struct {
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
	seenRequests := make(map[string]bool) // For deduplication

	for scanner.Scan() {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		lineNum++
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}

		var entry claudeJSONLEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			// Skip malformed lines
			continue
		}

		// Skip entries without message or usage
		if entry.Message == nil || entry.Message.Usage == nil {
			continue
		}

		// Only process assistant entries (root type field)
		if entry.Type != "assistant" {
			continue
		}

		// Deduplication using messageId:requestId
		dedupKey := fmt.Sprintf("%s:%s", entry.Message.ID, entry.RequestID)
		if entry.Message.ID != "" && entry.RequestID != "" {
			if seenRequests[dedupKey] {
				continue
			}
			seenRequests[dedupKey] = true
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
		if entry.SessionID != "" && result.SessionID == "" {
			result.SessionID = entry.SessionID
		}

		// Create log record
		logRecord := api.LogRecord{
			Timestamp:      ts,
			ServiceName:    SourceClaude.ServiceName(),
			SeverityText:   "INFO",
			SeverityNumber: 9,
			Body:           "api_request",
			LogAttributes: map[string]string{
				"event.name":      "claude_code.api_request",
				"session.id":      entry.SessionID,
				"model":           entry.Message.Model,
				"import_source":   "local_jsonl",
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
			result.Metrics = append(result.Metrics, createTokenMetrics(ts, model, "input", float64(usage.InputTokens))...)
		}
		if usage.OutputTokens > 0 {
			result.Metrics = append(result.Metrics, createTokenMetrics(ts, model, "output", float64(usage.OutputTokens))...)
		}
		if usage.CacheCreationInputTokens > 0 {
			result.Metrics = append(result.Metrics, createTokenMetrics(ts, model, "cacheCreation", float64(usage.CacheCreationInputTokens))...)
		}
		if usage.CacheReadInputTokens > 0 {
			result.Metrics = append(result.Metrics, createTokenMetrics(ts, model, "cacheRead", float64(usage.CacheReadInputTokens))...)
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
			result.Metrics = append(result.Metrics, createCostMetrics(ts, model, cost)...)
		}

		result.RecordCount++
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading file: %w", err)
	}

	return result, nil
}

// Metric name constants for Claude Code imports
const (
	claudeTokenUsageMetric           = "claude_code.token.usage"
	claudeUserFacingTokenUsageMetric = "claude_code.token.usage_user_facing"
	claudeCostMetric                 = "claude_code.cost.usage"
	claudeUserFacingCostMetric       = "claude_code.cost.usage_user_facing"
)

// createTokenMetric creates a token usage metric with the specified name
func createTokenMetric(ts time.Time, metricName, model, tokenType string, value float64) api.MetricDataPoint {
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

// createTokenMetrics creates both regular and user-facing token usage metrics.
// JSONL data is already user-facing (only assistant messages with cache tokens),
// so both metrics have identical values for consistency with OTLP-derived metrics.
func createTokenMetrics(ts time.Time, model, tokenType string, value float64) []api.MetricDataPoint {
	return []api.MetricDataPoint{
		createTokenMetric(ts, claudeTokenUsageMetric, model, tokenType, value),
		createTokenMetric(ts, claudeUserFacingTokenUsageMetric, model, tokenType, value),
	}
}

// createCostMetric creates a cost metric with the specified name
func createCostMetric(ts time.Time, metricName, model string, value float64) api.MetricDataPoint {
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

// createCostMetrics creates both regular and user-facing cost metrics.
// JSONL data is already user-facing (only assistant messages with cache tokens),
// so both metrics have identical values for consistency with OTLP-derived metrics.
func createCostMetrics(ts time.Time, model string, value float64) []api.MetricDataPoint {
	return []api.MetricDataPoint{
		createCostMetric(ts, claudeCostMetric, model, value),
		createCostMetric(ts, claudeUserFacingCostMetric, model, value),
	}
}
