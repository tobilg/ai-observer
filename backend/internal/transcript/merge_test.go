package transcript

import (
	"context"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/tobilg/ai-observer/internal/api"
	"github.com/tobilg/ai-observer/internal/storage"
	"github.com/tobilg/ai-observer/internal/tools"
)

var mergeT0 = time.Date(2026, 11, 8, 17, 58, 24, 750_000_000, time.UTC)

func at(ms int) time.Time { return mergeT0.Add(time.Duration(ms) * time.Millisecond) }

// fileMsg is a message of a session file as imports store it
func fileMsg(sessionID, service string, ms int, role, body string, attrs map[string]string) storage.SessionLog {
	a := map[string]string{"event.name": tools.TranscriptMessageEvent, "message.role": role, "session.id": sessionID, "import_source": "local_jsonl"}
	for k, v := range attrs {
		a[k] = v
	}
	return storage.SessionLog{Timestamp: at(ms), ServiceName: service, Body: body, Attrs: a}
}

// otlpEvent is an OTLP log event
func otlpEvent(sessionID, service string, ms int, event string, attrs map[string]string) storage.SessionLog {
	a := map[string]string{"event.name": event, "session.id": sessionID}
	for k, v := range attrs {
		a[k] = v
	}
	body := event
	switch {
	case service == "claude-code":
		body = "claude_code." + event
	case event == "gemini_cli.user_prompt":
		body = "User prompt. Length: " + a["prompt_length"] + "."
	}
	return storage.SessionLog{Timestamp: at(ms), ServiceName: service, Body: body, Attrs: a}
}

// mergeScenario is a session's logs and the messages its transcript shows
type mergeScenario struct {
	name string
	logs []storage.SessionLog
	// want lists the shown messages as "role:content" (content cut to 20)
	want []string
}

