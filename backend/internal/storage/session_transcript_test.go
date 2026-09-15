package storage

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/tobilg/ai-observer/internal/api"
)

func TestExtractGenAIMessageText(t *testing.T) {
	tests := []struct {
		name     string
		raw      string
		role     string
		expected string
	}{
		{
			name:     "copilot parts content",
			raw:      `[{"role":"user","parts":[{"type":"text","content":"Refactor this function"}]}]`,
			role:     "user",
			expected: "Refactor this function",
		},
		{
			name:     "parts text field",
			raw:      `[{"role":"assistant","parts":[{"type":"text","text":"Done."}]}]`,
			role:     "assistant",
			expected: "Done.",
		},
		{
			name:     "plain content string",
			raw:      `[{"role":"user","content":"Hello"}]`,
			role:     "user",
			expected: "Hello",
		},
		{
			name:     "filters other roles",
			raw:      `[{"role":"system","content":"ignore"},{"role":"user","content":"keep"}]`,
			role:     "user",
			expected: "keep",
		},
		{
			name:     "empty",
			raw:      "",
			role:     "user",
			expected: "",
		},
		{
			name:     "invalid json",
			raw:      "not-json",
			role:     "user",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractGenAIMessageText(tt.raw, tt.role)
			if got != tt.expected {
				t.Fatalf("expected %q, got %q", tt.expected, got)
			}
		})
	}
}

func TestTranscriptMessagesFromSpan(t *testing.T) {
	now := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)

	t.Run("invoke_agent user message", func(t *testing.T) {
		msgs := transcriptMessagesFromSpan(api.Span{
			Timestamp:   now,
			ServiceName: "copilot-chat",
			SpanAttributes: map[string]string{
				"gen_ai.operation.name": "invoke_agent",
				"gen_ai.input.messages": `[{"role":"user","parts":[{"type":"text","content":"What is 2+2?"}]}]`,
				"gen_ai.request.model":  "gpt-4o",
			},
		})
		if len(msgs) != 1 {
			t.Fatalf("expected 1 message, got %d", len(msgs))
		}
		if msgs[0].Role != "user" || msgs[0].Content != "What is 2+2?" {
			t.Fatalf("unexpected message: %+v", msgs[0])
		}
	})

	t.Run("chat assistant with tokens", func(t *testing.T) {
		msgs := transcriptMessagesFromSpan(api.Span{
			Timestamp:   now,
			Duration:    int64(1500 * time.Millisecond),
			ServiceName: "copilot-chat",
			SpanAttributes: map[string]string{
				"gen_ai.operation.name":      "chat",
				"gen_ai.output.messages":     `[{"role":"assistant","parts":[{"type":"text","content":"4"}]}]`,
				"gen_ai.response.model":      "gpt-4o-2024-08-06",
				"gen_ai.usage.input_tokens":  "12",
				"gen_ai.usage.output_tokens": "3",
			},
		})
		if len(msgs) != 1 {
			t.Fatalf("expected 1 message, got %d", len(msgs))
		}
		if msgs[0].Role != "assistant" || msgs[0].Content != "4" || msgs[0].InputTokens != 12 || msgs[0].DurationMs != 1500 {
			t.Fatalf("unexpected message: %+v", msgs[0])
		}
	})

	t.Run("execute_tool use and result", func(t *testing.T) {
		ok := true
		msgs := transcriptMessagesFromSpan(api.Span{
			Timestamp:   now,
			ServiceName: "copilot-chat",
			SpanName:    "execute_tool readFile",
			StatusCode:  "OK",
			SpanAttributes: map[string]string{
				"gen_ai.operation.name":      "execute_tool",
				"gen_ai.tool.name":           "readFile",
				"gen_ai.tool.call.arguments": `{"filePath":"README.md"}`,
				"gen_ai.tool.call.result":    "hello",
			},
		})
		if len(msgs) != 2 {
			t.Fatalf("expected 2 messages, got %d", len(msgs))
		}
		if msgs[0].Role != "tool_use" || msgs[0].ToolName != "readFile" || msgs[0].ToolInput != `{"filePath":"README.md"}` {
			t.Fatalf("unexpected tool_use: %+v", msgs[0])
		}
		if msgs[1].Role != "tool_result" || msgs[1].ToolOutput != "hello" {
			t.Fatalf("unexpected tool_result: %+v", msgs[1])
		}
		if msgs[0].Success == nil || *msgs[0].Success != ok {
			t.Fatalf("expected success=true, got %+v", msgs[0].Success)
		}
	})
}

