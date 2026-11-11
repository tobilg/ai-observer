package msgmerge

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/tobilg/ai-observer/internal/api"
	"github.com/tobilg/ai-observer/internal/attrval"
	"github.com/tobilg/ai-observer/internal/tools"
)

// Gemini CLI's records of a conversation (gemini-cli packages/core: the chat
// recording service writes the session file, telemetry/types.ts the events)
const (
	// geminiResponseEvent carries a response's token counts
	// (input_token_count includes cached_content_token_count)
	geminiResponseEvent = "gemini_cli.api_response"
	// geminiDetailsEvent is the GenAI semantic convention record Gemini CLI
	// sends beside each api_response (same callback): the response's text
	// in gen_ai.output.messages and its input and output tokens
	geminiDetailsEvent = "gen_ai.client.inference.operation.details"
	// geminiToolCallEvent is a completed tool call: function_name,
	// function_args, duration_ms, success; no call ID
	geminiToolCallEvent = "gemini_cli.tool_call"
	// Token counts of a response besides input, output and cached ones,
	// as api_response names them; imports store them under the same names
	geminiThoughtsKey   = "thoughts_token_count"
	geminiToolTokensKey = "tool_token_count"
	// geminiPromptLengthKey is the length of a gemini_cli.user_prompt's
	// text (JavaScript string length: UTF-16 code units), sent also when
	// prompt logging is off and the event carries no text
	geminiPromptLengthKey = "prompt_length"
	// geminiArgsKey holds the arguments of a gemini_cli.tool_call
	geminiArgsKey = "function_args"
)

// Key identifies what a log record says about its transcript message, as
// far as the merge rules (Merge) compare records: storage stores it with
// every log (otel_logs.MessageKey), so session lists apply the rules
// without reading the records' attributes.
type Key struct {
	// File tells whether the record is a session file's message
	// (transcript.message); others are OTLP events
	File bool
	// Tool is the tool of the record's service, or the service
	Tool  string
	Event string
	Role  string
	// CallID is the tool call ID of a tool message (tools.ToolCallIDKeys)
	CallID string
	// UUID is the session file entry's UUID (tools.MessageUUIDKey)
	UUID string
	// Request is the API request ID (tools.RequestIDKey)
	Request string
	// Prompt is the start of a user message's text (tools.MatchPrefix);
	// PromptUnrecorded tells that the tool did not record the text of the
	// prompt (empty, tools.RedactedPrompt, or a Gemini CLI prompt event
	// without its text)
	Prompt           string
	PromptUnrecorded bool
	// PromptLength is the length of a Gemini CLI prompt in UTF-16 code
	// units, as gemini_cli.user_prompt events send it (0: unknown)
	PromptLength int
	// Reply is a hash of the text of a Claude Code reply (tools.MatchText)
	Reply string
	// Usage is the usage the transcript shows on the message
	Usage Usage
	// Thoughts and ToolTokens are a Gemini CLI response's thought and tool
	// prompt tokens, -1 when the record does not carry them
	Thoughts, ToolTokens int
	// ToolName and Args (a hash of the arguments as canonical JSON)
	// identify a Gemini CLI tool call
	ToolName, Args string
}

// Usage is the usage of a transcript message, as transcripts total it
type Usage struct {
	InputTokens, OutputTokens, CacheRead, CacheWrite int
	CostUSD                                          float64
	// HasInput, HasOutput and HasCost tell whether the record carries the
	// value as a number
	HasInput, HasOutput, HasCost bool
}

// Fill sets the values of u that are 0 to those of other, as transcripts
// fill in a message from the records merged into it
// (transcript.enrichTranscriptMessage); u then has a value when either has
// it
func (u *Usage) Fill(other Usage) {
	u.HasInput = u.HasInput || other.HasInput
	u.HasOutput = u.HasOutput || other.HasOutput
	u.HasCost = u.HasCost || other.HasCost
	if u.InputTokens == 0 {
		u.InputTokens = other.InputTokens
	}
	if u.OutputTokens == 0 {
		u.OutputTokens = other.OutputTokens
	}
	if u.CacheRead == 0 {
		u.CacheRead = other.CacheRead
	}
	if u.CacheWrite == 0 {
		u.CacheWrite = other.CacheWrite
	}
	if u.CostUSD == 0 {
		u.CostUSD = other.CostUSD
	}
}

