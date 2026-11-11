package transcript

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/tobilg/ai-observer/internal/api"
	"github.com/tobilg/ai-observer/internal/redact"
	"github.com/tobilg/ai-observer/internal/storage"
)

// A transcript carries the totals of all of its messages and the title the
// session list shows, the same as the list's
func TestLoad_TotalsAndTitle(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()
	ctx := context.Background()
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	token := "gh" + "p_" + strings.Repeat("Ab3x", 9) // credential-shaped, built at runtime

	log := func(offset int, event string, attrs map[string]string) api.LogRecord {
		attrs["session.id"] = "s1"
		attrs["event.name"] = event
		return api.LogRecord{Timestamp: now.Add(time.Duration(offset) * time.Second), ServiceName: "claude-code", LogAttributes: attrs}
	}
	if err := store.InsertLogs(ctx, []api.LogRecord{
		log(0, "user_prompt", map[string]string{"prompt": "<REDACTED>"}),
		log(1, "user_prompt", map[string]string{"prompt": "deploy\nwith " + token}),
		log(2, "api_request", map[string]string{"cost_usd": "0.5", "input_tokens": "100", "output_tokens": "20",
			"cache_read_tokens": "1000", "cache_creation_tokens": "50"}),
		log(3, "tool_decision", map[string]string{"tool_name": "Bash"}),
		log(4, "tool_result", map[string]string{"tool_name": "Bash", "success": "false"}),
		log(5, "api_request", map[string]string{"cost_usd": "0.25", "input_tokens": "10", "output_tokens": "5"}),
		log(6, "tool_decision", map[string]string{"tool_name": "Read"}),
		log(7, "tool_result", map[string]string{"tool_name": "Read", "success": "true"}),
	}); err != nil {
		t.Fatal(err)
	}
	store.SetRedactor(redact.New(redact.Secrets))

	resp, err := Load(ctx, store, "s1")
	if err != nil {
		t.Fatal(err)
	}
	want := api.TranscriptTotals{
		CostUSD: 0.75, InputTokens: 110, OutputTokens: 25, CacheRead: 1000, CacheWrite: 50,
		UserMessages: 2, AssistantMessages: 2, ToolCalls: 2, ToolErrors: 1,
	}
	if resp.Totals == nil || *resp.Totals != want {
		t.Errorf("totals = %+v, want %+v", resp.Totals, want)
	}
	if resp.Title != "deploy with [REDACTED:github_token]" {
		t.Errorf("title = %q, want the first recorded prompt, redacted, on one line", resp.Title)
	}

	listed, err := store.QuerySessions(ctx, "", now.Add(-time.Hour), now.Add(time.Hour), storage.Page{Limit: 10})
	if err != nil || len(listed.Sessions) != 1 {
		t.Fatalf("sessions = %+v, %v", listed, err)
	}
	s := listed.Sessions[0]
	if s.Title != resp.Title || s.CostUSD == nil || *s.CostUSD != want.CostUSD ||
		s.InputTokens == nil || *s.InputTokens != want.InputTokens || s.OutputTokens == nil || *s.OutputTokens != want.OutputTokens {
		t.Errorf("session row = %+v, want the transcript's title and usage", s)
	}

	// Pages carry the totals of the whole transcript
	page, err := Page(resp, 2, "")
	if err != nil || page.Totals == nil || *page.Totals != want || page.Title != resp.Title {
		t.Errorf("page = %+v, %v", page, err)
	}
}

func TestSearch(t *testing.T) {
	resp := &api.TranscriptResponse{Messages: []api.TranscriptMessage{
		{Role: "user", Content: "Why does the Parser fail?"},
		{Role: "assistant", Content: "The parser fails on empty input."},
		{Role: "tool_use", ToolName: "Bash", ToolInput: `{"command": "go test ./parser"}`},
		{Role: "tool_result", ToolName: "Bash", ToolOutput: "FAIL parser_test.go"},
		{Role: "assistant", Content: "Fixed."},
	}}
	for terms, want := range map[string][]int{
		"parser":      {0, 1, 2, 3},
		"PARSER fail": {0, 1, 3},
		"bash parser": {2, 3}, // the tool name and the input of one message
		`"go test"`:   {2},
		"go test":     {2, 3}, // parser_test.go has both words
		"missing":     {},
	} {
		parsed, err := storage.SearchTerms(terms)
		if err != nil {
			t.Fatal(err)
		}
		got := Search(resp, parsed)
		if got == nil || !reflect.DeepEqual(got, want) {
			t.Errorf("Search(%q) = %v, want %v", terms, got, want)
		}
	}
}