func TestGetSessionTranscript_CopilotSessionStartWithoutMappedEvents(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)
	sessionID := "a1b2c3d4-e5f6-7890-abcd-ef1234567890"

	if err := store.InsertLogs(ctx, []api.LogRecord{{
		Timestamp:   now,
		ServiceName: "copilot-chat",
		Body:        "copilot_chat.session.start",
		LogAttributes: map[string]string{
			"event.name":           "copilot_chat.session.start",
			"session.id":           sessionID,
			"gen_ai.request.model": "gpt-4o",
			"gen_ai.agent.name":    "copilot",
		},
	}}); err != nil {
		t.Fatalf("InsertLogs failed: %v", err)
	}

	listed, err := store.QuerySessions(ctx, "", now.Add(-time.Hour), now.Add(time.Hour), 10, 0)
	if err != nil {
		t.Fatalf("QuerySessions failed: %v", err)
	}
	if listed.Total != 1 || len(listed.Sessions) != 1 || listed.Sessions[0].SessionID != sessionID {
		t.Fatalf("expected listed copilot session %q, got %+v", sessionID, listed)
	}

	resp, err := store.GetSessionTranscript(ctx, sessionID)
	if err != nil {
		t.Fatalf("GetSessionTranscript should not 404 for a listed copilot session: %v", err)
	}
	if resp.ServiceName != "copilot-chat" {
		t.Fatalf("expected service copilot-chat, got %q", resp.ServiceName)
	}
	if len(resp.Messages) != 0 {
		t.Fatalf("expected no mapped log messages, got %+v", resp.Messages)
	}
}

func TestGetSessionTranscript_CopilotReconstructsFromSpans(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)
	sessionID := "conv-copilot-42"

	if err := store.InsertLogs(ctx, []api.LogRecord{{
		Timestamp:   now,
		ServiceName: "copilot-chat",
		Body:        "copilot_chat.session.start",
		LogAttributes: map[string]string{
			"event.name": "copilot_chat.session.start",
			"session.id": sessionID,
		},
	}}); err != nil {
		t.Fatalf("InsertLogs failed: %v", err)
	}

	if err := store.InsertSpans(ctx, []api.Span{
		{
			Timestamp:   now.Add(time.Second),
			TraceID:     "trace-1",
			SpanID:      "span-agent",
			ServiceName: "copilot-chat",
			SpanName:    "invoke_agent copilot",
			Duration:    int64(5 * time.Second),
			StatusCode:  "OK",
			SpanAttributes: map[string]string{
				"gen_ai.operation.name":   "invoke_agent",
				"gen_ai.conversation.id":  sessionID,
				"copilot_chat.session_id": sessionID,
				"gen_ai.input.messages":   `[{"role":"user","parts":[{"type":"text","content":"Fix the flaky test"}]}]`,
				"gen_ai.request.model":    "gpt-4o",
			},
		},
		{
			Timestamp:    now.Add(2 * time.Second),
			TraceID:      "trace-1",
			SpanID:       "span-tool",
			ParentSpanID: "span-agent",
			ServiceName:  "copilot-chat",
			SpanName:     "execute_tool readFile",
			Duration:     int64(50 * time.Millisecond),
			StatusCode:   "OK",
			SpanAttributes: map[string]string{
				"gen_ai.operation.name":      "execute_tool",
				"gen_ai.conversation.id":     sessionID,
				"gen_ai.tool.name":           "readFile",
				"gen_ai.tool.call.arguments": `{"filePath":"test.go"}`,
				"gen_ai.tool.call.result":    "package test",
			},
		},
		{
			Timestamp:    now.Add(3 * time.Second),
			TraceID:      "trace-1",
			SpanID:       "span-chat",
			ParentSpanID: "span-agent",
			ServiceName:  "copilot-chat",
			SpanName:     "chat gpt-4o",
			Duration:     int64(1200 * time.Millisecond),
			StatusCode:   "OK",
			SpanAttributes: map[string]string{
				"gen_ai.operation.name":      "chat",
				"gen_ai.conversation.id":     sessionID,
				"gen_ai.response.model":      "gpt-4o-2024-08-06",
				"gen_ai.output.messages":     `[{"role":"assistant","parts":[{"type":"text","content":"The test is racy because of shared state."}]}]`,
				"gen_ai.usage.input_tokens":  "80",
				"gen_ai.usage.output_tokens": "20",
			},
		},
	}); err != nil {
		t.Fatalf("InsertSpans failed: %v", err)
	}

	resp, err := store.GetSessionTranscript(ctx, sessionID)
	if err != nil {
		t.Fatalf("GetSessionTranscript failed: %v", err)
	}
	if resp.ServiceName != "copilot-chat" {
		t.Fatalf("expected service copilot-chat, got %q", resp.ServiceName)
	}
	if len(resp.Messages) != 4 {
		t.Fatalf("expected user + tool_use + tool_result + assistant, got %d: %+v", len(resp.Messages), rolesOf(resp.Messages))
	}
	if resp.Messages[0].Role != "user" || !strings.Contains(resp.Messages[0].Content, "Fix the flaky test") {
		t.Fatalf("expected user prompt, got %+v", resp.Messages[0])
	}
	if resp.Messages[1].Role != "tool_use" || resp.Messages[1].ToolName != "readFile" {
		t.Fatalf("expected tool_use, got %+v", resp.Messages[1])
	}
	if resp.Messages[2].Role != "tool_result" || resp.Messages[2].ToolOutput != "package test" {
		t.Fatalf("expected tool_result, got %+v", resp.Messages[2])
	}
	if resp.Messages[3].Role != "assistant" || !strings.Contains(resp.Messages[3].Content, "racy") {
		t.Fatalf("expected assistant reply, got %+v", resp.Messages[3])
	}
	if resp.Messages[3].InputTokens != 80 || resp.Messages[3].OutputTokens != 20 {
		t.Fatalf("expected token counts on assistant message, got %+v", resp.Messages[3])
	}
}

