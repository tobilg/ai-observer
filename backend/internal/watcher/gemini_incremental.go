package watcher

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/tobilg/ai-observer/internal/importer"
	"github.com/tobilg/ai-observer/internal/storage"
)

// geminiParserState is the state of a watched file (imports record it too)
type geminiParserState = importer.GeminiWatchState

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
	// The start of watching, and imports of earlier versions, record only
	// the message count
	if state.ParserState == "" && state.MessageCount > 0 {
		parserState.MessageCount = state.MessageCount
	}
	read := parserState.MessageCount
	if read > len(session.Messages) {
		read = len(session.Messages)
	}

	result := &IncrementalResult{}
	// What Gemini CLI added to the last message read
	if read > 0 && parserState.Last != nil {
		last, err := geminiMessageRecords(ctx, &session, read-1)
		if err != nil {
			return nil, err
		}
		if len(last.Logs) > parserState.Last.Logs {
			result.Logs = append(result.Logs, last.Logs[parserState.Last.Logs:]...)
		}
		if !parserState.Last.Tokens {
			result.Metrics = append(result.Metrics, last.Metrics...)
		}
	}

	// Messages added since
	if read < len(session.Messages) {
		records, err := importer.ConvertGeminiMessages(ctx, &session, read, "file_watcher")
		if err != nil {
			return nil, err
		}
		result.Logs = append(result.Logs, records.Logs...)
		result.Metrics = append(result.Metrics, records.Metrics...)
	}
	if len(result.Logs) == 0 && len(result.Metrics) == 0 && read >= len(session.Messages) {
		return &IncrementalResult{}, nil
	}
	last, err := geminiMessageRecords(ctx, &session, len(session.Messages)-1)
	if err != nil {
		return nil, err
	}
	parserState.Last = &importer.GeminiLastMessage{Logs: len(last.Logs), Tokens: len(last.Metrics) > 0}
	state.RecordCount += len(result.Logs)

	// Update state
	parserState.MessageCount = len(session.Messages)
	state.ByteOffset = int64(len(data))
	state.MessageCount = parserState.MessageCount
	stateJSON, _ := json.Marshal(parserState)
	state.ParserState = string(stateJSON)

	return result, nil
}

// geminiMessageRecords returns the records of message i of session, as
// watch stores them
func geminiMessageRecords(ctx context.Context, session *importer.GeminiSession, i int) (*importer.GeminiRecords, error) {
	return importer.GeminiMessageRecords(ctx, session, i, "file_watcher")
}