func mergeScenarios() []mergeScenario {
	const claude, codexFile, codexApp, gemini = "claude-code", "codex_cli_rs", "codex-app-server", "gemini_cli"
	return []mergeScenario{{
		// A session imported by this version (entry UUIDs, request and tool
		// call IDs on the file's messages) that also arrived over OTLP; the
		// file's tool result is stored after the OTLP event, its tool call
		// before
		name: "claude-ids",
		logs: []storage.SessionLog{
			fileMsg("s-ids", claude, 0, "user", "Fix the flaky test", map[string]string{"message.uuid": "u1"}),
			otlpEvent("s-ids", claude, 0, "user_prompt", map[string]string{"prompt": "Fix the flaky test", "message.uuid": "u1"}),
			fileMsg("s-ids", claude, 1000, "tool_use", "Tool call: Bash", map[string]string{"tool.name": "Bash", "tool.use_id": "toolu_1", "request_id": "req_1", "tool.input": `{"command":"go test"}`}),
			otlpEvent("s-ids", claude, 1300, "tool_decision", map[string]string{"tool_name": "Bash", "tool_use_id": "toolu_1", "request_id": "req_1", "decision": "accept"}),
			otlpEvent("s-ids", claude, 1310, "api_request", map[string]string{"request_id": "req_1", "input_tokens": "100", "output_tokens": "20", "cost_usd": "0.01", "model": "claude-sonnet-5-5"}),
			otlpEvent("s-ids", claude, 2000, "tool_result", map[string]string{"tool_name": "Bash", "tool_use_id": "toolu_1", "request_id": "req_1", "success": "true", "duration_ms": "120"}),
			fileMsg("s-ids", claude, 2001, "tool_result", "ok", map[string]string{"tool.use_id": "toolu_1", "tool.output": "ok"}),
			fileMsg("s-ids", claude, 3000, "assistant", "Done.", map[string]string{"message.uuid": "u2", "request_id": "req_2"}),
			otlpEvent("s-ids", claude, 3009, "api_request", map[string]string{"request_id": "req_2", "input_tokens": "50", "output_tokens": "10", "cost_usd": "0.02"}),
			otlpEvent("s-ids", claude, 3009, "assistant_response", map[string]string{"request_id": "req_2", "message.uuid": "u2", "response": "Done."}),
			// Not written to the session file
			otlpEvent("s-ids", claude, 9000, "user_prompt", map[string]string{"prompt": "/exit", "message.uuid": "u3"}),
		},
		want: []string{"user:Fix the flaky test", "tool_use:Tool call: Bash", "tool_result:ok", "assistant:Done.", "user:/exit"},
	}, {
		// A session imported by an earlier version: the file's messages carry
		// no entry UUIDs, request IDs or IDs of tool calls (only results), and
		// prompt logging is off
		name: "claude-legacy",
		logs: []storage.SessionLog{
			otlpEvent("s-legacy", claude, 0, "user_prompt", map[string]string{"prompt": tools.RedactedPrompt, "message.uuid": "u1"}),
			fileMsg("s-legacy", claude, 0, "user", "Fix it", nil),
			fileMsg("s-legacy", claude, 1000, "tool_use", "Tool call: Read", map[string]string{"tool.name": "Read"}),
			otlpEvent("s-legacy", claude, 1300, "tool_decision", map[string]string{"tool_name": "Read", "tool_use_id": "toolu_9", "request_id": "req_1"}),
			otlpEvent("s-legacy", claude, 1310, "api_request", map[string]string{"request_id": "req_1", "input_tokens": "7", "cost_usd": "0.001"}),
			fileMsg("s-legacy", claude, 1500, "tool_result", "file contents", map[string]string{"tool.use_id": "toolu_9", "tool.output": "file contents"}),
			otlpEvent("s-legacy", claude, 1501, "tool_result", map[string]string{"tool_name": "Read", "tool_use_id": "toolu_9", "success": "true"}),
			fileMsg("s-legacy", claude, 3000, "assistant", "  Done.\n", nil),
			otlpEvent("s-legacy", claude, 3009, "api_request", map[string]string{"request_id": "req_2", "output_tokens": "5", "cost_usd": "0.002"}),
			otlpEvent("s-legacy", claude, 3009, "assistant_response", map[string]string{"request_id": "req_2", "message.uuid": "u2", "response": "Done."}),
		},
		// req_1's usage has no file message to go into (its tool call has no
		// request ID): it stays a message of its own, as over OTLP only
		want: []string{"user:Fix it", "tool_use:Tool call: Read", "assistant:claude_code.api_requ", "tool_result:file contents", "assistant:  Done.\n"},
	}, {
		// Codex sends the conversation from its app server and records it in
		// its CLI's session file: prompts match by time and text start, tool
		// calls by call ID
		name: "codex",
		logs: []storage.SessionLog{
			fileMsg("s-codex", codexFile, 0, "user", "<environment_context>\n  <cwd>/src</cwd>", nil),
			fileMsg("s-codex", codexFile, 9, "user", "Add a retry to the HTTP client of the exporter", nil),
			otlpEvent("s-codex", codexApp, 11, "codex.user_prompt", map[string]string{"prompt": "Add a retry to the HTTP client of the exporter\n[Image #1]", "conversation.id": "s-codex"}),
			fileMsg("s-codex", codexFile, 1000, "tool_use", "Tool call: exec", map[string]string{"tool.name": "exec", "tool.call_id": "call_1", "tool.input": "go test"}),
			otlpEvent("s-codex", codexApp, 1005, "codex.tool_decision", map[string]string{"tool_name": "exec", "call_id": "call_1", "decision": "approved", "conversation.id": "s-codex"}),
			otlpEvent("s-codex", codexApp, 2000, "codex.tool_result", map[string]string{"tool_name": "exec", "call_id": "call_1", "output": "PASS", "duration_ms": "50", "success": "true", "conversation.id": "s-codex"}),
			fileMsg("s-codex", codexFile, 2003, "tool_result", "PASS", map[string]string{"tool.call_id": "call_1", "tool.output": "PASS"}),
			// A call the session file does not record
			otlpEvent("s-codex", codexApp, 2100, "codex.tool_result", map[string]string{"tool_name": "write_stdin", "call_id": "call_2", "output": "x", "conversation.id": "s-codex"}),
			fileMsg("s-codex", codexFile, 3000, "assistant", "Added the retry.", nil),
			// A prompt and a file message at the same time with other texts
			// are two messages
			fileMsg("s-codex", codexFile, 5000, "user", "Run the tests again", nil),
			otlpEvent("s-codex", codexApp, 5001, "codex.user_prompt", map[string]string{"prompt": "Something else entirely", "conversation.id": "s-codex"}),
		},
		want: []string{"user:<environment_context", "user:Add a retry to the H", "tool_use:Tool call: exec", "tool_result:PASS", "tool_result:x",
			"assistant:Added the retry.", "user:Run the tests again", "user:Something else entir"},
	}, {
		// Over OTLP only: a response's usage and its text are one message;
		// a response of tool calls only keeps its usage message
		name: "claude-otlp",
		logs: []storage.SessionLog{
			otlpEvent("s-otlp", claude, 0, "user_prompt", map[string]string{"prompt": "Hi"}),
			otlpEvent("s-otlp", claude, 900, "api_request", map[string]string{"request_id": "req_a", "output_tokens": "40", "cost_usd": "0.004"}),
			otlpEvent("s-otlp", claude, 900, "tool_decision", map[string]string{"tool_name": "Read", "tool_use_id": "toolu_a", "request_id": "req_a"}),
			otlpEvent("s-otlp", claude, 950, "tool_result", map[string]string{"tool_name": "Read", "tool_use_id": "toolu_a", "request_id": "req_a"}),
			otlpEvent("s-otlp", claude, 2000, "api_request", map[string]string{"request_id": "req_b", "output_tokens": "8", "cost_usd": "0.001"}),
			otlpEvent("s-otlp", claude, 2000, "assistant_response", map[string]string{"request_id": "req_b", "response": "Hello!"}),
		},
		want: []string{"user:Hi", "assistant:claude_code.api_requ", "tool_use:claude_code.tool_dec", "tool_result:claude_code.tool_res", "assistant:Hello!"},
	}, {
		// From session files only: unchanged, imported indices kept
		name: "claude-file",
		logs: []storage.SessionLog{
			fileMsg("s-file", claude, 0, "user", "Hi", map[string]string{"message.index": "5", "message.uuid": "u1"}),
			fileMsg("s-file", claude, 1000, "assistant", "Hello", map[string]string{"message.index": "6", "request_id": "req_1"}),
		},
		want: []string{"user:Hi", "assistant:Hello"},
	}, {
		// Gemini CLI sends a response as an api_response event and an
		// operation details record, and records it in its session file
		// after the event (a response of tool calls only once the calls
		// ran); its events carry no IDs: prompts match by time and text, or
		// length when prompt logging is off, responses by their token
		// counts, tool calls by tool and arguments
		name: "gemini",
		logs: []storage.SessionLog{
			otlpEvent("s-gemini", gemini, -5, "gemini_cli.user_prompt", map[string]string{"prompt": "Fix the build", "prompt_length": "13"}),
			fileMsg("s-gemini", gemini, 0, "user", "Fix the build", nil),
			otlpEvent("s-gemini", gemini, 10, "gemini_cli.api_request", map[string]string{"model": "gemini-2.5-pro"}),
			otlpEvent("s-gemini", gemini, 1500, "gemini_cli.api_response", geminiResponseAttrs(7777, 90, 3114, 260, 0)),
			otlpEvent("s-gemini", gemini, 1501, "gen_ai.client.inference.operation.details", geminiDetailsAttrs(7777, 90, "I will read the README.")),
			fileMsg("s-gemini", gemini, 1530, "assistant", "I will read the README.", geminiFileTokens(7777, 90, 3114, 260, 0)),
			otlpEvent("s-gemini", gemini, 1555, "gemini_cli.tool_call", map[string]string{"function_name": "read_file",
				"function_args": "{\n  \"file_path\": \"README.md\",\n  \"limit\": 20\n}", "success": "true", "duration_ms": "8"}),
			fileMsg("s-gemini", gemini, 1560, "tool_use", "Tool call: read_file", map[string]string{"tool.name": "read_file",
				"tool.call_id": "read_file-1766747231641-63d9ab01f7dc4", "tool.input": "{\n            \"limit\": 20,\n            \"file_path\": \"README.md\"\n          }"}),
			fileMsg("s-gemini", gemini, 1560, "tool_result", "# Project", map[string]string{"tool.name": "read_file",
				"tool.call_id": "read_file-1766747231641-63d9ab01f7dc4", "tool.output": "# Project", "success": "true"}),
			otlpEvent("s-gemini", gemini, 2000, "gemini_cli.api_request", map[string]string{"model": "gemini-2.5-pro"}),
			otlpEvent("s-gemini", gemini, 3000, "gemini_cli.api_response", geminiResponseAttrs(7874, 26, 6080, 29, 0)),
			otlpEvent("s-gemini", gemini, 3000, "gen_ai.client.inference.operation.details", geminiDetailsAttrs(7874, 26, "")),
			// A request the session file does not record (next speaker check)
			otlpEvent("s-gemini", gemini, 3500, "gemini_cli.api_response", geminiResponseAttrs(100, 5, 0, 0, 0)),
			otlpEvent("s-gemini", gemini, 3500, "gen_ai.client.inference.operation.details", geminiDetailsAttrs(100, 5, "user")),
			// The response of tool calls only, recorded once they ran
			fileMsg("s-gemini", gemini, 43040, "assistant", "", geminiFileTokens(7874, 26, 6080, 29, 0)),
			// Prompt logging off: the event's body describes the prompt
			otlpEvent("s-gemini", gemini, 60000, "gemini_cli.user_prompt", map[string]string{"prompt_length": "13"}),
			fileMsg("s-gemini", gemini, 60010, "user", "Run the tests", nil),
		},
		want: []string{"user:Fix the build", "assistant:gemini_cli.api_reque", "assistant:I will read the READ", "tool_use:Tool call: read_file",
			"tool_result:# Project", "assistant:gemini_cli.api_reque", "assistant:user", "assistant:", "user:Run the tests"},
	}}
}

