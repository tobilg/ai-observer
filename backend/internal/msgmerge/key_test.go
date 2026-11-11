package msgmerge

import (
	"strings"
	"testing"
	"time"

	"github.com/tobilg/ai-observer/internal/tools"
)

// A stored key reads back as the key it was made from
func TestKeyEncodeDecode(t *testing.T) {
	keys := []Key{
		{File: true, Tool: "claude-code", Event: tools.TranscriptMessageEvent, Role: "assistant", UUID: "u1", Request: "req_1",
			Reply: hash("Done."), Usage: Usage{InputTokens: 10, OutputTokens: 2, CostUSD: 0.0125, HasInput: true, HasCost: true},
			Thoughts: -1, ToolTokens: -1},
		{Tool: "gemini", Event: "gemini_cli.user_prompt", Role: "user", Prompt: "a\x1fb", PromptUnrecorded: true, PromptLength: 13,
			Thoughts: -1, ToolTokens: -1},
		{Tool: "gemini", Event: "gemini_cli.tool_call", Role: "tool_use", ToolName: "read_file", Args: canonicalHash(`{"a":1}`),
			Thoughts: 260, ToolTokens: 0, Usage: Usage{CacheRead: 5, CacheWrite: 7}},
	}
	for _, k := range keys {
		got, ok := DecodeKey(k.Encode())
		want := k
		want.Prompt = clean(want.Prompt)
		if !ok || got != want {
			t.Errorf("DecodeKey(Encode(%+v)) = %+v, %v", k, got, ok)
		}
		if !strings.HasPrefix(k.Encode(), KeyPrefix()) {
			t.Errorf("key %q does not start with the prefix", k.Encode())
		}
	}
	for _, s := range []string{"", NotMessage(), "0.abc\x1f1", strings.Replace(keys[0].Encode(), KeyVersion, "0.00000000", 1)} {
		if _, ok := DecodeKey(s); ok {
			t.Errorf("DecodeKey(%q) accepted", s)
		}
	}
	if !strings.HasPrefix(KeyVersion, keyRevision+".") || len(KeyVersion) != len(keyRevision)+9 {
		t.Errorf("KeyVersion %q", KeyVersion)
	}
}

func TestKeyOf(t *testing.T) {
	if _, ok := KeyOf("claude-code", "claude_code.tool_decision", map[string]string{"event.name": "conversation_starts"}); ok {
		t.Error("an event that is no conversation event has a key")
	}
	k, ok := KeyOf("codex-app-server", "codex.tool_result", map[string]string{"call_id": "call_1", "output": "x"})
	if !ok || k.Event != "codex.tool_result" || k.Tool != "codex" || k.CallID != "call_1" || k.File {
		t.Errorf("event named by its body: %+v", k)
	}
	k, _ = KeyOf("claude-code", "  Fix the flaky test in the exporter package, please  ", map[string]string{
		"event.name": tools.TranscriptMessageEvent, "message.role": "user", "message.uuid": "u1"})
	if !k.File || k.Prompt != "Fix the flaky test in the export" || k.PromptUnrecorded || k.UUID != "u1" {
		t.Errorf("file prompt: %+v", k)
	}
	k, _ = KeyOf("claude-code", "claude_code.user_prompt", map[string]string{"event.name": "user_prompt", "prompt": tools.RedactedPrompt})
	if !k.PromptUnrecorded {
		t.Errorf("redacted prompt: %+v", k)
	}
	// Gemini CLI prompts: text, or the length when prompt logging is off
	k, _ = KeyOf("gemini_cli", "User prompt. Length: 5.", map[string]string{"event.name": "gemini_cli.user_prompt", "prompt_length": "5"})
	if !k.PromptUnrecorded || k.PromptLength != 5 {
		t.Errorf("gemini prompt without text: %+v", k)
	}
	k, _ = KeyOf("gemini_cli", "hellö 😀", map[string]string{"event.name": tools.TranscriptMessageEvent, "message.role": "user"})
	if k.PromptLength != 8 || k.PromptUnrecorded {
		t.Errorf("gemini file prompt length (UTF-16 code units): %+v", k)
	}
	// Gemini CLI responses and tool calls
	k, _ = KeyOf("gemini_cli", "API response", map[string]string{"event.name": "gemini_cli.api_response", "input_token_count": "7777",
		"output_token_count": "90", "cached_content_token_count": "3114", "thoughts_token_count": "260", "tool_token_count": "0"})
	if k.Usage.InputTokens != 7777 || k.Usage.OutputTokens != 90 || k.Usage.CacheRead != 3114 || k.Thoughts != 260 || k.ToolTokens != 0 {
		t.Errorf("gemini response: %+v", k)
	}
	k, _ = KeyOf("gemini_cli", "Done", map[string]string{"event.name": tools.TranscriptMessageEvent, "message.role": "assistant", "input_tokens": "7"})
	if k.Thoughts != -1 || k.ToolTokens != -1 || !k.Usage.HasInput || k.Usage.HasOutput || k.Usage.HasCost {
		t.Errorf("gemini reply imported before thoughts were stored: %+v", k)
	}
	file, _ := KeyOf("gemini_cli", "Tool call: read_file", map[string]string{"event.name": tools.TranscriptMessageEvent, "message.role": "tool_use",
		"tool.name": "read_file", "tool.input": "{\n      \"limit\": 20,\n      \"file_path\": \"README.md\"\n    }"})
	event, _ := KeyOf("gemini_cli", "Tool call", map[string]string{"event.name": "gemini_cli.tool_call", "function_name": "read_file",
		"function_args": "{\n  \"file_path\": \"README.md\",\n  \"limit\": 20\n}"})
	if file.Args == "" || file.Args != event.Args || file.ToolName != "read_file" || event.ToolName != "read_file" {
		t.Errorf("gemini tool call arguments differ: %+v, %+v", file, event)
	}
	if canonicalHash(`{"limit": 20.0}`) == canonicalHash(`{"limit": 20}`) || canonicalHash("not json") != "" {
		t.Error("canonicalHash")
	}
	// Claude Code replies compare by their text
	a, _ := KeyOf("claude-code", "  Done.\n", map[string]string{"event.name": tools.TranscriptMessageEvent, "message.role": "assistant"})
	b, _ := KeyOf("claude-code", "x", map[string]string{"event.name": tools.ReplyEvent, "response": "Done."})
	u, _ := KeyOf("claude-code", "x", map[string]string{"event.name": tools.UsageEvent, "cost_usd": "0.5"})
	if a.Reply == "" || a.Reply != b.Reply || u.Reply != "" || !u.Usage.HasCost || u.Usage.CostUSD != 0.5 {
		t.Errorf("claude replies: %+v %+v %+v", a, b, u)
	}
}

