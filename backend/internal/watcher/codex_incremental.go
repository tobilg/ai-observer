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

	"github.com/tobilg/ai-observer/internal/importer"
	"github.com/tobilg/ai-observer/internal/storage"
)

type codexParserState struct {
	Repository *importer.RepositoryMetadata `json:"repository,omitempty"`
	// Session ID, model, message index and last token totals, shared with
	// the importer's conversion
	importer.CodexState
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

	if parserState.Repository == nil {
		parserState.Repository = &importer.RepositoryMetadata{}
		recoverTokens := parserState.LastTokenCount == nil
		recoverModel := parserState.CurrentModel == ""
		recoverSession := parserState.SessionID == ""
		if err := replayRepositoryPrefix(ctx, file, workingState.ByteOffset, func(line []byte) {
			var entry importer.CodexJSONLEntry
			if json.Unmarshal(line, &entry) != nil {
				return
			}
			if _, err := time.Parse(time.RFC3339Nano, entry.Timestamp); err != nil {
				return
			}
			parserState.Repository.ObserveCodex(entry)
			switch entry.Type {
			case "session_meta":
				var meta importer.CodexSessionMeta
				if json.Unmarshal(entry.Payload, &meta) == nil {
					if recoverSession && meta.ID != "" {
						parserState.SessionID = meta.ID
					}
					if recoverModel && meta.Model != "" {
						parserState.CurrentModel = meta.Model
					}
				}
			case "turn_context":
				var turn struct{ Model string }
				if recoverModel && json.Unmarshal(entry.Payload, &turn) == nil && turn.Model != "" {
					parserState.CurrentModel = turn.Model
				}
			case "event_msg":
				var event importer.CodexEventMsg
				if recoverTokens && json.Unmarshal(entry.Payload, &event) == nil && event.Type == "token_count" && event.Info != nil && event.Info.TotalTokenUsage != nil {
					parserState.LastTokenCount = event.Info.TotalTokenUsage
				}
			}
		}); err != nil {
			return nil, err
		}
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

		parserState.Repository.ObserveCodex(entry)

		logs, metrics, records := importer.ConvertCodexEntry(entry, ts, &parserState.CodexState, "file_watcher")
		result.Logs = append(result.Logs, logs...)
		result.Metrics = append(result.Metrics, metrics...)
		workingState.RecordCount += records

		commitLine()
		if readErr == io.EOF {
			break readLoop
		}
	}

	parserState.Repository.Enrich(result.Metrics, result.Logs)

	// Update state
	workingState.ByteOffset += committedBytes
	workingState.MessageCount = parserState.MessageIndex
	stateJSON, _ := json.Marshal(parserState)
	workingState.ParserState = string(stateJSON)
	*state = workingState

	return result, nil
}