// geminiResponseAttrs are the token counts of a gemini_cli.api_response event
func geminiResponseAttrs(input, output, cached, thoughts, tool int) map[string]string {
	return map[string]string{"model": "gemini-2.5-pro", "input_token_count": fmt.Sprint(input), "output_token_count": fmt.Sprint(output),
		"cached_content_token_count": fmt.Sprint(cached), "thoughts_token_count": fmt.Sprint(thoughts), "tool_token_count": fmt.Sprint(tool)}
}

// geminiDetailsAttrs are Gemini CLI's operation details record of a response
func geminiDetailsAttrs(input, output int, text string) map[string]string {
	return map[string]string{"gen_ai.usage.input_tokens": fmt.Sprint(input), "gen_ai.usage.output_tokens": fmt.Sprint(output),
		"gen_ai.output.messages": fmt.Sprintf(`[{"role":"assistant","parts":[{"type":"text","content":%q}]}]`, text)}
}

// geminiFileTokens are the token attributes of an imported Gemini CLI reply
func geminiFileTokens(input, output, cached, thoughts, tool int) map[string]string {
	return map[string]string{"model": "gemini-2.5-pro", "input_tokens": fmt.Sprint(input), "output_tokens": fmt.Sprint(output),
		"cache_read_input_tokens": fmt.Sprint(cached), "thoughts_token_count": fmt.Sprint(thoughts), "tool_token_count": fmt.Sprint(tool)}
}

