package storage

import "testing"

func TestOpenCodeTranscriptHelpers(t *testing.T) {
	roleTests := []struct {
		eventName string
		expected  string
	}{
		{"user_prompt", "user"},
		{"api_request", "assistant"},
		{"api_error", "system"},
		{"tool_result", "tool_result"},
		{"tool_decision", "tool_use"},
		{"session.created", "system"},
		{"session.idle", "system"},
		{"session.error", "system"},
		{"commit", "system"},
	}

	for _, tt := range roleTests {
		t.Run(tt.eventName, func(t *testing.T) {
			if got := mapEventToRole(tt.eventName, "opencode"); got != tt.expected {
				t.Fatalf("expected role %q, got %q", tt.expected, got)
			}
		})
	}

	attrs := map[string]string{
		"llm.model_name":             "anthropic/claude-sonnet-4",
		"llm.token_count.prompt":     "123",
		"llm.token_count.completion": "45",
		"llm.cost":                   "0.0123",
		"tool.parameters":            `{"path":"README.md"}`,
		"tool.result":                "ok",
		"openinference.tool.name":    "read",
	}

	if got := getModelName(attrs); got != "anthropic/claude-sonnet-4" {
		t.Fatalf("expected model from llm.model_name, got %q", got)
	}
	if got := parseIntAttr(attrs, "llm.token_count.prompt"); got != 123 {
		t.Fatalf("expected prompt token count 123, got %d", got)
	}
	if got := parseIntAttr(attrs, "llm.token_count.completion"); got != 45 {
		t.Fatalf("expected completion token count 45, got %d", got)
	}
	if got := parseFloatAttr(attrs, "llm.cost"); got != 0.0123 {
		t.Fatalf("expected cost 0.0123, got %f", got)
	}
	if got := getToolName(attrs, "tool_result"); got != "read" {
		t.Fatalf("expected tool name read, got %q", got)
	}
	if got := getToolInput(attrs); got != `{"path":"README.md"}` {
		t.Fatalf("expected tool parameters, got %q", got)
	}
	if got := getToolOutput(attrs); got != "ok" {
		t.Fatalf("expected tool result ok, got %q", got)
	}
}
