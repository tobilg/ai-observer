package watcher

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/tobilg/ai-observer/internal/importer"
	"github.com/tobilg/ai-observer/internal/storage"
)

type geminiParserState struct {
	MessageCount int `json:"messageCount"`
}

type geminiIncrementalParser struct{}

// EndMessageCount returns the number of messages in the session file, where
// watching starts when existing content is not imported
func (p *geminiIncrementalParser) EndMessageCount(filePath string) (int, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return 0, fmt.Errorf("reading file: %w", err)
	}
	var session importer.GeminiSession
	if err := json.Unmarshal(data, &session); err != nil {
		return 0, fmt.Errorf("parsing JSON: %w", err)
	}
	return len(session.Messages), nil
}

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
	// A full import or the start of watching records only the message count
	if state.ParserState == "" && state.MessageCount > 0 {
		parserState.MessageCount = state.MessageCount
	}

	// Skip already-processed messages
	if parserState.MessageCount >= len(session.Messages) {
		return &IncrementalResult{}, nil
	}
	records, err := importer.ConvertGeminiMessages(ctx, &session, parserState.MessageCount, "file_watcher")
	if err != nil {
		return nil, err
	}
	result := &IncrementalResult{Logs: records.Logs, Metrics: records.Metrics}
	state.RecordCount += len(records.Logs)

	// Update state
	parserState.MessageCount = len(session.Messages)
	state.ByteOffset = int64(len(data))
	state.MessageCount = parserState.MessageCount
	stateJSON, _ := json.Marshal(parserState)
	state.ParserState = string(stateJSON)

	return result, nil
}
