package storage_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/tobilg/ai-observer/internal/api"
	"github.com/tobilg/ai-observer/internal/importer"
	"github.com/tobilg/ai-observer/internal/storage"
)

func transcriptRegressionStore(t *testing.T) *storage.DuckDBStore {
	t.Helper()
	s, err := storage.NewDuckDBStore(filepath.Join(t.TempDir(), "review.duckdb"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestSessionTranscript_PreserveDistinctImportedToolResults(t *testing.T) {
	s := transcriptRegressionStore(t)
	now := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)
	index := 12
	p := &importer.ClaudeParser{}
	logs := p.CreateTranscriptLogs(importer.ClaudeJSONLEntry{
		Type: "user",
		Message: &importer.ClaudeMessage{Content: []importer.ClaudeContent{
			{Type: "tool_result", ToolUseID: "call-a", Content: "success"},
			{Type: "tool_result", ToolUseID: "call-b", Content: "success"},
		}},
	}, now, "claude-session", &index)
	if len(logs) != 2 {
		t.Fatalf("parser produced %d logs", len(logs))
	}
	if err := s.InsertLogs(context.Background(), logs); err != nil {
		t.Fatal(err)
	}
	if err := s.InsertSpans(context.Background(), []api.Span{{
		Timestamp: now.Add(time.Second), ServiceName: "claude-code", TraceID: "live", SpanID: "chat",
		SpanAttributes: map[string]string{
			"gen_ai.operation.name": "chat", "session.id": "claude-session",
			"gen_ai.output.messages": `[{"role":"assistant","content":"live response"}]`,
		},
	}}); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetSessionTranscript(context.Background(), "claude-session")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Messages) != 2 {
		t.Fatalf("two distinct imported tool results (call-a and call-b) must survive; got %d messages: %+v", len(got.Messages), got.Messages)
	}
	if got.Messages[0].Index != 12 || got.Messages[1].Index != 13 || !got.LastTime.Equal(now) {
		t.Fatalf("imported indices and bounds changed: %+v", got)
	}
}

func TestSessionTranscript_CopilotChildSpanCorrelation(t *testing.T) {
	for _, hasChatSessionID := range []bool{true, false} {
		name := "no_session_attribute"
		if hasChatSessionID {
			name = "alternate_chat_session_id"
		}
		t.Run(name, func(t *testing.T) {
			s := transcriptRegressionStore(t)
			now := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)
			ctx := context.Background()
			if err := s.InsertLogs(ctx, []api.LogRecord{{
				Timestamp: now, ServiceName: "copilot-chat", Body: "copilot_chat.session.start",
				LogAttributes: map[string]string{"event.name": "copilot_chat.session.start", "session.id": "conversation-1"},
			}}); err != nil {
				t.Fatal(err)
			}
			toolAttrs := map[string]string{
				"gen_ai.operation.name": "execute_tool", "gen_ai.tool.name": "readFile",
				"gen_ai.tool.call.arguments": `{"filePath":"README.md"}`, "gen_ai.tool.call.result": "file contents",
			}
			if hasChatSessionID {
				toolAttrs["copilot_chat.chat_session_id"] = "vscode-chat-1"
			}
			if err := s.InsertSpans(ctx, []api.Span{
				{Timestamp: now, ServiceName: "copilot-chat", TraceID: "trace-1", SpanID: "agent", Duration: int64(3 * time.Second),
					SpanAttributes: map[string]string{
						"gen_ai.operation.name": "invoke_agent", "gen_ai.conversation.id": "conversation-1",
						"copilot_chat.session_id": "conversation-1", "copilot_chat.chat_session_id": "vscode-chat-1",
						"gen_ai.input.messages": `[{"role":"user","parts":[{"type":"text","content":"Read README"}]}]`,
					}},
				{Timestamp: now.Add(time.Second), ServiceName: "copilot-chat", TraceID: "trace-1", SpanID: "tool", ParentSpanID: "agent", Duration: int64(time.Second), SpanAttributes: toolAttrs},
				// These share a trace or parent ID but are not part of this session.
				{Timestamp: now.Add(time.Second), ServiceName: "copilot-chat", TraceID: "trace-1", SpanID: "sibling", SpanAttributes: map[string]string{"gen_ai.operation.name": "execute_tool"}},
				{Timestamp: now.Add(time.Second), ServiceName: "other-service", TraceID: "trace-1", SpanID: "other-service-tool", ParentSpanID: "agent", SpanAttributes: map[string]string{"gen_ai.operation.name": "execute_tool"}},
				{Timestamp: now.Add(time.Second), ServiceName: "copilot-chat", TraceID: "trace-1", SpanID: "other-session-tool", ParentSpanID: "agent", SpanAttributes: map[string]string{"gen_ai.operation.name": "execute_tool", "gen_ai.conversation.id": "other-session"}},
			}); err != nil {
				t.Fatal(err)
			}
			listed, err := s.QuerySessions(ctx, "", now.Add(-time.Hour), now.Add(time.Hour), 10, 0)
			if err != nil {
				t.Fatal(err)
			}
			if len(listed.Sessions) != 1 {
				t.Fatalf("expected one listed session: %+v", listed)
			}
			got, err := s.GetSessionTranscript(ctx, listed.Sessions[0].SessionID)
			if err != nil {
				t.Fatal(err)
			}
			if len(got.Messages) != 3 {
				t.Fatalf("expected user, tool_use, tool_result for linked child span; got %d messages: %+v", len(got.Messages), got.Messages)
			}
		})
	}
}

