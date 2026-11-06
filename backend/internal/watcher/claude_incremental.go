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
	"github.com/tobilg/ai-observer/internal/pricing"
	"github.com/tobilg/ai-observer/internal/storage"
)

type claudeParserState struct {
	Repository   *importer.RepositoryMetadata `json:"repository,omitempty"`
	MessageIndex int                          `json:"messageIndex"`
	// RecentRequests are the latest usage dedup keys, oldest first
	RecentRequests []string `json:"recentRequests,omitempty"`
	// SeenRequests holds all keys in states written by earlier versions
	SeenRequests map[string]bool `json:"seenRequests,omitempty"`

	seen map[string]bool // index of RecentRequests
}

// maxRecentRequests bounds the usage dedup keys kept in the parser state,
// which is rewritten on every update. Claude Code repeats a message's usage
// on the lines of the same turn, so only recent keys are needed.
const maxRecentRequests = 1000

// indexRequests prepares the dedup keys after decoding, taking over the keys
// of an earlier version's state
func (s *claudeParserState) indexRequests() {
	keys := s.RecentRequests
	for key := range s.SeenRequests {
		keys = append(keys, key)
	}
	s.SeenRequests = nil
	s.RecentRequests = nil
	s.seen = make(map[string]bool)
	for _, key := range keys {
		s.addRequest(key)
	}
}

func (s *claudeParserState) seenRequest(key string) bool { return s.seen[key] }

func (s *claudeParserState) addRequest(key string) {
	if s.seen[key] {
		return
	}
	s.seen[key] = true
	s.RecentRequests = append(s.RecentRequests, key)
	if len(s.RecentRequests) > maxRecentRequests {
		delete(s.seen, s.RecentRequests[0])
		s.RecentRequests = s.RecentRequests[1:]
	}
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
			parserState = claudeParserState{}
		}
	}
	parserState.indexRequests()
	if parserState.MessageIndex == 0 && workingState.MessageCount > 0 {
		parserState.MessageIndex = workingState.MessageCount
	}

	if parserState.Repository == nil {
		parserState.Repository = &importer.RepositoryMetadata{}
		if err := replayRepositoryPrefix(ctx, file, workingState.ByteOffset, func(line []byte) {
			var entry importer.ClaudeJSONLEntry
			if json.Unmarshal(line, &entry) != nil {
				return
			}
			parserState.Repository.ObserveClaude(entry)
			// Empty legacy state also lacks the full import's usage dedup keys.
			if entry.Type == "assistant" && entry.Message != nil && entry.Message.Usage != nil && entry.Message.ID != "" && entry.RequestID != "" {
				parserState.addRequest(entry.Message.ID + ":" + entry.RequestID)
			}
		}); err != nil {
			return nil, err
		}
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

		parserState.Repository.ObserveClaude(entry)

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
				if parserState.seenRequest(dedupKey) {
					commitLine()
					if readErr == io.EOF {
						break
					}
					continue
				}
				parserState.addRequest(dedupKey)
			}

			log, metrics := claudeParser.UsageRecords(entry, ts, entrySessionID)
			result.Logs = append(result.Logs, log)
			result.Metrics = append(result.Metrics, metrics...)
		}

		workingState.RecordCount++
		commitLine()
		if readErr == io.EOF {
			break
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
