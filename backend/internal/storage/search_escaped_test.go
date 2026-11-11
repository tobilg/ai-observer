package storage

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/tobilg/ai-observer/internal/api"
)

// escapedTerms are searches with characters JSON escapes: quotes,
// backslashes, control characters, and <, > and & (Go's encoder)
var escapedTerms = []string{"&&", `"quoted"`, "a<b", "b>c", `path\dir`, "line1\nline2"}

const escapedText = "x && y \"quoted\" a<b b>c path\\dir line1\nline2 plain words"

// Log and trace searches find terms with characters JSON escapes in
// attribute values as in bodies and plain columns, also a Codex tool result
// whose output is only in tool.output; plain terms still match the JSON
// text, keys included, as before
func TestSearchFindsEscapedCharactersInAttributes(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)

	logs := []api.LogRecord{
		{Timestamp: now, ServiceName: "svc-body", Body: escapedText},
		{Timestamp: now, ServiceName: "codex_cli_rs", Body: "exec_command: exit 0, 60 bytes output",
			LogAttributes: map[string]string{"event.name": "transcript.message", "message.role": "tool_result",
				"tool.name": "exec_command", "tool.call_id": "c1", "tool.output": escapedText, "tool_result_size_bytes": "60"}},
		{Timestamp: now, ServiceName: "svc-none", Body: "nothing here", LogAttributes: map[string]string{"other": "value"}},
	}
	if err := store.InsertLogs(ctx, logs); err != nil {
		t.Fatal(err)
	}
	spans := []api.Span{
		{Timestamp: now, TraceID: strings.Repeat("a", 32), SpanID: strings.Repeat("1", 16), SpanName: "tool", ServiceName: "svc-span",
			SpanAttributes: map[string]string{"output": escapedText}},
		{Timestamp: now, TraceID: strings.Repeat("b", 32), SpanID: strings.Repeat("2", 16), SpanName: "other", ServiceName: "svc-span",
			SpanAttributes: map[string]string{"output": "nothing"}},
	}
	if err := store.InsertTelemetry(ctx, nil, nil, spans); err != nil {
		t.Fatal(err)
	}

	from, to := now.Add(-time.Hour), now.Add(time.Hour)
	searchLogs := func(term string) []string {
		t.Helper()
		resp, err := store.QueryLogs(ctx, "", "", "", term, from, to, Page{Limit: 10})
		if err != nil {
			t.Fatal(err)
		}
		var services []string
		for _, l := range resp.Logs {
			services = append(services, l.ServiceName)
		}
		if resp.Total != len(services) {
			t.Errorf("%q: total %d, page %d", term, resp.Total, len(services))
		}
		return services
	}
	searchTraces := func(term string) int {
		t.Helper()
		resp, err := store.QueryTraces(ctx, TraceFilter{Search: term}, from, to, Page{Limit: 10})
		if err != nil {
			t.Fatal(err)
		}
		return len(resp.Traces)
	}

	for _, term := range append(escapedTerms, "plain words", "QUOTED") {
		got := searchLogs(term)
		if len(got) != 2 || !strings.Contains(strings.Join(got, ","), "svc-body") || !strings.Contains(strings.Join(got, ","), "codex_cli_rs") {
			t.Errorf("log search %q found %v, want the body and the tool output", term, got)
		}
		if n := searchTraces(term); n != 1 {
			t.Errorf("trace search %q found %d traces, want 1", term, n)
		}
	}
	// Plain terms match the JSON text, keys included, as before
	if got := searchLogs("tool.call_id"); len(got) != 1 {
		t.Errorf("key search found %v", got)
	}
	if got := searchLogs("&&&"); len(got) != 0 {
		t.Errorf("absent term found %v", got)
	}
}
