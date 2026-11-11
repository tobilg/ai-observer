package importer

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Tool calls in Codex session files (rollout-*.jsonl)
//
// A Codex session file records each tool call the model makes as a
// response_item: function_call (exec_command, write_stdin, MCP tools, ...),
// custom_tool_call (freeform tools: apply_patch, and exec, code mode's
// JavaScript cell), local_shell_call (earlier versions), tool_search_call,
// and the hosted web_search_call and image_generation_call. Its output
// follows as function_call_output, custom_tool_call_output or
// tool_search_output with the same call_id.
//
// What a call ran is recorded besides as an execution: item_completed events
// (CommandExecution, FileChange, McpToolCall, WebSearch, ImageView,
// Extension (sleep, web search, image generation), DynamicToolCall,
// CollabAgentToolCall), or in files whose history mode is "legacy"
// patch_apply_end, mcp_tool_call_end, web_search_end and
// image_generation_end events. An execution of a call the model made has the
// call's ID and adds what the output lacks (exit code, duration, success);
// the tools that code mode's exec cell calls (since Codex 0.144: commands,
// patches, MCP tools, images) are recorded only as executions, with IDs of
// their own. Codex's OTLP events (codex.tool_decision, codex.tool_result)
// carry the same IDs as call_id.
//
// Import and watch store each call once: a tool_use and a tool_result
// transcript message with the call's ID in tool.call_id, from the
// response_items where the model made the call, else from the execution.
// Executions of the model's calls only add to the result. Tool calls code
// mode makes that Codex records nowhere in the file (write_stdin, curr_time)
// are known from OTLP only.

// codexToolTextLimit bounds the text of a tool call's input and output a
// record stores. Codex itself keeps at most 64 KiB of a command's output in
// the file and sends 2 KiB over OTLP; the middle of longer text is replaced
// by a marker (codexToolText).
const codexToolTextLimit = 16 << 10

// codexCallLimit bounds the tool calls a session's state keeps, of the
// calls whose output was not read yet (CodexState.ToolCalls; at most 17 at a
// time were measured) and of those whose output was (AnsweredCalls). An
// execution read after its call's output follows within 28 other calls in
// 99% of 2,873 measured cases; with 1,000 one came later (after 3,501
// calls) and was stored as a call of its own.
const codexCallLimit = 1000

// Kinds of the model's tool calls (CodexToolCall.Kind)
const (
	codexCallFunction        = "function"
	codexCallCustom          = "custom"
	codexCallLocalShell      = "local_shell"
	codexCallToolSearch      = "tool_search"
	codexCallWebSearch       = "web_search"
	codexCallImageGeneration = "image_generation"
)

// CodexToolCall is a tool call the model made whose output has not been
// read yet. The watcher keeps it in its parser state, so a call and its
// output or execution may be read in different runs.
type CodexToolCall struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
	Kind string `json:"kind,omitempty"`
	// What an execution read before the output adds to it
	Execution  bool  `json:"execution,omitempty"`
	Success    *bool `json:"success,omitempty"`
	ExitCode   *int  `json:"exitCode,omitempty"`
	DurationMs int   `json:"durationMs,omitempty"`
}

// hasOutputItem reports whether the call's result is a response_item of its
// own (an output), not its execution
func (c *CodexToolCall) hasOutputItem() bool {
	return c.Kind != codexCallWebSearch
}

// pendingCall returns the call with the ID whose output was not read yet,
// or nil
func (s *CodexState) pendingCall(id string) *CodexToolCall {
	if id == "" {
		return nil
	}
	s.index()
	if i, ok := s.pending[id]; ok {
		return &s.ToolCalls[i]
	}
	return nil
}

// answeredCall reports whether the call with the ID was answered: its
// output was read, or it has none and its execution was
func (s *CodexState) answeredCall(id string) bool {
	s.index()
	_, ok := s.answered[id]
	return ok
}

