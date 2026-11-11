package importer

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// Gemini CLI declares message content and tool results as PartListUnion: a
// string, a Part, or a list of strings and Parts. Text parts are joined
// (without thoughts), function calls and responses among the parts become
// tool calls unless the message lists them, and a function response's
// output is its output or error text, else its JSON.
func TestGeminiContentParts(t *testing.T) {
	session := `{"sessionId":"s1","messages":[
		{"id":"m1","timestamp":"2025-01-02T10:00:00Z","type":"user","content":[{"text":"read "},"the readme"]},
		{"id":"m2","timestamp":"2025-01-02T10:00:01Z","type":"gemini","model":"gemini-2.5-pro",
			"content":[{"text":"thinking","thought":true},{"text":"I will read it."},
				{"functionCall":{"id":"c1","name":"read_file","args":{"file_path":"README.md"}}},
				{"functionCall":{"id":"c2","name":"ls","args":{"dir":"."}}},
				{"functionResponse":{"id":"c2","name":"ls","response":{"output":["a","b"]}}}],
			"toolCalls":[{"id":"c1","name":"read_file","args":{"file_path":"README.md"},"status":"error",
				"result":{"functionResponse":{"id":"c1","name":"read_file","response":{"error":"no such file"}}}}]},
		{"id":"m3","timestamp":"2025-01-02T10:00:02Z","type":"gemini","content":{"text":"Done."},
			"toolCalls":[{"id":"c3","name":"shell","result":"plain text result","status":"success"},
				{"id":"c4","name":"noop","result":null}]},
		{"id":"m4","timestamp":"2025-01-02T10:00:03Z","type":"info","content":null}
	]}`
	path := filepath.Join(t.TempDir(), "session-s1.json")
	if err := os.WriteFile(path, []byte(session), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := NewGeminiParser().ParseFile(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	type log struct{ role, body, callID, output string }
	want := []log{
		{"user", "read the readme", "", ""},
		{"assistant", "I will read it.", "", ""},
		{"tool_use", "Tool call: read_file", "c1", ""},
		{"tool_result", "no such file", "c1", "no such file"},
		{"tool_use", "Tool call: ls", "c2", ""},
		{"tool_result", `["a","b"]`, "c2", `["a","b"]`},
		{"assistant", "Done.", "", ""},
		{"tool_use", "Tool call: shell", "c3", ""},
		{"tool_result", "plain text result", "c3", "plain text result"},
		{"tool_use", "Tool call: noop", "c4", ""},
		{"assistant", "", "", ""}, // info messages are shown as the assistant's
	}
	if len(res.Logs) != len(want) {
		for _, l := range res.Logs {
			t.Logf("%q %v", l.Body, l.LogAttributes)
		}
		t.Fatalf("%d logs, want %d", len(res.Logs), len(want))
	}
	for i, w := range want {
		l := res.Logs[i]
		got := log{l.LogAttributes["message.role"], l.Body, l.LogAttributes["tool.call_id"], l.LogAttributes["tool.output"]}
		if got != w {
			t.Errorf("log %d = %+v, want %+v", i, got, w)
		}
	}
	if in := res.Logs[2].LogAttributes["tool.input"]; in != `{"file_path":"README.md"}` {
		t.Errorf("tool input %q", in)
	}
	if res.Logs[3].LogAttributes["success"] != "false" {
		t.Errorf("failed call's result %v", res.Logs[3].LogAttributes)
	}

	// Watch reads the same file the same way
	var s GeminiSession
	if err := json.Unmarshal([]byte(session), &s); err != nil {
		t.Fatal(err)
	}
	records, err := ConvertGeminiMessages(context.Background(), &s, 0, "file_watcher")
	if err != nil || len(records.Logs) != len(want) {
		t.Fatalf("watch converted %d logs, %v", len(records.Logs), err)
	}
}

// Content as a string, as Gemini CLI 0.24 records it, reads as before
func TestGeminiContentString(t *testing.T) {
	var m GeminiMessage
	if err := json.Unmarshal([]byte(`{"id":"m","type":"user","content":"hi","toolCalls":[{"id":"c","name":"x",
		"result":[{"functionResponse":{"id":"c","name":"x","response":{"output":"ok"}}}]}]}`), &m); err != nil {
		t.Fatal(err)
	}
	if m.Content != "hi" || len(m.ToolCalls) != 1 {
		t.Fatalf("message %+v", m)
	}
	if out, ok := m.ToolCalls[0].output(); !ok || out != "ok" {
		t.Errorf("output %q, %v", out, ok)
	}
	if err := json.Unmarshal([]byte(`{"content":[1]}`), &m); err == nil {
		t.Error("content of numbers accepted")
	}
}
