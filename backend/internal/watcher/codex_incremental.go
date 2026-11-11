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

	// The file, or its compressed form (backfill or startup scan of a file
	// Codex compressed), which is read as a stream from the start
	content, _, err := importer.OpenSessionFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("opening file: %w", err)
	}
	defer content.Close()
	file, seekable := content.(*os.File)

	// Seek to last known position
	if seekable && workingState.ByteOffset > 0 {
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

	// Rebuild from the part of the file read before what this state lacks:
	// the repository context and the session's context when an import read
	// it, and the open tool calls when an earlier version kept the state
	replayRepository := parserState.Repository == nil
	replayCalls := !parserState.ToolCallsTracked && workingState.ByteOffset > 0
	if replayRepository || replayCalls {
		if replayRepository {
			parserState.Repository = &importer.RepositoryMetadata{}
		}
		recoverTokens := replayRepository && parserState.LastTokenCount == nil
		recoverModel := replayRepository && parserState.CurrentModel == ""
		recoverSession := replayRepository && parserState.SessionID == ""
		observe := func(line []byte) {
			var entry importer.CodexJSONLEntry
			if json.Unmarshal(line, &entry) != nil {
				return
			}
			ts, err := time.Parse(time.RFC3339Nano, entry.Timestamp)
			if err != nil {
				return
			}
			if replayCalls {
				importer.ObserveCodexToolCalls(entry, ts, &parserState.CodexState)
			}
			if !replayRepository {
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
		}
		if seekable {
			err = replayRepositoryPrefix(ctx, file, workingState.ByteOffset, observe)
		} else {
			err = replayLines(ctx, io.LimitReader(content, workingState.ByteOffset), observe)
		}
		if err != nil {
			return nil, err
		}
	} else if !seekable && workingState.ByteOffset > 0 {
		// Skip what was read before
		if _, err := io.CopyN(io.Discard, content, workingState.ByteOffset); err != nil && err != io.EOF {
			return nil, fmt.Errorf("skipping to offset %d: %w", workingState.ByteOffset, err)
		}
	}
	parserState.ToolCallsTracked = true

	// Use filename as fallback session ID
	if parserState.SessionID == "" {
		parserState.SessionID = strings.TrimSuffix(filepath.Base(filePath), ".jsonl")
	}

	result := &IncrementalResult{}
	reader := bufio.NewReaderSize(content, 1024*1024)
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
