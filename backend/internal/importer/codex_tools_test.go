package importer

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/tobilg/ai-observer/internal/api"
)

// codexToolsFixture is a synthesized Codex session (testdata) with a record
// of each kind of tool call: code mode's exec cell and the executions of the
// tools it ran, function and freeform tools with executions before and after
// their outputs, hosted web search and image generation, local shell and
// tool search calls of earlier versions, and the end events of the legacy
// history mode
const codexToolsFixture = "testdata/codex_tools.jsonl"

// parseCodexFixture imports the fixture and returns its tool messages by
// call ID and role
func parseCodexFixture(t *testing.T) (*ImportResult, map[string]map[string][]api.LogRecord) {
	t.Helper()
	result, err := (&CodexParser{}).ParseFile(context.Background(), codexToolsFixture)
	if err != nil {
		t.Fatal(err)
	}
	return result, toolMessagesByCall(t, result.Logs)
}

func toolMessagesByCall(t *testing.T, logs []api.LogRecord) map[string]map[string][]api.LogRecord {
	t.Helper()
	calls := map[string]map[string][]api.LogRecord{}
	for _, l := range logs {
		role := l.LogAttributes["message.role"]
		if role != "tool_use" && role != "tool_result" {
			continue
		}
		id := l.LogAttributes["tool.call_id"]
		if id == "" {
			t.Errorf("tool message without call ID: %v", l.LogAttributes)
			continue
		}
		if calls[id] == nil {
			calls[id] = map[string][]api.LogRecord{}
		}
		calls[id][role] = append(calls[id][role], l)
	}
	return calls
}

