package watcher

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tobilg/ai-observer/internal/api"
	"github.com/tobilg/ai-observer/internal/importer"
	"github.com/tobilg/ai-observer/internal/storage"
)

// Gemini CLI rewrites its session file and adds to its last message later:
// a response's tool calls once they ran, its tokens when they arrive after
// its text. Watch stores what was added, and in the end the same records as
// an import of the final file.
func TestGeminiWatchStoresWhatTheLastMessageGains(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "session-s1.json")
	session := importer.GeminiSession{SessionID: "s1", StartTime: "2025-01-02T10:00:00Z", Messages: []importer.GeminiMessage{
		{ID: "m1", Timestamp: "2025-01-02T10:00:00Z", Type: "user", Content: "read the readme"},
		{ID: "m2", Timestamp: "2025-01-02T10:00:01Z", Type: "gemini", Model: "gemini-2.5-pro", Content: "I will read it.",
			Tokens: &importer.GeminiTokens{Input: 1000, Cached: 800, Output: 50, Thoughts: 20}},
	}}
	write := func() {
		t.Helper()
		data, err := json.Marshal(session)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	parser := &geminiIncrementalParser{}
	state := &storage.ImportState{FilePath: path}
	var logs []api.LogRecord
	var metrics []api.MetricDataPoint
	run := func(wantLogs, wantMetrics int) {
		t.Helper()
		res, err := parser.ParseIncremental(ctx, path, state)
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Logs) != wantLogs || len(res.Metrics) != wantMetrics {
			t.Fatalf("watch stored %d logs and %d metrics, want %d and %d", len(res.Logs), len(res.Metrics), wantLogs, wantMetrics)
		}
		logs, metrics = append(logs, res.Logs...), append(metrics, res.Metrics...)
	}

	write()
	run(2, 5) // the prompt, the reply and its usage (4 token types, cost)
	run(0, 0)

	// The reply's tool call, once it ran
	session.Messages[1].ToolCalls = []importer.GeminiToolCall{{ID: "read_file-1", Name: "read_file", Args: json.RawMessage(`{"file_path":"README.md"}`),
		Status: "success", Timestamp: "2025-01-02T10:00:05Z",
		Result: []importer.GeminiToolResult{{FunctionResponse: &importer.GeminiFunctionResponse{ID: "read_file-1", Name: "read_file",
			Response: &importer.GeminiFunctionResponseData{Output: "# Project"}}}}}}
	write()
	run(2, 0) // its use and result

	// A response recorded before its tokens arrived, and a second tool call
	// of the first one
	session.Messages = append(session.Messages, importer.GeminiMessage{ID: "m3", Timestamp: "2025-01-02T10:00:09Z", Type: "gemini",
		Model: "gemini-2.5-pro", Content: "Done."})
	write()
	run(1, 0)
	session.Messages[2].Tokens = &importer.GeminiTokens{Input: 1300, Cached: 1000, Output: 10}
	write()
	run(0, 4) // input, output, cache, cost
	run(0, 0)

	imported, err := importer.NewGeminiParser().ParseFile(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != len(imported.Logs) || len(metrics) != len(imported.Metrics) {
		t.Fatalf("watch stored %d logs and %d metrics, import %d and %d", len(logs), len(metrics), len(imported.Logs), len(imported.Metrics))
	}
	for i, l := range imported.Logs {
		if logs[i].Body != l.Body || logs[i].LogAttributes["message.index"] != l.LogAttributes["message.index"] ||
			logs[i].LogAttributes["tool.call_id"] != l.LogAttributes["tool.call_id"] {
			t.Errorf("log %d: watch %q %v, import %q %v", i, logs[i].Body, logs[i].LogAttributes, l.Body, l.LogAttributes)
		}
	}
	for i, m := range imported.Metrics {
		if metrics[i].MetricName != m.MetricName || *metrics[i].Value != *m.Value || metrics[i].Attributes["session.id"] != "s1" {
			t.Errorf("metric %d: watch %s=%v, import %s=%v", i, metrics[i].MetricName, *metrics[i].Value, m.MetricName, *m.Value)
		}
	}
}

