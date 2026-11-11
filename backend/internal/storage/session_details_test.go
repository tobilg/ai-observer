package storage

import (
	"context"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/tobilg/ai-observer/internal/api"
	"github.com/tobilg/ai-observer/internal/redact"
)

// fakeGitHubToken is credential-shaped test data built at runtime
var fakeGitHubToken = "gh" + "p_" + strings.Repeat("Ab3x", 9)

// Session rows carry the first user prompt as title, the repository and
// branch of imported records, and the usage of the messages
func TestSessionDetails(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	at := func(minutes int) time.Time { return now.Add(time.Duration(minutes) * time.Minute) }
	logs := []api.LogRecord{
		// Claude Code over OTLP: a prompt Claude Code did not record, then
		// a recorded one; api_request carries the usage
		{Timestamp: at(-50), ServiceName: "claude-code", LogAttributes: map[string]string{"session.id": "live", "event.name": "user_prompt", "prompt": "<REDACTED>"}},
		{Timestamp: at(-40), ServiceName: "claude-code", LogAttributes: map[string]string{"session.id": "live", "event.name": "user_prompt", "prompt": "  fix\n\tthe   build  "}},
		{Timestamp: at(-30), ServiceName: "claude-code", LogAttributes: map[string]string{"session.id": "live", "event.name": "user_prompt", "prompt": "a later prompt"}},
		{Timestamp: at(-29), ServiceName: "claude-code", LogAttributes: map[string]string{"session.id": "live", "event.name": "api_request",
			"cost_usd": "0.25", "input_tokens": "100", "output_tokens": "40", "cache_read_tokens": "1000"}},
		{Timestamp: at(-28), ServiceName: "claude-code", LogAttributes: map[string]string{"session.id": "live", "event.name": "api_request",
			"cost_usd": "0.5", "input_tokens": "200.9", "output_tokens": "60"}},
		// Not a message: its usage does not count
		{Timestamp: at(-27), ServiceName: "claude-code", LogAttributes: map[string]string{"session.id": "live", "event.name": "compaction", "input_tokens": "999"}},

		// An imported transcript: the user message's body is the title;
		// the latest repository and branch win
		{Timestamp: at(-20), ServiceName: "claude-code", Body: "Tool call: Read", LogAttributes: map[string]string{"session.id": "imported", "event.name": "transcript.message",
			"message.role": "tool_use", "repository": "acme/old", "git_branch": "main"}},
		{Timestamp: at(-19), ServiceName: "claude-code", Body: "add a login page", LogAttributes: map[string]string{"session.id": "imported", "event.name": "transcript.message",
			"message.role": "user", "repository": "acme/web", "git_branch": "feature/login"}},
		{Timestamp: at(-18), ServiceName: "claude-code", Body: "Sure", LogAttributes: map[string]string{"session.id": "imported", "event.name": "transcript.message",
			"message.role": "assistant"}},

		// Gemini CLI over OTLP: a prompt with a secret; token counts of
		// api_response, which has no cost
		{Timestamp: at(-10), ServiceName: "gemini_cli", LogAttributes: map[string]string{"session.id": "gemini", "event.name": "gemini_cli.user_prompt",
			"prompt": "use token " + fakeGitHubToken + " " + strings.Repeat("and more words ", 30)}},
		{Timestamp: at(-9), ServiceName: "gemini_cli", LogAttributes: map[string]string{"session.id": "gemini", "event.name": "gemini_cli.api_response",
			"input_token_count": "300", "output_token_count": "20", "cached_content_token_count": "100"}},

		// Codex over OTLP: the prompt is in the body when the attribute is empty
		{Timestamp: at(-5), ServiceName: "codex-app-server", Body: "hello codex", LogAttributes: map[string]string{"conversation.id": "codex", "event.name": "codex.user_prompt", "prompt": ""}},

		// An imported Codex session: the context Codex sends as user
		// messages before the prompt gives no title
		{Timestamp: at(-8), ServiceName: "codex_cli_rs", Body: "# AGENTS.md instructions for /home/u/p\n\n<INSTRUCTIONS>\nrules\n</INSTRUCTIONS>",
			LogAttributes: map[string]string{"session.id": "codex-file", "event.name": "transcript.message", "message.role": "user"}},
		{Timestamp: at(-7), ServiceName: "codex_cli_rs", Body: "<environment_context>\n  <cwd>/home/u/p</cwd>\n</environment_context>",
			LogAttributes: map[string]string{"session.id": "codex-file", "event.name": "transcript.message", "message.role": "user"}},
		{Timestamp: at(-6), ServiceName: "codex_cli_rs", Body: "rename the config flag",
			LogAttributes: map[string]string{"session.id": "codex-file", "event.name": "transcript.message", "message.role": "user"}},
	}
	if err := store.InsertLogs(ctx, logs); err != nil {
		t.Fatal(err)
	}
	// Stored before redaction was enabled: lists redact titles again
	store.SetRedactor(redact.New(redact.Secrets))

	resp, err := store.QuerySessions(ctx, "", now.Add(-time.Hour), now, Page{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	sessions := map[string]api.Session{}
	for _, s := range resp.Sessions {
		sessions[s.SessionID] = s
	}

	live := sessions["live"]
	if live.Title != "fix the build" {
		t.Errorf("live title = %q, want the first recorded prompt on one line", live.Title)
	}
	if live.CostUSD == nil || *live.CostUSD != 0.75 || live.InputTokens == nil || *live.InputTokens != 300 ||
		live.OutputTokens == nil || *live.OutputTokens != 100 {
		t.Errorf("live usage = %v %v %v, want 0.75, 300 (200.9 truncated), 100", deref(live.CostUSD), deref(live.InputTokens), deref(live.OutputTokens))
	}
	if live.Repository != "" || live.Branch != "" {
		t.Errorf("live attribution = %q %q, want none", live.Repository, live.Branch)
	}

	imported := sessions["imported"]
	if imported.Title != "add a login page" || imported.Repository != "acme/web" || imported.Branch != "feature/login" {
		t.Errorf("imported = %q %q %q", imported.Title, imported.Repository, imported.Branch)
	}
	if imported.CostUSD != nil || imported.InputTokens != nil || imported.OutputTokens != nil {
		t.Errorf("imported usage = %v %v %v, want none", deref(imported.CostUSD), deref(imported.InputTokens), deref(imported.OutputTokens))
	}

	gemini := sessions["gemini"]
	if strings.Contains(gemini.Title, fakeGitHubToken) || !strings.HasPrefix(gemini.Title, "use token [REDACTED:github_token] and more words") ||
		!strings.HasSuffix(gemini.Title, "…") || utf8.RuneCountInString(gemini.Title) > TitleLength {
		t.Errorf("gemini title = %q, want it redacted and cut to %d characters", gemini.Title, TitleLength)
	}
	if gemini.CostUSD != nil || gemini.InputTokens == nil || *gemini.InputTokens != 300 || gemini.OutputTokens == nil || *gemini.OutputTokens != 20 {
		t.Errorf("gemini usage = %v %v %v, want no cost, 300 and 20", deref(gemini.CostUSD), deref(gemini.InputTokens), deref(gemini.OutputTokens))
	}

	if codexFile := sessions["codex-file"]; codexFile.Title != "rename the config flag" {
		t.Errorf("imported codex title = %q, want the first prompt after the injected context", codexFile.Title)
	}
	if codex := sessions["codex"]; codex.Title != "hello codex" {
		t.Errorf("codex title = %q, want the body", codex.Title)
	}

	// Search results carry the same details
	found, err := store.SearchSessionsPage(ctx, []string{"login"}, "", now.Add(-time.Hour), now, Page{Limit: 10})
	if err != nil || len(found.Sessions) != 1 || found.Sessions[0].Repository != "acme/web" || found.Sessions[0].Title != "add a login page" {
		t.Errorf("search = %+v, %v", found, err)
	}
}

func TestSessionTitle(t *testing.T) {
	upper := func(s string) string { return strings.ToUpper(s) }
	long := strings.Repeat("ab ", 100)
	for _, c := range []struct {
		prompt string
		redact func(string) string
		want   string
	}{
		{"", nil, ""},
		{" \n\t ", nil, ""},
		{"<REDACTED>", nil, ""},
		{" <REDACTED>\n", nil, ""},
		// Context a tool sends as a user message
		{"<environment_context>\n  <cwd>/home/u/p</cwd>\n</environment_context>", nil, ""},
		{"# AGENTS.md instructions for /home/u/p\n\n<INSTRUCTIONS>\nbe nice\n</INSTRUCTIONS>", nil, ""},
		{"  <user_instructions>x</user_instructions>", nil, ""},
		{"<image name=[Image #1] path=/tmp/a.png>", nil, ""},
		{"</image>", nil, ""},
		{"# Files mentioned by the user:\n\n## a.go", nil, ""},
		{"<command-name>/model</command-name>", nil, ""},
		{"# Fix the AGENTS.md file", nil, "# Fix the AGENTS.md file"},
		{"a < b in the parser", nil, "a < b in the parser"},
		{"fix  the\n\nbuild", nil, "fix the build"},
		{"redact me", upper, "REDACT ME"},
		{long, nil, strings.TrimSpace(long[:TitleLength-1]) + "…"},
		{strings.Repeat("ä", TitleLength), nil, strings.Repeat("ä", TitleLength)},
		{strings.Repeat("ä", TitleLength+1), nil, strings.Repeat("ä", TitleLength-1) + "…"},
	} {
		if got := SessionTitle(c.prompt, c.redact); got != c.want {
			t.Errorf("SessionTitle(%q) = %q, want %q", c.prompt, got, c.want)
		}
	}

	// A cut prompt never shows its end, where a secret may have been cut
	// so that the redactor no longer recognizes it: here the cut text
	// collapses to "a " and 99 x, all of them in the margin
	spaced := "a" + strings.Repeat(" ", titleSourceLength-100) + strings.Repeat("x", 200)
	if got := SessionTitle(spaced, nil); got != "" {
		t.Errorf("title of a cut prompt that is all margin = %q, want none", got)
	}
	// "b " and 1,100 y: the 102 characters before the margin are shown
	shown := "b" + strings.Repeat(" ", titleSourceLength-titleSourceMargin-101) + strings.Repeat("y", titleSourceMargin+200)
	if got := SessionTitle(shown, nil); got != "b "+strings.Repeat("y", 99)+"…" {
		t.Errorf("title of a cut prompt = %q, want the characters before the margin", got)
	}
}

func deref[T any](p *T) any {
	if p == nil {
		return nil
	}
	return *p
}
