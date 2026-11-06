// Package tools provides centralized definitions for supported AI coding tools.
// This ensures consistent service names across import, export, and query operations.
package tools

// Tool represents a supported AI coding tool by its CLI name
type Tool string

const (
	Claude        Tool = "claude-code"
	Codex         Tool = "codex"
	Gemini        Tool = "gemini"
	OpenCode      Tool = "opencode"
	CopilotVSCode Tool = "copilot-chat"
	CopilotCLI    Tool = "github-copilot"
)

// Provider describes a supported tool. This table is the single source of
// truth for tool names, service names and capabilities in the backend; add a
// provider here first (see "Adding a provider" in CLAUDE.md for the rest).
type Provider struct {
	Tool Tool
	// ServiceName is the OTLP service.name the tool sends
	ServiceName string
	DisplayName string
	// SessionFiles reports whether local session files can be imported and watched
	SessionFiles bool
	// TranscriptRoles maps the event names (log event.name) of the tool's
	// conversation events to transcript roles: user, assistant, tool_use,
	// tool_result or system. Imported transcripts ("transcript.message")
	// carry their role in the message.role attribute instead.
	TranscriptRoles map[string]string
}

// Transcript roles
const (
	RoleUser       = "user"
	RoleAssistant  = "assistant"
	RoleToolUse    = "tool_use"
	RoleToolResult = "tool_result"
	RoleSystem     = "system"
)

// copilotTranscriptRoles are shared by the VS Code extension and the CLI
var copilotTranscriptRoles = map[string]string{
	"gen_ai.client.inference.operation.details": RoleAssistant,
	"copilot_chat.agent.turn":                   RoleAssistant,
	"copilot_chat.tool.call":                    RoleToolUse,
}

var providers = []Provider{
	{Tool: Claude, ServiceName: "claude-code", DisplayName: "Claude Code", SessionFiles: true,
		TranscriptRoles: map[string]string{
			"user_prompt":   RoleUser,
			"api_request":   RoleAssistant,
			"api_response":  RoleAssistant,
			"tool_result":   RoleToolResult,
			"tool_decision": RoleToolUse,
			"api_error":     RoleSystem,
		}},
	{Tool: Codex, ServiceName: "codex_cli_rs", DisplayName: "OpenAI Codex CLI", SessionFiles: true,
		TranscriptRoles: map[string]string{
			"codex.user_prompt":   RoleUser,
			"codex.api_request":   RoleAssistant,
			"codex.tool_result":   RoleToolResult,
			"codex.tool_decision": RoleToolUse,
		}},
	{Tool: Gemini, ServiceName: "gemini_cli", DisplayName: "Gemini CLI", SessionFiles: true,
		TranscriptRoles: map[string]string{
			"gemini_cli.user_prompt":  RoleUser,
			"gemini_cli.api_request":  RoleAssistant,
			"gemini_cli.api_response": RoleAssistant,
			"gemini_cli.tool_call":    RoleToolUse,
		}},
	{Tool: OpenCode, ServiceName: "opencode", DisplayName: "OpenCode",
		TranscriptRoles: map[string]string{
			"opencode.user_prompt":     RoleUser,
			"opencode.api_request":     RoleAssistant,
			"opencode.tool_result":     RoleToolResult,
			"opencode.tool_decision":   RoleToolUse,
			"opencode.api_error":       RoleSystem,
			"opencode.session.created": RoleSystem,
			"opencode.session.idle":    RoleSystem,
			"opencode.session.error":   RoleSystem,
			"opencode.commit":          RoleSystem,
			"session.created":          RoleSystem,
			"session.idle":             RoleSystem,
			"session.error":            RoleSystem,
			"commit":                   RoleSystem,
		}},
	{Tool: CopilotVSCode, ServiceName: "copilot-chat", DisplayName: "GitHub Copilot (VS Code)", TranscriptRoles: copilotTranscriptRoles},
	{Tool: CopilotCLI, ServiceName: "github-copilot", DisplayName: "GitHub Copilot CLI", TranscriptRoles: copilotTranscriptRoles},
}

// Attribute keys that the tools use for the same value, in priority order.
// Callers must not modify them.
var (
	// SessionIDKeys identify the session of a log or span. Some tools
	// (Copilot) carry several of them as aliases of one session.
	SessionIDKeys = []string{
		"session.id",
		"conversation.id",
		"gen_ai.conversation.id",
		"copilot_chat.session_id",
		"copilot_chat.chat_session_id",
	}
	ModelKeys      = []string{"model", "gen_ai.response.model", "gen_ai.request.model", "llm.model_name"}
	ToolNameKeys   = []string{"tool.name", "gen_ai.tool.name", "tool_name", "function_name", "openinference.tool.name"}
	ToolInputKeys  = []string{"tool.input", "gen_ai.tool.call.arguments", "tool_parameters", "tool.parameters", "arguments"} // Codex CLI uses "arguments"
	ToolOutputKeys = []string{"tool.output", "gen_ai.tool.call.result", "tool_result", "tool.result", "output"}              // Codex CLI OTLP uses "output"
)

// transcriptRoles merges the providers' TranscriptRoles. Event names are
// unique across tools, so the role does not depend on the sending service.
var transcriptRoles = mergeTranscriptRoles(providers)

func mergeTranscriptRoles(providers []Provider) map[string]string {
	roles := make(map[string]string)
	for _, p := range providers {
		for event, role := range p.TranscriptRoles {
			if existing, ok := roles[event]; ok && existing != role {
				panic("tools: event " + event + " has transcript roles " + existing + " and " + role)
			}
			roles[event] = role
		}
	}
	return roles
}

// TranscriptRole returns the transcript role of a conversation event, or ""
// for events that are not part of a transcript
func TranscriptRole(eventName string) string {
	return transcriptRoles[eventName]
}

// Providers returns all supported tools
func Providers() []Provider {
	return append([]Provider(nil), providers...)
}

// Lookup returns the provider with the given CLI name
func Lookup(name string) (Provider, bool) {
	for _, p := range providers {
		if string(p.Tool) == name {
			return p, true
		}
	}
	return Provider{}, false
}

// ServiceName returns the OTLP service.name attribute for this tool.
// These must match the service.name sent by the actual tools via OTLP telemetry.
func (t Tool) ServiceName() string {
	if p, ok := Lookup(string(t)); ok {
		return p.ServiceName
	}
	return string(t)
}

// All returns all supported tools
func All() []Tool {
	all := make([]Tool, len(providers))
	for i, p := range providers {
		all[i] = p.Tool
	}
	return all
}

// Names returns the CLI names of all supported tools
func Names() []string {
	names := make([]string, len(providers))
	for i, p := range providers {
		names[i] = string(p.Tool)
	}
	return names
}

// Parse converts a string to a Tool, returning ok=false if invalid
func Parse(s string) (Tool, bool) {
	p, ok := Lookup(s)
	return p.Tool, ok
}

// ServiceNameFor returns the service name for a tool string, or empty if invalid
func ServiceNameFor(tool string) string {
	if t, ok := Parse(tool); ok {
		return t.ServiceName()
	}
	return ""
}

// NormalizeServiceName accepts either a tool name (claude-code, codex, gemini,
// opencode, copilot-chat, github-copilot) or a service name (codex_cli_rs,
// gemini_cli, ...) and returns the canonical service name. Returns empty
// string if invalid.
func NormalizeServiceName(input string) string {
	for _, p := range providers {
		if string(p.Tool) == input || p.ServiceName == input {
			return p.ServiceName
		}
	}
	return ""
}