// addCall records a call of the model whose output follows. Lists drop
// their oldest entries beyond codexCallLimit in batches, so adding stays
// cheap; import and watch drop the same ones, since they add the same calls
// in the same order (the watcher stores the lists as they are).
func (s *CodexState) addCall(call CodexToolCall) {
	s.index()
	s.ToolCalls = append(s.ToolCalls, call)
	s.pending[call.ID] = len(s.ToolCalls) - 1
	if len(s.ToolCalls) > codexCallLimit+codexCallLimit/4 {
		s.ToolCalls = append([]CodexToolCall(nil), s.ToolCalls[len(s.ToolCalls)-codexCallLimit:]...)
		s.pending = nil
	}
}

// answer records that the call's output was read (or, for a call without
// output, its execution) and returns the call (nil if it was not pending).
// It stays known by ID while its execution may follow.
func (s *CodexState) answer(id string) *CodexToolCall {
	call := s.pendingCall(id)
	if call == nil {
		return nil
	}
	answered := *call
	i := s.pending[id]
	s.ToolCalls = append(s.ToolCalls[:i], s.ToolCalls[i+1:]...)
	s.pending = nil
	if !answered.Execution {
		s.addAnswered(id)
	}
	return &answered
}

func (s *CodexState) addAnswered(id string) {
	s.index()
	s.AnsweredCalls = append(s.AnsweredCalls, id)
	s.answered[id] = struct{}{}
	if len(s.AnsweredCalls) > codexCallLimit+codexCallLimit/4 {
		s.AnsweredCalls = append([]string(nil), s.AnsweredCalls[len(s.AnsweredCalls)-codexCallLimit:]...)
		s.answered = nil
	}
}

// index builds the lookups of the lists when they are missing
func (s *CodexState) index() {
	if s.pending == nil {
		s.pending = make(map[string]int, len(s.ToolCalls))
		for i, c := range s.ToolCalls {
			s.pending[c.ID] = i
		}
	}
	if s.answered == nil {
		s.answered = make(map[string]struct{}, len(s.AnsweredCalls))
		for _, id := range s.AnsweredCalls {
			s.answered[id] = struct{}{}
		}
	}
}

// codexResponsePayload is a response_item's payload: a message, reasoning,
// or a tool call or its output. Fields whose type differs between item
// types are raw.
type codexResponsePayload struct {
	Type      string                  `json:"type"`
	ID        string                  `json:"id"`
	Role      string                  `json:"role"`
	Content   []CodexContentItem      `json:"content"`
	Summary   []CodexReasoningSummary `json:"summary"`
	Name      string                  `json:"name"`
	Namespace string                  `json:"namespace"`
	CallID    string                  `json:"call_id"`
	Status    string                  `json:"status"`
	// arguments: a JSON string for function calls, a value for tool
	// search; input: the freeform text of custom tool calls; output: text
	// or content items. Decoded with the payload (as any), not again later:
	// outputs are most of a session file.
	Arguments     any             `json:"arguments"`
	Input         any             `json:"input"`
	Output        any             `json:"output"`
	Action        json.RawMessage `json:"action"`
	Tools         json.RawMessage `json:"tools"`
	RevisedPrompt string          `json:"revised_prompt"`
}

// codexExecution is what an execution (item_completed item or legacy end
// event) records of a tool call
type codexExecution struct {
	ID         string
	Name       string
	Namespace  string
	MCPServer  string
	Input      string
	Output     string
	Success    *bool
	ExitCode   *int
	DurationMs int
	// Start and End of the execution, zero when the file does not say
	Start, End time.Time
	// Hosted web searches can be recorded before the model's call, which
	// then names the same search (web_search_call)
	precedesCall bool
}

// toolMessage adds a tool_use or tool_result transcript message of a call
type toolMessage struct {
	at                   time.Time
	role, callID, name   string
	namespace, mcpServer string
	input, output        string
	success              *bool
	exitCode             *int
	durationMs           int
}

