package watcher

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/tobilg/ai-observer/internal/importer"
	"github.com/tobilg/ai-observer/internal/storage"
)

func TestClaudeIncrementalParserRetainsPartialTrailingLine(t *testing.T) {
	validLine := mustClaudeLine(t, "msg-1", "hello")
	partialLine := `{"type":"assistant","timestamp":"2025-01-02T10:00:01Z"`
	path := writeWatcherTestFile(t, validLine+"\n"+partialLine)

	state := &storage.ImportState{Source: string(importer.SourceClaude), FilePath: path}
	result, err := (&claudeIncrementalParser{}).ParseIncremental(context.Background(), path, state)
	if err != nil {
		t.Fatalf("ParseIncremental failed: %v", err)
	}
	if len(result.Logs) == 0 {
		t.Fatal("expected parsed logs from complete line")
	}
	if got, want := state.ByteOffset, int64(len(validLine)+1); got != want {
		t.Fatalf("ByteOffset = %d, want %d", got, want)
	}
}

func TestClaudeIncrementalParserCommitsValidEOFLine(t *testing.T) {
	validLine := mustClaudeLine(t, "msg-1", "hello")
	path := writeWatcherTestFile(t, validLine)

	state := &storage.ImportState{Source: string(importer.SourceClaude), FilePath: path}
	result, err := (&claudeIncrementalParser{}).ParseIncremental(context.Background(), path, state)
	if err != nil {
		t.Fatalf("ParseIncremental failed: %v", err)
	}
	if len(result.Logs) == 0 {
		t.Fatal("expected parsed logs")
	}
	if got, want := state.ByteOffset, int64(len(validLine)); got != want {
		t.Fatalf("ByteOffset = %d, want %d", got, want)
	}
}

func TestCodexIncrementalParserSkipsMalformedCompleteLine(t *testing.T) {
	validLine := mustCodexMessageLine(t, "hello")
	content := "{malformed-json}\n" + validLine
	path := writeWatcherTestFile(t, content)

	state := &storage.ImportState{Source: string(importer.SourceCodex), FilePath: path}
	result, err := (&codexIncrementalParser{}).ParseIncremental(context.Background(), path, state)
	if err != nil {
		t.Fatalf("ParseIncremental failed: %v", err)
	}
	if len(result.Logs) != 1 {
		t.Fatalf("logs = %d, want 1", len(result.Logs))
	}
	if got, want := state.ByteOffset, int64(len(content)); got != want {
		t.Fatalf("ByteOffset = %d, want %d", got, want)
	}
}

func TestCodexIncrementalParserRetainsPartialTrailingLine(t *testing.T) {
	validLine := mustCodexMessageLine(t, "hello")
	partialLine := `{"timestamp":"2025-01-02T10:00:01Z","type":"response_item","payload":`
	path := writeWatcherTestFile(t, validLine+"\n"+partialLine)

	state := &storage.ImportState{Source: string(importer.SourceCodex), FilePath: path}
	result, err := (&codexIncrementalParser{}).ParseIncremental(context.Background(), path, state)
	if err != nil {
		t.Fatalf("ParseIncremental failed: %v", err)
	}
	if len(result.Logs) != 1 {
		t.Fatalf("logs = %d, want 1", len(result.Logs))
	}
	if got, want := state.ByteOffset, int64(len(validLine)+1); got != want {
		t.Fatalf("ByteOffset = %d, want %d", got, want)
	}
}

func writeWatcherTestFile(t *testing.T, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("writing test file: %v", err)
	}
	return path
}

func mustClaudeLine(t *testing.T, id, text string) string {
	t.Helper()

	entry := importer.ClaudeJSONLEntry{
		Type:      "assistant",
		Timestamp: "2025-01-02T10:00:00Z",
		SessionID: "session-1",
		RequestID: "req-" + id,
		Message: &importer.ClaudeMessage{
			ID:    id,
			Model: "claude-sonnet-4-20250514",
			Role:  "assistant",
			Type:  "message",
			Content: []importer.ClaudeContent{
				{Type: "text", Text: text},
			},
			Usage: &importer.ClaudeUsage{
				InputTokens:  10,
				OutputTokens: 5,
			},
		},
	}
	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("marshaling Claude entry: %v", err)
	}
	return string(data)
}

func mustCodexMessageLine(t *testing.T, text string) string {
	t.Helper()

	payload, err := json.Marshal(importer.CodexResponseItem{
		Type: "message",
		Role: "assistant",
		Content: []importer.CodexContentItem{
			{Type: "output_text", Text: text},
		},
	})
	if err != nil {
		t.Fatalf("marshaling Codex payload: %v", err)
	}
	entry := importer.CodexJSONLEntry{
		Timestamp: "2025-01-02T10:00:00Z",
		Type:      "response_item",
		Payload:   payload,
	}
	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("marshaling Codex entry: %v", err)
	}
	return string(data)
}
