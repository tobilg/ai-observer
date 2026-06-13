package watcher

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tobilg/ai-observer/internal/api"
	"github.com/tobilg/ai-observer/internal/importer"
	"github.com/tobilg/ai-observer/internal/pricing"
	"github.com/tobilg/ai-observer/internal/storage"
)

type claudeParserState struct {
	MessageIndex int             `json:"messageIndex"`
	SeenRequests map[string]bool `json:"seenRequests"`
}

type claudeIncrementalParser struct {
	pricingMode pricing.PricingMode
}

func (p *claudeIncrementalParser) ParseIncremental(ctx context.Context, filePath string, state *storage.ImportState) (*IncrementalResult, error) {
	workingState := *state

	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("opening file: %w", err)
	}
	defer file.Close()

	// Seek to last known position
	if workingState.ByteOffset > 0 {
		if _, err := file.Seek(workingState.ByteOffset, 0); err != nil {
			return nil, fmt.Errorf("seeking to offset %d: %w", workingState.ByteOffset, err)
		}
	}

	// Deserialize parser state
	var parserState claudeParserState
	if workingState.ParserState != "" {
		if err := json.Unmarshal([]byte(workingState.ParserState), &parserState); err != nil {
			parserState = claudeParserState{SeenRequests: make(map[string]bool)}
		}
	} else {
		parserState = claudeParserState{SeenRequests: make(map[string]bool)}
	}
	if parserState.SeenRequests == nil {
		parserState.SeenRequests = make(map[string]bool)
	}
	if parserState.MessageIndex == 0 && workingState.MessageCount > 0 {
		parserState.MessageIndex = workingState.MessageCount
	}

	result := &IncrementalResult{}
	reader := bufio.NewReaderSize(file, 1024*1024)
	committedBytes := int64(0)
	sessionID := strings.TrimSuffix(filepath.Base(filePath), ".jsonl")
	claudeParser := importer.NewClaudeParser()
	if p.pricingMode != "" {
		claudeParser.SetPricingMode(p.pricingMode)
	}

	for {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		rawLine, readErr := reader.ReadString('\n')
		if readErr != nil && readErr != io.EOF {
			return nil, fmt.Errorf("reading file: %w", readErr)
		}
		if rawLine == "" && readErr == io.EOF {
			break
		}

		hasNewline := strings.HasSuffix(rawLine, "\n")
		line := strings.TrimSuffix(rawLine, "\n")
		line = strings.TrimSuffix(line, "\r")
		commitLine := func() {
			committedBytes += int64(len(rawLine))
		}

		if strings.TrimSpace(line) == "" {
			commitLine()
			if readErr == io.EOF {
				break
			}
			continue
		}

		var entry importer.ClaudeJSONLEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			if !hasNewline {
				break
			}
			commitLine()
			if readErr == io.EOF {
				break
			}
			continue
		}

		if entry.Message == nil {
			commitLine()
			if readErr == io.EOF {
				break
			}
			continue
		}

		if entry.Type != "user" && entry.Type != "assistant" {
			commitLine()
			if readErr == io.EOF {
				break
			}
			continue
		}

		ts, err := time.Parse(time.RFC3339Nano, entry.Timestamp)
		if err != nil {
			ts, err = time.Parse(time.RFC3339, entry.Timestamp)
			if err != nil {
				commitLine()
				if readErr == io.EOF {
					break
				}
				continue
			}
		}

		entrySessionID := entry.SessionID
		if entrySessionID == "" {
			entrySessionID = sessionID
		}

		// Create transcript logs
		transcriptLogs := claudeParser.CreateTranscriptLogs(entry, ts, entrySessionID, &parserState.MessageIndex)
		result.Logs = append(result.Logs, transcriptLogs...)

		// For assistant entries with usage, create metrics
		if entry.Type == "assistant" && entry.Message.Usage != nil {
			dedupKey := fmt.Sprintf("%s:%s", entry.Message.ID, entry.RequestID)
			if entry.Message.ID != "" && entry.RequestID != "" {
				if parserState.SeenRequests[dedupKey] {
					commitLine()
					if readErr == io.EOF {
						break
					}
					continue
				}
				parserState.SeenRequests[dedupKey] = true
			}

			logRecord := api.LogRecord{
				Timestamp:      ts,
				ServiceName:    importer.SourceClaude.ServiceName(),
				SeverityText:   "INFO",
				SeverityNumber: 9,
				Body:           "api_request",
				LogAttributes: map[string]string{
					"event.name":    "claude_code.api_request",
					"session.id":    entrySessionID,
					"model":         entry.Message.Model,
					"import_source": "file_watcher",
				},
			}
			if entry.Cwd != "" {
				logRecord.LogAttributes["cwd"] = entry.Cwd
			}
			if entry.RequestID != "" {
				logRecord.LogAttributes["request_id"] = entry.RequestID
			}
			result.Logs = append(result.Logs, logRecord)

			usage := entry.Message.Usage
			model := entry.Message.Model

			if usage.InputTokens > 0 {
				result.Metrics = append(result.Metrics, importer.CreateTokenMetrics(ts, model, "input", float64(usage.InputTokens))...)
			}
			if usage.OutputTokens > 0 {
				result.Metrics = append(result.Metrics, importer.CreateTokenMetrics(ts, model, "output", float64(usage.OutputTokens))...)
			}
			if usage.CacheCreationInputTokens > 0 {
				result.Metrics = append(result.Metrics, importer.CreateTokenMetrics(ts, model, "cacheCreation", float64(usage.CacheCreationInputTokens))...)
			}
			if usage.CacheReadInputTokens > 0 {
				result.Metrics = append(result.Metrics, importer.CreateTokenMetrics(ts, model, "cacheRead", float64(usage.CacheReadInputTokens))...)
			}

			tokenUsage := pricing.ClaudeTokenUsage{
				InputTokens:              int64(usage.InputTokens),
				OutputTokens:             int64(usage.OutputTokens),
				CacheCreationInputTokens: int64(usage.CacheCreationInputTokens),
				CacheReadInputTokens:     int64(usage.CacheReadInputTokens),
			}
			cost := pricing.GetClaudeCostWithMode(p.pricingMode, model, tokenUsage, entry.CostUSD)
			if cost > 0 {
				result.Metrics = append(result.Metrics, importer.CreateCostMetrics(ts, model, cost)...)
			}
		}

		workingState.RecordCount++
		commitLine()
		if readErr == io.EOF {
			break
		}
	}

	// Update state
	workingState.ByteOffset += committedBytes
	workingState.MessageCount = parserState.MessageIndex
	stateJSON, _ := json.Marshal(parserState)
	workingState.ParserState = string(stateJSON)
	*state = workingState

	return result, nil
}
