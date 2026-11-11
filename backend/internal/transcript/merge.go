package transcript

import (
	"fmt"
	"sort"

	"github.com/tobilg/ai-observer/internal/api"
	"github.com/tobilg/ai-observer/internal/msgmerge"
	"github.com/tobilg/ai-observer/internal/storage"
)

// toolCallID returns the ID of the tool call that a tool message with these
// attributes belongs to, or "" for other messages and unnamed calls
func toolCallID(attrs map[string]string, role string) string {
	return msgmerge.ToolCallID(attrs, role)
}

// The records of a message are merged by the rules of msgmerge.Merge (the
// session file's message first, then the OTLP event), which session lists
// count by as well; spans are merged here, after the logs.

// origin is where a transcript message was recorded, in order of
// precedence
type origin int

const (
	fromFile origin = iota
	fromEvent
	fromSpan
)

// record is a transcript message with what identifies it
type record struct {
	msg    api.TranscriptMessage
	origin origin
	tool   string
	callID string
	key    msgmerge.Key
	// source is the span a log event or span message belongs to
	source    sessionSpanKey
	hasSource bool
	// into is the record this one was merged into, or shown or dropped
	into int
}

// Values of record.into besides the record merged into: shown, and dropped
// for a record of a session file message that has no ID to merge into
const (
	shown   = msgmerge.Shown
	dropped = msgmerge.Dropped
)

// toolOf returns the tool that sends service, or service for other services
func toolOf(service string) string { return msgmerge.ToolOf(service) }

// logRecords returns the transcript messages of the logs with what
// identifies them, indexed as transcriptMessagesFromLogs indexes them
func logRecords(rows []storage.SessionLog) []record {
	var records []record
	index := 0
	for _, row := range rows {
		msg, event, ok := logMessage(row)
		if !ok {
			continue
		}
		if idxStr, ok := row.Attrs["message.index"]; ok {
			fmt.Sscanf(idxStr, "%d", &index)
		}
		msg.Index = index
		index++
		key := msgmerge.KeyOfMessage(row.ServiceName, event, row.Attrs, msg)
		r := record{msg: msg, origin: fromEvent, tool: key.Tool, callID: key.CallID, key: key, into: shown}
		if key.File {
			r.origin = fromFile
		} else if row.TraceID != "" && row.SpanID != "" {
			r.source, r.hasSource = sessionSpanKey{row.ServiceName, row.TraceID, row.SpanID}, true
		}
		records = append(records, r)
	}
	return records
}

func mergeTranscriptMessages(rows []storage.SessionLog, spans []spanTranscriptMessage) []api.TranscriptMessage {
	records := logRecords(rows)
	mergeLogs(records)
	records = mergeSpans(records, spans)

	var mixed [2]bool // file and other messages shown
	messages := make([]api.TranscriptMessage, 0, len(records))
	for _, r := range records {
		if r.into != shown {
			continue
		}
		mixed[min(int(r.origin), 1)] = true
		messages = append(messages, r.msg)
	}
	switch {
	case len(spans) > 0:
		sort.SliceStable(messages, func(i, j int) bool {
			return messages[i].Timestamp.Before(messages[j].Timestamp)
		})
		fallthrough
	case mixed[0] && mixed[1]:
		for i := range messages {
			messages[i].Index = i
		}
	}
	// Otherwise imported messages keep their indices
	return messages
}

// mergeLogs merges the records of the logs by msgmerge's rules, filling in
// the shown messages in the rules' order
func mergeLogs(records []record) {
	entries := make([]msgmerge.Entry, len(records))
	for i, r := range records {
		entries[i] = msgmerge.Entry{Key: r.key, Time: r.msg.Timestamp}
	}
	plan := msgmerge.Merge(entries)
	for i := range records {
		records[i].into = plan.Into[i]
	}
	plan.Replay(func(into, from int) {
		other := records[from].msg
		if records[into].origin == fromFile && records[from].origin != fromFile {
			// The file has the content; an event's is at most its name
			other.Content = ""
		}
		records[into].msg = enrichTranscriptMessage(records[into].msg, other)
	})
}

// merge merges record i into record j, which is shown instead
func merge(records []record, i, j int) {
	for records[j].into >= 0 {
		j = records[j].into
	}
	if records[j].into == dropped {
		records[i].into = dropped
		return
	}
	if i == j {
		return
	}
	other := records[i].msg
	if records[j].origin == fromFile && records[i].origin != fromFile {
		// The file has the content; an event's is at most its name
		other.Content = ""
	}
	records[j].msg = enrichTranscriptMessage(records[j].msg, other)
	records[i].into = j
}

// mergeSpans merges span messages into the log messages that describe the
// same message (by span and content, or tool call ID) and adds the others
func mergeSpans(records []record, spans []spanTranscriptMessage) []record {
	if len(spans) == 0 {
		return records
	}
	type messageKey struct {
		Source                                         sessionSpanKey
		Role, Content, ToolName, ToolInput, ToolOutput string
	}
	keyFor := func(source sessionSpanKey, msg api.TranscriptMessage) messageKey {
		return messageKey{source, msg.Role, msg.Content, msg.ToolName, msg.ToolInput, msg.ToolOutput}
	}
	type toolCallKey struct{ Tool, Role, ID string }
	logMessages := make(map[messageKey]int)
	logToolCalls := make(map[toolCallKey]int)
	for i, r := range records {
		if r.hasSource {
			logMessages[keyFor(r.source, r.msg)] = i
		}
		if r.callID != "" {
			key := toolCallKey{r.tool, r.msg.Role, r.callID}
			if _, seen := logToolCalls[key]; !seen {
				logToolCalls[key] = i
			}
		}
	}
	for _, span := range spans {
		spanRecord := record{msg: span.Message, origin: fromSpan, tool: toolOf(span.Source.ServiceName), into: shown}
		records = append(records, spanRecord)
		i := len(records) - 1
		if j, ok := logMessages[keyFor(span.Source, span.Message)]; ok {
			merge(records, i, j)
			continue
		}
		if span.ToolCallID != "" {
			if j, ok := logToolCalls[toolCallKey{spanRecord.tool, span.Message.Role, span.ToolCallID}]; ok {
				merge(records, i, j)
			}
		}
	}
	return records
}

func enrichTranscriptMessage(log, span api.TranscriptMessage) api.TranscriptMessage {
	// Content the span captured and the log event did not (messages matched
	// by content have the same already)
	if log.Content == "" {
		log.Content = span.Content
	}
	if log.ToolName == "" {
		log.ToolName = span.ToolName
	}
	if log.ToolInput == "" {
		log.ToolInput = span.ToolInput
	}
	if log.ToolOutput == "" {
		log.ToolOutput = span.ToolOutput
	}
	if log.Model == "" {
		log.Model = span.Model
	}
	if log.InputTokens == 0 {
		log.InputTokens = span.InputTokens
	}
	if log.OutputTokens == 0 {
		log.OutputTokens = span.OutputTokens
	}
	if log.CacheRead == 0 {
		log.CacheRead = span.CacheRead
	}
	if log.CacheWrite == 0 {
		log.CacheWrite = span.CacheWrite
	}
	if log.CostUSD == 0 {
		log.CostUSD = span.CostUSD
	}
	if log.DurationMs == 0 {
		log.DurationMs = span.DurationMs
	}
	if log.Success == nil {
		log.Success = span.Success
	}
	if log.OutputSize == 0 {
		log.OutputSize = span.OutputSize
	}
	return log
}
