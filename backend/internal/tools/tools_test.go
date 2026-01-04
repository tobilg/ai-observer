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
	if len(all) != 3 {
		t.Errorf("expected 3 tools, got %d", len(all))
	}

	expected := map[Tool]bool{Claude: true, Codex: true, Gemini: true}
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
