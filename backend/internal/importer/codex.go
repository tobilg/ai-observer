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
	"sort"
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

// FindSessionFiles returns all JSONL session files, compressed ones
// (.jsonl.zst) by their plain path (codex_rollout.go), each once
func (p *CodexParser) FindSessionFiles(ctx context.Context) ([]string, error) {
	if p.sessionsPath == "" {
		return nil, nil
	}

	var files []string
	seen := make(map[string]bool)

	err := filepath.Walk(p.sessionsPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip errors, continue walking
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if info.IsDir() {
			return nil
		}
		if plain, ok := CompressedSessionFile(path); ok {
			path = plain
		} else if !strings.HasSuffix(path, ".jsonl") {
			return nil
		}
		if !seen[path] {
			seen[path] = true
			files = append(files, path)
		}
		return nil
	})

	if err != nil && err != context.Canceled {
		return nil, fmt.Errorf("walking sessions directory: %w", err)
	}

	sort.Strings(files)
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

// CodexTokenCount represents token count data. Input includes the cached
// input and the cache writes, output the reasoning; total_tokens (input plus
// output) is not read.
type CodexTokenCount struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheWriteInputTokens    int `json:"cache_write_input_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"` // Alternative field name
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
	CachedInputTokens        int `json:"cached_input_tokens"` // Alternative field name
	ReasoningTokens          int `json:"reasoning_output_tokens"`
}

// ParseFile parses a Codex CLI JSONL file
func (p *CodexParser) ParseFile(ctx context.Context, path string) (*ImportResult, error) {
	return p.parse(ctx, path, nil, 0, nil)
}

// ParseFileInBatches parses a Codex CLI JSONL file like ParseFile, but hands
// its records to emit in batches of about batchSize records instead of
// returning them, so a session of several hundred MB is never held in memory
// as a whole. The batches carry the repository of the whole file, which a
// first pass over the file reads. The returned result has no records.
func (p *CodexParser) ParseFileInBatches(ctx context.Context, path string, batchSize int, emit func(*ImportResult) error) (*ImportResult, error) {
	var attribution RepositoryMetadata
	if err := readCodexEntries(ctx, path, nil, nil, func(entry CodexJSONLEntry, _ time.Time) error {
		attribution.ObserveCodex(entry)
		return nil
	}); err != nil {
		return nil, err
	}
	return p.parse(ctx, path, &attribution, batchSize, emit)
}

