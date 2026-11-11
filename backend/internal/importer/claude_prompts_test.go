package importer

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// Lines as Claude Code 2.1 writes them: a typed prompt has its text as a
// string (content), the other entries lists of blocks
var claudePromptLines = []string{
	`{"parentUuid":"0fc44927-e54f-4748-bf8d-f9e34f8154c7","isSidechain":false,"promptId":"d31ffa03-e5a4-4c93-ae1c-60882519c1d5","type":"user","message":{"role":"user","content":"ok, claude works. I thought this should be redacted now?"},"uuid":"9a91d9db-f5b1-4460-937a-e5b73a3725b9","timestamp":"2026-11-08T17:58:24.750Z","permissionMode":"auto","origin":{"kind":"human"},"promptSource":"typed","turnOrigin":"human","turnPosition":{"promptIndex":11,"turnIndex":11},"userType":"external","entrypoint":"cli","cwd":"/home/user/project","sessionId":"4e9234c7-6725-47a1-92e1-d9db74e65420","version":"2.1.319","gitBranch":"main"}`,
	`{"parentUuid":"9a91d9db-f5b1-4460-937a-e5b73a3725b9","isSidechain":false,"message":{"model":"claude-opus-5-5","id":"msg_01","type":"message","role":"assistant","content":[{"type":"tool_use","id":"toolu_01","name":"Bash","input":{"command":"grep -rn email"}}],"usage":{"input_tokens":3,"output_tokens":40,"cache_read_input_tokens":1000,"cache_creation_input_tokens":0}},"requestId":"req_01","type":"assistant","uuid":"a1","timestamp":"2026-11-08T17:58:38.399Z","sessionId":"4e9234c7-6725-47a1-92e1-d9db74e65420","cwd":"/home/user/project","version":"2.1.319","gitBranch":"main"}`,
	`{"parentUuid":"a1","isSidechain":false,"type":"user","message":{"role":"user","content":[{"tool_use_id":"toolu_01","type":"tool_result","content":"redact.go","is_error":false}]},"uuid":"r1","timestamp":"2026-11-08T17:58:38.854Z","toolUseResult":{"stdout":"redact.go","stderr":"","interrupted":false},"sessionId":"4e9234c7-6725-47a1-92e1-d9db74e65420","cwd":"/home/user/project","version":"2.1.319","gitBranch":"main"}`,
	`{"parentUuid":"r1","isSidechain":false,"message":{"model":"claude-opus-5-5","id":"msg_02","type":"message","role":"assistant","content":[{"type":"text","text":"That's how the default works, not a bug."}],"usage":{"input_tokens":5,"output_tokens":12,"cache_read_input_tokens":2000,"cache_creation_input_tokens":0}},"requestId":"req_02","type":"assistant","uuid":"3589fe7b","timestamp":"2026-11-08T17:59:04.068Z","sessionId":"4e9234c7-6725-47a1-92e1-d9db74e65420","cwd":"/home/user/project","version":"2.1.319","gitBranch":"main"}`,
}

// Typed prompts, whose content is a string, are imported (they were skipped
// with their whole line since transcripts were first imported, v0.3.0), and
// imported messages carry the identities OTLP events of the same messages
// carry (internal/transcript merges the two)
func TestClaudeParserImportsTypedPromptsAndIdentities(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "4e9234c7-6725-47a1-92e1-d9db74e65420.jsonl")
	if err := os.WriteFile(path, []byte(joinLines(claudePromptLines)), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := NewClaudeParser().ParseFile(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	var messages []map[string]string
	bodies := map[string]string{}
	for _, l := range result.Logs {
		if l.LogAttributes["event.name"] != "transcript.message" {
			continue
		}
		messages = append(messages, l.LogAttributes)
		bodies[l.LogAttributes["message.role"]] = l.Body
	}
	if len(messages) != 4 {
		t.Fatalf("%d transcript messages, want prompt, tool call, tool result and reply: %v", len(messages), messages)
	}
	want := []map[string]string{
		{"message.role": "user", "message.uuid": "9a91d9db-f5b1-4460-937a-e5b73a3725b9"},
		{"message.role": "tool_use", "message.uuid": "a1", "request_id": "req_01", "tool.use_id": "toolu_01", "tool.name": "Bash"},
		{"message.role": "tool_result", "message.uuid": "r1", "tool.use_id": "toolu_01"},
		{"message.role": "assistant", "message.uuid": "3589fe7b", "request_id": "req_02"},
	}
	for i, attrs := range want {
		for k, v := range attrs {
			if messages[i][k] != v {
				t.Errorf("message %d: %s = %q, want %q", i, k, messages[i][k], v)
			}
		}
	}
	if bodies["user"] != "ok, claude works. I thought this should be redacted now?" {
		t.Errorf("prompt text %q", bodies["user"])
	}
	if result.FirstTime.Format("15:04:05.000") != "17:58:24.750" {
		t.Errorf("session starts at %s, want the prompt's time", result.FirstTime)
	}
}

// A message whose content is a string is one text block; an empty string
// none
func TestClaudeMessageStringContent(t *testing.T) {
	var m ClaudeMessage
	if err := m.UnmarshalJSON([]byte(`{"role":"user","content":"hi"}`)); err != nil || len(m.Content) != 1 || m.Content[0].Type != "text" || m.Content[0].Text != "hi" {
		t.Fatalf("string content: %+v, %v", m, err)
	}
	if err := m.UnmarshalJSON([]byte(`{"role":"user","content":""}`)); err != nil || len(m.Content) != 0 {
		t.Fatalf("empty string content: %+v, %v", m, err)
	}
	if err := m.UnmarshalJSON([]byte(`{"role":"assistant","model":"m","content":[{"type":"text","text":"a"}]}`)); err != nil || len(m.Content) != 1 || m.Model != "m" {
		t.Fatalf("list content: %+v, %v", m, err)
	}
	if err := m.UnmarshalJSON([]byte(`{"content":7}`)); err == nil {
		t.Fatal("content of another type is an error")
	}
}