func messageList(messages []api.TranscriptMessage) []string {
	out := make([]string, len(messages))
	for i, m := range messages {
		out[i] = m.Role + ":" + m.Content[:min(20, len(m.Content))]
	}
	return out
}

func TestMergeFileAndOTLPRecords(t *testing.T) {
	for _, sc := range mergeScenarios() {
		t.Run(sc.name, func(t *testing.T) {
			resp := Build("s", sc.logs, nil)
			if got := messageList(resp.Messages); strings.Join(got, "|") != strings.Join(sc.want, "|") {
				t.Fatalf("messages\n got %q\nwant %q", got, sc.want)
			}
		})
	}
}

// The file's messages are shown, filled in with what the OTLP events add,
// and each response's usage is counted once
func TestMergeFillsInAndCountsUsageOnce(t *testing.T) {
	var ids, legacy mergeScenario
	for _, sc := range mergeScenarios() {
		switch sc.name {
		case "claude-ids":
			ids = sc
		case "claude-legacy":
			legacy = sc
		}
	}
	resp := Build("s-ids", ids.logs, nil)
	call, result, reply := resp.Messages[1], resp.Messages[2], resp.Messages[3]
	if call.InputTokens != 100 || call.OutputTokens != 20 || call.CostUSD != 0.01 || call.Model != "claude-sonnet-5-5" {
		t.Errorf("tool call of a response without text lacks its usage: %+v", call)
	}
	if call.ToolInput != `{"command":"go test"}` {
		t.Errorf("tool input %q, want the file's", call.ToolInput)
	}
	if result.ToolName != "Bash" || result.DurationMs != 120 || result.Success == nil || !*result.Success || result.Content != "ok" {
		t.Errorf("tool result not filled in from the OTLP event: %+v", result)
	}
	if reply.OutputTokens != 10 || reply.CostUSD != 0.02 {
		t.Errorf("reply lacks its usage: %+v", reply)
	}
	tot := resp.Totals
	if tot.InputTokens != 150 || tot.OutputTokens != 30 || math.Abs(tot.CostUSD-0.03) > 1e-9 ||
		tot.UserMessages != 2 || tot.AssistantMessages != 1 || tot.ToolCalls != 1 {
		t.Errorf("totals %+v", *tot)
	}
	for i, m := range resp.Messages {
		if m.Index != i {
			t.Errorf("message %d has index %d: a merged transcript is numbered by position", i, m.Index)
		}
	}

	resp = Build("s-legacy", legacy.logs, nil)
	if tot := resp.Totals; tot.InputTokens != 7 || tot.OutputTokens != 5 || math.Abs(tot.CostUSD-0.003) > 1e-9 {
		t.Errorf("legacy totals %+v", *tot)
	}
	if reply := resp.Messages[len(resp.Messages)-1]; reply.OutputTokens != 5 {
		t.Errorf("legacy reply lacks the usage of its request: %+v", reply)
	}
}