// A full import records what it stored of the last message, so watch
// stores what Gemini CLI adds to it afterwards (its tool calls once they
// ran, its tokens when they arrive late), as an import of the final file
// would
func TestGeminiWatchAfterFullImport(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	chats := filepath.Join(dir, "2d31728c", "chats")
	if err := os.MkdirAll(chats, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(chats, "session-2025-01-02T10-00-s1.json")
	session := importer.GeminiSession{SessionID: "s1", StartTime: "2025-01-02T10:00:00Z", Messages: []importer.GeminiMessage{
		{ID: "m1", Timestamp: "2025-01-02T10:00:00Z", Type: "user", Content: "read the readme"},
		{ID: "m2", Timestamp: "2025-01-02T10:00:01Z", Type: "gemini", Model: "gemini-2.5-pro", Content: "I will read it."},
	}}
	write := func() {
		t.Helper()
		data, err := json.Marshal(session)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write()
	t.Setenv("AI_OBSERVER_GEMINI_PATH", dir)
	store, err := storage.NewDuckDBStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	imp := importer.NewImporter(store, false)
	imp.SetIO(&strings.Builder{}, strings.NewReader(""))
	imp.RegisterParser(importer.NewGeminiParser())
	if err := imp.Import(ctx, []importer.SourceType{importer.SourceGemini}, importer.Options{SkipConfirm: true}); err != nil {
		t.Fatal(err)
	}
	state, err := store.GetImportState(ctx, string(importer.SourceGemini), path)
	if err != nil || state == nil {
		t.Fatalf("import state %+v, %v", state, err)
	}
	if want := `{"messageCount":2,"last":{"logs":1,"tokens":false}}`; state.ParserState != want {
		t.Errorf("import recorded watch state %q, want %q", state.ParserState, want)
	}

	// The reply's tool call once it ran, and its tokens
	session.Messages[1].ToolCalls = []importer.GeminiToolCall{{ID: "read_file-1", Name: "read_file", Args: json.RawMessage(`{"file_path":"README.md"}`),
		Status: "success", Timestamp: "2025-01-02T10:00:05Z",
		Result: []importer.GeminiToolResult{{FunctionResponse: &importer.GeminiFunctionResponse{ID: "read_file-1", Name: "read_file",
			Response: &importer.GeminiFunctionResponseData{Output: "# Project"}}}}}}
	session.Messages[1].Tokens = &importer.GeminiTokens{Input: 1000, Cached: 800, Output: 50}
	write()
	res, err := (&geminiIncrementalParser{}).ParseIncremental(ctx, path, state)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Logs) != 2 || res.Logs[0].LogAttributes["tool.call_id"] != "read_file-1" || len(res.Metrics) != 4 {
		t.Fatalf("watch stored %d logs and %d metrics after the import, want the tool use and result and 4 usage points", len(res.Logs), len(res.Metrics))
	}
	imported, err := importer.NewGeminiParser().ParseFile(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	for i, l := range imported.Logs[2:] {
		if res.Logs[i].Body != l.Body || res.Logs[i].LogAttributes["message.index"] != l.LogAttributes["message.index"] {
			t.Errorf("log %d: watch %q %v, import %q %v", i, res.Logs[i].Body, res.Logs[i].LogAttributes, l.Body, l.LogAttributes)
		}
	}

	// A ranged import that left out later records leaves watch to read the
	// file again
	if err := imp.Import(ctx, []importer.SourceType{importer.SourceGemini}, importer.Options{SkipConfirm: true, Force: true,
		ToDate: ptrTime(time.Date(2025, 1, 2, 10, 0, 2, 0, time.UTC))}); err != nil {
		t.Fatal(err)
	}
	if state, err := store.GetImportState(ctx, string(importer.SourceGemini), path); err != nil || state.ParserState != "" || state.MessageCount != 0 {
		t.Errorf("ranged import state %+v, %v", state, err)
	}
}

func ptrTime(t time.Time) *time.Time { return &t }

// At the start of watching without an import, and after imports of
// earlier versions, which record only the message count, the last
// message's records are taken as complete
func TestGeminiWatchAfterImport(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "session-s1.json")
	session := importer.GeminiSession{SessionID: "s1", Messages: []importer.GeminiMessage{
		{ID: "m1", Timestamp: "2025-01-02T10:00:00Z", Type: "user", Content: "hi"},
	}}
	data, _ := json.Marshal(session)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	state := &storage.ImportState{FilePath: path, MessageCount: 1}
	res, err := (&geminiIncrementalParser{}).ParseIncremental(ctx, path, state)
	if err != nil || len(res.Logs) != 0 || state.ParserState != "" {
		t.Fatalf("unchanged file: %+v, %v, state %q", res, err, state.ParserState)
	}
}
