package watcher

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/tobilg/ai-observer/internal/api"
	"github.com/tobilg/ai-observer/internal/importer"
	"github.com/tobilg/ai-observer/internal/pricing"
	"github.com/tobilg/ai-observer/internal/storage"
)

type geminiParserState struct {
	MessageCount int `json:"messageCount"`
}

type geminiIncrementalParser struct{}

func (p *geminiIncrementalParser) ParseIncremental(ctx context.Context, filePath string, state *storage.ImportState) (*IncrementalResult, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("reading file: %w", err)
	}

	var session importer.GeminiSession
	if err := json.Unmarshal(data, &session); err != nil {
		return nil, fmt.Errorf("parsing JSON: %w", err)
	}

	// Deserialize parser state
	var parserState geminiParserState
	if state.ParserState != "" {
		if err := json.Unmarshal([]byte(state.ParserState), &parserState); err != nil {
			parserState = geminiParserState{}
		}
	}

	// Skip already-processed messages
	if parserState.MessageCount >= len(session.Messages) {
		return &IncrementalResult{}, nil
	}
	newMessages := session.Messages[parserState.MessageCount:]

	result := &IncrementalResult{}
	messageIndex := parserState.MessageCount

	for _, msg := range newMessages {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		ts, err := importer.ParseGeminiTime(msg.Timestamp)
		if err != nil {
			continue
		}

		// Create transcript log record
		logRecord := api.LogRecord{
			Timestamp:      ts,
			ServiceName:    importer.SourceGemini.ServiceName(),
			SeverityText:   importer.MapGeminiSeverity(msg.Type),
			SeverityNumber: importer.MapGeminiSeverityNumber(msg.Type),
			Body:           msg.Content,
			LogAttributes: map[string]string{
				"event.name":    "transcript.message",
				"session.id":    session.SessionID,
				"message.id":    msg.ID,
				"message.index": fmt.Sprintf("%d", messageIndex),
				"message.role":  importer.MapGeminiTypeToRole(msg.Type),
				"import_source": "file_watcher",
			},
		}
		if msg.Model != "" {
			logRecord.LogAttributes["model"] = msg.Model
		}
		if session.ProjectHash != "" {
			logRecord.LogAttributes["project_hash"] = session.ProjectHash
		}
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
		state.RecordCount++
		messageIndex++

		// Create tool call log entries
		for _, toolCall := range msg.ToolCalls {
			toolTs := ts
			if toolCall.Timestamp != "" {
				if parsed, err := importer.ParseGeminiTime(toolCall.Timestamp); err == nil {
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
				"import_source": "file_watcher",
			}
			if len(toolCall.Args) > 0 {
				toolUseAttrs["tool.input"] = string(toolCall.Args)
			}
			toolUseLog := api.LogRecord{
				Timestamp:      toolTs,
				ServiceName:    importer.SourceGemini.ServiceName(),
				SeverityText:   "INFO",
				SeverityNumber: 9,
				Body:           fmt.Sprintf("Tool call: %s", toolCall.Name),
				LogAttributes:  toolUseAttrs,
			}
			result.Logs = append(result.Logs, toolUseLog)
			state.RecordCount++
			messageIndex++

			// Tool result entry
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
					"import_source": "file_watcher",
				}
				if toolCall.Status != "" {
					toolResultAttrs["success"] = fmt.Sprintf("%v", toolCall.Status == "success")
				}
				if toolOutput != "" {
					toolResultAttrs["tool.output"] = toolOutput
				}

				toolResultLog := api.LogRecord{
					Timestamp:      toolTs,
					ServiceName:    importer.SourceGemini.ServiceName(),
					SeverityText:   "INFO",
					SeverityNumber: 9,
					Body:           toolOutput,
					LogAttributes:  toolResultAttrs,
				}
				result.Logs = append(result.Logs, toolResultLog)
				state.RecordCount++
				messageIndex++
			}
		}

		// Create metrics for gemini messages with tokens
		if msg.Type == "gemini" && msg.Tokens != nil {
			tokens := msg.Tokens
			model := msg.Model
			var totalCost float64

			if tokens.Input > 0 {
				result.Metrics = append(result.Metrics, importer.CreateGeminiTokenMetric(ts, model, "input", float64(tokens.Input)))
				if cost := pricing.CalculateGeminiCostForTokenType(model, "input", int64(tokens.Input)); cost != nil {
					totalCost += *cost
				}
			}
			if tokens.Output > 0 {
				result.Metrics = append(result.Metrics, importer.CreateGeminiTokenMetric(ts, model, "output", float64(tokens.Output)))
				if cost := pricing.CalculateGeminiCostForTokenType(model, "output", int64(tokens.Output)); cost != nil {
					totalCost += *cost
				}
			}
			if tokens.Cached > 0 {
				result.Metrics = append(result.Metrics, importer.CreateGeminiTokenMetric(ts, model, "cached", float64(tokens.Cached)))
				if cost := pricing.CalculateGeminiCostForTokenType(model, "cache", int64(tokens.Cached)); cost != nil {
					totalCost += *cost
				}
			}
			if tokens.Thoughts > 0 {
				result.Metrics = append(result.Metrics, importer.CreateGeminiTokenMetric(ts, model, "thoughts", float64(tokens.Thoughts)))
				if cost := pricing.CalculateGeminiCostForTokenType(model, "thought", int64(tokens.Thoughts)); cost != nil {
					totalCost += *cost
				}
			}
			if tokens.Tool > 0 {
				result.Metrics = append(result.Metrics, importer.CreateGeminiTokenMetric(ts, model, "tool", float64(tokens.Tool)))
			}

			if totalCost > 0 {
				result.Metrics = append(result.Metrics, importer.CreateGeminiCostMetric(ts, model, totalCost))
			}
		}
	}

	// Update state
	parserState.MessageCount = len(session.Messages)
	state.ByteOffset = int64(len(data))
	state.MessageCount = parserState.MessageCount
	stateJSON, _ := json.Marshal(parserState)
	state.ParserState = string(stateJSON)

	return result, nil
}