// Sessions recorded one way only keep their transcripts: imported indices,
// and OTLP events in order
func TestMergeLeavesSinglePathSessions(t *testing.T) {
	for _, sc := range mergeScenarios() {
		if sc.name != "claude-file" {
			continue
		}
		resp := Build("s-file", sc.logs, nil)
		if resp.Messages[0].Index != 5 || resp.Messages[1].Index != 6 {
			t.Fatalf("imported indices not kept: %d, %d", resp.Messages[0].Index, resp.Messages[1].Index)
		}
	}
}

// A prompt matches the closest file message whose text starts alike,
// within the window, one to one
func TestMergePromptWindowAndPrefix(t *testing.T) {
	const svc = "claude-code"
	window := int(tools.PromptMatchWindow / time.Millisecond)
	tests := []struct {
		name string
		logs []storage.SessionLog
		want int
	}{
		{"inside window", []storage.SessionLog{
			fileMsg("s", svc, 0, "user", "Run the tests", nil),
			otlpEvent("s", svc, window, "user_prompt", map[string]string{"prompt": "Run the tests"}),
		}, 1},
		{"outside window", []storage.SessionLog{
			fileMsg("s", svc, 0, "user", "Run the tests", nil),
			otlpEvent("s", svc, window+1, "user_prompt", map[string]string{"prompt": "Run the tests"}),
		}, 2},
		{"other text", []storage.SessionLog{
			fileMsg("s", svc, 0, "user", "Run the tests", nil),
			otlpEvent("s", svc, 0, "user_prompt", map[string]string{"prompt": "Deploy it"}),
		}, 2},
		{"one to one", []storage.SessionLog{
			fileMsg("s", svc, 0, "user", "yes", nil),
			otlpEvent("s", svc, 0, "user_prompt", map[string]string{"prompt": "yes"}),
			otlpEvent("s", svc, 500, "user_prompt", map[string]string{"prompt": "yes"}),
		}, 2},
		{"redacted matches by time", []storage.SessionLog{
			fileMsg("s", svc, 0, "user", "Secret plans", nil),
			otlpEvent("s", svc, 0, "user_prompt", map[string]string{"prompt": tools.RedactedPrompt}),
		}, 1},
		{"gemini within its window", []storage.SessionLog{
			otlpEvent("s", "gemini_cli", 0, "gemini_cli.user_prompt", map[string]string{"prompt": "hello"}),
			fileMsg("s", "gemini_cli", 90_000, "user", "hello", nil),
		}, 1},
		{"gemini outside its window", []storage.SessionLog{
			otlpEvent("s", "gemini_cli", 0, "gemini_cli.user_prompt", map[string]string{"prompt": "hello"}),
			fileMsg("s", "gemini_cli", 301_000, "user", "hello", nil),
		}, 2},
		{"gemini without text by length", []storage.SessionLog{
			otlpEvent("s", "gemini_cli", 0, "gemini_cli.user_prompt", map[string]string{"prompt_length": "5"}),
			fileMsg("s", "gemini_cli", 20, "user", "hellö", nil),
		}, 1},
		{"gemini without text, other length", []storage.SessionLog{
			otlpEvent("s", "gemini_cli", 0, "gemini_cli.user_prompt", map[string]string{"prompt_length": "6"}),
			fileMsg("s", "gemini_cli", 20, "user", "hello", nil),
		}, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := len(Build("s", tt.logs, nil).Messages); got != tt.want {
				t.Fatalf("%d messages, want %d", got, tt.want)
			}
		})
	}
}

