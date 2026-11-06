package importer

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// oversizedText makes a JSONL line larger than the 1 MB limit the parsers'
// bufio.Scanner used to have; such lines made the whole file fail to import.
var oversizedText = strings.Repeat("x", 3*1024*1024)

func TestJSONLLinesHandlesOversizedLines(t *testing.T) {
	big := `{"b":"` + oversizedText + `"}`
	// Blank lines are skipped and the last line has no trailing newline.
	input := "{\"a\":1}\n\n   \n" + big + "\n{\"c\":3}"

	var lines []string
	for line, err := range jsonlLines(strings.NewReader(input)) {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		lines = append(lines, string(line))
	}

	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(lines))
	}
	if lines[0] != `{"a":1}` || lines[2] != `{"c":3}` {
		t.Errorf("unexpected surrounding lines: %q, %q", lines[0], lines[2])
	}
	if lines[1] != big {
		t.Errorf("oversized line was altered: got %d bytes, want %d", len(lines[1]), len(big))
	}
}

func TestClaudeParseFileWithOversizedLine(t *testing.T) {
	usage := &ClaudeUsage{InputTokens: 100, OutputTokens: 50}
	entries := []ClaudeJSONLEntry{
		{
			Type: "assistant", Timestamp: "2025-01-02T10:00:00.000Z", RequestID: "req-1",
			Message: &ClaudeMessage{ID: "msg-1", Model: "claude-sonnet-4-20250514", Role: "assistant", Type: "message", Usage: usage},
		},
		{
			Type: "assistant", Timestamp: "2025-01-02T10:01:00.000Z", RequestID: "req-2",
			Message: &ClaudeMessage{
				ID: "msg-2", Model: "claude-sonnet-4-20250514", Role: "assistant", Type: "message", Usage: usage,
				Content: []ClaudeContent{{Type: "text", Text: oversizedText}},
			},
		},
		{
			Type: "assistant", Timestamp: "2025-01-02T10:02:00.000Z", RequestID: "req-3",
			Message: &ClaudeMessage{ID: "msg-3", Model: "claude-sonnet-4-20250514", Role: "assistant", Type: "message", Usage: usage},
		},
	}

	var lines []string
	for _, entry := range entries {
		data, err := json.Marshal(entry)
		if err != nil {
			t.Fatalf("marshal entry: %v", err)
		}
		lines = append(lines, string(data))
	}
	path := filepath.Join(t.TempDir(), "big-session.jsonl")
	if err := os.WriteFile(path, []byte(joinLines(lines)), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	result, err := NewClaudeParser().ParseFile(context.Background(), path)
	if err != nil {
		t.Fatalf("ParseFile failed on oversized line: %v", err)
	}
	if result.RecordCount != 3 {
		t.Errorf("expected 3 records, got %d", result.RecordCount)
	}
	wantLast, _ := time.Parse(time.RFC3339, "2025-01-02T10:02:00Z")
	if !result.LastTime.Equal(wantLast) {
		t.Errorf("records after the oversized line were not parsed: last time %v, want %v", result.LastTime, wantLast)
	}
}

func TestCodexParseFileWithOversizedLine(t *testing.T) {
	lines := []string{
		`{"timestamp":"2025-01-02T10:00:00.000Z","type":"session_meta","payload":{"id":"session-abc123","timestamp":"2025-01-02T10:00:00.000Z","cwd":"/home/user/project","originator":"cli","cli_version":"1.0.0","model_provider":"openai","model":"gpt-4o"}}`,
		`{"timestamp":"2025-01-02T10:01:00.000Z","type":"event_msg","payload":{"type":"user_message","message":"` + oversizedText + `"}}`,
		`{"timestamp":"2025-01-02T10:02:00.000Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":500,"output_tokens":200,"cached_input_tokens":0,"reasoning_output_tokens":50}}}}`,
	}
	path := filepath.Join(t.TempDir(), "rollout-2025-01-02T10-00-00-abc123.jsonl")
	if err := os.WriteFile(path, []byte(joinLines(lines)), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	result, err := NewCodexParser().ParseFile(context.Background(), path)
	if err != nil {
		t.Fatalf("ParseFile failed on oversized line: %v", err)
	}
	wantLast, _ := time.Parse(time.RFC3339, "2025-01-02T10:02:00Z")
	if !result.LastTime.Equal(wantLast) {
		t.Errorf("records after the oversized line were not parsed: last time %v, want %v", result.LastTime, wantLast)
	}
	if len(result.Metrics) == 0 {
		t.Error("expected token metrics from the entry after the oversized line")
	}
}

// A final line without newline counts as read when it is valid JSON, so watch
// does not import it again; one still being written does not
func TestJSONLLinesCountsCompleteFinalLine(t *testing.T) {
	for _, tc := range []struct {
		input string
		want  int64
	}{
		{"{\"a\":1}\n{\"b\":2}", 15},
		{"{\"a\":1}\n{\"b\":", 8},
		{"{\"a\":1}\n", 8},
	} {
		var complete int64
		for _, err := range jsonlLinesCounted(strings.NewReader(tc.input), &complete) {
			if err != nil {
				t.Fatal(err)
			}
		}
		if complete != tc.want {
			t.Errorf("%q: complete = %d, want %d", tc.input, complete, tc.want)
		}
	}
}
