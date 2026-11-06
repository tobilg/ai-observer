package watcher

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

// Watching a Gemini session message by message stores the same records as
// importing it at once
func TestGeminiWatchMatchesImport(t *testing.T) {
	session := importer.GeminiSession{
		SessionID: "s1",
		StartTime: "2025-01-02T10:00:00Z",
		Messages: []importer.GeminiMessage{
			{ID: "m1", Timestamp: "2025-01-02T10:00:00Z", Type: "user", Content: "hi"},
			{ID: "m2", Timestamp: "2025-01-02T10:00:01Z", Type: "gemini", Model: "gemini-2.5-flash",
				Tokens:    &importer.GeminiTokens{Input: 1000, Cached: 800, Output: 50, Thoughts: 20},
				ToolCalls: []importer.GeminiToolCall{{Name: "read_file", Status: "success"}}},
			{ID: "m3", Timestamp: "2025-01-02T10:00:02Z", Type: "gemini", Model: "gemini-2.5-flash",
				Tokens: &importer.GeminiTokens{Input: 1200, Cached: 1000, Output: 30}},
		},
	}
	data, err := json.Marshal(session)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "session-s1.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	imported, err := importer.NewGeminiParser().ParseFile(ctx, path)
	if err != nil {
		t.Fatal(err)
	}

	// Watch read the first two messages earlier and now the third
	parser := &geminiIncrementalParser{}
	first, err := importer.ConvertGeminiMessages(ctx, &importer.GeminiSession{SessionID: "s1", Messages: session.Messages[:2]}, 0, "file_watcher")
	if err != nil {
		t.Fatal(err)
	}
	state := &storage.ImportState{FilePath: path, MessageCount: 2}
	rest, err := parser.ParseIncremental(ctx, path, state)
	if err != nil {
		t.Fatal(err)
	}
	watchedLogs := append(first.Logs, rest.Logs...)
	watchedMetrics := append(first.Metrics, rest.Metrics...)

	if len(watchedLogs) != len(imported.Logs) || len(watchedMetrics) != len(imported.Metrics) {
		t.Fatalf("watch stored %d logs and %d metrics, import %d and %d",
			len(watchedLogs), len(watchedMetrics), len(imported.Logs), len(imported.Metrics))
	}
	for i, log := range imported.Logs {
		watched := watchedLogs[i]
		if watched.Body != log.Body || watched.LogAttributes["message.index"] != log.LogAttributes["message.index"] {
			t.Errorf("log %d: watch %q (index %s), import %q (index %s)", i,
				watched.Body, watched.LogAttributes["message.index"], log.Body, log.LogAttributes["message.index"])
		}
	}
	for i, m := range imported.Metrics {
		watched := watchedMetrics[i]
		if watched.MetricName != m.MetricName || watched.Attributes["type"] != m.Attributes["type"] || *watched.Value != *m.Value {
			t.Errorf("metric %d: watch %s %s=%v, import %s %s=%v", i,
				watched.MetricName, watched.Attributes["type"], *watched.Value, m.MetricName, m.Attributes["type"], *m.Value)
		}
	}
}

// Watching a Codex session stores the same records as importing it, also when
// watch resumes in the middle of the file
func TestCodexWatchMatchesImport(t *testing.T) {
	lines := []string{
		`{"timestamp":"2025-01-02T10:00:00Z","type":"session_meta","payload":{"id":"c1","model":"gpt-5","cwd":"/tmp/p"}}`,
		mustCodexMessageLine(t, "fix the bug"),
		`{"timestamp":"2025-01-02T10:00:02Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":1000,"cached_input_tokens":200,"output_tokens":50}}}}`,
		`{"timestamp":"2025-01-02T10:00:03Z","type":"response_item","payload":{"type":"function_call","name":"shell","arguments":"{}","call_id":"x"}}`,
		`{"timestamp":"2025-01-02T10:00:04Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":1500,"cached_input_tokens":900,"output_tokens":80}}}}`,
	}
	head := strings.Join(lines[:3], "\n") + "\n"
	path := writeWatcherTestFile(t, head+strings.Join(lines[3:], "\n")+"\n")
	ctx := context.Background()

	imported, err := importer.NewCodexParser().ParseFile(ctx, path)
	if err != nil {
		t.Fatal(err)
	}

	parser := &codexIncrementalParser{}
	state := &storage.ImportState{Source: string(importer.SourceCodex), FilePath: path}
	first, err := parser.ParseIncremental(ctx, path, &storage.ImportState{FilePath: path, ByteOffset: 0})
	if err != nil {
		t.Fatal(err)
	}
	// Resume after the first three lines, as if watch had read them before
	state.ByteOffset = int64(len(head))
	rest, err := parser.ParseIncremental(ctx, path, state)
	if err != nil {
		t.Fatal(err)
	}
	var importedRest []float64
	for _, m := range imported.Metrics {
		if !m.Timestamp.Before(time.Date(2025, 1, 2, 10, 0, 3, 0, time.UTC)) {
			importedRest = append(importedRest, *m.Value)
		}
	}
	var watchedRest []float64
	for _, m := range rest.Metrics {
		watchedRest = append(watchedRest, *m.Value)
	}
	if fmt.Sprint(watchedRest) != fmt.Sprint(importedRest) {
		t.Errorf("resumed watch metrics %v, import %v", watchedRest, importedRest)
	}
	if len(first.Logs) != len(imported.Logs) || len(first.Metrics) != len(imported.Metrics) {
		t.Errorf("watch stored %d logs and %d metrics, import %d and %d",
			len(first.Logs), len(first.Metrics), len(imported.Logs), len(imported.Metrics))
	}
	for i, log := range imported.Logs {
		if i < len(first.Logs) && (first.Logs[i].Body != log.Body || first.Logs[i].LogAttributes["message.index"] != log.LogAttributes["message.index"] ||
			first.Logs[i].LogAttributes["session.id"] != log.LogAttributes["session.id"]) {
			t.Errorf("log %d: watch %q %v, import %q %v", i, first.Logs[i].Body, first.Logs[i].LogAttributes, log.Body, log.LogAttributes)
		}
	}
}
