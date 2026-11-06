package tools

import "testing"

func TestServiceName(t *testing.T) {
	tests := []struct {
		tool     Tool
		expected string
	}{
		{Claude, "claude-code"},
		{Codex, "codex_cli_rs"},
		{Gemini, "gemini_cli"},
		{OpenCode, "opencode"},
		{Tool("unknown"), "unknown"}, // Falls back to string representation
	}

	for _, tt := range tests {
		t.Run(string(tt.tool), func(t *testing.T) {
			result := tt.tool.ServiceName()
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestParse(t *testing.T) {
	tests := []struct {
		input    string
		expected Tool
		ok       bool
	}{
		{"claude-code", Claude, true},
		{"codex", Codex, true},
		{"gemini", Gemini, true},
		{"opencode", OpenCode, true},
		{"copilot-chat", CopilotVSCode, true},
		{"github-copilot", CopilotCLI, true},
		{"all", "", false},
		{"invalid", "", false},
		{"", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result, ok := Parse(tt.input)
			if ok != tt.ok {
				t.Errorf("expected ok=%v, got ok=%v", tt.ok, ok)
			}
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestAll(t *testing.T) {
	all := All()
	if len(all) != 6 {
		t.Errorf("expected 6 tools, got %d", len(all))
	}

	expected := map[Tool]bool{Claude: true, Codex: true, Gemini: true, OpenCode: true, CopilotVSCode: true, CopilotCLI: true}
	for _, tool := range all {
		if !expected[tool] {
			t.Errorf("unexpected tool: %s", tool)
		}
	}
}

func TestServiceNameFor(t *testing.T) {
	tests := []struct {
		tool     string
		expected string
	}{
		{"claude-code", "claude-code"},
		{"codex", "codex_cli_rs"},
		{"gemini", "gemini_cli"},
		{"opencode", "opencode"},
		{"invalid", ""},
	}

	for _, tt := range tests {
		t.Run(tt.tool, func(t *testing.T) {
			result := ServiceNameFor(tt.tool)
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestNormalizeServiceName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"claude-code", "claude-code"},
		{"codex", "codex_cli_rs"},
		{"codex_cli_rs", "codex_cli_rs"},
		{"gemini", "gemini_cli"},
		{"gemini_cli", "gemini_cli"},
		{"opencode", "opencode"},
		{"copilot-chat", "copilot-chat"},
		{"github-copilot", "github-copilot"},
		{"invalid", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := NormalizeServiceName(tt.input)
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestTranscriptRole(t *testing.T) {
	cases := map[string]string{
		"user_prompt":                "user",
		"codex.tool_result":          "tool_result",
		"gemini_cli.tool_call":       "tool_use",
		"copilot_chat.agent.turn":    "assistant",
		"opencode.session.created":   "system",
		"transcript.message":         "",
		"claude_code.something_else": "",
	}
	for event, want := range cases {
		if got := TranscriptRole(event); got != want {
			t.Errorf("TranscriptRole(%q) = %q, want %q", event, got, want)
		}
	}
}

func TestMergeTranscriptRolesRejectsConflicts(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("conflicting roles for one event did not panic")
		}
	}()
	mergeTranscriptRoles([]Provider{
		{Tool: "a", TranscriptRoles: map[string]string{"event": RoleUser}},
		{Tool: "b", TranscriptRoles: map[string]string{"event": RoleAssistant}},
	})
}
