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

type codexParserState struct {
	SessionID      string                    `json:"sessionId"`
	CurrentModel   string                    `json:"currentModel"`
	MessageIndex   int                       `json:"messageIndex"`
	LastTokenCount *importer.CodexTokenCount `json:"lastTokenCount"`
}

type codexIncrementalParser struct{}

func (p *codexIncrementalParser) ParseIncremental(ctx context.Context, filePath string, state *storage.ImportState) (*IncrementalResult, error) {
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
	var parserState codexParserState
	if workingState.ParserState != "" {
		if err := json.Unmarshal([]byte(workingState.ParserState), &parserState); err != nil {
			parserState = codexParserState{}
		}
	}
	if parserState.MessageIndex == 0 && workingState.MessageCount > 0 {
		parserState.MessageIndex = workingState.MessageCount
	}

	// Use filename as fallback session ID
	if parserState.SessionID == "" {
		parserState.SessionID = strings.TrimSuffix(filepath.Base(filePath), ".jsonl")
	}

	result := &IncrementalResult{}
	reader := bufio.NewReaderSize(file, 1024*1024)
	committedBytes := int64(0)

readLoop:
	for {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		rawLine, readErr := reader.ReadString('\n')
		if readErr != nil && readErr != io.EOF {
			return nil, fmt.Errorf("reading file: %w", readErr)
		}
		if rawLine == "" && readErr == io.EOF {
			break readLoop
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
				break readLoop
			}
			continue
		}

		var entry importer.CodexJSONLEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			if !hasNewline {
				break readLoop
			}
			commitLine()
			if readErr == io.EOF {
				break readLoop
			}
			continue
		}

		ts, err := time.Parse(time.RFC3339Nano, entry.Timestamp)
		if err != nil {
			ts, err = time.Parse(time.RFC3339, entry.Timestamp)
			if err != nil {
				commitLine()
				if readErr == io.EOF {
					break readLoop
				}
				continue
			}
		}

		switch entry.Type {
		case "session_meta":
			var meta importer.CodexSessionMeta
			if err := json.Unmarshal(entry.Payload, &meta); err == nil {
				if meta.ID != "" {
					parserState.SessionID = meta.ID
				}
				if meta.Model != "" {
					parserState.CurrentModel = meta.Model
				}

				logRecord := api.LogRecord{
					Timestamp:      ts,
					ServiceName:    importer.SourceCodex.ServiceName(),
					SeverityText:   "INFO",
					SeverityNumber: 9,
					Body:           "conversation_starts",
					LogAttributes: map[string]string{
						"event.name":     "codex.conversation_starts",
						"session.id":     meta.ID,
						"model":          meta.Model,
						"model_provider": meta.ModelProvider,
						"cli_version":    meta.CliVersion,
						"import_source":  "file_watcher",
					},
				}
				if meta.Cwd != "" {
					logRecord.LogAttributes["cwd"] = meta.Cwd
				}
				result.Logs = append(result.Logs, logRecord)
				workingState.RecordCount++
			}

		case "event_msg":
			var eventMsg importer.CodexEventMsg
			if err := json.Unmarshal(entry.Payload, &eventMsg); err != nil {
				commitLine()
				if readErr == io.EOF {
					break readLoop
				}
				continue
			}

			switch eventMsg.Type {
			case "token_count":
				if eventMsg.Info != nil && eventMsg.Info.TotalTokenUsage != nil {
					tokenCount := eventMsg.Info.TotalTokenUsage

					cachedTokens := tokenCount.CacheReadInputTokens
					if cachedTokens == 0 {
						cachedTokens = tokenCount.CachedInputTokens
					}

					var deltaInput, deltaOutput, deltaCacheCreation, deltaCacheRead, deltaReasoning, deltaTool int

					if parserState.LastTokenCount == nil {
						deltaInput = tokenCount.InputTokens
						deltaOutput = tokenCount.OutputTokens
						deltaCacheCreation = tokenCount.CacheCreationInputTokens
						deltaCacheRead = cachedTokens
						deltaReasoning = tokenCount.ReasoningTokens
						deltaTool = tokenCount.ToolTokens
					} else {
						lastCached := parserState.LastTokenCount.CacheReadInputTokens
						if lastCached == 0 {
							lastCached = parserState.LastTokenCount.CachedInputTokens
						}
						deltaInput = tokenCount.InputTokens - parserState.LastTokenCount.InputTokens
						deltaOutput = tokenCount.OutputTokens - parserState.LastTokenCount.OutputTokens
						deltaCacheCreation = tokenCount.CacheCreationInputTokens - parserState.LastTokenCount.CacheCreationInputTokens
						deltaCacheRead = cachedTokens - lastCached
						deltaReasoning = tokenCount.ReasoningTokens - parserState.LastTokenCount.ReasoningTokens
						deltaTool = tokenCount.ToolTokens - parserState.LastTokenCount.ToolTokens
					}

					model := parserState.CurrentModel

					if deltaInput > 0 {
						result.Metrics = append(result.Metrics, importer.CreateCodexTokenMetric(ts, model, "input", float64(deltaInput)))
					}
					if deltaOutput > 0 {
						result.Metrics = append(result.Metrics, importer.CreateCodexTokenMetric(ts, model, "output", float64(deltaOutput)))
					}
					if deltaCacheCreation > 0 {
						result.Metrics = append(result.Metrics, importer.CreateCodexTokenMetric(ts, model, "cache_creation", float64(deltaCacheCreation)))
					}
					if deltaCacheRead > 0 {
						result.Metrics = append(result.Metrics, importer.CreateCodexTokenMetric(ts, model, "cache_read", float64(deltaCacheRead)))
					}
					if deltaReasoning > 0 {
						result.Metrics = append(result.Metrics, importer.CreateCodexTokenMetric(ts, model, "reasoning", float64(deltaReasoning)))
					}
					if deltaTool > 0 {
						result.Metrics = append(result.Metrics, importer.CreateCodexTokenMetric(ts, model, "tool", float64(deltaTool)))
					}

					cost := pricing.CalculateCodexCost(model, int64(deltaInput), int64(deltaCacheRead), int64(deltaOutput))
					if cost != nil && *cost > 0 {
						result.Metrics = append(result.Metrics, importer.CreateCodexCostMetric(ts, model, *cost))
					}

					parserState.LastTokenCount = tokenCount
					workingState.RecordCount++
				}

			case "user_message", "agent_message":
				logRecord := api.LogRecord{
					Timestamp:      ts,
					ServiceName:    importer.SourceCodex.ServiceName(),
					SeverityText:   "INFO",
					SeverityNumber: 9,
					Body:           eventMsg.Type,
					LogAttributes: map[string]string{
						"event.name":    "codex." + eventMsg.Type,
						"import_source": "file_watcher",
					},
				}
				if parserState.SessionID != "" {
					logRecord.LogAttributes["session.id"] = parserState.SessionID
				}
				result.Logs = append(result.Logs, logRecord)
				workingState.RecordCount++
			}

		case "turn_context":
			var turnCtx struct {
				Model string `json:"model"`
			}
			if err := json.Unmarshal(entry.Payload, &turnCtx); err == nil {
				if turnCtx.Model != "" {
					parserState.CurrentModel = turnCtx.Model
				}
			}

		case "response_item":
			var respItem importer.CodexResponseItem
			if err := json.Unmarshal(entry.Payload, &respItem); err != nil {
				commitLine()
				if readErr == io.EOF {
					break readLoop
				}
				continue
			}

			sessionID := parserState.SessionID
			model := parserState.CurrentModel

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
					commitLine()
					if readErr == io.EOF {
						break readLoop
					}
					continue
				}

				role := respItem.Role
				if role == "" {
					role = "assistant"
				}

				logRecord := api.LogRecord{
					Timestamp:      ts,
					ServiceName:    importer.SourceCodex.ServiceName(),
					SeverityText:   "INFO",
					SeverityNumber: 9,
					Body:           textContent,
					LogAttributes: map[string]string{
						"event.name":    "transcript.message",
						"session.id":    sessionID,
						"message.index": fmt.Sprintf("%d", parserState.MessageIndex),
						"message.role":  role,
						"import_source": "file_watcher",
					},
				}
				if model != "" {
					logRecord.LogAttributes["model"] = model
				}
				result.Logs = append(result.Logs, logRecord)
				workingState.RecordCount++
				parserState.MessageIndex++

			case "function_call":
				logRecord := api.LogRecord{
					Timestamp:      ts,
					ServiceName:    importer.SourceCodex.ServiceName(),
					SeverityText:   "INFO",
					SeverityNumber: 9,
					Body:           fmt.Sprintf("Tool call: %s", respItem.Name),
					LogAttributes: map[string]string{
						"event.name":    "transcript.message",
						"session.id":    sessionID,
						"message.index": fmt.Sprintf("%d", parserState.MessageIndex),
						"message.role":  "tool_use",
						"tool.name":     respItem.Name,
						"import_source": "file_watcher",
					},
				}
				if respItem.Arguments != "" {
					logRecord.LogAttributes["tool.input"] = respItem.Arguments
				}
				if respItem.CallID != "" {
					logRecord.LogAttributes["tool.call_id"] = respItem.CallID
				}
				result.Logs = append(result.Logs, logRecord)
				workingState.RecordCount++
				parserState.MessageIndex++

			case "function_call_output":
				var outputContent string
				if len(respItem.Output) > 0 {
					var strOutput string
					if err := json.Unmarshal(respItem.Output, &strOutput); err == nil {
						outputContent = strOutput
					} else {
						outputContent = string(respItem.Output)
					}
				}

				logRecord := api.LogRecord{
					Timestamp:      ts,
					ServiceName:    importer.SourceCodex.ServiceName(),
					SeverityText:   "INFO",
					SeverityNumber: 9,
					Body:           outputContent,
					LogAttributes: map[string]string{
						"event.name":    "transcript.message",
						"session.id":    sessionID,
						"message.index": fmt.Sprintf("%d", parserState.MessageIndex),
						"message.role":  "tool_result",
						"import_source": "file_watcher",
					},
				}
				if outputContent != "" {
					logRecord.LogAttributes["tool.output"] = outputContent
				}
				if respItem.CallID != "" {
					logRecord.LogAttributes["tool.call_id"] = respItem.CallID
				}
				result.Logs = append(result.Logs, logRecord)
				workingState.RecordCount++
				parserState.MessageIndex++

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
					commitLine()
					if readErr == io.EOF {
						break readLoop
					}
					continue
				}

				logRecord := api.LogRecord{
					Timestamp:      ts,
					ServiceName:    importer.SourceCodex.ServiceName(),
					SeverityText:   "INFO",
					SeverityNumber: 9,
					Body:           reasoningText,
					LogAttributes: map[string]string{
						"event.name":    "transcript.message",
						"session.id":    sessionID,
						"message.index": fmt.Sprintf("%d", parserState.MessageIndex),
						"message.role":  "assistant",
						"import_source": "file_watcher",
					},
				}
				if model != "" {
					logRecord.LogAttributes["model"] = model
				}
				result.Logs = append(result.Logs, logRecord)
				workingState.RecordCount++
				parserState.MessageIndex++
			}
		}

		commitLine()
		if readErr == io.EOF {
			break readLoop
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