func (m toolMessage) attrs() map[string]string {
	attrs := map[string]string{}
	if m.name != "" {
		attrs["tool.name"] = m.name
	}
	if m.namespace != "" {
		attrs["tool.namespace"] = m.namespace
	}
	if m.mcpServer != "" {
		attrs["mcp_server"] = m.mcpServer
	}
	if m.callID != "" {
		attrs["tool.call_id"] = m.callID
	}
	if m.input != "" {
		attrs["tool.input"] = codexToolText(m.input)
	}
	if m.output != "" {
		attrs["tool.output"] = m.output
	}
	if m.success != nil {
		attrs["success"] = strconv.FormatBool(*m.success)
	}
	if m.exitCode != nil {
		attrs["exit_code"] = strconv.Itoa(*m.exitCode)
	}
	if m.durationMs > 0 {
		attrs["duration_ms"] = strconv.Itoa(m.durationMs)
	}
	return attrs
}

// toolCallBody is the body of a tool_use message, as the other importers
// write it
func toolCallBody(name string) string { return fmt.Sprintf("Tool call: %s", name) }

// codexToolText returns text cut to about codexToolTextLimit bytes: its
// start and end, cut at UTF-8 character boundaries, with a marker naming the
// bytes left out in between
func codexToolText(text string) string {
	if len(text) <= codexToolTextLimit {
		return text
	}
	head := codexToolTextLimit / 2
	for head > 0 && !utf8.RuneStart(text[head]) {
		head--
	}
	tail := len(text) - codexToolTextLimit/2
	for tail < len(text) && !utf8.RuneStart(text[tail]) {
		tail++
	}
	return fmt.Sprintf("%s\n[… %d bytes truncated …]\n%s", text[:head], tail-head, text[tail:])
}

// rawString returns a raw JSON string's value, or the raw JSON of other
// values ("" for null and absent values)
func rawString(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	return string(raw)
}

// anyString returns a decoded JSON string's value, or the JSON of other
// values ("" for null)
func anyString(v any) string {
	switch v := v.(type) {
	case nil:
		return ""
	case string:
		return v
	}
	data, _ := json.Marshal(v)
	return string(data)
}

// codexOutputText returns the text of a raw tool output (codexOutputValue)
func codexOutputText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return string(raw)
	}
	return codexOutputValue(v)
}

// codexOutputValue returns the text of a decoded tool output: a string, or
// content items (text joined by newlines, images as "[image]")
func codexOutputValue(v any) string {
	switch v := v.(type) {
	case nil:
		return ""
	case string:
		return v
	case []any:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			m, _ := item.(map[string]any)
			switch m["type"] {
			case "input_text", "output_text", "text":
				text, _ := m["text"].(string)
				parts = append(parts, text)
			case "input_image", "image":
				parts = append(parts, "[image]")
			}
		}
		return strings.Join(parts, "\n")
	case map[string]any:
		if content, ok := v["content"].(string); ok {
			return content
		}
	}
	return anyString(v)
}

// legacyShellOutput reads the JSON text earlier versions' shell tool
// returned ({"output": ..., "metadata": {"exit_code": ..., "duration_seconds":
// ...}}), or reports false
func legacyShellOutput(text string) (output string, exitCode *int, durationMs int, ok bool) {
	if !strings.HasPrefix(text, `{"output":`) {
		return "", nil, 0, false
	}
	var v struct {
		Output   string `json:"output"`
		Metadata *struct {
			ExitCode        *int    `json:"exit_code"`
			DurationSeconds float64 `json:"duration_seconds"`
		} `json:"metadata"`
	}
	if json.Unmarshal([]byte(text), &v) != nil || v.Metadata == nil {
		return "", nil, 0, false
	}
	return v.Output, v.Metadata.ExitCode, int(v.Metadata.DurationSeconds * 1000), true
}