func TestSessionTranscript_ToolResultUsesCompletionTime(t *testing.T) {
	s := transcriptRegressionStore(t)
	now := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)
	if err := s.InsertSpans(context.Background(), []api.Span{{
		Timestamp: now, ServiceName: "copilot-chat", TraceID: "trace-1", SpanID: "tool", Duration: int64(5 * time.Second),
		SpanAttributes: map[string]string{"gen_ai.operation.name": "execute_tool", "gen_ai.conversation.id": "conversation-1", "gen_ai.tool.name": "runCommand", "gen_ai.tool.call.result": "done"},
	}}); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetSessionTranscript(context.Background(), "conversation-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Messages) != 2 {
		t.Fatalf("expected two messages: %+v", got.Messages)
	}
	if want := now.Add(5 * time.Second); !got.Messages[1].Timestamp.Equal(want) {
		t.Fatalf("tool result must occur at completion %s; got %s", want.Format(time.RFC3339Nano), got.Messages[1].Timestamp.Format(time.RFC3339Nano))
	}
}

func TestSessionTranscript_AgentOutputFallback(t *testing.T) {
	s := transcriptRegressionStore(t)
	now := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)
	if err := s.InsertSpans(context.Background(), []api.Span{{
		Timestamp: now, ServiceName: "copilot-chat", TraceID: "trace-1", SpanID: "agent", Duration: int64(time.Second),
		SpanAttributes: map[string]string{
			"gen_ai.operation.name": "invoke_agent", "gen_ai.conversation.id": "conversation-1",
			"gen_ai.input.messages":     `[{"role":"user","parts":[{"type":"text","content":"Hello"}]}]`,
			"gen_ai.output.messages":    `[{"role":"assistant","parts":[{"type":"text","content":"Hi there"}]}]`,
			"gen_ai.usage.input_tokens": "11", "gen_ai.usage.output_tokens": "3",
		},
	}}); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetSessionTranscript(context.Background(), "conversation-1")
	if err != nil {
		t.Fatal(err)
	}
	for _, message := range got.Messages {
		if message.Role == "assistant" && message.Content == "Hi there" {
			if !message.Timestamp.Equal(now.Add(time.Second)) || message.InputTokens != 11 || message.OutputTokens != 3 {
				t.Fatalf("unexpected fallback timestamp or usage: %+v", message)
			}
			return
		}
	}
	t.Fatalf("captured assistant output on agent span was discarded: %+v", got.Messages)
}

func TestSessionTranscript_AgentFallbackDoesNotDuplicateChildOutputOrUsage(t *testing.T) {
	for _, output := range []string{"done", "intermediate", ""} {
		t.Run("child_output="+output, func(t *testing.T) {
			s := transcriptRegressionStore(t)
			now := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)
			chatAttrs := map[string]string{"gen_ai.operation.name": "chat", "gen_ai.usage.input_tokens": "11", "gen_ai.usage.output_tokens": "3"}
			if output != "" {
				chatAttrs["gen_ai.output.messages"] = `[{"role":"assistant","content":"` + output + `"}]`
			}
			if err := s.InsertSpans(context.Background(), []api.Span{
				{Timestamp: now, ServiceName: "copilot-chat", TraceID: "trace", SpanID: "agent", Duration: int64(5 * time.Second), SpanAttributes: map[string]string{
					"gen_ai.operation.name": "invoke_agent", "gen_ai.conversation.id": "session",
					"gen_ai.output.messages":    `[{"role":"assistant","content":"done"}]`,
					"gen_ai.usage.input_tokens": "11", "gen_ai.usage.output_tokens": "3",
				}},
				{Timestamp: now.Add(time.Second), ServiceName: "copilot-chat", TraceID: "trace", SpanID: "wrapper", ParentSpanID: "agent"},
				{Timestamp: now.Add(2 * time.Second), ServiceName: "copilot-chat", TraceID: "trace", SpanID: "chat", ParentSpanID: "wrapper", SpanAttributes: chatAttrs},
			}); err != nil {
				t.Fatal(err)
			}
			got, err := s.GetSessionTranscript(context.Background(), "session")
			if err != nil {
				t.Fatal(err)
			}
			var finalReplies, inputTokens, outputTokens int
			for _, msg := range got.Messages {
				if msg.Content == "done" {
					finalReplies++
				}
				inputTokens += msg.InputTokens
				outputTokens += msg.OutputTokens
			}
			if finalReplies != 1 || inputTokens != 11 || outputTokens != 3 {
				t.Fatalf("duplicated or missing reply/usage: %+v", got.Messages)
			}
		})
	}
}