// ToolOf returns the tool that sends service, or service for other services
func ToolOf(service string) string {
	if tool, ok := tools.ToolForService(service); ok {
		return string(tool)
	}
	return service
}

// KeyOf returns the key of a log record, or false when it is no transcript
// message
func KeyOf(service, body string, attrs map[string]string) (Key, bool) {
	msg, event, ok := LogMessage(time.Time{}, body, attrs)
	if !ok {
		return Key{}, false
	}
	return KeyOfMessage(service, event, attrs, msg), true
}

// KeyOfMessage returns the key of a log record whose transcript message and
// event LogMessage returned
func KeyOfMessage(service, event string, attrs map[string]string, msg api.TranscriptMessage) Key {
	k := Key{
		File:    event == tools.TranscriptMessageEvent,
		Tool:    ToolOf(service),
		Event:   event,
		Role:    msg.Role,
		CallID:  ToolCallID(attrs, msg.Role),
		UUID:    attrs[tools.MessageUUIDKey],
		Request: attrs[tools.RequestIDKey],
		Usage: Usage{
			InputTokens: msg.InputTokens, OutputTokens: msg.OutputTokens,
			CacheRead: msg.CacheRead, CacheWrite: msg.CacheWrite, CostUSD: msg.CostUSD,
			HasInput:  hasNumber(attrs, tools.InputTokenKeys),
			HasOutput: hasNumber(attrs, tools.OutputTokenKeys),
			HasCost:   hasNumber(attrs, tools.CostKeys),
		},
		Thoughts: -1, ToolTokens: -1,
	}
	gemini := k.Tool == string(tools.Gemini)
	switch msg.Role {
	case tools.RoleUser:
		text := tools.MatchText(msg.Content)
		k.Prompt = tools.MatchPrefix(text)
		k.PromptUnrecorded = text == "" || text == tools.RedactedPrompt
		if gemini {
			if k.File {
				k.PromptLength = len(utf16.Encode([]rune(msg.Content)))
			} else {
				k.PromptLength = attrval.Int(attrs, geminiPromptLengthKey)
				// Without prompt logging the event's body describes the
				// prompt ("User prompt. Length: 42.")
				k.PromptUnrecorded = k.PromptUnrecorded || attrs[tools.PromptKey] == ""
			}
		}
	case tools.RoleAssistant:
		if k.Tool == string(tools.Claude) && event != tools.UsageEvent {
			k.Reply = hash(tools.MatchText(msg.Content))
		}
		if gemini {
			if v, ok := number(attrs, geminiThoughtsKey); ok {
				k.Thoughts = v
			}
			if v, ok := number(attrs, geminiToolTokensKey); ok {
				k.ToolTokens = v
			}
		}
	case tools.RoleToolUse:
		if gemini {
			k.ToolName = msg.ToolName
			k.Args = canonicalHash(attrval.String(attrs, "tool.input", geminiArgsKey))
		}
	}
	return k
}

// hasNumber reports whether the first of keys with a non-blank value holds
// a number, as session lists read usage (the value attrval reads)
func hasNumber(attrs map[string]string, keys []string) bool {
	for _, key := range keys {
		if v := strings.TrimSpace(attrs[key]); v != "" {
			_, err := strconv.ParseFloat(v, 64)
			return err == nil
		}
	}
	return false
}

// number returns the integer value of key, if it has one
func number(attrs map[string]string, key string) (int, bool) {
	v := strings.TrimSpace(attrs[key])
	if v == "" {
		return 0, false
	}
	n, err := strconv.Atoi(v)
	return n, err == nil
}

// hash returns a short hash of text
func hash(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:16])
}

// canonicalHash returns a hash of a JSON document in canonical form
// (compact, object keys sorted, numbers as written), so that arguments
// written with other whitespace compare equal, or "" when raw is no JSON
func canonicalHash(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return ""
	}
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return ""
	}
	return hash(strings.TrimSpace(b.String()))
}

// Keys are stored as their fields separated by keySep, KeyVersion first;
// a record that is no message stores KeyVersion alone. KeyVersion changes
// with keyRevision and with the attribute keys and constants of tools that
// keys are made of (keyInputs), so keys stored by another version are
// recognized: readers compute them from the attributes, and storage refills
// them at startup. Bump keyRevision when KeyOf or LogMessage changes what
// they return for a record.
const (
	keyRevision = "1"
	keySep      = "\x1f"
	keyFields   = 22
)