// convertToolCall converts a tool call or output response_item. It returns
// false for other items.
func (c *codexConverter) convertToolCall(item codexResponsePayload) bool {
	switch item.Type {
	case "function_call", "custom_tool_call", "local_shell_call", "tool_search_call":
		call := CodexToolCall{ID: item.CallID, Name: item.Name}
		input := ""
		switch item.Type {
		case "function_call":
			call.Kind = codexCallFunction
			input = anyString(item.Arguments)
		case "custom_tool_call":
			call.Kind = codexCallCustom
			input = anyString(item.Input)
		case "local_shell_call":
			call.Kind, call.Name = codexCallLocalShell, "local_shell"
			if call.ID == "" {
				call.ID = item.ID
			}
			input = localShellInput(item.Action)
		case "tool_search_call":
			call.Kind, call.Name = codexCallToolSearch, "tool_search"
			input = anyString(item.Arguments)
		}
		c.modelCall(call, item.Namespace, input)

	case "function_call_output", "custom_tool_call_output", "tool_search_output":
		output := codexOutputValue(item.Output)
		if item.Type == "tool_search_output" {
			output = rawString(item.Tools)
		}
		msg := toolMessage{at: c.ts, callID: item.CallID, name: item.Name, output: output}
		if text, exitCode, durationMs, ok := legacyShellOutput(output); ok {
			msg.output, msg.exitCode, msg.durationMs = text, exitCode, durationMs
			if exitCode != nil {
				success := *exitCode == 0
				msg.success = &success
			}
		}
		if call := c.state.answer(item.CallID); call != nil {
			if msg.name == "" {
				msg.name = call.Name
			}
			if call.Execution {
				msg.success, msg.durationMs = firstNonNil(call.Success, msg.success), firstPositive(call.DurationMs, msg.durationMs)
				msg.exitCode = firstNonNil(call.ExitCode, msg.exitCode)
			}
		}
		c.toolResult(msg)

	case "web_search_call":
		call := CodexToolCall{ID: firstNonEmptyString(item.CallID, item.ID), Name: "web_search", Kind: codexCallWebSearch}
		c.modelCall(call, "", rawString(item.Action))

	case "image_generation_call":
		// The hosted tool's item holds its result (the image, not stored)
		call := CodexToolCall{ID: firstNonEmptyString(item.CallID, item.ID), Name: "image_generation", Kind: codexCallImageGeneration}
		if c.modelCall(call, "", item.RevisedPrompt) {
			c.state.answer(call.ID)
			var success *bool
			if item.Status != "" {
				ok := item.Status == "completed"
				success = &ok
			}
			c.toolResult(toolMessage{at: c.ts, callID: call.ID, name: call.Name, success: success})
		}

	default:
		return false
	}
	return true
}

// modelCall stores a tool call of the model, unless the file recorded it
// before (a hosted web search's execution can precede the call), and
// reports whether it did
func (c *codexConverter) modelCall(call CodexToolCall, namespace, input string) bool {
	if call.ID != "" && (c.state.pendingCall(call.ID) != nil || c.state.answeredCall(call.ID)) {
		return false
	}
	c.toolUse(toolMessage{at: c.ts, callID: call.ID, name: call.Name, namespace: namespace, input: input})
	if call.ID != "" {
		c.state.addCall(call)
	}
	return true
}

// convertExecution stores an execution: as the result of the model's call
// of its ID when that call has no output of its own, as what its output
// adds when that follows, and as a call of its own (tool_use and
// tool_result) when the model did not make the call (code mode)
func (c *codexConverter) convertExecution(ex codexExecution) {
	if ex.ID == "" {
		return
	}
	end := ex.End
	if end.IsZero() {
		end = c.ts
	}
	result := toolMessage{
		at: end, callID: ex.ID, name: ex.Name, namespace: ex.Namespace, mcpServer: ex.MCPServer, output: ex.Output,
		success: ex.Success, exitCode: ex.ExitCode, durationMs: ex.DurationMs,
	}
	if call := c.state.pendingCall(ex.ID); call != nil {
		if !call.hasOutputItem() {
			// A hosted web search: its execution has the result
			if call.Name != "" {
				result.name = call.Name
			}
			c.state.answer(ex.ID)
			c.toolResult(result)
			return
		}
		// The output follows and shows what the execution adds
		call.Execution, call.Success, call.ExitCode, call.DurationMs = true, ex.Success, ex.ExitCode, ex.DurationMs
		return
	}
	if c.state.answeredCall(ex.ID) {
		// The model's call, whose output was read before
		return
	}
	start := ex.Start
	if start.IsZero() || start.After(end) {
		start = end
	}
	c.toolUse(toolMessage{at: start, callID: ex.ID, name: ex.Name, namespace: ex.Namespace, mcpServer: ex.MCPServer, input: ex.Input})
	c.toolResult(result)
	if ex.precedesCall {
		c.state.addAnswered(ex.ID)
	}
}