func TestCodexImportStoresEveryToolCallOnce(t *testing.T) {
	type want struct {
		name, namespace, mcpServer string
		input, output              string // contained in tool.input, equal to tool.output
		success                    string // "" when not recorded
		exitCode, durationMs       string
	}
	cases := map[string]want{
		// code mode: the exec cell and the tools it ran
		"call_exec1": {name: "exec", input: "tools.exec_command", output: "ok  \tdemo\t0.2s\n[image]"},
		"exec-1":     {name: "exec_command", input: `{"cmd":"go test ./...","workdir":"/work/demo"}`, output: "ok  \tdemo\t0.2s\n", success: "true", exitCode: "0", durationMs: "2500"},
		"exec-2": {name: "apply_patch", input: "*** Update File: /work/demo/a.go\n@@ -1 +1 @@\n-a\n+b\n",
			output: "Success. Updated the following files:\nA b.go\nM a.go\n", success: "true"},
		"exec-3": {name: "github.fetch", mcpServer: "codex_apps", input: `{"url":"https://example.com/issue/1"}`, output: "issue fetched", success: "true", durationMs: "300"},
		"exec-4": {name: "view_image", input: `{"path":"/work/demo/shot.png"}`},
		"exec-5": {name: "web_search", namespace: "web", input: `"query":"duckdb zonemaps"`, output: "Zonemaps https://duckdb.org/z", durationMs: "800"},
		// the model's calls: an execution before the output adds to it, one
		// after the output adds nothing
		"call_sleep1": {name: "sleep", namespace: "clock", input: `{"duration_ms":1000}`, output: "slept 1000 ms", durationMs: "1000"},
		"call_cmd1":   {name: "exec_command", input: `"cmd":"ls missing"`, output: "Process exited with code 1\nOutput:\nls: missing: No such file or directory\n", success: "false", exitCode: "1", durationMs: "250"},
		"call_patch1": {name: "apply_patch", input: "*** Begin Patch", output: "Success. Updated the following files:\nM a.go\n"},
		// hosted tools
		"ws_1": {name: "web_search", input: `"url":"https://example.com/doc"`, output: "Doc https://example.com/doc"},
		"ig_1": {name: "image_generation", input: "a red square", success: "true"},
		// a hosted web search recorded before the model's call
		"ws_3": {name: "web_search", input: `"query":"otlp grpc"`},
		// earlier versions
		"call_ls1": {name: "local_shell", input: `{"cmd":"ls -la","workdir":"/work/demo"}`, output: "total 0\n", success: "true", exitCode: "0", durationMs: "100"},
		"call_ts1": {name: "tool_search", input: `{"query":"github issues"}`, output: `[{"name":"github.fetch"}]`},
		// legacy history mode
		"legacy_patch1": {name: "apply_patch", input: "*** Add File: /work/demo/c.txt\nhi\n", output: "Success.", success: "true"},
		"call_mcp1":     {name: "search", namespace: "mcp__docs", input: `{"q":"zonemaps"}`, output: "found", success: "true", durationMs: "1000"},
		"legacy_mcp2":   {name: "fetch", mcpServer: "docs", output: "server unavailable", success: "false", durationMs: "5"},
		"legacy_ws2":    {name: "web_search", input: `"query":"q2"`},
	}

	result, calls := parseCodexFixture(t)
	for id, w := range cases {
		roles := calls[id]
		if len(roles["tool_use"]) != 1 || len(roles["tool_result"]) != 1 {
			t.Errorf("%s: %d tool_use and %d tool_result messages, want one each", id, len(roles["tool_use"]), len(roles["tool_result"]))
			continue
		}
		use, res := roles["tool_use"][0].LogAttributes, roles["tool_result"][0]
		if use["tool.name"] != w.name || res.LogAttributes["tool.name"] != w.name {
			t.Errorf("%s: tool names %q and %q, want %q", id, use["tool.name"], res.LogAttributes["tool.name"], w.name)
		}
		if use["tool.namespace"] != w.namespace && res.LogAttributes["tool.namespace"] != w.namespace {
			t.Errorf("%s: namespace %q, want %q", id, use["tool.namespace"], w.namespace)
		}
		if w.mcpServer != "" && (use["mcp_server"] != w.mcpServer || res.LogAttributes["mcp_server"] != w.mcpServer) {
			t.Errorf("%s: MCP server %q, want %q", id, use["mcp_server"], w.mcpServer)
		}
		if !strings.Contains(use["tool.input"], w.input) || (w.input == "") != (use["tool.input"] == "") {
			t.Errorf("%s: input %q, want it to contain %q", id, use["tool.input"], w.input)
		}
		// The output once, in tool.output; the body summarizes it
		if res.LogAttributes["tool.output"] != w.output {
			t.Errorf("%s: output %q, want %q", id, res.LogAttributes["tool.output"], w.output)
		}
		wantSize := ""
		if w.output != "" {
			wantSize = fmt.Sprint(len(w.output))
		}
		if res.LogAttributes["tool_result_size_bytes"] != wantSize {
			t.Errorf("%s: tool_result_size_bytes %q, want %q", id, res.LogAttributes["tool_result_size_bytes"], wantSize)
		}
		if !strings.HasPrefix(res.Body, w.name+": ") || strings.Contains(res.Body, "\n") ||
			(w.exitCode != "" && !strings.Contains(res.Body, "exit "+w.exitCode)) ||
			(w.output == "") != strings.HasSuffix(res.Body, "no output") {
			t.Errorf("%s: tool_result body %q", id, res.Body)
		}
		for key, value := range map[string]string{"success": w.success, "exit_code": w.exitCode, "duration_ms": w.durationMs} {
			if res.LogAttributes[key] != value {
				t.Errorf("%s: %s = %q, want %q", id, key, res.LogAttributes[key], value)
			}
		}
		if use["event.name"] != "transcript.message" || use["session.id"] != "sess-tools" || use["import_source"] != "local_jsonl" {
			t.Errorf("%s: tool_use attributes %v", id, use)
		}
		if roles["tool_use"][0].Body != "Tool call: "+w.name {
			t.Errorf("%s: tool_use body %q", id, roles["tool_use"][0].Body)
		}
	}
	for id := range calls {
		if _, ok := cases[id]; !ok {
			t.Errorf("unexpected tool call %s: %v", id, calls[id])
		}
	}

	// Message indices count every message once, in file order; the image
	// generation's result (an image) is not stored
	for i, l := range transcriptLogs(result.Logs) {
		if got := l.LogAttributes["message.index"]; got != fmt.Sprint(i) {
			t.Errorf("message %d has index %s", i, got)
		}
		if strings.Contains(l.Body, "iVBORw0KGgo") || strings.Contains(fmt.Sprint(l.LogAttributes), "iVBORw0KGgo") {
			t.Errorf("message %d stores the generated image", i)
		}
	}
	if result.MessageCount != len(transcriptLogs(result.Logs)) {
		t.Errorf("MessageCount = %d, want %d", result.MessageCount, len(transcriptLogs(result.Logs)))
	}
}