func TestGetSessionTranscript_ClaudeImportedStillWorks(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Date(2026, 1, 2, 15, 4, 5, 0, time.UTC)

	if err := store.InsertLogs(ctx, []api.LogRecord{
		{
			Timestamp:   now,
			ServiceName: "claude-code",
			Body:        "Hello Claude",
			LogAttributes: map[string]string{
				"event.name":    "transcript.message",
				"session.id":    "claude-sess-1",
				"message.role":  "user",
				"message.index": "0",
			},
		},
		{
			Timestamp:   now.Add(time.Second),
			ServiceName: "claude-code",
			Body:        "Hi there",
			LogAttributes: map[string]string{
				"event.name":    "transcript.message",
				"session.id":    "claude-sess-1",
				"message.role":  "assistant",
				"message.index": "1",
				"model":         "claude-sonnet-4",
			},
		},
	}); err != nil {
		t.Fatalf("InsertLogs failed: %v", err)
	}

	resp, err := store.GetSessionTranscript(ctx, "claude-sess-1")
	if err != nil {
		t.Fatalf("GetSessionTranscript failed: %v", err)
	}
	if len(resp.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(resp.Messages))
	}
	if resp.Messages[0].Role != "user" || resp.Messages[0].Content != "Hello Claude" {
		t.Fatalf("unexpected user message: %+v", resp.Messages[0])
	}
	if resp.Messages[1].Role != "assistant" || resp.Messages[1].Content != "Hi there" {
		t.Fatalf("unexpected assistant message: %+v", resp.Messages[1])
	}
}

func TestGetSessionTranscript_NotFound(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	_, err := store.GetSessionTranscript(context.Background(), "missing")
	if err == nil {
		t.Fatal("expected error for missing session")
	}
	if !strings.Contains(err.Error(), "session not found") {
		t.Fatalf("expected session not found error, got %v", err)
	}
}

func rolesOf(messages []api.TranscriptMessage) []string {
	roles := make([]string, len(messages))
	for i, msg := range messages {
		roles[i] = msg.Role
	}
	return roles
}
