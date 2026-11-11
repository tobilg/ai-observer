package msgmerge

import (
	"time"

	"github.com/tobilg/ai-observer/internal/tools"
)

// A session's messages can be recorded more than once. Claude Code
// describes a tool call with a tool_decision and a tool_result event and a
// claude_code.tool span, and each response with an api_request event (its
// usage) and an assistant_response event (its text); Gemini CLI each
// response with an api_response event (its usage) and a
// gen_ai.client.inference.operation.details record (its text). A session
// that arrived over OTLP and from its session file (import, watch) has each
// message in the file and as OTLP events. A transcript shows each message
// once: of the records of one message, the first in this order is shown,
// filled in with what the others add (tool name, usage, duration, success):
//
//  1. the session file's message (transcript.message), which has the content
//  2. the OTLP log event
//  3. the GenAI span (internal/transcript)
//
// Records are matched only by an identity they share or, where a tool sends
// none, by what both sides record about the same moment; equal text in
// distinct records is not a duplicate:
//
//   - tool calls by their ID (tools.ToolCallIDKeys): results with results,
//     calls with calls; a call event is also the file's when the file has
//     the call's result (files imported before their tool_use messages
//     carried the ID)
//   - messages by the session file entry's UUID (Claude Code's user_prompt
//     and assistant_response events, tools.MessageUUIDKey)
//   - a response's usage event (Claude Code's api_request) goes into a
//     message of the same API request (tools.RequestIDKey): the file's
//     reply or first tool call, else the assistant_response event
//     (mergeResponses)
//   - user prompts of tools whose events carry no ID (Codex, Gemini CLI;
//     Claude Code files imported before entries' UUIDs were stored): the
//     file's user message closest in time whose text starts like the
//     prompt's (PromptWindow); a prompt whose text the tool did not record
//     matches by time alone (Gemini CLI: by time and the length it sends)
//   - Claude Code replies without UUID: the file's reply of equal text
//     within tools.ReplyMatchWindow
//   - Gemini CLI responses: the session file records each response's
//     token counts (input, output, cached, thoughts, tool) as its
//     api_response event does, from the same usage metadata; the event goes
//     into the file's reply with equal counts closest in time within
//     GeminiMatchWindow. Its operation details record goes into the same
//     message: into the api_response of equal input and output tokens
//     within geminiDetailsWindow, or, when no file message has the
//     response, the api_response goes into it, which has the text.
//   - Gemini CLI tool calls: its tool_call events carry no call ID; an event
//     goes into the file's tool call of the same tool with equal arguments
//     closest in time within GeminiMatchWindow
//
// A pairing by time and value is one to one, nearest first. Records that
// match nothing are all shown: Gemini CLI's api_request events, for
// example, which no file records.
//
// Session lists count messages and usage by the same rules (storage's
// fillSessionExtents runs Merge on the stored keys); a test checks that
// lists and transcripts agree.

// Match windows of Gemini CLI records. Its session file records a prompt
// after its event, once the chat history is compressed when needed (a
// model request); a response when its text has streamed or, for a response
// of tool calls only, when the tool calls have run, which can wait for an
// approval; a tool call when all calls of its response have run.
const (
	GeminiPromptMatchWindow = 5 * time.Minute
	GeminiMatchWindow       = time.Hour
	// geminiDetailsWindow bounds the time between an api_response event
	// and its operation details record, which Gemini CLI emits right after it
	geminiDetailsWindow = time.Second
)

// PromptWindow returns the window within which a prompt event of tool
// matches the session file's user message, or 0 when tool's prompts are not
// matched by time
func PromptWindow(tool string) time.Duration {
	switch {
	case tools.MatchesPromptsByTime(tool):
		return tools.PromptMatchWindow
	case tool == string(tools.Gemini):
		return GeminiPromptMatchWindow
	}
	return 0
}

// Entry is a record of a session's transcript: its key and time
type Entry struct {
	Key  Key
	Time time.Time
}

// Values of Plan.Into besides the record merged into: Shown, and Dropped for
// a record of a session file message that has no ID to merge into
const (
	Shown   = -1
	Dropped = -2
)

// Step is a record merged into another, which is shown instead and filled
// in with what the record adds
type Step struct {
	From, Into int
}

// Plan is what Merge decided for records
type Plan struct {
	// Into is Shown, Dropped, or the record the record was merged into
	// (which may in turn be merged into another)
	Into []int
	// Steps are the merges in the order transcripts fill in messages
	Steps []Step
}