func transcriptLogs(logs []api.LogRecord) []api.LogRecord {
	var out []api.LogRecord
	for _, l := range logs {
		if l.LogAttributes["event.name"] == "transcript.message" {
			out = append(out, l)
		}
	}
	return out
}

// Regression: code mode (Codex 0.144 and later) runs commands, patches and
// MCP tools from its exec cell, and the session file records them only as
// item_completed executions, which imports used to skip, as they skipped
// freeform tool calls (custom_tool_call: exec, apply_patch)
func TestCodexImportStoresCodeModeToolCalls(t *testing.T) {
	_, calls := parseCodexFixture(t)
	for _, id := range []string{"call_exec1", "exec-1", "exec-2", "exec-3", "call_patch1"} {
		if len(calls[id]["tool_use"]) != 1 || len(calls[id]["tool_result"]) != 1 {
			t.Errorf("call %s: %d tool_use, %d tool_result messages, want one each",
				id, len(calls[id]["tool_use"]), len(calls[id]["tool_result"]))
		}
	}
}

// An execution's call and result are recorded at its start and end, never
// after the entry that records it
func TestCodexExecutionTimes(t *testing.T) {
	_, calls := parseCodexFixture(t)
	t0 := time.Date(2026, 10, 26, 7, 33, 20, 0, time.UTC)
	use, res := calls["exec-1"]["tool_use"][0], calls["exec-1"]["tool_result"][0]
	if want := t0.Add(1200 * time.Millisecond); !use.Timestamp.Equal(want) {
		t.Errorf("tool_use at %v, want the execution's start %v", use.Timestamp, want)
	}
	if want := t0.Add(3999 * time.Millisecond); !res.Timestamp.Equal(want) {
		t.Errorf("tool_result at %v, want the execution's end %v", res.Timestamp, want)
	}
	if want := t0.Add(1000 * time.Millisecond); !calls["call_exec1"]["tool_use"][0].Timestamp.Equal(want) {
		t.Errorf("model call at %v, want its entry's time %v", calls["call_exec1"]["tool_use"][0].Timestamp, want)
	}
}