// parse parses the file at path; with emit, in batches (ParseFileInBatches)
// enriched with the given attribution
func (p *CodexParser) parse(ctx context.Context, path string, fileAttribution *RepositoryMetadata, batchSize int, emit func(*ImportResult) error) (*ImportResult, error) {
	result := &ImportResult{
		FilePath: path,
	}

	// Extract session ID from filename (format: rollout-YYYY-MM-DDThh-mm-ss-{id}.jsonl)
	filename := filepath.Base(path)
	result.SessionID = strings.TrimSuffix(filename, ".jsonl")

	codexState := CodexState{SessionID: result.SessionID}
	var attribution RepositoryMetadata
	batch := &ImportResult{FilePath: path}
	flush := func() error {
		if len(batch.Logs) == 0 && len(batch.Metrics) == 0 {
			return nil
		}
		fileAttribution.Enrich(batch.Metrics, batch.Logs)
		if err := emit(batch); err != nil {
			return err
		}
		batch = &ImportResult{FilePath: path}
		return nil
	}

	// Hash exactly the content that is parsed
	hasher := sha256.New()
	err := readCodexEntries(ctx, path, hasher, &result.ParsedBytes, func(entry CodexJSONLEntry, ts time.Time) error {
		if emit == nil {
			attribution.ObserveCodex(entry)
		}

		// Update time range
		if result.FirstTime.IsZero() || ts.Before(result.FirstTime) {
			result.FirstTime = ts
		}
		if ts.After(result.LastTime) {
			result.LastTime = ts
		}

		logs, metrics, records := ConvertCodexEntry(entry, ts, &codexState, "local_jsonl")
		result.RecordCount += records
		if emit == nil {
			result.Logs = append(result.Logs, logs...)
			result.Metrics = append(result.Metrics, metrics...)
			return nil
		}
		batch.Logs = append(batch.Logs, logs...)
		batch.Metrics = append(batch.Metrics, metrics...)
		if len(batch.Logs)+len(batch.Metrics) >= batchSize {
			return flush()
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if emit != nil {
		if err := flush(); err != nil {
			return nil, err
		}
	}

	result.FileHash = hex.EncodeToString(hasher.Sum(nil))
	result.SessionID = codexState.SessionID
	result.MessageCount = codexState.MessageIndex

	if emit == nil {
		attribution.Enrich(result.Metrics, result.Logs)
	}
	return result, nil
}

// readCodexEntries passes the entries of the session file at path that have
// a valid timestamp to fn, in order. The content read is written to hash and
// the length of its complete lines counted in parsed, when they are not nil.
func readCodexEntries(ctx context.Context, path string, hash io.Writer, parsed *int64, fn func(CodexJSONLEntry, time.Time) error) error {
	// The file or its compressed form, read as a stream
	file, _, err := OpenSessionFile(path)
	if err != nil {
		return fmt.Errorf("opening file: %w", err)
	}
	defer file.Close()
	var content io.Reader = file
	if hash != nil {
		content = io.TeeReader(file, hash)
	}

	for line, err := range jsonlLinesCounted(content, parsed) {
		if err != nil {
			return fmt.Errorf("reading file: %w", err)
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}

		var entry CodexJSONLEntry
		if err := json.Unmarshal(line, &entry); err != nil {
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
		if err := fn(entry, ts); err != nil {
			return err
		}
	}
	return nil
}

// CodexState is the context a Codex session carries from entry to entry. The
// file watcher keeps it between runs as part of its parser state.
type CodexState struct {
	SessionID      string           `json:"sessionId"`
	CurrentModel   string           `json:"currentModel"`
	MessageIndex   int              `json:"messageIndex"`
	LastTokenCount *CodexTokenCount `json:"lastTokenCount"`
	// ToolCalls are the model's tool calls whose output was not read yet,
	// AnsweredCalls the IDs of recent calls whose output was, whose
	// execution may still follow (codex_tools.go); ToolCallsTracked is set
	// once the conversion has kept them, which parser states of earlier
	// versions lack
	ToolCalls        []CodexToolCall `json:"toolCalls,omitempty"`
	AnsweredCalls    []string        `json:"answeredCalls,omitempty"`
	ToolCallsTracked bool            `json:"toolCallsTracked,omitempty"`
	// Lookups of ToolCalls (positions) and AnsweredCalls
	pending  map[string]int
	answered map[string]struct{}
}

// ConvertCodexEntry converts one entry of a Codex session file into records,
// updating state. Import and watch both use it, so they store the same
// records; importSource marks them ("local_jsonl" for imports,
// "file_watcher" for watch). records counts the entries that were recorded.
func ConvertCodexEntry(entry CodexJSONLEntry, ts time.Time, state *CodexState, importSource string) (logs []api.LogRecord, metrics []api.MetricDataPoint, records int) {
	c := codexConverter{ts: ts, state: state, importSource: importSource, emit: true}
	c.convert(entry)
	return c.logs, c.metrics, c.records
}

// ObserveCodexToolCalls updates the tool calls state keeps (ToolCalls) as
// ConvertCodexEntry would, without converting the entry. The file watcher
// uses it to rebuild them from the part of a file an import read.
func ObserveCodexToolCalls(entry CodexJSONLEntry, ts time.Time, state *CodexState) {
	switch entry.Type {
	case "response_item", "event_msg":
		c := codexConverter{ts: ts, state: state}
		c.convert(entry)
	}
	state.ToolCallsTracked = true
}

// codexConverter converts the entries of a Codex session file
type codexConverter struct {
	ts           time.Time
	state        *CodexState
	importSource string
	// emit is false when only the state is updated (ObserveCodexToolCalls)
	emit    bool
	logs    []api.LogRecord
	metrics []api.MetricDataPoint
	records int
}

func (c *codexConverter) log(at time.Time, body string, attrs map[string]string) api.LogRecord {
	attrs["import_source"] = c.importSource
	return api.LogRecord{
		Timestamp:      at,
		ServiceName:    SourceCodex.ServiceName(),
		SeverityText:   "INFO",
		SeverityNumber: 9,
		Body:           body,
		LogAttributes:  attrs,
	}
}

// transcript adds a transcript message with the next message index
func (c *codexConverter) transcript(body, role string, attrs map[string]string) {
	c.transcriptAt(c.ts, body, role, attrs)
}

// transcriptAt adds a transcript message recorded at at, which is never
// after the entry's time
func (c *codexConverter) transcriptAt(at time.Time, body, role string, attrs map[string]string) {
	if !c.emit {
		return
	}
	if at.IsZero() || at.After(c.ts) {
		at = c.ts
	}
	attrs["event.name"] = "transcript.message"
	attrs["session.id"] = c.state.SessionID
	attrs["message.index"] = fmt.Sprintf("%d", c.state.MessageIndex)
	attrs["message.role"] = role
	c.logs = append(c.logs, c.log(at, body, attrs))
	c.records++
	c.state.MessageIndex++
}

func (c *codexConverter) convert(entry CodexJSONLEntry) {
	state := c.state
	if c.emit {
		state.ToolCallsTracked = true
	}
	switch entry.Type {
	case "session_meta":
		if !c.emit {
			return
		}
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
		c.logs = append(c.logs, c.log(c.ts, "conversation_starts", attrs))
		c.records++

	case "event_msg":
		var eventMsg CodexEventMsg
		if err := json.Unmarshal(entry.Payload, &eventMsg); err != nil {
			return
		}
		switch eventMsg.Type {
		case "item_completed":
			c.convertItemCompleted(entry.Payload)
		case "token_count":
			if !c.emit {
				return
			}
			// Info is at the same level as type in the event_msg payload
			if eventMsg.Info != nil && eventMsg.Info.TotalTokenUsage != nil {
				c.metrics = codexTokenMetrics(c.ts, state.CurrentModel, state.SessionID, eventMsg.Info.TotalTokenUsage, state.LastTokenCount)
				state.LastTokenCount = eventMsg.Info.TotalTokenUsage
				c.records++
			}
		case "user_message", "agent_message":
			if !c.emit {
				return
			}
			attrs := map[string]string{"event.name": "codex." + eventMsg.Type}
			if state.SessionID != "" {
				attrs["session.id"] = state.SessionID
			}
			c.logs = append(c.logs, c.log(c.ts, eventMsg.Type, attrs))
			c.records++
		default:
			c.convertLegacyEvent(eventMsg.Type, entry.Payload)
		}

	case "turn_context":
		if !c.emit {
			return
		}
		// Extract model from turn context if available
		var turnCtx struct {
			Model string `json:"model"`
		}
		if err := json.Unmarshal(entry.Payload, &turnCtx); err == nil && turnCtx.Model != "" {
			state.CurrentModel = turnCtx.Model
		}

	case "response_item":
		// ResponseItem entries hold the transcript content
		var respItem codexResponsePayload
		if err := json.Unmarshal(entry.Payload, &respItem); err != nil {
			return
		}
		if c.convertToolCall(respItem) || !c.emit {
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
			c.transcript(textContent, role, modelAttrs())

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
			c.transcript(reasoningText, "assistant", modelAttrs()) // Reasoning is from the assistant
		}
	}
}

// codexTokenMetrics returns the token and cost metrics of a token_count
// event. Codex reports running totals, so the metrics hold the increase over
// the previous event's totals (last, nil for the first event). They carry
// the session's ID, as the usage Codex sends over OTLP does, so storage can
// recognize a response recorded both ways (storage/usage_overlap.go).
func codexTokenMetrics(ts time.Time, model, sessionID string, total, last *CodexTokenCount) []api.MetricDataPoint {
	// Handle alternative field names
	cached := func(c *CodexTokenCount) int {
		if c.CacheReadInputTokens != 0 {
			return c.CacheReadInputTokens
		}
		return c.CachedInputTokens
	}
	cacheWrite := func(c *CodexTokenCount) int {
		if c.CacheWriteInputTokens != 0 {
			return c.CacheWriteInputTokens
		}
		return c.CacheCreationInputTokens
	}
	if last == nil {
		last = &CodexTokenCount{}
	}
	deltaInput := total.InputTokens - last.InputTokens
	deltaCacheRead := cached(total) - cached(last)
	deltaCacheWrite := cacheWrite(total) - cacheWrite(last)
	deltaOutput := total.OutputTokens - last.OutputTokens

	var metrics []api.MetricDataPoint
	for _, t := range []struct {
		tokenType string
		delta     int
	}{
		{pricing.CodexTokenTypeInput, deltaInput},
		{pricing.CodexTokenTypeOutput, deltaOutput},
		{pricing.CodexTokenTypeCacheRead, deltaCacheRead},
		{pricing.CodexTokenTypeCacheWrite, deltaCacheWrite},
		{pricing.CodexTokenTypeReasoning, total.ReasoningTokens - last.ReasoningTokens},
	} {
		if t.delta > 0 {
			metrics = append(metrics, CreateCodexTokenMetric(ts, model, t.tokenType, float64(t.delta)))
		}
	}

	cost := pricing.CalculateCodexCost(model, int64(deltaInput), int64(deltaCacheRead), int64(deltaCacheWrite), int64(deltaOutput))
	if cost != nil && *cost > 0 {
		metrics = append(metrics, CreateCodexCostMetric(ts, model, *cost))
	}
	if sessionID != "" {
		for i := range metrics {
			metrics[i].Attributes["session.id"] = sessionID
		}
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
