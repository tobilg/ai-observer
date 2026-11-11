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
	// ServiceName is the OTLP service.name the tool sends, and the service
	// of records imported from its session files
	ServiceName string
	// OtherServices are further service names the tool sends over OTLP
	OtherServices []Service
	DisplayName   string
	// SessionFiles reports whether local session files can be imported and watched
	SessionFiles bool
	// TranscriptRoles maps the event names (log event.name) of the tool's
	// conversation events to transcript roles: user, assistant, tool_use,
	// tool_result or system. Imported transcripts ("transcript.message")
	// carry their role in the message.role attribute instead.
	TranscriptRoles map[string]string
	// NoiseSpans names the spans of the tool's services that are stored
	// only when a stored span has them as parent (IsNoiseSpan): spans the
	// tool sends in the hundreds of thousands a day without content of a
	// conversation
	NoiseSpans []string
	// BackgroundRoots names root spans of the tool's services whose traces
	// are background traces whatever their spans carry (see
	// Service.BackgroundTraces): work the tool does on its own rather than
	// a turn of a conversation
	BackgroundRoots []string
}

// Service is a further OTLP service.name of a tool
type Service struct {
	Name        string
	DisplayName string
	// BackgroundTraces marks services whose traces that belong to no
	// session are internal requests (reading files, settings and account
	// state), which trace lists leave out unless asked for
	BackgroundTraces bool
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
	// Codex 0.167 and later run conversations in an app server, which the
	// Codex app and the CLI share (originator Codex_Desktop or codex-tui),
	// and commands in an exec server; codex_cli_rs then sends only the
	// CLI's own startup telemetry
	{Tool: Codex, ServiceName: "codex_cli_rs", DisplayName: "OpenAI Codex CLI", SessionFiles: true,
		OtherServices: []Service{
			{Name: "codex-app-server", DisplayName: "OpenAI Codex", BackgroundTraces: true},
			{Name: "codex-exec-server", DisplayName: "OpenAI Codex exec server", BackgroundTraces: true},
		},
		TranscriptRoles: map[string]string{
			"codex.user_prompt":   RoleUser,
			"codex.api_request":   RoleAssistant,
			"codex.tool_result":   RoleToolResult,
			"codex.tool_decision": RoleToolUse,
		},
		// The app server traces each streamed response event
		// (handle_responses > receiving), each write of the session file and
		// each file status read: 86% of its spans in a day of use, and
		// 100,000 spans in a long turn. handle_responses is the parent of a
		// turn's tool calls (handle_output_item_done), which keep it.
		NoiseSpans: []string{
			"receiving",
			"handle_responses",
			"append_items",
			"persist_rollout_items",
			"realtime_conversation.running_state",
			"fs.get_metadata",
		},
		BackgroundRoots: []string{
			// The automatic approval review ("guardian") scores each
			// command a turn wants to run in a model session of its own:
			// nine of ten session traces in a day of use, about 550 spans each
			"decide_request",
			// What the CLI (codex_cli_rs, not a background service: earlier
			// versions send their sessions as it) still traces itself since
			// conversations run in the app server: single spans at startup
			"auth", "fs.get_metadata", "fs.read_file", "serve_inner", "transport_worker",
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

// ServiceNames returns all service names of the tool, ServiceName first
func (t Tool) ServiceNames() []string {
	if p, ok := Lookup(string(t)); ok {
		return p.ServiceNames()
	}
	return []string{string(t)}
}

// ServiceNames returns all service names of the provider, ServiceName first
func (p Provider) ServiceNames() []string {
	names := []string{p.ServiceName}
	for _, s := range p.OtherServices {
		names = append(names, s.Name)
	}
	return names
}

// ToolForService returns the tool that sends a service name
func ToolForService(service string) (Tool, bool) {
	for _, p := range providers {
		for _, name := range p.ServiceNames() {
			if name == service {
				return p.Tool, true
			}
		}
	}
	return "", false
}

// FilterServices returns the service names a service filter matches: all of
// a tool's for a tool name (codex), only itself for a service name, none
// for "" (no filter)
func FilterServices(filter string) []string {
	if filter == "" {
		return nil
	}
	if p, ok := Lookup(filter); ok {
		return p.ServiceNames()
	}
	return []string{filter}
}

// noiseSpans holds the providers' NoiseSpans by service name
var noiseSpans = func() map[string]map[string]bool {
	byService := make(map[string]map[string]bool)
	for _, p := range providers {
		if len(p.NoiseSpans) == 0 {
			continue
		}
		names := make(map[string]bool, len(p.NoiseSpans))
		for _, name := range p.NoiseSpans {
			names[name] = true
		}
		for _, service := range p.ServiceNames() {
			byService[service] = names
		}
	}
	return byService
}()

// IsNoiseSpan reports whether a service's span of this name is one of its
// tool's NoiseSpans
func IsNoiseSpan(service, spanName string) bool {
	return noiseSpans[service][spanName]
}

// BackgroundTraceServices returns the services whose traces outside a
// session are internal requests (Service.BackgroundTraces)
func BackgroundTraceServices() []string {
	var names []string
	for _, p := range providers {
		for _, s := range p.OtherServices {
			if s.BackgroundTraces {
				names = append(names, s.Name)
			}
		}
	}
	return names
}

// BackgroundTraceRoots returns the names of root spans that make a trace a
// background trace (Provider.BackgroundRoots), and the services whose spans
// they are
func BackgroundTraceRoots() (services, names []string) {
	for _, p := range providers {
		if len(p.BackgroundRoots) > 0 {
			services = append(services, p.ServiceNames()...)
			names = append(names, p.BackgroundRoots...)
		}
	}
	return services, names
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

// ResolveFilter accepts a tool name (claude-code, codex, gemini, opencode,
// copilot-chat, github-copilot) or one of the tools' service names
// (codex_cli_rs, codex-app-server, gemini_cli, ...) and returns the service
// filter for it (see FilterServices): the tool name, which matches all of
// the tool's services, or the service name, which matches only that one.
// Returns "" for other input.
func ResolveFilter(input string) string {
	if p, ok := Lookup(input); ok {
		return string(p.Tool)
	}
	if _, ok := ToolForService(input); ok {
		return input
	}
	return ""
}