func TestSessionTranscript_IdenticalRepliesInDifferentTurnsSurvive(t *testing.T) {
	s := transcriptRegressionStore(t)
	now := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)
	var spans []api.Span
	for _, id := range []string{"first", "second"} {
		spans = append(spans, api.Span{Timestamp: now, ServiceName: "copilot-chat", TraceID: "trace", SpanID: id, SpanAttributes: map[string]string{
			"gen_ai.operation.name": "chat", "gen_ai.conversation.id": "session", "gen_ai.output.messages": `[{"role":"assistant","content":"done"}]`,
		}})
	}
	if err := s.InsertSpans(context.Background(), spans); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetSessionTranscript(context.Background(), "session")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Messages) != 2 {
		t.Fatalf("distinct replies were removed: %+v", got.Messages)
	}
}

func TestSessionTranscript_LogSpanCorrelationPreservesDistinctEventsAndUsage(t *testing.T) {
	s := transcriptRegressionStore(t)
	now := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)
	var logs []api.LogRecord
	var spans []api.Span
	for _, id := range []string{"first", "second"} {
		logs = append(logs, api.LogRecord{Timestamp: now, ServiceName: "copilot-chat", TraceID: "trace", SpanID: id, LogAttributes: map[string]string{
			"event.name": "gen_ai.client.inference.operation.details", "session.id": "session", "gen_ai.output.messages": `[{"role":"assistant","content":"done"}]`,
		}})
		spans = append(spans, api.Span{Timestamp: now, ServiceName: "copilot-chat", TraceID: "trace", SpanID: id, SpanAttributes: map[string]string{
			"gen_ai.operation.name": "chat", "gen_ai.conversation.id": "session", "gen_ai.output.messages": `[{"role":"assistant","content":"done"}]`, "gen_ai.usage.input_tokens": "11",
		}})
	}
	if err := s.InsertLogs(context.Background(), logs); err != nil {
		t.Fatal(err)
	}
	if err := s.InsertSpans(context.Background(), spans); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetSessionTranscript(context.Background(), "session")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Messages) != 2 || got.Messages[0].InputTokens != 11 || got.Messages[1].InputTokens != 11 {
		t.Fatalf("expected two distinct replies enriched with span usage: %+v", got.Messages)
	}
}

func TestSessionTranscript_OverlappingToolCompletionOrder(t *testing.T) {
	s := transcriptRegressionStore(t)
	now := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)
	var spans []api.Span
	for i, name := range []string{"slow", "fast"} {
		duration := 5 * time.Second
		if name == "fast" {
			duration = time.Second
		}
		spans = append(spans, api.Span{Timestamp: now.Add(time.Duration(i) * time.Second), ServiceName: "copilot-chat", TraceID: "trace", SpanID: name, Duration: int64(duration), SpanAttributes: map[string]string{
			"gen_ai.operation.name": "execute_tool", "gen_ai.conversation.id": "session", "gen_ai.tool.name": name, "gen_ai.tool.call.result": name + " result",
		}})
	}
	if err := s.InsertSpans(context.Background(), spans); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetSessionTranscript(context.Background(), "session")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Messages) != 4 || got.Messages[1].Role != "tool_use" || got.Messages[2].ToolName != "fast" || got.Messages[3].ToolName != "slow" {
		t.Fatalf("unexpected overlapping tool order: %+v", got.Messages)
	}
}

func TestSessionTranscript_AgentFallbackRecognizesChildLogOutput(t *testing.T) {
	s := transcriptRegressionStore(t)
	now := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)
	if err := s.InsertSpans(context.Background(), []api.Span{
		{Timestamp: now, ServiceName: "copilot-chat", TraceID: "trace", SpanID: "agent", SpanAttributes: map[string]string{
			"gen_ai.operation.name": "invoke_agent", "gen_ai.conversation.id": "session",
			"gen_ai.output.messages": `[{"role":"assistant","content":"done"}]`, "gen_ai.usage.input_tokens": "11",
		}},
		{Timestamp: now.Add(time.Second), ServiceName: "copilot-chat", TraceID: "trace", SpanID: "chat", ParentSpanID: "agent", SpanAttributes: map[string]string{"gen_ai.operation.name": "chat"}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.InsertLogs(context.Background(), []api.LogRecord{{
		Timestamp: now.Add(2 * time.Second), ServiceName: "copilot-chat", TraceID: "trace", SpanID: "chat", LogAttributes: map[string]string{
			"event.name": "gen_ai.client.inference.operation.details", "session.id": "session",
			"gen_ai.output.messages": `[{"role":"assistant","content":"done"}]`, "gen_ai.usage.input_tokens": "11",
		},
	}}); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetSessionTranscript(context.Background(), "session")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Messages) != 1 || got.Messages[0].Content != "done" || got.Messages[0].InputTokens != 11 {
		t.Fatalf("parent reply duplicates correlated child log: %+v", got.Messages)
	}
}