func (c *codexConverter) toolUse(m toolMessage) {
	m.role = "tool_use"
	c.transcriptAt(m.at, toolCallBody(m.name), m.role, m.attrs())
}

func (c *codexConverter) toolResult(m toolMessage) {
	m.role = "tool_result"
	size := len(m.output)
	m.output = codexToolText(m.output)
	attrs := m.attrs()
	if size > 0 {
		attrs["tool_result_size_bytes"] = strconv.Itoa(size)
	}
	// The output is stored once, in tool.output; the body summarizes it
	c.transcriptAt(m.at, toolResultBody(m, size), m.role, attrs)
}

// toolResultBody is the one-line body of a tool_result message: the tool,
// its exit code or status, and the size of its whole output, e.g.
// "exec_command: exit 1, 12.3 KB output"
func toolResultBody(m toolMessage, outputSize int) string {
	name := m.name
	if name == "" {
		name = "tool"
	}
	parts := []string{}
	switch {
	case m.exitCode != nil:
		parts = append(parts, fmt.Sprintf("exit %d", *m.exitCode))
	case m.success != nil && *m.success:
		parts = append(parts, "completed")
	case m.success != nil:
		parts = append(parts, "failed")
	}
	if outputSize > 0 {
		parts = append(parts, formatOutputSize(outputSize)+" output")
	} else {
		parts = append(parts, "no output")
	}
	return name + ": " + strings.Join(parts, ", ")
}

// formatOutputSize writes a byte count as bytes, KB or MB (1024-based)
func formatOutputSize(n int) string {
	switch {
	case n < 1024:
		return fmt.Sprintf("%d bytes", n)
	case n < 1024*1024:
		return fmt.Sprintf("%.1f KB", float64(n)/1024)
	default:
		return fmt.Sprintf("%.1f MB", float64(n)/(1024*1024))
	}
}

// codexItemEvent is an item_completed event's payload
type codexItemEvent struct {
	Item          json.RawMessage `json:"item"`
	StartedAtMs   int64           `json:"started_at_ms"`
	CompletedAtMs int64           `json:"completed_at_ms"`
}

// codexDuration is a Rust Duration as serde writes it
type codexDuration struct {
	Secs  int64 `json:"secs"`
	Nanos int64 `json:"nanos"`
}

func (d *codexDuration) ms() int {
	if d == nil {
		return 0
	}
	return int(d.Secs*1000 + d.Nanos/1_000_000)
}

// codexItem holds the fields of the item_completed items that record tool
// executions
type codexItem struct {
	Type      string `json:"type"`
	ID        string `json:"id"`
	Kind      string `json:"kind"` // Extension items
	Status    string `json:"status"`
	Namespace string `json:"namespace"`
	// CommandExecution
	Command          []string       `json:"command"`
	Cwd              string         `json:"cwd"`
	AggregatedOutput *string        `json:"aggregated_output"`
	FormattedOutput  string         `json:"formatted_output"`
	Stdout           string         `json:"stdout"`
	Stderr           string         `json:"stderr"`
	ExitCode         *int           `json:"exit_code"`
	Duration         *codexDuration `json:"duration"`
	// FileChange
	Changes map[string]codexFileChange `json:"changes"`
	// McpToolCall, DynamicToolCall, CollabAgentToolCall
	Server       string          `json:"server"`
	Tool         json.RawMessage `json:"tool"`
	Arguments    json.RawMessage `json:"arguments"`
	Result       json.RawMessage `json:"result"`
	Error        json.RawMessage `json:"error"`
	ContentItems json.RawMessage `json:"content_items"`
	Success      *bool           `json:"success"`
	Prompt       string          `json:"prompt"`
	// WebSearch and the web search extension
	Query   string          `json:"query"`
	Action  json.RawMessage `json:"action"`
	Results json.RawMessage `json:"results"`
	// ImageView
	Path string `json:"path"`
	// Extension items (camelCase)
	DurationMsCamel int    `json:"durationMs"`
	RevisedPrompt   string `json:"revisedPrompt"`
	SavedPath       string `json:"savedPath"`
}

