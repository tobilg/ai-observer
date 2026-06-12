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
	Type      string         `json:"type,omitempty"` // Root type: "assistant", "user", "pr-link", "system", etc.
	Timestamp string         `json:"timestamp"`
	SessionID string         `json:"sessionId,omitempty"`
	Version   string         `json:"version,omitempty"`
	Cwd       string         `json:"cwd,omitempty"`
	RequestID string         `json:"requestId,omitempty"`
	CostUSD   *float64       `json:"costUSD,omitempty"`
	Message   *ClaudeMessage `json:"message,omitempty"`
	GitBranch string         `json:"gitBranch,omitempty"`
	// PR link fields (present when type == "pr-link")
	PRNumber     int    `json:"prNumber,omitempty"`
	PRUrl        string `json:"prUrl,omitempty"`
	PRRepository string `json:"prRepository,omitempty"`
	// System entry fields (present when type == "system")
	Subtype    string  `json:"subtype,omitempty"`
	DurationMs float64 `json:"durationMs,omitempty"`
	// Worktree-state fields (present when type == "worktree-state")
	WorktreeSession *claudeWorktreeSession `json:"worktreeSession,omitempty"`
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

// claudeEditInput holds the input for Edit tool calls
type claudeEditInput struct {
	FilePath  string `json:"file_path"`
	OldString string `json:"old_string"`
	NewString string `json:"new_string"`
}

// claudeWriteInput holds the input for Write tool calls
type claudeWriteInput struct {
	FilePath string `json:"file_path"`
	Content  string `json:"content"`
}

// claudeMultiEditInput holds the input for MultiEdit tool calls
type claudeMultiEditInput struct {
	FilePath string `json:"file_path"`
	Edits    []struct {
		OldString string `json:"old_string"`
		NewString string `json:"new_string"`
	} `json:"edits"`
}

// claudeWorktreeSession holds fields from a "worktree-state" entry's worktreeSession object.
type claudeWorktreeSession struct {
	OriginalCwd string `json:"originalCwd,omitempty"`
}

// claudeSessionMeta holds session-level metadata collected in a first pass
type claudeSessionMeta struct {
	GitBranch      string
	Cwd            string
	OriginalCwd    string   // from worktree-state.worktreeSession.originalCwd
	CandidateRepos []string // owner/repo references from pr-link entries (most-frequent first)
	PRNumber       int
	PRUrl          string
	PRRepository   string
	PRTimestamp    time.Time
}

// collectSessionMeta does a first pass over raw JSONL lines to collect session-level metadata
func (p *ClaudeParser) collectSessionMeta(lines []string) claudeSessionMeta {
	meta := claudeSessionMeta{}
	seenPRs := make(map[int]bool)
	seenRepos := make(map[string]bool)

	for _, line := range lines {
		var entry ClaudeJSONLEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		if entry.GitBranch != "" && meta.GitBranch == "" {
			meta.GitBranch = entry.GitBranch
		}
		if entry.Cwd != "" && meta.Cwd == "" {
			meta.Cwd = entry.Cwd
		}
		if entry.Type == "worktree-state" && entry.WorktreeSession != nil {
			if oc := entry.WorktreeSession.OriginalCwd; oc != "" && meta.OriginalCwd == "" {
				meta.OriginalCwd = oc
			}
		}
		if entry.Type == "pr-link" && entry.PRNumber != 0 && !seenPRs[entry.PRNumber] {
			seenPRs[entry.PRNumber] = true
			// Collect all pr-link repositories as candidates for resolution.
			if r := entry.PRRepository; r != "" && !seenRepos[r] {
				seenRepos[r] = true
				meta.CandidateRepos = append(meta.CandidateRepos, r)
			}
			// Use the first PR linked in the session for structured PR metadata.
			if meta.PRNumber == 0 {
				meta.PRNumber = entry.PRNumber
				meta.PRUrl = entry.PRUrl
				meta.PRRepository = entry.PRRepository
				if ts, err := time.Parse(time.RFC3339Nano, entry.Timestamp); err == nil {
					meta.PRTimestamp = ts
				} else if ts, err := time.Parse(time.RFC3339, entry.Timestamp); err == nil {
					meta.PRTimestamp = ts
				}
			}
		}
	}
	return meta
}