// Merge decides which of a session's records, in the order transcripts
// list them (time, then storage order), are shown and which are merged into
// which (the rules above)
func Merge(entries []Entry) Plan {
	p := &planner{entries: entries, into: make([]int, len(entries))}
	for i := range p.into {
		p.into[i] = Shown
	}
	p.mergeFileAndEvents()
	p.mergeResponses()
	p.mergeGeminiDetails()
	return Plan{Into: p.into, Steps: p.steps}
}

// Replay applies plan's steps in order to per-record values: fill(into,
// from) fills in the value of record into from that of record from, which
// was merged into it
func (p Plan) Replay(fill func(into, from int)) {
	for _, s := range p.Steps {
		fill(s.Into, s.From)
	}
}

type planner struct {
	entries []Entry
	into    []int
	steps   []Step
}

// root returns the record that record j is shown as
func (p *planner) root(j int) int {
	for p.into[j] >= 0 {
		j = p.into[j]
	}
	return j
}

// merge merges record i into record j, which is shown instead
func (p *planner) merge(i, j int) {
	j = p.root(j)
	if p.into[j] == Dropped {
		p.into[i] = Dropped
		return
	}
	if i == j {
		return
	}
	p.steps = append(p.steps, Step{From: i, Into: j})
	p.into[i] = j
}

// mergeFileAndEvents merges the OTLP events of messages the session file
// has into the file's messages
func (p *planner) mergeFileAndEvents() {
	type callKey struct{ tool, role, id string }
	type idKey struct{ tool, id string }
	type textKey struct{ tool, text string }
	fileCalls := make(map[callKey]int)
	fileUUIDs := make(map[idKey]int)
	fileUsers := make(map[string][]int)
	fileReplies := make(map[textKey][]int)
	var geminiReplies, geminiCalls []int
	events := false
	for i, e := range p.entries {
		k := e.Key
		if !k.File {
			events = true
			continue
		}
		if k.CallID != "" {
			if _, seen := fileCalls[callKey{k.Tool, k.Role, k.CallID}]; !seen {
				fileCalls[callKey{k.Tool, k.Role, k.CallID}] = i
			}
		}
		if k.UUID != "" {
			if _, seen := fileUUIDs[idKey{k.Tool, k.UUID}]; !seen {
				fileUUIDs[idKey{k.Tool, k.UUID}] = i
			}
		}
		switch k.Role {
		case tools.RoleUser:
			fileUsers[k.Tool] = append(fileUsers[k.Tool], i)
		case tools.RoleAssistant:
			key := textKey{k.Tool, k.Reply}
			fileReplies[key] = append(fileReplies[key], i)
			if k.Tool == string(tools.Gemini) && k.Usage.InputTokens > 0 {
				geminiReplies = append(geminiReplies, i)
			}
		case tools.RoleToolUse:
			if k.Tool == string(tools.Gemini) && k.Args != "" {
				geminiCalls = append(geminiCalls, i)
			}
		}
	}
	if !events || len(fileCalls)+len(fileUUIDs)+len(fileUsers)+len(fileReplies)+len(geminiCalls) == 0 {
		return
	}
	claimed := make(map[int]bool)
	claim := func(i, j int) {
		p.merge(i, j)
		claimed[j] = true
	}
	for i, e := range p.entries {
		k := e.Key
		if k.File {
			continue
		}
		if k.CallID != "" {
			if j, ok := fileCalls[callKey{k.Tool, k.Role, k.CallID}]; ok {
				p.merge(i, j)
				continue
			}
			if k.Role == tools.RoleToolUse {
				if _, ok := fileCalls[callKey{k.Tool, tools.RoleToolResult, k.CallID}]; ok {
					p.into[i] = Dropped // the file's call, which lacks the ID
					continue
				}
			}
		}
		if k.UUID != "" {
			if j, ok := fileUUIDs[idKey{k.Tool, k.UUID}]; ok {
				claim(i, j)
				continue
			}
		}
		switch {
		case k.Role == tools.RoleUser && PromptWindow(k.Tool) > 0:
			if j := p.nearestPrompt(fileUsers[k.Tool], claimed, i); j >= 0 {
				claim(i, j)
			}
		case k.Role == tools.RoleAssistant && k.Tool == string(tools.Claude) && k.Event != tools.UsageEvent:
			if j := p.nearest(fileReplies[textKey{k.Tool, k.Reply}], claimed, e.Time, tools.ReplyMatchWindow, nil); j >= 0 {
				claim(i, j)
			}
		case k.Tool == string(tools.Gemini) && k.Event == geminiResponseEvent && k.Usage.InputTokens > 0:
			if j := p.nearest(geminiReplies, claimed, e.Time, GeminiMatchWindow, func(f Key) bool { return sameGeminiResponse(f, k) }); j >= 0 {
				claim(i, j)
			}
		case k.Tool == string(tools.Gemini) && k.Event == geminiToolCallEvent && k.Args != "":
			if j := p.nearest(geminiCalls, claimed, e.Time, GeminiMatchWindow, func(f Key) bool {
				return f.ToolName == k.ToolName && f.Args == k.Args
			}); j >= 0 {
				claim(i, j)
			}
		}
	}
}

