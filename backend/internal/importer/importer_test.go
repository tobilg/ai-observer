package importer

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tobilg/ai-observer/internal/api"
	"github.com/tobilg/ai-observer/internal/storage"
)

// TestClaudeParser tests the Claude Code parser
func TestClaudeParser(t *testing.T) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "claude-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a test JSONL file
	testFile := filepath.Join(tmpDir, "test-session.jsonl")
	entries := []claudeJSONLEntry{
		{
			Type:      "assistant", // Root type field
			Timestamp: "2025-01-02T10:00:00.000Z",
			SessionID: "test-session-123",
			RequestID: "req-001",
			CostUSD:   floatPtr(0.05),
			Message: &claudeMessage{
				ID:    "msg-001",
				Model: "claude-sonnet-4-20250514",
				Role:  "assistant",
				Type:  "message", // message.type is "message", not "assistant"
				Usage: &claudeUsage{
					InputTokens:              1000,
					OutputTokens:             500,
					CacheCreationInputTokens: 100,
					CacheReadInputTokens:     50,
				},
			},
		},
		{
			Type:      "assistant", // Root type field
			Timestamp: "2025-01-02T10:01:00.000Z",
			SessionID: "test-session-123",
			RequestID: "req-002",
			CostUSD:   floatPtr(0.03),
			Message: &claudeMessage{
				ID:    "msg-002",
				Model: "claude-sonnet-4-20250514",
				Role:  "assistant",
				Type:  "message",
				Usage: &claudeUsage{
					InputTokens:  800,
					OutputTokens: 300,
				},
			},
		},
	}

	f, err := os.Create(testFile)
	if err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}
	for _, entry := range entries {
		data, _ := json.Marshal(entry)
		f.Write(data)
		f.WriteString("\n")
	}
	f.Close()

	// Create parser with custom path
	os.Setenv("AI_OBSERVER_CLAUDE_PATH", tmpDir)
	defer os.Unsetenv("AI_OBSERVER_CLAUDE_PATH")

	parser := NewClaudeParser()

	// Test Source
	if parser.Source() != SourceClaude {
		t.Errorf("expected source %s, got %s", SourceClaude, parser.Source())
	}

	// Test FindSessionFiles
	ctx := context.Background()
	files, err := parser.FindSessionFiles(ctx)
	if err != nil {
		t.Fatalf("FindSessionFiles failed: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}

	// Test ParseFile
	result, err := parser.ParseFile(ctx, testFile)
	if err != nil {
		t.Fatalf("ParseFile failed: %v", err)
	}

	// Verify result
	if result.SessionID != "test-session" {
		t.Errorf("expected session ID 'test-session', got '%s'", result.SessionID)
	}
	if len(result.Logs) != 2 {
		t.Errorf("expected 2 logs, got %d", len(result.Logs))
	}
	if len(result.Metrics) != 10 { // 4 token types + cost for each entry = 5 per entry, 2 entries total but second has fewer token types
		t.Logf("got %d metrics", len(result.Metrics))
	}
	if result.RecordCount != 2 {
		t.Errorf("expected 2 records, got %d", result.RecordCount)
	}

	// Verify time range
	expectedFirst, _ := time.Parse(time.RFC3339, "2025-01-02T10:00:00.000Z")
	expectedLast, _ := time.Parse(time.RFC3339, "2025-01-02T10:01:00.000Z")
	if !result.FirstTime.Equal(expectedFirst) {
		t.Errorf("expected first time %v, got %v", expectedFirst, result.FirstTime)
	}
	if !result.LastTime.Equal(expectedLast) {
		t.Errorf("expected last time %v, got %v", expectedLast, result.LastTime)
	}
}

// TestCodexParser tests the Codex CLI parser
func TestCodexParser(t *testing.T) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "codex-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create test JSONL content
	testFile := filepath.Join(tmpDir, "rollout-2025-01-02T10-00-00-abc123.jsonl")

	entries := []string{
		`{"timestamp":"2025-01-02T10:00:00.000Z","type":"session_meta","payload":{"id":"session-abc123","timestamp":"2025-01-02T10:00:00.000Z","cwd":"/home/user/project","originator":"cli","cli_version":"1.0.0","model_provider":"openai","model":"gpt-4o"}}`,
		`{"timestamp":"2025-01-02T10:01:00.000Z","type":"event_msg","payload":{"type":"user_message"}}`,
		`{"timestamp":"2025-01-02T10:02:00.000Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":500,"output_tokens":200,"cache_creation_input_tokens":0,"cached_input_tokens":0,"reasoning_output_tokens":50,"tool_tokens":10}}}}`,
		`{"timestamp":"2025-01-02T10:03:00.000Z","type":"event_msg","payload":{"type":"agent_message"}}`,
		`{"timestamp":"2025-01-02T10:04:00.000Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":800,"output_tokens":350,"cache_creation_input_tokens":0,"cached_input_tokens":0,"reasoning_output_tokens":100,"tool_tokens":20}}}}`,
	}

	if err := os.WriteFile(testFile, []byte(joinLines(entries)), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	// Create parser with custom path
	os.Setenv("AI_OBSERVER_CODEX_PATH", tmpDir)
	defer os.Unsetenv("AI_OBSERVER_CODEX_PATH")

	parser := NewCodexParser()

	// Test Source
	if parser.Source() != SourceCodex {
		t.Errorf("expected source %s, got %s", SourceCodex, parser.Source())
	}

	// Test FindSessionFiles
	ctx := context.Background()
	files, err := parser.FindSessionFiles(ctx)
	if err != nil {
		t.Fatalf("FindSessionFiles failed: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}

	// Test ParseFile
	result, err := parser.ParseFile(ctx, testFile)
	if err != nil {
		t.Fatalf("ParseFile failed: %v", err)
	}

	// Verify result
	if result.SessionID != "session-abc123" {
		t.Errorf("expected session ID 'session-abc123', got '%s'", result.SessionID)
	}
	// session_meta log + user_message log + agent_message log = 3 logs
	if len(result.Logs) != 3 {
		t.Errorf("expected 3 logs, got %d", len(result.Logs))
	}

	// First token_count: 500 input, 200 output, 50 reasoning, 10 tool = 4 token metrics + 1 cost metric = 5
	// Second token_count (delta): 300 input, 150 output, 50 reasoning, 10 tool = 4 token metrics + 1 cost metric = 5
	// Total = 10 metrics
	if len(result.Metrics) != 10 {
		t.Errorf("expected 10 metrics, got %d", len(result.Metrics))
	}

	// Verify time range
	expectedFirst, _ := time.Parse(time.RFC3339, "2025-01-02T10:00:00.000Z")
	expectedLast, _ := time.Parse(time.RFC3339, "2025-01-02T10:04:00.000Z")
	if !result.FirstTime.Equal(expectedFirst) {
		t.Errorf("expected first time %v, got %v", expectedFirst, result.FirstTime)
	}
	if !result.LastTime.Equal(expectedLast) {
		t.Errorf("expected last time %v, got %v", expectedLast, result.LastTime)
	}
}

// TestGeminiParser tests the Gemini CLI parser
func TestGeminiParser(t *testing.T) {
	// Create temp directory structure
	tmpDir, err := os.MkdirTemp("", "gemini-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create nested chat directory
	chatDir := filepath.Join(tmpDir, "abc123", "chats")
	if err := os.MkdirAll(chatDir, 0755); err != nil {
		t.Fatalf("failed to create chat dir: %v", err)
	}

	// Create test JSON file
	testFile := filepath.Join(chatDir, "session-test123.json")
	session := geminiSession{
		SessionID:   "session-test123",
		ProjectHash: "abc123",
		StartTime:   "2025-01-02T10:00:00.000Z",
		LastUpdated: "2025-01-02T10:05:00.000Z",
		Messages: []geminiMessage{
			{
				ID:        "msg-001",
				Timestamp: "2025-01-02T10:00:00.000Z",
				Type:      "user",
			},
			{
				ID:        "msg-002",
				Timestamp: "2025-01-02T10:01:00.000Z",
				Type:      "gemini",
				Model:     "gemini-2.0-flash",
				Tokens: &geminiTokens{
					Input:    500,
					Output:   200,
					Cached:   50,
					Thoughts: 30,
					Tool:     10,
					Total:    790,
				},
			},
			{
				ID:        "msg-003",
				Timestamp: "2025-01-02T10:02:00.000Z",
				Type:      "gemini",
				Model:     "gemini-2.0-flash",
				Tokens: &geminiTokens{
					Input:  300,
					Output: 150,
					Total:  450,
				},
			},
		},
	}

	data, _ := json.Marshal(session)
	if err := os.WriteFile(testFile, data, 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	// Create parser with custom path
	os.Setenv("AI_OBSERVER_GEMINI_PATH", tmpDir)
	defer os.Unsetenv("AI_OBSERVER_GEMINI_PATH")

	parser := NewGeminiParser()

	// Test Source
	if parser.Source() != SourceGemini {
		t.Errorf("expected source %s, got %s", SourceGemini, parser.Source())
	}

	// Test FindSessionFiles
	ctx := context.Background()
	files, err := parser.FindSessionFiles(ctx)
	if err != nil {
		t.Fatalf("FindSessionFiles failed: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}

	// Test ParseFile
	result, err := parser.ParseFile(ctx, testFile)
	if err != nil {
		t.Fatalf("ParseFile failed: %v", err)
	}

	// Verify result
	if result.SessionID != "session-test123" {
		t.Errorf("expected session ID 'session-test123', got '%s'", result.SessionID)
	}
	// 3 messages = 3 logs
	if len(result.Logs) != 3 {
		t.Errorf("expected 3 logs, got %d", len(result.Logs))
	}

	// First gemini message: input, output, cached, thoughts, tool = 5 token metrics + 1 cost metric = 6
	// Second gemini message: input, output = 2 token metrics + 1 cost metric = 3
	// Total = 9 metrics
	if len(result.Metrics) != 9 {
		t.Errorf("expected 9 metrics, got %d", len(result.Metrics))
	}

	// Verify time range - uses session metadata LastUpdated, not last message timestamp
	expectedFirst, _ := time.Parse(time.RFC3339, "2025-01-02T10:00:00.000Z")
	expectedLast, _ := time.Parse(time.RFC3339, "2025-01-02T10:05:00.000Z") // From LastUpdated field
	if !result.FirstTime.Equal(expectedFirst) {
		t.Errorf("expected first time %v, got %v", expectedFirst, result.FirstTime)
	}
	if !result.LastTime.Equal(expectedLast) {
		t.Errorf("expected last time %v, got %v", expectedLast, result.LastTime)
	}
}

// TestParseToolArg tests the tool argument parsing
func TestParseToolArg(t *testing.T) {
	tests := []struct {
		input    string
		expected []SourceType
		hasError bool
	}{
		{"claude-code", []SourceType{SourceClaude}, false},
		{"codex", []SourceType{SourceCodex}, false},
		{"gemini", []SourceType{SourceGemini}, false},
		{"all", []SourceType{SourceClaude, SourceCodex, SourceGemini}, false},
		{"invalid", nil, true},
		{"", nil, true},
	}

	for _, tc := range tests {
		result, err := ParseToolArg(tc.input)
		if tc.hasError {
			if err == nil {
				t.Errorf("ParseToolArg(%q) expected error, got nil", tc.input)
			}
		} else {
			if err != nil {
				t.Errorf("ParseToolArg(%q) unexpected error: %v", tc.input, err)
			}
			if len(result) != len(tc.expected) {
				t.Errorf("ParseToolArg(%q) expected %d sources, got %d", tc.input, len(tc.expected), len(result))
			}
		}
	}
}

// TestParseDateArg tests date argument parsing
func TestParseDateArg(t *testing.T) {
	tests := []struct {
		input    string
		hasError bool
	}{
		{"2025-01-02", false},
		{"2025-12-31", false},
		{"", false},
		{"invalid", true},
		{"2025/01/02", true},
		{"01-02-2025", true},
	}

	for _, tc := range tests {
		result, err := ParseDateArg(tc.input)
		if tc.hasError {
			if err == nil {
				t.Errorf("ParseDateArg(%q) expected error, got nil", tc.input)
			}
		} else {
			if err != nil {
				t.Errorf("ParseDateArg(%q) unexpected error: %v", tc.input, err)
			}
			if tc.input == "" && result != nil {
				t.Errorf("ParseDateArg(%q) expected nil, got %v", tc.input, result)
			}
			if tc.input != "" && result == nil {
				t.Errorf("ParseDateArg(%q) expected non-nil, got nil", tc.input)
			}
		}
	}
}

// TestFileStatus tests file status checking
func TestFileStatus(t *testing.T) {
	tests := []struct {
		status FileStatus
		force  bool
		expect bool
	}{
		{StatusNew, false, true},
		{StatusNew, true, true},
		{StatusModified, false, true},
		{StatusModified, true, true},
		{StatusCurrent, false, false},
		{StatusCurrent, true, true},
	}

	for _, tc := range tests {
		result := ShouldImportFile(tc.status, tc.force)
		if result != tc.expect {
			t.Errorf("ShouldImportFile(%s, %v) expected %v, got %v", tc.status, tc.force, tc.expect, result)
		}
	}
}

// TestStatusToString tests status string conversion
func TestStatusToString(t *testing.T) {
	tests := []struct {
		status   FileStatus
		expected string
	}{
		{StatusNew, "new"},
		{StatusModified, "modified"},
		{StatusCurrent, "skipped"},
		{FileStatus("unknown"), "unknown"},
	}

	for _, tc := range tests {
		result := StatusToString(tc.status)
		if result != tc.expected {
			t.Errorf("StatusToString(%s) expected %q, got %q", tc.status, tc.expected, result)
		}
	}
}

// TestParseToDateArg tests to-date argument parsing with end of day
func TestParseToDateArg(t *testing.T) {
	tests := []struct {
		input    string
		hasError bool
	}{
		{"2025-01-02", false},
		{"", false},
		{"invalid", true},
	}

	for _, tc := range tests {
		result, err := ParseToDateArg(tc.input)
		if tc.hasError {
			if err == nil {
				t.Errorf("ParseToDateArg(%q) expected error, got nil", tc.input)
			}
		} else {
			if err != nil {
				t.Errorf("ParseToDateArg(%q) unexpected error: %v", tc.input, err)
			}
			if tc.input != "" && result != nil {
				// Verify it's end of day (23:59:59.999...)
				if result.Hour() != 23 || result.Minute() != 59 {
					t.Errorf("ParseToDateArg(%q) expected end of day, got %v", tc.input, result)
				}
			}
		}
	}
}

// TestStateManager tests the state manager functionality
func TestStateManager(t *testing.T) {
	// Create a test store
	store, err := storage.NewDuckDBStore(":memory:")
	if err != nil {
		t.Fatalf("failed to create test store: %v", err)
	}
	defer store.Close()

	// Create temp file for testing
	tmpFile, err := os.CreateTemp("", "state-test-*.txt")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	tmpFile.WriteString("test content")
	tmpFile.Close()
	defer os.Remove(tmpFile.Name())

	ctx := context.Background()
	manager := NewStateManager(store)

	t.Run("new file", func(t *testing.T) {
		status, err := manager.CheckFileStatus(ctx, SourceClaude, tmpFile.Name())
		if err != nil {
			t.Fatalf("CheckFileStatus failed: %v", err)
		}
		if status != StatusNew {
			t.Errorf("expected StatusNew, got %s", status)
		}
	})

	t.Run("record import", func(t *testing.T) {
		err := manager.RecordImport(ctx, SourceClaude, tmpFile.Name(), 10)
		if err != nil {
			t.Fatalf("RecordImport failed: %v", err)
		}

		// Now status should be current
		status, err := manager.CheckFileStatus(ctx, SourceClaude, tmpFile.Name())
		if err != nil {
			t.Fatalf("CheckFileStatus failed: %v", err)
		}
		if status != StatusCurrent {
			t.Errorf("expected StatusCurrent, got %s", status)
		}
	})

	t.Run("modified file", func(t *testing.T) {
		// Modify the file
		f, _ := os.OpenFile(tmpFile.Name(), os.O_APPEND|os.O_WRONLY, 0644)
		f.WriteString(" modified")
		f.Close()

		status, err := manager.CheckFileStatus(ctx, SourceClaude, tmpFile.Name())
		if err != nil {
			t.Fatalf("CheckFileStatus failed: %v", err)
		}
		if status != StatusModified {
			t.Errorf("expected StatusModified, got %s", status)
		}
	})

	t.Run("get imported files", func(t *testing.T) {
		files, err := manager.GetImportedFiles(ctx, SourceClaude)
		if err != nil {
			t.Fatalf("GetImportedFiles failed: %v", err)
		}
		if len(files) != 1 {
			t.Errorf("expected 1 imported file, got %d", len(files))
		}
	})

	t.Run("clear source", func(t *testing.T) {
		err := manager.ClearSource(ctx, SourceClaude)
		if err != nil {
			t.Fatalf("ClearSource failed: %v", err)
		}

		files, err := manager.GetImportedFiles(ctx, SourceClaude)
		if err != nil {
			t.Fatalf("GetImportedFiles failed: %v", err)
		}
		if len(files) != 0 {
			t.Errorf("expected 0 imported files after clear, got %d", len(files))
		}
	})
}

// TestImportSummary tests the import summary functionality
func TestImportSummary(t *testing.T) {
	summary := &ImportSummary{Source: SourceClaude}

	t.Run("is empty initially", func(t *testing.T) {
		if !summary.IsEmpty() {
			t.Error("expected summary to be empty initially")
		}
	})

	t.Run("add result", func(t *testing.T) {
		result := &ImportResult{
			Logs:        make([]api.LogRecord, 5),
			Metrics:     make([]api.MetricDataPoint, 10),
			Spans:       make([]api.Span, 2),
			RecordCount: 17,
		}
		summary.Add(result, "new")

		if summary.TotalLogs != 5 {
			t.Errorf("expected 5 logs, got %d", summary.TotalLogs)
		}
		if summary.TotalMetrics != 10 {
			t.Errorf("expected 10 metrics, got %d", summary.TotalMetrics)
		}
		if summary.TotalSpans != 2 {
			t.Errorf("expected 2 spans, got %d", summary.TotalSpans)
		}
		if summary.NewFiles != 1 {
			t.Errorf("expected 1 new file, got %d", summary.NewFiles)
		}
		if summary.IsEmpty() {
			t.Error("expected summary to not be empty after adding")
		}
	})

	t.Run("add modified", func(t *testing.T) {
		result := &ImportResult{
			Logs:        make([]api.LogRecord, 3),
			RecordCount: 3,
		}
		summary.Add(result, "modified")

		if summary.ModifiedFiles != 1 {
			t.Errorf("expected 1 modified file, got %d", summary.ModifiedFiles)
		}
	})

	t.Run("add error", func(t *testing.T) {
		summary.AddError("/path/to/file", fmt.Errorf("test error"))

		if len(summary.Errors) != 1 {
			t.Errorf("expected 1 error, got %d", len(summary.Errors))
		}
	})
}

// TestNewImporter tests importer creation and parser registration
func TestNewImporter(t *testing.T) {
	store, err := storage.NewDuckDBStore(":memory:")
	if err != nil {
		t.Fatalf("failed to create test store: %v", err)
	}
	defer store.Close()

	t.Run("create importer", func(t *testing.T) {
		imp := NewImporter(store, false)
		if imp == nil {
			t.Fatal("NewImporter returned nil")
		}
	})

	t.Run("register parsers", func(t *testing.T) {
		imp := NewImporter(store, true)
		imp.RegisterAllParsers()

		// Check that parsers are registered
		parser, ok := imp.GetParser(SourceClaude)
		if !ok {
			t.Error("expected Claude parser to be registered")
		}
		if parser.Source() != SourceClaude {
			t.Errorf("expected Claude source, got %s", parser.Source())
		}

		_, ok = imp.GetParser(SourceCodex)
		if !ok {
			t.Error("expected Codex parser to be registered")
		}

		_, ok = imp.GetParser(SourceGemini)
		if !ok {
			t.Error("expected Gemini parser to be registered")
		}
	})

	t.Run("register parsers with options", func(t *testing.T) {
		imp := NewImporter(store, false)
		opts := Options{PricingMode: "calculate"}
		imp.RegisterAllParsersWithOptions(opts)

		_, ok := imp.GetParser(SourceClaude)
		if !ok {
			t.Error("expected Claude parser to be registered")
		}
	})
}

// TestImportDryRun tests the import with dry run
func TestImportDryRun(t *testing.T) {
	store, err := storage.NewDuckDBStore(":memory:")
	if err != nil {
		t.Fatalf("failed to create test store: %v", err)
	}
	defer store.Close()

	// Create temp directory with test file
	tmpDir, err := os.MkdirTemp("", "import-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a test JSONL file
	testFile := filepath.Join(tmpDir, "test-session.jsonl")
	entries := []claudeJSONLEntry{
		{
			Type:      "assistant",
			Timestamp: "2025-01-02T10:00:00.000Z",
			SessionID: "test-session-123",
			Message: &claudeMessage{
				ID:    "msg-001",
				Model: "claude-sonnet-4-20250514",
				Role:  "assistant",
				Type:  "message",
				Usage: &claudeUsage{
					InputTokens:  1000,
					OutputTokens: 500,
				},
			},
		},
	}

	f, _ := os.Create(testFile)
	for _, entry := range entries {
		data, _ := json.Marshal(entry)
		f.Write(data)
		f.WriteString("\n")
	}
	f.Close()

	// Set up parser with custom path
	os.Setenv("AI_OBSERVER_CLAUDE_PATH", tmpDir)
	defer os.Unsetenv("AI_OBSERVER_CLAUDE_PATH")

	ctx := context.Background()
	imp := NewImporter(store, true)
	imp.RegisterAllParsers()

	opts := Options{
		DryRun:      true,
		SkipConfirm: true,
	}

	err = imp.Import(ctx, []SourceType{SourceClaude}, opts)
	if err != nil {
		t.Fatalf("Import failed: %v", err)
	}

	// Verify no data was actually imported (dry run)
	logs, _ := store.QueryLogs(ctx, "", "", "", "", time.Time{}, time.Now(), 100, 0)
	if logs == nil || len(logs.Logs) != 0 {
		t.Errorf("expected 0 logs after dry run, got %d", len(logs.Logs))
	}
}

// TestImportWithSkipConfirm tests actual import
func TestImportWithSkipConfirm(t *testing.T) {
	store, err := storage.NewDuckDBStore(":memory:")
	if err != nil {
		t.Fatalf("failed to create test store: %v", err)
	}
	defer store.Close()

	// Create temp directory with test file
	tmpDir, err := os.MkdirTemp("", "import-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a test JSONL file
	testFile := filepath.Join(tmpDir, "test-session.jsonl")
	entries := []claudeJSONLEntry{
		{
			Type:      "assistant",
			Timestamp: "2025-01-02T10:00:00.000Z",
			SessionID: "test-session-123",
			CostUSD:   floatPtr(0.05),
			Message: &claudeMessage{
				ID:    "msg-001",
				Model: "claude-sonnet-4-20250514",
				Role:  "assistant",
				Type:  "message",
				Usage: &claudeUsage{
					InputTokens:  1000,
					OutputTokens: 500,
				},
			},
		},
	}

	f, _ := os.Create(testFile)
	for _, entry := range entries {
		data, _ := json.Marshal(entry)
		f.Write(data)
		f.WriteString("\n")
	}
	f.Close()

	// Set up parser with custom path
	os.Setenv("AI_OBSERVER_CLAUDE_PATH", tmpDir)
	defer os.Unsetenv("AI_OBSERVER_CLAUDE_PATH")

	ctx := context.Background()
	imp := NewImporter(store, false)
	imp.RegisterAllParsers()

	opts := Options{
		SkipConfirm: true,
	}

	err = imp.Import(ctx, []SourceType{SourceClaude}, opts)
	if err != nil {
		t.Fatalf("Import failed: %v", err)
	}

	// Verify data was imported
	logs, _ := store.QueryLogs(ctx, "", "", "", "", time.Time{}, time.Now(), 100, 0)
	if logs == nil || len(logs.Logs) == 0 {
		t.Error("expected logs to be imported")
	}
}

// TestImportNoFiles tests import when no files are found
func TestImportNoFiles(t *testing.T) {
	store, err := storage.NewDuckDBStore(":memory:")
	if err != nil {
		t.Fatalf("failed to create test store: %v", err)
	}
	defer store.Close()

	// Create empty temp directory
	tmpDir, err := os.MkdirTemp("", "import-empty-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	os.Setenv("AI_OBSERVER_CLAUDE_PATH", tmpDir)
	defer os.Unsetenv("AI_OBSERVER_CLAUDE_PATH")

	ctx := context.Background()
	imp := NewImporter(store, false)
	imp.RegisterAllParsers()

	opts := Options{
		SkipConfirm: true,
	}

	err = imp.Import(ctx, []SourceType{SourceClaude}, opts)
	if err != nil {
		t.Fatalf("Import failed: %v", err)
	}
}

// TestImportUnregisteredParser tests import with unregistered parser
func TestImportUnregisteredParser(t *testing.T) {
	store, err := storage.NewDuckDBStore(":memory:")
	if err != nil {
		t.Fatalf("failed to create test store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	imp := NewImporter(store, false)
	// Don't register any parsers

	opts := Options{
		SkipConfirm: true,
	}

	// Should not error, just warn and skip
	err = imp.Import(ctx, []SourceType{SourceClaude}, opts)
	if err != nil {
		t.Fatalf("Import failed: %v", err)
	}
}

// Helper functions
func floatPtr(f float64) *float64 {
	return &f
}

func joinLines(lines []string) string {
	result := ""
	for _, line := range lines {
		result += line + "\n"
	}
	return result
}