// extractRepository returns the best repository identifier for a session.
//
// Priority:
//  1. PRRepository from a structured pr-link entry (authoritative - this is
//     exactly where the linked PR lives, not just a text-mined reference).
//  2. resolveSessionRepository: on-disk git remote -> matching candidate -> cwd name.
func extractRepository(meta claudeSessionMeta) string {
	if meta.PRRepository != "" {
		return meta.PRRepository
	}
	return resolveSessionRepository(meta.CandidateRepos, "", meta.OriginalCwd, meta.Cwd)
}

// countLines returns the number of lines in s (at least 1 for non-empty strings)
func countLines(s string) int {
	if s == "" {
		return 0
	}
	return strings.Count(s, "\n") + 1
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

	// Collect all non-empty lines
	var lines []string
	for scanner.Scan() {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if line := scanner.Text(); strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading file: %w", err)
	}

	// First pass: collect session-level metadata (gitBranch, PR info, cwd)
	meta := p.collectSessionMeta(lines)
	repository := extractRepository(meta)

	// Emit pull_request.count metric if a PR was linked
	if meta.PRNumber != 0 && !meta.PRTimestamp.IsZero() {
		result.Metrics = append(result.Metrics, createPRMetric(meta, repository))
	}

	messageIndex := 0
	seenRequests := make(map[string]bool) // For deduplication of metrics

	// Second pass: process records
	for _, line := range lines {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		var entry ClaudeJSONLEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}

		// Handle system/turn_duration entries for active time (no message field)
		if entry.Type == "system" && entry.Subtype == "turn_duration" && entry.DurationMs > 0 {
			ts, err := time.Parse(time.RFC3339Nano, entry.Timestamp)
			if err != nil {
				ts, err = time.Parse(time.RFC3339, entry.Timestamp)
			}
			if err == nil {
				if result.FirstTime.IsZero() || ts.Before(result.FirstTime) {
					result.FirstTime = ts
				}
				if ts.After(result.LastTime) {
					result.LastTime = ts
				}
				secs := entry.DurationMs / 1000.0
				result.Metrics = append(result.Metrics, createActiveTimeMetric(ts, secs, meta, repository))
			}
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
		transcriptLogs := p.createTranscriptLogs(entry, ts, sessionID, meta, &messageIndex)
		result.Logs = append(result.Logs, transcriptLogs...)

		// For assistant entries: create metrics and process file-editing tool calls
		if entry.Type == "assistant" {
			// Deduplication using messageId:requestId for metrics only
			dedupKey := fmt.Sprintf("%s:%s", entry.Message.ID, entry.RequestID)
			if entry.Message.ID != "" && entry.RequestID != "" {
				if seenRequests[dedupKey] {
					continue
				}
				seenRequests[dedupKey] = true
			}

			// Lines of code from file-editing tool calls
			locMetrics := p.extractLinesOfCodeMetrics(entry, ts, meta, repository)
			result.Metrics = append(result.Metrics, locMetrics...)

			// Commits from Bash tool calls
			commitMetrics := p.extractCommitMetrics(entry, ts, meta, repository)
			result.Metrics = append(result.Metrics, commitMetrics...)

			if entry.Message.Usage != nil {
				// Create api_request log record
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
				if meta.GitBranch != "" {
					logRecord.LogAttributes["git_branch"] = meta.GitBranch
				}
				if repository != "" {
					logRecord.LogAttributes["repository"] = repository
				}
				result.Logs = append(result.Logs, logRecord)

				// Token usage metrics
				usage := entry.Message.Usage
				model := entry.Message.Model

				if usage.InputTokens > 0 {
					result.Metrics = append(result.Metrics, createTokenMetrics(ts, model, "input", float64(usage.InputTokens), meta, repository)...)
				}
				if usage.OutputTokens > 0 {
					result.Metrics = append(result.Metrics, createTokenMetrics(ts, model, "output", float64(usage.OutputTokens), meta, repository)...)
				}
				if usage.CacheCreationInputTokens > 0 {
					result.Metrics = append(result.Metrics, createTokenMetrics(ts, model, "cacheCreation", float64(usage.CacheCreationInputTokens), meta, repository)...)
				}
				if usage.CacheReadInputTokens > 0 {
					result.Metrics = append(result.Metrics, createTokenMetrics(ts, model, "cacheRead", float64(usage.CacheReadInputTokens), meta, repository)...)
				}

				// Cost metrics
				tokenUsage := pricing.ClaudeTokenUsage{
					InputTokens:              int64(usage.InputTokens),
					OutputTokens:             int64(usage.OutputTokens),
					CacheCreationInputTokens: int64(usage.CacheCreationInputTokens),
					CacheReadInputTokens:     int64(usage.CacheReadInputTokens),
				}
				cost := pricing.GetClaudeCostWithMode(p.pricingMode, model, tokenUsage, entry.CostUSD)
				if cost > 0 {
					result.Metrics = append(result.Metrics, createCostMetrics(ts, model, cost, meta, repository)...)
				}
			}
		}

		result.RecordCount++
	}

	// Session count metric - one per file, timestamped at first activity
	if !result.FirstTime.IsZero() {
		result.Metrics = append(result.Metrics, createSessionMetric(result.FirstTime, meta, repository))
	}

	return result, nil
}