// Gemini CLI's records of a response are one message with the response's
// usage once; a tool call is filled in from its event
func TestMergeGemini(t *testing.T) {
	var sc mergeScenario
	for _, s := range mergeScenarios() {
		if s.name == "gemini" {
			sc = s
		}
	}
	resp := Build("s-gemini", sc.logs, nil)
	tot := resp.Totals
	if tot.InputTokens != 7777+7874+100 || tot.OutputTokens != 90+26+5 || tot.CacheRead != 3114+6080 {
		t.Errorf("totals %+v: each response's usage once", *tot)
	}
	if tot.UserMessages != 2 || tot.ToolCalls != 1 {
		t.Errorf("totals %+v", *tot)
	}
	call := resp.Messages[3]
	if call.DurationMs != 8 || call.Success == nil || !*call.Success {
		t.Errorf("tool call not filled in from its event: %+v", call)
	}
	if resp.Title != "Fix the build" {
		t.Errorf("title %q", resp.Title)
	}
	// Over OTLP only, a response is its operation details record, with the
	// api_response's usage
	var otlp []storage.SessionLog
	for _, l := range sc.logs {
		if l.Attrs["event.name"] != tools.TranscriptMessageEvent {
			otlp = append(otlp, l)
		}
	}
	resp = Build("s-gemini", otlp, nil)
	got := messageList(resp.Messages)
	want := []string{"user:Fix the build", "assistant:gemini_cli.api_reque", "assistant:I will read the READ", "tool_use:gemini_cli.tool_call",
		"assistant:gemini_cli.api_reque", "assistant:[{\"role\":\"assistant\"", "assistant:user", "user:User prompt. Length:"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("OTLP only\n got %q\nwant %q", got, want)
	}
	if tot := resp.Totals; tot.InputTokens != 7777+7874+100 || tot.CacheRead != 3114+6080 {
		t.Errorf("OTLP only totals %+v", *tot)
	}
	if resp.Title != "Fix the build" {
		t.Errorf("OTLP only title %q", resp.Title)
	}
}

