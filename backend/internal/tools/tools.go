// Package tools provides centralized definitions for supported AI coding tools.
// This ensures consistent service names across import, export, and query operations.
package tools

import (
	"sort"
	"strings"
	"time"
)

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
			"user_prompt": RoleUser,
			// api_request carries a response's usage, assistant_response
			// its text (Claude Code 2.1 and later, with content logging);
			// a transcript shows a response once (internal/transcript)
			"api_request":   RoleAssistant,
			ReplyEvent:      RoleAssistant,
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
	ModelKeys     = []string{"model", "gen_ai.response.model", "gen_ai.request.model", "llm.model_name"}
	ToolNameKeys  = []string{"tool.name", "gen_ai.tool.name", "tool_name", "function_name", "openinference.tool.name"}
	ToolInputKeys = []string{"tool.input", "gen_ai.tool.call.arguments", "tool_parameters", "tool.parameters", "arguments"} // Codex CLI uses "arguments"
	// ToolCallIDKeys identify one tool call across the spans, log events
	// and imported messages that describe it: Claude Code's spans and OTLP
	// events send the first two, its session files' messages tool.use_id;
	// Codex's OTLP events send call_id, its session files' messages
	// tool.call_id
	ToolCallIDKeys = []string{"gen_ai.tool.call.id", "tool_use_id", "tool.use_id", "call_id", "tool.call_id"}
	ToolOutputKeys = []string{"tool.output", "gen_ai.tool.call.result", "tool_result", "tool.result", "output"} // Codex CLI OTLP uses "output"

	// Usage of a conversation event: tokens and cost of a model request.
	// Claude Code's api_request sends cost_usd, input_tokens,
	// output_tokens, cache_read_tokens and cache_creation_tokens; Gemini
	// CLI's api_response *_token_count (its input includes the cached
	// tokens); imported Gemini messages input_tokens, output_tokens and
	// cache_read_input_tokens.
	InputTokenKeys      = []string{"input_tokens", "inputTokens", "gen_ai.usage.input_tokens", "llm.token_count.prompt", "input_token_count"}
	OutputTokenKeys     = []string{"output_tokens", "outputTokens", "gen_ai.usage.output_tokens", "llm.token_count.completion", "output_token_count"}
	CacheReadTokenKeys  = []string{"cache_read_input_tokens", "cacheRead", "gen_ai.usage.cache_read.input_tokens", "cache_read_tokens", "cached_content_token_count"}
	CacheWriteTokenKeys = []string{"cache_creation_input_tokens", "cacheWrite", "gen_ai.usage.cache_creation.input_tokens", "cache_creation_tokens"}
	CostKeys            = []string{"cost_usd", "costUsd", "llm.cost"}

	// PromptEvents are the user prompt events whose text is in the
	// PromptKey attribute (the body otherwise)
	PromptEvents = []string{"user_prompt", "codex.user_prompt", "gemini_cli.user_prompt"}
)

// PromptKey is the attribute with the text of a PromptEvents event
const PromptKey = "prompt"

// Identities a message of a session file shares with the OTLP events that
// describe it (Claude Code): the file entry's UUID, which user_prompt and
// assistant_response events carry, and the API request ID, which
// api_request, assistant_response and the tool events carry
const (
	MessageUUIDKey = "message.uuid"
	RequestIDKey   = "request_id"
)

// ResponseKey is the attribute with the text of Claude Code's
// assistant_response event
const ResponseKey = "response"

// UsageEvent is Claude Code's event of a response's usage (tokens, cost,
// duration), ReplyEvent its event of a response's text (Claude Code 2.1 and
// later, with content logging, in ResponseKey). A transcript shows both in
// one message of the API request; a session that arrived over OTLP only has
// both events for every response with text.
const (
	UsageEvent = "api_request"
	ReplyEvent = "assistant_response"
)

// Matching the messages of a session file to the OTLP events of the same
// messages where they share no ID (internal/transcript; session lists count
// messages by the same rules): a prompt matches the file's user message
// within PromptMatchWindow whose text, trimmed of MatchTrim, starts with the
// same PromptMatchPrefix characters; a Claude Code reply the file's reply of
// equal text within ReplyMatchWindow. Both sides record a prompt within
// 14 ms (measured on Claude Code and Codex sessions).
const (
	PromptMatchWindow = 2 * time.Second
	PromptMatchPrefix = 32
	ReplyMatchWindow  = time.Minute
	MatchTrim         = " \t\n\r"
)

// MatchText returns text as messages are compared by: trimmed of MatchTrim
func MatchText(text string) string {
	return strings.Trim(text, MatchTrim)
}

// MatchPrefix returns the start of text that prompts are compared by: the
// first PromptMatchPrefix characters of MatchText
func MatchPrefix(text string) string {
	text = MatchText(text)
	n := PromptMatchPrefix
	for i := range text {
		if n == 0 {
			return text[:i]
		}
		n--
	}
	return text
}

// MatchesPromptsByTime reports whether the OTLP prompts of tool are matched
// to its session files' user messages by time and text (verified on
// recorded sessions; Gemini CLI's are recorded at other times)
func MatchesPromptsByTime(tool string) bool {
	return tool == string(Claude) || tool == string(Codex)
}

// Repository attribution of imported and watched records (importer/repos.go)
const (
	RepositoryKey = "repository"
	BranchKey     = "git_branch"
)

// RedactedPrompt is what Claude Code sends as the prompt of a user_prompt
// event unless prompt logging is enabled (OTEL_LOG_USER_PROMPTS)
const RedactedPrompt = "<REDACTED>"

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

// TranscriptMessageEvent is the event of an imported transcript message,
// which carries its role in the message.role attribute
const TranscriptMessageEvent = "transcript.message"

// TranscriptEvents returns the names of the log events that become transcript
// messages: the tools' conversation events and imported messages, sorted
func TranscriptEvents() []string {
	events := []string{TranscriptMessageEvent}
	for event := range transcriptRoles {
		events = append(events, event)
	}
	sort.Strings(events)
	return events
}

// EventsWithRole returns the names of the conversation events that become
// transcript messages of role, sorted (imported transcript messages carry
// their role in message.role and are not among them)
func EventsWithRole(role string) []string {
	var events []string
	for event, r := range transcriptRoles {
		if r == role {
			events = append(events, event)
		}
	}
	sort.Strings(events)
	return events
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

// HasService reports whether service is one of the tool's service names
func (t Tool) HasService(service string) bool {
	tool, ok := ToolForService(service)
	return ok && tool == t
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
