package mcpserver

import (
	"strconv"
	"strings"
	"testing"
)

func TestSessionsCarryTitleAndUsage(t *testing.T) {
	cs, _, _ := testSession(t)
	var sessions listSessionsOut
	if errText := call(t, cs, "list_sessions", map[string]any{"tool": "claude-code"}, &sessions); errText != "" {
		t.Fatal(errText)
	}
	if len(sessions.Sessions) != 1 {
		t.Fatalf("sessions = %+v", sessions)
	}
	s := sessions.Sessions[0]
	if s.Title != "Fix the flaky test in parser_test.go" {
		t.Errorf("title = %q", s.Title)
	}
	if s.InputTokens == nil || *s.InputTokens != 1200 || s.OutputTokens == nil || *s.OutputTokens != 300 {
		t.Errorf("tokens = %v, %v", s.InputTokens, s.OutputTokens)
	}

	var found searchConversationsOut
	if errText := call(t, cs, "search_conversations", map[string]any{"query": "flaky"}, &found); errText != "" {
		t.Fatal(errText)
	}
	if len(found.Sessions) != 1 || found.Sessions[0].Title != s.Title || found.Sessions[0].InputTokens == nil {
		t.Errorf("search result = %+v", found.Sessions)
	}
}

func TestTranscriptTitleTotalsAndSearch(t *testing.T) {
	cs, _, _ := testSession(t)
	var tr getTranscriptOut
	if errText := call(t, cs, "get_transcript", map[string]any{"session_id": "s-claude"}, &tr); errText != "" {
		t.Fatal(errText)
	}
	if tr.Title != "Fix the flaky test in parser_test.go" {
		t.Errorf("title = %q", tr.Title)
	}
	if tr.Totals.UserMessages != 3 || tr.Totals.InputTokens != 1200 || tr.Totals.OutputTokens != 300 {
		t.Errorf("totals = %+v", tr.Totals)
	}
	all := tr.Total
	for _, m := range tr.Messages {
		if m.Position != nil {
			t.Fatal("positions are only sent for selected messages")
		}
	}

	// Search selects the matching messages, with their positions
	if errText := call(t, cs, "get_transcript", map[string]any{"session_id": "s-claude", "search": "push WITH"}, &tr); errText != "" {
		t.Fatal(errText)
	}
	if tr.Total != 1 || len(tr.Messages) != 1 || tr.Messages[0].Position == nil || strings.Join(tr.Terms, "|") != "push|WITH" {
		t.Fatalf("search = %+v", tr)
	}
	if tr.Totals.UserMessages != 3 {
		t.Errorf("totals of a search = %+v, want those of the whole session", tr.Totals)
	}
	if strings.Contains(tr.Messages[0].Content, fakeToken) {
		t.Error("search result not redacted")
	}
	// The position reads the messages from that one on
	pos := *tr.Messages[0].Position
	var around getTranscriptOut
	call(t, cs, "get_transcript", map[string]any{"session_id": "s-claude", "cursor": strconv.Itoa(pos)}, &around)
	if around.Total != all || len(around.Messages) == 0 || around.Messages[0].Index != tr.Messages[0].Index {
		t.Errorf("from position %d: %+v", pos, around.Messages)
	}

	// Search and roles combine
	call(t, cs, "get_transcript", map[string]any{"session_id": "s-claude", "search": "flaky", "roles": []string{"assistant"}}, &tr)
	if tr.Total != 0 || len(tr.Messages) != 0 {
		t.Errorf("assistant messages with flaky = %+v", tr.Messages)
	}
	if errText := call(t, cs, "get_transcript", map[string]any{"session_id": "s-claude", "search": `""`}, &tr); !strings.Contains(errText, "invalid search") {
		t.Errorf("empty search error = %q", errText)
	}
}