// CreateTranscriptLogs creates transcript log records from message content.
// This public form is used by the file watcher; ParseFile uses createTranscriptLogs
// (private) which also enriches records with session metadata.
func (p *ClaudeParser) CreateTranscriptLogs(entry ClaudeJSONLEntry, ts time.Time, sessionID string, messageIndex *int) []api.LogRecord {
	return p.createTranscriptLogs(entry, ts, sessionID, claudeSessionMeta{}, messageIndex)
}

// extractLinesOfCodeMetrics parses file-editing tool calls in an assistant entry and returns
// claude_code.lines_of_code.count metrics broken down by file type and path.
func (p *ClaudeParser) extractLinesOfCodeMetrics(entry ClaudeJSONLEntry, ts time.Time, meta claudeSessionMeta, repository string) []api.MetricDataPoint {
	var metrics []api.MetricDataPoint

	for _, content := range entry.Message.Content {
		if content.Type != "tool_use" {
			continue
		}

		// Marshal input back to JSON so we can decode into typed structs
		inputBytes, err := json.Marshal(content.Input)
		if err != nil {
			continue
		}

		switch content.Name {
		case "Edit":
			var inp claudeEditInput
			if err := json.Unmarshal(inputBytes, &inp); err != nil || inp.FilePath == "" {
				continue
			}
			removed := countLines(inp.OldString)
			added := countLines(inp.NewString)
			fileType := filepath.Ext(inp.FilePath)
			relPath := relativeFilePath(inp.FilePath, meta.Cwd)
			if removed > 0 {
				metrics = append(metrics, createLOCMetric(ts, "removed", relPath, fileType, float64(removed), meta, repository))
			}
			if added > 0 {
				metrics = append(metrics, createLOCMetric(ts, "added", relPath, fileType, float64(added), meta, repository))
			}

		case "Write":
			var inp claudeWriteInput
			if err := json.Unmarshal(inputBytes, &inp); err != nil || inp.FilePath == "" {
				continue
			}
			added := countLines(inp.Content)
			fileType := filepath.Ext(inp.FilePath)
			relPath := relativeFilePath(inp.FilePath, meta.Cwd)
			if added > 0 {
				metrics = append(metrics, createLOCMetric(ts, "added", relPath, fileType, float64(added), meta, repository))
			}

		case "MultiEdit":
			var inp claudeMultiEditInput
			if err := json.Unmarshal(inputBytes, &inp); err != nil || inp.FilePath == "" {
				continue
			}
			fileType := filepath.Ext(inp.FilePath)
			relPath := relativeFilePath(inp.FilePath, meta.Cwd)
			var totalRemoved, totalAdded int
			for _, edit := range inp.Edits {
				totalRemoved += countLines(edit.OldString)
				totalAdded += countLines(edit.NewString)
			}
			if totalRemoved > 0 {
				metrics = append(metrics, createLOCMetric(ts, "removed", relPath, fileType, float64(totalRemoved), meta, repository))
			}
			if totalAdded > 0 {
				metrics = append(metrics, createLOCMetric(ts, "added", relPath, fileType, float64(totalAdded), meta, repository))
			}
		}
	}

	return metrics
}

// claudeBashInput holds the input for Bash tool calls
type claudeBashInput struct {
	Command string `json:"command"`
}

