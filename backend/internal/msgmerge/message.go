// Package msgmerge turns the log records of a session into transcript
// messages and decides which records describe the same message (Merge).
// Transcripts (internal/transcript) show each message once by these rules,
// and session lists (storage) count messages and usage by the same rules,
// from a key each log record stores (Key), so both agree.
package msgmerge

import (
	"encoding/json"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/tobilg/ai-observer/internal/api"
	"github.com/tobilg/ai-observer/internal/attrval"
	"github.com/tobilg/ai-observer/internal/tools"
)

// EventName returns the event a log records: its event.name attribute, else
// its body when that looks like an event name (Codex CLI's events have it
// there)
func EventName(body string, attrs map[string]string) string {
	if event := attrs["event.name"]; event != "" {
		return event
	}
	if strings.Contains(body, ".") && !strings.Contains(body, " ") {
		return body
	}
	return ""
}

// UnrecordedPromptPattern matches, with surrounding whitespace, the text
// of a user message that stands for a prompt whose text the tool did not
// record: tools.RedactedPrompt, or the body of a Gemini CLI prompt event
// sent without its text (prompt logging off), which a transcript shows as
// the prompt. Such texts are shorter than UnrecordedPromptMaxLen bytes.
var UnrecordedPromptPattern = `\s*(?:` + regexp.QuoteMeta(tools.RedactedPrompt) + `|User prompt\. Length: \d+\.)\s*`

// UnrecordedPromptMaxLen bounds the length of the texts that
// UnrecordedPromptPattern matches, so that session lists test only short
// prompts (trimming every prompt made them 35% slower)
const UnrecordedPromptMaxLen = 64

var unrecordedPrompt = regexp.MustCompile(`^(?:` + UnrecordedPromptPattern + `)$`)

// InjectedPromptPattern matches the start of a user message that a tool
// adds to the conversation instead of the user typing it, so it gives no
// session title. Codex sends its context as user messages: tagged blocks
// such as <environment_context>, <user_instructions>, <skill ...>,
// <user_shell_command> and image markers, and "# AGENTS.md instructions
// for ..." and "# Files mentioned by the user" (codex-rs core
// user_instructions.rs, event_mapping.rs; the latter seen in rollouts);
// Claude Code's slash commands arrive as <command-name> blocks. A prompt
// the user starts with a tag gives no title either; the next one does.
var InjectedPromptPattern = `^\s*(?:<[A-Za-z/]|# AGENTS\.md instructions for |# Files mentioned by the user)`

var injectedPrompt = regexp.MustCompile(InjectedPromptPattern)

// InjectedPrompt reports whether the text of a user message is context a
// tool injected (InjectedPromptPattern)
func InjectedPrompt(text string) bool {
	return injectedPrompt.MatchString(text)
}

// UnrecordedPrompt reports whether the text of a user message stands for a
// prompt whose text the tool did not record (UnrecordedPromptPattern): such
// a message gives no session title
func UnrecordedPrompt(text string) bool {
	return len(text) <= UnrecordedPromptMaxLen && unrecordedPrompt.MatchString(text)
}

// LogMessage returns the transcript message of a log and its event name,
// or false when the log is no message. The message's index is not set.
func LogMessage(at time.Time, body string, attrs map[string]string) (api.TranscriptMessage, string, bool) {
	eventName := EventName(body, attrs)
	role := tools.TranscriptRole(eventName)
	if eventName == tools.TranscriptMessageEvent {
		role = attrs["message.role"]
	}
	if role == "" {
		return api.TranscriptMessage{}, "", false
	}
	return api.TranscriptMessage{
		Timestamp:    at,
		Role:         role,
		Content:      messageContent(eventName, attrs, body),
		Model:        ModelName(attrs),
		ToolName:     ToolName(attrs),
		ToolInput:    ToolInput(attrs),
		ToolOutput:   ToolOutput(attrs),
		InputTokens:  attrval.Int(attrs, tools.InputTokenKeys...),
		OutputTokens: attrval.Int(attrs, tools.OutputTokenKeys...),
		CacheRead:    attrval.Int(attrs, tools.CacheReadTokenKeys...),
		CacheWrite:   attrval.Int(attrs, tools.CacheWriteTokenKeys...),
		CostUSD:      attrval.Float(attrs, tools.CostKeys...),
		DurationMs:   attrval.Int(attrs, "duration_ms", "durationMs"),
		Success:      attrval.Bool(attrs, "success", "tool_success"),
		OutputSize:   attrval.Int(attrs, "tool_result_size_bytes", "outputSize"),
	}, eventName, true
}