// codexFileChange is a file's change in a FileChange item or patch event
type codexFileChange struct {
	Type        string `json:"type"` // add, delete, update
	Content     string `json:"content"`
	UnifiedDiff string `json:"unified_diff"`
	MovePath    string `json:"move_path"`
}

// convertItemCompleted converts an item_completed event whose item records
// a tool execution
func (c *codexConverter) convertItemCompleted(payload json.RawMessage) {
	var event codexItemEvent
	if json.Unmarshal(payload, &event) != nil || len(event.Item) == 0 {
		return
	}
	var item codexItem
	if json.Unmarshal(event.Item, &item) != nil {
		return
	}
	ex := codexExecution{ID: item.ID, DurationMs: item.Duration.ms()}
	if event.StartedAtMs > 0 {
		ex.Start = time.UnixMilli(event.StartedAtMs).UTC()
	}
	if event.CompletedAtMs > 0 {
		ex.End = time.UnixMilli(event.CompletedAtMs).UTC()
	}
	completed := func() *bool {
		if item.Status == "" {
			return nil
		}
		ok := item.Status == "completed"
		return &ok
	}
	switch item.Type {
	case "CommandExecution":
		ex.Name, ex.Input = "exec_command", commandInput(item.Command, item.Cwd)
		ex.Output = commandOutput(item.AggregatedOutput, item.FormattedOutput, item.Stdout, item.Stderr)
		ex.Success, ex.ExitCode = completed(), item.ExitCode
	case "FileChange":
		ex.Name, ex.Input = "apply_patch", patchText(item.Changes)
		ex.Output, ex.Success = joinNonEmpty(item.Stdout, item.Stderr), completed()
	case "McpToolCall":
		ex.Name, ex.MCPServer = rawString(item.Tool), item.Server
		ex.Input = rawString(item.Arguments)
		ex.Output, ex.Success = mcpResult(item.Result, item.Error)
		if s := completed(); s != nil && !*s {
			ex.Success = s
		}
	case "DynamicToolCall":
		ex.Name, ex.Namespace, ex.Input = rawString(item.Tool), item.Namespace, rawString(item.Arguments)
		ex.Output = joinNonEmpty(codexOutputText(item.ContentItems), rawString(item.Error))
		ex.Success = firstNonNil(item.Success, completed())
	case "CollabAgentToolCall":
		ex.Name, ex.Input, ex.Success = rawString(item.Tool), item.Prompt, completed()
	case "WebSearch":
		ex.Name, ex.Input, ex.Output = "web_search", webSearchInput(item.Query, item.Action), webSearchResults(item.Results)
		ex.precedesCall = true
	case "ImageView":
		ex.Name, ex.Input = "view_image", jsonObject("path", item.Path)
	case "ImageGeneration":
		ex.Name, ex.Input, ex.Output, ex.Success = "image_generation", item.RevisedPrompt, item.SavedPath, completed()
	case "Extension":
		ex.DurationMs = item.DurationMsCamel
		switch item.Kind {
		case "clock.sleep":
			ex.Name, ex.Namespace = "sleep", "clock"
			ex.Input = fmt.Sprintf(`{"duration_ms":%d}`, item.DurationMsCamel)
		case "web.search":
			ex.Name, ex.Namespace = "web_search", "web"
			ex.Input, ex.Output = webSearchInput(item.Query, item.Action), webSearchResults(item.Results)
		case "image_gen.generation":
			ex.Name, ex.Input, ex.Output, ex.Success = "image_generation", item.RevisedPrompt, item.SavedPath, completed()
		default:
			return
		}
	default:
		// Messages, reasoning and plans are response_items; other items
		// record no tool call
		return
	}
	c.convertExecution(ex)
}