// sameGeminiResponse reports whether a session file's reply f records the
// response of api_response event e: equal token counts (thoughts and tool
// tokens when the file has them: imports before they were stored lack them)
func sameGeminiResponse(f, e Key) bool {
	return f.Usage.InputTokens == e.Usage.InputTokens && f.Usage.OutputTokens == e.Usage.OutputTokens &&
		f.Usage.CacheRead == e.Usage.CacheRead &&
		(f.Thoughts < 0 || f.Thoughts == e.Thoughts) && (f.ToolTokens < 0 || f.ToolTokens == e.ToolTokens)
}

// nearestPrompt returns the unclaimed file user message among candidates
// that records the prompt of event i, or -1
func (p *planner) nearestPrompt(candidates []int, claimed map[int]bool, i int) int {
	e := p.entries[i]
	window := PromptWindow(e.Key.Tool)
	if e.Key.PromptUnrecorded {
		var match func(Key) bool
		if e.Key.PromptLength > 0 {
			match = func(f Key) bool { return f.PromptLength == e.Key.PromptLength }
		}
		return p.nearest(candidates, claimed, e.Time, window, match)
	}
	return p.nearest(candidates, claimed, e.Time, window, func(f Key) bool { return f.Prompt == e.Key.Prompt })
}

// nearest returns the unclaimed record among candidates closest to at,
// within window and accepted by match (nil accepts all), or -1
func (p *planner) nearest(candidates []int, claimed map[int]bool, at time.Time, window time.Duration, match func(Key) bool) int {
	best, bestDist := -1, window+1
	for _, j := range candidates {
		dist := p.entries[j].Time.Sub(at).Abs()
		if dist > window || dist >= bestDist || claimed[j] || (match != nil && !match(p.entries[j].Key)) {
			continue
		}
		best, bestDist = j, dist
	}
	return best
}

// mergeResponses merges Claude Code's usage event of a response into a
// message of the same API request: the session file's reply, else its first
// tool call of the request (a response of tool calls only), else the
// assistant_response event. Responses recorded by neither keep it as a
// message of its own, as in sessions sent over OTLP only.
func (p *planner) mergeResponses() {
	type requestKey struct{ tool, id string }
	// rank orders the messages a usage event goes into, lowest first
	rank := func(k Key) int {
		switch {
		case k.File && k.Role == tools.RoleAssistant:
			return 0
		case k.File && k.Role == tools.RoleToolUse:
			return 1
		case !k.File && k.Role == tools.RoleAssistant && k.Event != tools.UsageEvent:
			return 2
		}
		return -1
	}
	targets := make(map[requestKey]int)
	for i, e := range p.entries {
		if e.Key.Request == "" || rank(e.Key) < 0 {
			continue
		}
		key := requestKey{e.Key.Tool, e.Key.Request}
		if j, seen := targets[key]; !seen || rank(e.Key) < rank(p.entries[j].Key) {
			targets[key] = i
		}
	}
	for i, e := range p.entries {
		k := e.Key
		if k.Event != tools.UsageEvent || k.Request == "" || p.into[i] != Shown || k.Tool != string(tools.Claude) {
			continue
		}
		if j, ok := targets[requestKey{k.Tool, k.Request}]; ok {
			p.merge(i, j)
		}
	}
}

// mergeGeminiDetails merges Gemini CLI's operation details record of a
// response and its api_response event: into the file's message of the
// response when the event went into one, else the event into the details
// record, which has the response's text
func (p *planner) mergeGeminiDetails() {
	var responses []int
	for i, e := range p.entries {
		if e.Key.Tool == string(tools.Gemini) && e.Key.Event == geminiResponseEvent {
			responses = append(responses, i)
		}
	}
	if len(responses) == 0 {
		return
	}
	claimed := make(map[int]bool)
	for i, e := range p.entries {
		k := e.Key
		if k.Tool != string(tools.Gemini) || k.Event != geminiDetailsEvent || p.into[i] != Shown {
			continue
		}
		j := p.nearest(responses, claimed, e.Time, geminiDetailsWindow, func(r Key) bool {
			return r.Usage.InputTokens == k.Usage.InputTokens && r.Usage.OutputTokens == k.Usage.OutputTokens
		})
		if j < 0 {
			continue
		}
		claimed[j] = true
		if p.into[j] == Shown {
			p.merge(j, i)
		} else {
			p.merge(i, j)
		}
	}
}
