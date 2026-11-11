package storage

import (
	"context"
	"testing"
	"time"

	"github.com/tobilg/ai-observer/internal/api"
)

// The session list counts the logs that become transcript messages, not
// every event of the session
func TestSessionMessageCountLeavesOutOtherEvents(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	log := func(session, event string, offset time.Duration) api.LogRecord {
		return api.LogRecord{Timestamp: now.Add(offset), ServiceName: "codex-app-server",
			LogAttributes: map[string]string{"conversation.id": session, "event.name": event}}
	}
	if err := store.InsertLogs(ctx, []api.LogRecord{
		log("chat", "codex.conversation_starts", -4*time.Minute),
		log("chat", "codex.user_prompt", -3*time.Minute),
		log("chat", "codex.websocket_event", -2*time.Minute),
		log("chat", "codex.tool_result", -time.Minute),
		// A session without conversation events has an empty transcript
		log("events-only", "codex.conversation_starts", -2*time.Minute),
		log("events-only", "codex.websocket_event", -time.Minute),
		// A conversation on spans (Copilot), listed by another alias than
		// the first of its spans
		{Timestamp: now.Add(-time.Minute), ServiceName: "copilot-chat",
			LogAttributes: map[string]string{"copilot_chat.session_id": "vscode-chat", "event.name": "copilot_chat.session.start"}},
	}); err != nil {
		t.Fatal(err)
	}
	chat := func(id string) api.Span {
		return api.Span{Timestamp: now.Add(-time.Minute), TraceID: "t-" + id, SpanID: id, ServiceName: "copilot-chat", SpanName: "chat",
			SpanAttributes: map[string]string{"gen_ai.conversation.id": "conv", "copilot_chat.session_id": "vscode-chat", "gen_ai.operation.name": "chat"}}
	}
	other := chat("c3")
	other.SpanAttributes = map[string]string{"copilot_chat.session_id": "vscode-chat"} // not a GenAI span
	if err := store.InsertSpans(ctx, []api.Span{chat("c1"), chat("c2"), other}); err != nil {
		t.Fatal(err)
	}

	resp, err := store.QuerySessions(ctx, "", now.Add(-time.Hour), now, Page{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for _, s := range resp.Sessions {
		counts[s.SessionID] = s.MessageCount
	}
	if counts["chat"] != 2 || counts["events-only"] != 0 || counts["vscode-chat"] != 2 {
		t.Errorf("message counts = %v, want 2 messages for chat, none for events-only and 2 spans for vscode-chat", counts)
	}
}