// extractCommitMetrics parses Bash tool calls in an assistant entry and returns
// claude_code.commit.count metrics for git commit commands.
func (p *ClaudeParser) extractCommitMetrics(entry ClaudeJSONLEntry, ts time.Time, meta claudeSessionMeta, repository string) []api.MetricDataPoint {
	var metrics []api.MetricDataPoint

	for _, content := range entry.Message.Content {
		if content.Type != "tool_use" || content.Name != "Bash" {
			continue
		}
		inputBytes, err := json.Marshal(content.Input)
		if err != nil {
			continue
		}
		var inp claudeBashInput
		if err := json.Unmarshal(inputBytes, &inp); err != nil {
			continue
		}
		if strings.Contains(inp.Command, "git commit") {
			metrics = append(metrics, createCommitMetric(ts, meta, repository))
		}
	}

	return metrics
}

// relativeFilePath returns a path relative to baseDir, falling back to the base filename
func relativeFilePath(filePath, baseDir string) string {
	if baseDir == "" {
		return filepath.Base(filePath)
	}
	if rel, err := filepath.Rel(baseDir, filePath); err == nil && !strings.HasPrefix(rel, "..") {
		return rel
	}
	return filepath.Base(filePath)
}

// createTranscriptLogs creates transcript log records with optional session metadata enrichment.
func (p *ClaudeParser) createTranscriptLogs(entry ClaudeJSONLEntry, ts time.Time, sessionID string, meta claudeSessionMeta, messageIndex *int) []api.LogRecord {
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
			"event.name":    "transcript.message",
			"session.id":    sessionID,
			"message.index": strconv.Itoa(*messageIndex),
			"message.role":  baseRole,
			"import_source": "local_jsonl",
		}

		if entry.Message.Model != "" {
			attrs["model"] = entry.Message.Model
		}
		if entry.Message.ID != "" {
			attrs["message.id"] = entry.Message.ID
		}
		if meta.GitBranch != "" {
			attrs["git_branch"] = meta.GitBranch
		}
		if repo := extractRepository(meta); repo != "" {
			attrs["repository"] = repo
		}
		if meta.PRNumber != 0 {
			attrs["pr_number"] = strconv.Itoa(meta.PRNumber)
			attrs["pr_repository"] = meta.PRRepository
			attrs["pr_url"] = meta.PRUrl
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
	claudeLinesOfCodeMetric          = "claude_code.lines_of_code.count"
	claudePullRequestMetric          = "claude_code.pull_request.count"
	claudeCommitMetric               = "claude_code.commit.count"
	claudeSessionMetric              = "claude_code.session.count"
	claudeActiveTimeMetric           = "claude_code.active_time.total"
)

// CreateTokenMetric creates a token usage metric with the specified name.
// This public form is used by the file watcher; ParseFile uses createTokenMetric
// (private) which also attaches session attributes.
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

// sessionAttrs builds the common session-level attributes (git_branch, repository).
func sessionAttrs(meta claudeSessionMeta, repository string) map[string]string {
	attrs := make(map[string]string)
	if meta.GitBranch != "" {
		attrs["git_branch"] = meta.GitBranch
	}
	if repository != "" {
		attrs["repository"] = repository
	}
	return attrs
}