func TestCodexToolTextBound(t *testing.T) {
	short := strings.Repeat("x", codexToolTextLimit)
	if got := codexToolText(short); got != short {
		t.Error("text at the limit was cut")
	}
	// Multibyte characters around both cuts
	long := strings.Repeat("ä", codexToolTextLimit) + "END"
	got := codexToolText(long)
	if !utf8.ValidString(got) {
		t.Fatal("cut text is not valid UTF-8")
	}
	if len(got) > codexToolTextLimit+64 || !strings.HasPrefix(got, "ää") || !strings.HasSuffix(got, "äEND") {
		t.Errorf("cut text: %d bytes, %q…%q", len(got), got[:8], got[len(got)-8:])
	}
	head, rest, ok := strings.Cut(got, "\n[… ")
	count, _, ok2 := strings.Cut(rest, " bytes truncated …]\n")
	tail := got[strings.Index(got, "truncated …]\n")+len("truncated …]\n"):]
	if !ok || !ok2 || count != fmt.Sprint(len(long)-len(head)-len(tail)) {
		t.Errorf("marker does not name the bytes left out: %q", count)
	}

	// A command's output is stored cut, once, with its whole size
	dir := t.TempDir()
	path := filepath.Join(dir, "rollout-2026-10-26T07-33-20-big.jsonl")
	output := strings.Repeat("line of output\n", 5000)
	lines := []string{
		`{"timestamp":"2026-10-26T07:33:20.000Z","type":"session_meta","payload":{"id":"s-big"}}`,
		codexLine(t, "2026-10-26T07:33:21.000Z", "response_item", map[string]any{"type": "function_call", "name": "exec_command", "arguments": `{"cmd":"yes"}`, "call_id": "c1"}),
		codexLine(t, "2026-10-26T07:33:22.000Z", "response_item", map[string]any{"type": "function_call_output", "call_id": "c1", "output": output}),
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := (&CodexParser{}).ParseFile(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	res := toolMessagesByCall(t, result.Logs)["c1"]["tool_result"][0]
	if got := res.LogAttributes["tool.output"]; got != codexToolText(output) || len(got) >= len(output) {
		t.Errorf("output of %d bytes stored as %d bytes", len(output), len(got))
	}
	if res.LogAttributes["tool_result_size_bytes"] != fmt.Sprint(len(output)) {
		t.Errorf("tool_result_size_bytes = %q, want %d", res.LogAttributes["tool_result_size_bytes"], len(output))
	}
	if want := "exec_command: 73.2 KB output"; res.Body != want {
		t.Errorf("body %q, want %q", res.Body, want)
	}
}

func codexLine(t *testing.T, ts, typ string, payload any) string {
	t.Helper()
	data, err := json.Marshal(map[string]any{"timestamp": ts, "type": typ, "payload": payload})
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// The tool calls a state keeps are bounded, those waiting for their output
// and the IDs of answered ones alike
func TestCodexToolCallStateBounded(t *testing.T) {
	var s CodexState
	n := 3 * codexCallLimit
	for i := range n {
		s.addCall(CodexToolCall{ID: fmt.Sprint("c", i), Kind: codexCallFunction})
		if i%2 == 0 && s.answer(fmt.Sprint("c", i)) == nil {
			t.Fatalf("call c%d not pending", i)
		}
	}
	if len(s.ToolCalls) > codexCallLimit+codexCallLimit/4 || len(s.AnsweredCalls) > codexCallLimit+codexCallLimit/4 {
		t.Errorf("%d pending and %d answered calls kept", len(s.ToolCalls), len(s.AnsweredCalls))
	}
	newestPending, newestAnswered := fmt.Sprint("c", n-1), fmt.Sprint("c", n-2)
	if s.pendingCall(newestPending) == nil || !s.answeredCall(newestAnswered) {
		t.Error("the newest calls were dropped")
	}
	if s.pendingCall("c1") != nil || s.answeredCall("c0") || s.pendingCall(newestAnswered) != nil {
		t.Error("an old call is still kept, or an answered one still pending")
	}
	// The state survives the watcher's JSON round trip
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	var restored CodexState
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatal(err)
	}
	if restored.pendingCall(newestPending) == nil || !restored.answeredCall(newestAnswered) ||
		len(restored.ToolCalls) != len(s.ToolCalls) || len(restored.AnsweredCalls) != len(s.AnsweredCalls) {
		t.Error("calls lost in the round trip")
	}
}

func TestToolResultBody(t *testing.T) {
	exit := func(n int) *int { return &n }
	yes, no := true, false
	for _, c := range []struct {
		m    toolMessage
		size int
		want string
	}{
		{toolMessage{name: "exec_command", exitCode: exit(1)}, 12_595, "exec_command: exit 1, 12.3 KB output"},
		{toolMessage{name: "apply_patch", success: &yes}, 8, "apply_patch: completed, 8 bytes output"},
		{toolMessage{name: "fetch", success: &no}, 3 << 20, "fetch: failed, 3.0 MB output"},
		{toolMessage{}, 0, "tool: no output"},
	} {
		if got := toolResultBody(c.m, c.size); got != c.want {
			t.Errorf("toolResultBody = %q, want %q", got, c.want)
		}
	}
}