// convertLegacyEvent converts the events files of the legacy history mode
// record executions with. It returns false for other events.
func (c *codexConverter) convertLegacyEvent(eventType string, payload json.RawMessage) bool {
	var ev struct {
		CallID     string                     `json:"call_id"`
		Stdout     string                     `json:"stdout"`
		Stderr     string                     `json:"stderr"`
		Success    *bool                      `json:"success"`
		Changes    map[string]codexFileChange `json:"changes"`
		Invocation *struct {
			Server    string          `json:"server"`
			Tool      string          `json:"tool"`
			Arguments json.RawMessage `json:"arguments"`
		} `json:"invocation"`
		Duration *codexDuration  `json:"duration"`
		Result   json.RawMessage `json:"result"`
		Query    string          `json:"query"`
		Action   json.RawMessage `json:"action"`
		Results  json.RawMessage `json:"results"`
		Status   string          `json:"status"`
		Revised  string          `json:"revised_prompt"`
		Saved    string          `json:"saved_path"`
		// exec_command_end
		Command          []string `json:"command"`
		Cwd              string   `json:"cwd"`
		AggregatedOutput *string  `json:"aggregated_output"`
		FormattedOutput  string   `json:"formatted_output"`
		ExitCode         *int     `json:"exit_code"`
	}
	switch eventType {
	case "patch_apply_end", "mcp_tool_call_end", "web_search_end", "image_generation_end", "exec_command_end":
	default:
		return false
	}
	if json.Unmarshal(payload, &ev) != nil {
		return true
	}
	ex := codexExecution{ID: ev.CallID, DurationMs: ev.Duration.ms()}
	switch eventType {
	case "patch_apply_end":
		ex.Name, ex.Input, ex.Output, ex.Success = "apply_patch", patchText(ev.Changes), joinNonEmpty(ev.Stdout, ev.Stderr), ev.Success
	case "mcp_tool_call_end":
		if ev.Invocation != nil {
			ex.Name, ex.MCPServer, ex.Input = ev.Invocation.Tool, ev.Invocation.Server, rawString(ev.Invocation.Arguments)
		}
		// result is Result<CallToolResult, String>: {"Ok": ...} or {"Err": ...}
		var result struct {
			Ok  json.RawMessage `json:"Ok"`
			Err json.RawMessage `json:"Err"`
		}
		if json.Unmarshal(ev.Result, &result) == nil && (len(result.Ok) > 0 || len(result.Err) > 0) {
			ex.Output, ex.Success = mcpResult(result.Ok, result.Err)
		} else {
			ex.Output, ex.Success = mcpResult(ev.Result, nil)
		}
	case "web_search_end":
		ex.Name, ex.Input, ex.Output = "web_search", webSearchInput(ev.Query, ev.Action), webSearchResults(ev.Results)
		ex.precedesCall = true
	case "image_generation_end":
		ex.Name, ex.Input, ex.Output = "image_generation", ev.Revised, ev.Saved
		if ev.Status != "" {
			ok := ev.Status == "completed"
			ex.Success = &ok
		}
	case "exec_command_end":
		ex.Name, ex.Input = "exec_command", commandInput(ev.Command, ev.Cwd)
		ex.Output = commandOutput(ev.AggregatedOutput, ev.FormattedOutput, ev.Stdout, ev.Stderr)
		ex.ExitCode = ev.ExitCode
		if ev.ExitCode != nil {
			ok := *ev.ExitCode == 0
			ex.Success = &ok
		}
	}
	c.convertExecution(ex)
	return true
}

// commandInput describes a command like exec_command's arguments: the
// script of a shell's -c/-lc, else the arguments joined
func commandInput(command []string, cwd string) string {
	cmd := strings.Join(command, " ")
	if len(command) == 3 && (command[1] == "-lc" || command[1] == "-c") {
		cmd = command[2]
	}
	args := map[string]string{"cmd": cmd}
	if cwd != "" {
		args["workdir"] = cwd
	}
	data, _ := json.Marshal(args)
	return string(data)
}