// createTokenMetric creates a token usage metric with the specified name, including session attributes.
func createTokenMetric(ts time.Time, metricName, model, tokenType string, value float64, meta claudeSessionMeta, repository string) api.MetricDataPoint {
	attrs := sessionAttrs(meta, repository)
	attrs["type"] = tokenType
	attrs["model"] = model
	attrs["import_source"] = "local_jsonl"
	return api.MetricDataPoint{
		Timestamp:   ts,
		ServiceName: SourceClaude.ServiceName(),
		MetricName:  metricName,
		MetricType:  "sum",
		Value:       &value,
		Attributes:  attrs,
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

// createTokenMetrics creates both regular and user-facing token usage metrics with session attributes.
func createTokenMetrics(ts time.Time, model, tokenType string, value float64, meta claudeSessionMeta, repository string) []api.MetricDataPoint {
	return []api.MetricDataPoint{
		createTokenMetric(ts, ClaudeTokenUsageMetric, model, tokenType, value, meta, repository),
		createTokenMetric(ts, ClaudeUserFacingTokenUsageMetric, model, tokenType, value, meta, repository),
	}
}

// CreateCostMetric creates a cost metric with the specified name.
// This public form is used by the file watcher; ParseFile uses createCostMetric
// (private) which also attaches session attributes.
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

// createCostMetric creates a cost metric with the specified name, including session attributes.
func createCostMetric(ts time.Time, metricName, model string, value float64, meta claudeSessionMeta, repository string) api.MetricDataPoint {
	attrs := sessionAttrs(meta, repository)
	attrs["model"] = model
	attrs["import_source"] = "local_jsonl"
	return api.MetricDataPoint{
		Timestamp:   ts,
		ServiceName: SourceClaude.ServiceName(),
		MetricName:  metricName,
		MetricType:  "sum",
		Value:       &value,
		Attributes:  attrs,
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

// createCostMetrics creates both regular and user-facing cost metrics with session attributes.
func createCostMetrics(ts time.Time, model string, value float64, meta claudeSessionMeta, repository string) []api.MetricDataPoint {
	return []api.MetricDataPoint{
		createCostMetric(ts, ClaudeCostMetric, model, value, meta, repository),
		createCostMetric(ts, ClaudeUserFacingCostMetric, model, value, meta, repository),
	}
}

// createLOCMetric creates a lines_of_code metric for a file edit
func createLOCMetric(ts time.Time, lineType, filePath, fileType string, value float64, meta claudeSessionMeta, repository string) api.MetricDataPoint {
	attrs := sessionAttrs(meta, repository)
	attrs["type"] = lineType
	attrs["file_type"] = fileType
	attrs["file_path"] = filePath
	attrs["import_source"] = "local_jsonl"
	return api.MetricDataPoint{
		Timestamp:   ts,
		ServiceName: SourceClaude.ServiceName(),
		MetricName:  claudeLinesOfCodeMetric,
		MetricType:  "sum",
		Value:       &value,
		Attributes:  attrs,
	}
}

// createPRMetric creates a pull_request.count metric for a linked PR
func createPRMetric(meta claudeSessionMeta, repository string) api.MetricDataPoint {
	one := 1.0
	attrs := sessionAttrs(meta, repository)
	attrs["pr_number"] = strconv.Itoa(meta.PRNumber)
	attrs["pr_url"] = meta.PRUrl
	attrs["import_source"] = "local_jsonl"
	return api.MetricDataPoint{
		Timestamp:   meta.PRTimestamp,
		ServiceName: SourceClaude.ServiceName(),
		MetricName:  claudePullRequestMetric,
		MetricType:  "sum",
		Value:       &one,
		Attributes:  attrs,
	}
}

// createCommitMetric creates a commit.count metric for a git commit tool call
func createCommitMetric(ts time.Time, meta claudeSessionMeta, repository string) api.MetricDataPoint {
	one := 1.0
	attrs := sessionAttrs(meta, repository)
	attrs["import_source"] = "local_jsonl"
	return api.MetricDataPoint{
		Timestamp:   ts,
		ServiceName: SourceClaude.ServiceName(),
		MetricName:  claudeCommitMetric,
		MetricType:  "sum",
		Value:       &one,
		Attributes:  attrs,
	}
}

// createSessionMetric creates a session.count metric (one per JSONL file)
func createSessionMetric(ts time.Time, meta claudeSessionMeta, repository string) api.MetricDataPoint {
	one := 1.0
	attrs := sessionAttrs(meta, repository)
	attrs["import_source"] = "local_jsonl"
	return api.MetricDataPoint{
		Timestamp:   ts,
		ServiceName: SourceClaude.ServiceName(),
		MetricName:  claudeSessionMetric,
		MetricType:  "sum",
		Value:       &one,
		Attributes:  attrs,
	}
}

// createActiveTimeMetric creates an active_time.total metric from a turn_duration entry.
// Each turn emits its own data point (consistent with OTLP telemetry).
func createActiveTimeMetric(ts time.Time, secs float64, meta claudeSessionMeta, repository string) api.MetricDataPoint {
	attrs := sessionAttrs(meta, repository)
	attrs["import_source"] = "local_jsonl"
	return api.MetricDataPoint{
		Timestamp:   ts,
		ServiceName: SourceClaude.ServiceName(),
		MetricName:  claudeActiveTimeMetric,
		MetricType:  "sum",
		Value:       &secs,
		Attributes:  attrs,
	}
}
