// Package tools provides centralized definitions for supported AI coding tools.
// This ensures consistent service names across import, export, and query operations.
package tools

// Tool represents a supported AI coding tool
type Tool string

const (
	Claude Tool = "claude-code"
	Codex  Tool = "codex"
	Gemini Tool = "gemini"
)

// ServiceName returns the OTLP service.name attribute for this tool.
// These must match the service.name sent by the actual tools via OTLP telemetry.
func (t Tool) ServiceName() string {
	if name, ok := serviceNames[t]; ok {
		return name
	}
	return string(t)
}

// serviceNames maps tools to their OTLP service.name attribute values.
// This is the single source of truth for service names.
var serviceNames = map[Tool]string{
	Claude: "claude-code",  // Matches Claude Code OTLP telemetry
	Codex:  "codex_cli_rs", // Matches Codex CLI OTLP telemetry
	Gemini: "gemini_cli",   // Matches Gemini CLI OTLP telemetry
}

// All returns all supported tools
func All() []Tool {
	return []Tool{Claude, Codex, Gemini}
}

// Parse converts a string to a Tool, returning ok=false if invalid
func Parse(s string) (Tool, bool) {
	switch s {
	case "claude-code":
		return Claude, true
	case "codex":
		return Codex, true
	case "gemini":
		return Gemini, true
	default:
		return "", false
	}
}

// ServiceNameFor returns the service name for a tool string, or empty if invalid
func ServiceNameFor(tool string) string {
	if t, ok := Parse(tool); ok {
		return t.ServiceName()
	}
	return ""
}

// NormalizeServiceName accepts either a short tool name (claude, codex, gemini)
// or a full service name (claude-code, codex_cli_rs, gemini_cli) and returns
// the canonical service name. Returns empty string if invalid.
func NormalizeServiceName(input string) string {
	// First try as short tool name
	if t, ok := Parse(input); ok {
		return t.ServiceName()
	}
	// Check if it's already a valid service name
	for _, t := range All() {
		if t.ServiceName() == input {
			return input
		}
	}
	return ""
}