// KeyVersion is the version of the keys this code makes
var KeyVersion = keyRevision + "." + keyInputs()

// keyInputs returns a hash of the tool constants keys depend on
func keyInputs() string {
	var b strings.Builder
	write := func(values ...string) {
		for _, v := range values {
			b.WriteString(v)
			b.WriteByte(0)
		}
		b.WriteByte(1)
	}
	for _, event := range tools.TranscriptEvents() {
		write(event, tools.TranscriptRole(event))
	}
	for _, p := range tools.Providers() {
		write(append([]string{string(p.Tool)}, p.ServiceNames()...)...)
	}
	write(tools.ToolCallIDKeys...)
	write(tools.ToolNameKeys...)
	write(tools.InputTokenKeys...)
	write(tools.OutputTokenKeys...)
	write(tools.CacheReadTokenKeys...)
	write(tools.CacheWriteTokenKeys...)
	write(tools.CostKeys...)
	write(tools.PromptEvents...)
	write(tools.PromptKey, tools.MessageUUIDKey, tools.RequestIDKey, tools.ResponseKey, tools.UsageEvent, tools.ReplyEvent,
		tools.MatchTrim, strconv.Itoa(tools.PromptMatchPrefix), tools.RedactedPrompt, tools.TranscriptMessageEvent)
	return hash(b.String())[:8]
}

// NotMessage is the stored key of a record that is no transcript message
func NotMessage() string { return KeyVersion }

// clean makes a value storable in a key
func clean(s string) string {
	return strings.ReplaceAll(s, keySep, " ")
}

// Encode returns the stored form of k
func (k Key) Encode() string {
	flag := func(b bool) string {
		if b {
			return "1"
		}
		return ""
	}
	fields := []string{
		KeyVersion, flag(k.File), clean(k.Tool), clean(k.Event), clean(k.Role),
		clean(k.CallID), clean(k.UUID), clean(k.Request), clean(k.Prompt), flag(k.PromptUnrecorded),
		strconv.Itoa(k.PromptLength), k.Reply,
		strconv.Itoa(k.Usage.InputTokens), strconv.Itoa(k.Usage.OutputTokens),
		strconv.Itoa(k.Usage.CacheRead), strconv.Itoa(k.Usage.CacheWrite),
		strconv.FormatFloat(k.Usage.CostUSD, 'g', -1, 64),
		flag(k.Usage.HasInput) + "," + flag(k.Usage.HasOutput) + "," + flag(k.Usage.HasCost),
		strconv.Itoa(k.Thoughts), strconv.Itoa(k.ToolTokens), clean(k.ToolName), k.Args,
	}
	return strings.Join(fields, keySep)
}

// DecodeKey returns the key stored as s, or false when s is no key of this
// version
func DecodeKey(s string) (Key, bool) {
	// Split into an array: lists decode thousands of keys
	var f [keyFields]string
	n := 0
	for rest, ok := s, true; ok; n++ {
		if n == keyFields {
			return Key{}, false
		}
		f[n], rest, ok = strings.Cut(rest, keySep)
	}
	if n != keyFields || f[0] != KeyVersion {
		return Key{}, false
	}
	num := func(s string) int {
		n, _ := strconv.Atoi(s)
		return n
	}
	cost, _ := strconv.ParseFloat(f[16], 64)
	if len(f[17]) < 2 {
		return Key{}, false
	}
	hasInput, rest, _ := strings.Cut(f[17], ",")
	hasOutput, hasCost, _ := strings.Cut(rest, ",")
	return Key{
		File: f[1] == "1", Tool: f[2], Event: f[3], Role: f[4],
		CallID: f[5], UUID: f[6], Request: f[7], Prompt: f[8], PromptUnrecorded: f[9] == "1",
		PromptLength: num(f[10]), Reply: f[11],
		Usage: Usage{
			InputTokens: num(f[12]), OutputTokens: num(f[13]), CacheRead: num(f[14]), CacheWrite: num(f[15]),
			CostUSD: cost, HasInput: hasInput == "1", HasOutput: hasOutput == "1", HasCost: hasCost == "1",
		},
		Thoughts: num(f[18]), ToolTokens: num(f[19]), ToolName: f[20], Args: f[21],
	}, true
}

// KeyPrefix returns how every stored message key of this version starts,
// for readers that decide in SQL which stored keys they can use
func KeyPrefix() string { return KeyVersion + keySep }