func TestUnrecordedPrompt(t *testing.T) {
	for text, want := range map[string]bool{
		tools.RedactedPrompt:           true,
		" User prompt. Length: 42.\n":  true,
		"User prompt. Length: 42. Fix": false,
		"Fix the build":                false,
	} {
		if got := UnrecordedPrompt(text); got != want {
			t.Errorf("UnrecordedPrompt(%q) = %v", text, got)
		}
	}
}

// Merge replays its steps in the order records were merged; a record
// merged into one that is merged in turn ends at the shown one
func TestMergePlanSteps(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	key := func(tool, event string, attrs map[string]string, body string) Key {
		a := map[string]string{"event.name": event}
		for k, v := range attrs {
			a[k] = v
		}
		k, ok := KeyOf(tool, body, a)
		if !ok {
			t.Fatalf("no key for %s", event)
		}
		return k
	}
	entries := []Entry{
		// OTLP only: details shown, api_response merged into it
		{Key: key("gemini_cli", "gemini_cli.api_response", map[string]string{"input_token_count": "100", "output_token_count": "5", "cached_content_token_count": "40"}, ""), Time: t0},
		{Key: key("gemini_cli", "gen_ai.client.inference.operation.details", map[string]string{"gen_ai.usage.input_tokens": "100", "gen_ai.usage.output_tokens": "5"}, ""), Time: t0},
	}
	plan := Merge(entries)
	if plan.Into[0] != 1 || plan.Into[1] != Shown || len(plan.Steps) != 1 || plan.Steps[0] != (Step{From: 0, Into: 1}) {
		t.Fatalf("plan %+v", plan)
	}
	usage := []Usage{entries[0].Key.Usage, entries[1].Key.Usage}
	plan.Replay(func(into, from int) { usage[into].Fill(usage[from]) })
	if usage[1].InputTokens != 100 || usage[1].CacheRead != 40 {
		t.Errorf("filled usage %+v", usage[1])
	}
	// Details more than a second away, or of other counts, stay apart
	entries[1].Time = t0.Add(1100 * time.Millisecond)
	if plan := Merge(entries); plan.Into[0] != Shown || plan.Into[1] != Shown {
		t.Errorf("plan %+v", plan)
	}
}