func commandOutput(aggregated *string, formatted, stdout, stderr string) string {
	if aggregated != nil {
		return *aggregated
	}
	if formatted != "" {
		return formatted
	}
	return joinNonEmpty(stdout, stderr)
}

// localShellInput describes a local_shell_call's exec action like
// exec_command's arguments
func localShellInput(action json.RawMessage) string {
	var exec struct {
		Command          []string `json:"command"`
		WorkingDirectory string   `json:"working_directory"`
	}
	if json.Unmarshal(action, &exec) != nil || len(exec.Command) == 0 {
		return rawString(action)
	}
	return commandInput(exec.Command, exec.WorkingDirectory)
}

// patchText describes file changes like apply_patch's input, in path order
func patchText(changes map[string]codexFileChange) string {
	paths := make([]string, 0, len(changes))
	for path := range changes {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	var b strings.Builder
	for _, path := range paths {
		change := changes[path]
		text, move := firstNonEmptyString(change.UnifiedDiff, change.Content), change.MovePath
		switch change.Type {
		case "add":
			fmt.Fprintf(&b, "*** Add File: %s\n", path)
		case "delete":
			fmt.Fprintf(&b, "*** Delete File: %s\n", path)
			text = "" // the deleted content
		default:
			fmt.Fprintf(&b, "*** Update File: %s\n", path)
		}
		if move != "" {
			fmt.Fprintf(&b, "*** Move to: %s\n", move)
		}
		if text != "" {
			b.WriteString(text)
			if !strings.HasSuffix(text, "\n") {
				b.WriteByte('\n')
			}
		}
	}
	return b.String()
}

// mcpResult returns the text of an MCP tool call's result (CallToolResult)
// or error, and whether it succeeded
func mcpResult(result, errValue json.RawMessage) (string, *bool) {
	if text := rawString(errValue); text != "" {
		var e struct {
			Message string `json:"message"`
		}
		if json.Unmarshal(errValue, &e) == nil && e.Message != "" {
			text = e.Message
		}
		failed := false
		return text, &failed
	}
	if len(result) == 0 || string(result) == "null" {
		return "", nil
	}
	var r struct {
		Content           json.RawMessage `json:"content"`
		IsError           *bool           `json:"isError"`
		StructuredContent json.RawMessage `json:"structuredContent"`
	}
	if json.Unmarshal(result, &r) != nil {
		return rawString(result), nil
	}
	text := codexOutputText(r.Content)
	if text == "" {
		text = rawString(r.StructuredContent)
	}
	ok := r.IsError == nil || !*r.IsError
	return text, &ok
}

// webSearchInput describes a web search: its action, else its query
func webSearchInput(query string, action json.RawMessage) string {
	if a := rawString(action); a != "" {
		return a
	}
	if query != "" {
		return jsonObject("query", query)
	}
	return ""
}

// webSearchResults lists a web search's results, a line per result (title
// and URL)
func webSearchResults(raw json.RawMessage) string {
	var results []struct {
		Title string `json:"title"`
		URL   string `json:"url"`
	}
	if json.Unmarshal(raw, &results) != nil {
		return ""
	}
	lines := make([]string, 0, len(results))
	for _, r := range results {
		if line := strings.TrimSpace(r.Title + " " + r.URL); line != "" {
			lines = append(lines, line)
		}
	}
	return strings.Join(lines, "\n")
}

func jsonObject(key, value string) string {
	if value == "" {
		return ""
	}
	data, _ := json.Marshal(map[string]string{key: value})
	return string(data)
}

func joinNonEmpty(parts ...string) string {
	kept := parts[:0:0]
	for _, p := range parts {
		if p != "" {
			kept = append(kept, p)
		}
	}
	return strings.Join(kept, "\n")
}

func firstNonEmptyString(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func firstNonNil[T any](values ...*T) *T {
	for _, v := range values {
		if v != nil {
			return v
		}
	}
	return nil
}

func firstPositive(values ...int) int {
	for _, v := range values {
		if v > 0 {
			return v
		}
	}
	return 0
}