// Session lists count the messages their transcripts show, with the same
// usage and title: stored, the scenarios' sessions are listed with their
// transcripts' message counts, totals and titles (also when the same
// sessions arrived over OTLP only)
func TestSessionListCountsMatchTranscripts(t *testing.T) {
	store, err := storage.NewDuckDBStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	var logs []api.LogRecord
	want := map[string]int{}
	sessions := 0
	for _, sc := range mergeScenarios() {
		id := ""
		for _, l := range sc.logs {
			id = l.Attrs["session.id"]
			logs = append(logs, api.LogRecord{Timestamp: l.Timestamp, ServiceName: l.ServiceName, Body: l.Body, LogAttributes: l.Attrs})
			// The session's OTLP records alone, as another session
			if l.Attrs["event.name"] != tools.TranscriptMessageEvent {
				attrs := map[string]string{}
				for k, v := range l.Attrs {
					attrs[k] = v
				}
				for _, key := range tools.SessionIDKeys {
					if attrs[key] != "" {
						attrs[key] = "otlp-" + attrs[key]
					}
				}
				logs = append(logs, api.LogRecord{Timestamp: l.Timestamp, ServiceName: l.ServiceName, Body: l.Body, LogAttributes: attrs})
			}
		}
		want[id] = len(sc.want)
		sessions++
		if sc.name != "claude-file" {
			sessions++
		}
	}
	if err := store.InsertLogs(ctx, logs); err != nil {
		t.Fatal(err)
	}
	res, err := store.QuerySessions(ctx, "", mergeT0.Add(-time.Hour), mergeT0.Add(time.Hour), storage.Page{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	type listed struct {
		count, rows   int
		input, output int64
		cost          float64
		title         string
	}
	lists := map[string]*listed{}
	for _, s := range res.Sessions {
		l := lists[s.SessionID]
		if l == nil {
			l = &listed{}
			lists[s.SessionID] = l
		}
		l.count += s.MessageCount
		l.rows++
		l.title = s.Title
		if s.InputTokens != nil {
			l.input += *s.InputTokens
		}
		if s.OutputTokens != nil {
			l.output += *s.OutputTokens
		}
		if s.CostUSD != nil {
			l.cost += *s.CostUSD
		}
	}
	for id, n := range want {
		resp, err := Load(ctx, store, id)
		if err != nil {
			t.Fatal(err)
		}
		if len(resp.Messages) != n {
			t.Errorf("%s: transcript has %d messages, want %d", id, len(resp.Messages), n)
		}
	}
	if len(lists) != sessions {
		t.Errorf("%d sessions listed, want %d", len(lists), sessions)
	}
	for id, l := range lists {
		resp, err := Load(ctx, store, id)
		if err != nil {
			t.Fatal(err)
		}
		tot := resp.Totals
		if l.count != len(resp.Messages) {
			t.Errorf("%s: listed with %d messages, its transcript has %d", id, l.count, len(resp.Messages))
		}
		if l.input != tot.InputTokens || l.output != tot.OutputTokens || math.Abs(l.cost-tot.CostUSD) > 1e-9 {
			t.Errorf("%s: listed with usage %d/%d/%v, its transcript totals %d/%d/%v", id, l.input, l.output, l.cost,
				tot.InputTokens, tot.OutputTokens, tot.CostUSD)
		}
		if l.rows == 1 && l.title != resp.Title {
			t.Errorf("%s: listed with title %q, its transcript's is %q", id, l.title, resp.Title)
		}
	}
}