// ModelName returns the model a record names
func ModelName(attrs map[string]string) string { return attrval.String(attrs, tools.ModelKeys...) }

// ToolName returns the tool a record names
func ToolName(attrs map[string]string) string { return attrval.String(attrs, tools.ToolNameKeys...) }

// ToolInput returns the input of a tool call
func ToolInput(attrs map[string]string) string { return attrval.String(attrs, tools.ToolInputKeys...) }

// ToolOutput returns the output of a tool call
func ToolOutput(attrs map[string]string) string {
	return attrval.String(attrs, tools.ToolOutputKeys...)
}

// ToolCallID returns the ID of the tool call that a tool message with these
// attributes belongs to, or "" for other messages and unnamed calls
func ToolCallID(attrs map[string]string, role string) string {
	if role != tools.RoleToolUse && role != tools.RoleToolResult {
		return ""
	}
	return attrval.String(attrs, tools.ToolCallIDKeys...)
}

// messageContent extracts the content of a message from its attributes,
// depending on its event
func messageContent(eventName string, attrs map[string]string, body string) string {
	if slices.Contains(tools.PromptEvents, eventName) {
		// User prompts have the actual text in the 'prompt' attribute
		if prompt := attrs[tools.PromptKey]; prompt != "" {
			return prompt
		}
		return body
	}
	switch eventName {
	case tools.ReplyEvent:
		// Claude Code's reply text (with content logging)
		if response := attrs[tools.ResponseKey]; response != "" {
			return response
		}
		return body

	case tools.TranscriptMessageEvent:
		// Imported transcripts already have content in body
		return body

	case "codex.tool_result":
		// Codex tool results have output in attributes
		if output, ok := attrs["output"]; ok && output != "" {
			return output
		}
		return body

	case "gen_ai.client.inference.operation.details", "copilot_chat.agent.turn":
		if output := GenAIMessageText(attrs["gen_ai.output.messages"], "assistant"); output != "" {
			return output
		}
		if output, ok := attrs["gen_ai.output.messages"]; ok && output != "" {
			return output
		}
		if input := GenAIMessageText(attrs["gen_ai.input.messages"], "user"); input != "" {
			return input
		}
		return body

	case "copilot_chat.tool.call":
		if result, ok := attrs["gen_ai.tool.call.result"]; ok && result != "" {
			return result
		}
		return body

	default:
		// For other events, use the body as-is
		return body
	}
}

// GenAIMessageText returns the text of the messages of role (all roles for
// "") in a GenAI messages attribute (gen_ai.input.messages,
// gen_ai.output.messages), a JSON list of messages or a single message
func GenAIMessageText(raw, role string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}

	var messages []map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &messages); err != nil {
		var single map[string]interface{}
		if err := json.Unmarshal([]byte(raw), &single); err != nil {
			return ""
		}
		messages = []map[string]interface{}{single}
	}

	var parts []string
	for _, msg := range messages {
		msgRole, _ := msg["role"].(string)
		if role != "" && msgRole != "" && !strings.EqualFold(msgRole, role) {
			continue
		}
		if text := genAIContentText(msg["content"]); text != "" {
			parts = append(parts, text)
			continue
		}
		if text := genAIContentText(msg["parts"]); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n")
}

func genAIContentText(value interface{}) string {
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	case []interface{}:
		var parts []string
		for _, item := range v {
			if text := genAIContentText(item); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "\n")
	case map[string]interface{}:
		if text, ok := v["content"].(string); ok && strings.TrimSpace(text) != "" {
			return strings.TrimSpace(text)
		}
		if text, ok := v["text"].(string); ok && strings.TrimSpace(text) != "" {
			return strings.TrimSpace(text)
		}
		if inner, ok := v["parts"]; ok {
			return genAIContentText(inner)
		}
	}
	return ""
}
