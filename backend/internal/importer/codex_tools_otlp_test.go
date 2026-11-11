package importer

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tobilg/ai-observer/internal/api"
	"github.com/tobilg/ai-observer/internal/storage"
	"github.com/tobilg/ai-observer/internal/transcript"
)

// codexOTLPToolEvents are the events Codex's app server sends over OTLP for
// tool calls of the fixture's session: a decision and a result per command
// (exec_command), a result for others. Their call_id is the call's ID in the
// session file, also for the tools code mode ran.
func codexOTLPToolEvents() []api.LogRecord {
	t0 := time.Date(2026, 10, 26, 7, 33, 20, 0, time.UTC)
	event := func(offset time.Duration, name string, attrs map[string]string) api.LogRecord {
		attrs["event.name"] = name
		attrs["conversation.id"] = "sess-tools"
		return api.LogRecord{Timestamp: t0.Add(offset), ServiceName: "codex-app-server", Body: name, LogAttributes: attrs}
	}
	return []api.LogRecord{
		event(1200*time.Millisecond, "codex.tool_decision", map[string]string{"call_id": "exec-1", "tool_name": "exec_command", "decision": "approved"}),
		event(3990*time.Millisecond, "codex.tool_result", map[string]string{"call_id": "exec-1", "tool_name": "exec_command", "arguments": `{"cmd":"go test ./..."}`, "output": "ok", "duration_ms": "2490", "success": "true"}),
		event(4650*time.Millisecond, "codex.tool_result", map[string]string{"call_id": "exec-4", "tool_name": "view_image", "arguments": `{"path":"/work/demo/shot.png"}`, "duration_ms": "12", "success": "true"}),
		event(5690*time.Millisecond, "codex.tool_result", map[string]string{"call_id": "call_exec1", "tool_name": "exec", "output": "ok", "duration_ms": "4690", "success": "true"}),
		event(8005*time.Millisecond, "codex.tool_decision", map[string]string{"call_id": "call_cmd1", "tool_name": "exec_command", "decision": "approved"}),
		event(8305*time.Millisecond, "codex.tool_result", map[string]string{"call_id": "call_cmd1", "tool_name": "exec_command", "output": "ls: missing", "duration_ms": "260", "success": "true"}),
	}
}

// A Codex session imported from its file and received over OTLP shows each
// tool call once, matched by call ID, whichever arrived first; the session
// list counts its messages as the transcript shows them
func TestCodexToolCallsRecordedOverOTLPShownOnce(t *testing.T) {
	data, err := os.ReadFile(codexToolsFixture)
	if err != nil {
		t.Fatal(err)
	}
	fileOnly, _ := parseCodexFixture(t)
	wantMessages := len(transcriptLogs(fileOnly.Logs))

	for _, otlpFirst := range []bool{true, false} {
		name := "file first"
		if otlpFirst {
			name = "OTLP first"
		}
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			sessions := filepath.Join(dir, "2026", "10", "26")
			if err := os.MkdirAll(sessions, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(sessions, "rollout-2026-10-26T07-33-20-sess-tools.jsonl"), data, 0o644); err != nil {
				t.Fatal(err)
			}
			t.Setenv("AI_OBSERVER_CODEX_PATH", dir)
			store, err := storage.NewDuckDBStore(filepath.Join(t.TempDir(), "codex.duckdb"))
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			ctx := context.Background()

			sendOTLP := func() {
				if err := store.InsertTelemetry(ctx, codexOTLPToolEvents(), nil, nil); err != nil {
					t.Fatal(err)
				}
			}
			if otlpFirst {
				sendOTLP()
			}
			imp := NewImporter(store, false)
			imp.SetIO(&strings.Builder{}, strings.NewReader(""))
			imp.RegisterParser(NewCodexParser())
			if err := imp.Import(ctx, []SourceType{SourceCodex}, Options{SkipConfirm: true}); err != nil {
				t.Fatal(err)
			}
			if !otlpFirst {
				sendOTLP()
			}

			got, err := transcript.Load(ctx, store, "sess-tools")
			if err != nil {
				t.Fatal(err)
			}
			if len(got.Messages) != wantMessages {
				t.Errorf("transcript has %d messages, want the file's %d (OTLP events of its calls merged)", len(got.Messages), wantMessages)
			}
			uses := map[string]int{}
			for _, m := range got.Messages {
				if m.Role == "tool_use" || m.Role == "tool_result" {
					uses[m.Role+" "+m.ToolName]++
				}
				// The file's view_image result lacks a duration, which the
				// OTLP event adds
				if m.Role == "tool_result" && m.ToolName == "view_image" && m.DurationMs != 12 {
					t.Errorf("view_image result duration = %d, want OTLP's 12", m.DurationMs)
				}
				// The file says the failed command failed; OTLP's success
				// (the tool ran) does not override it
				if m.Role == "tool_result" && strings.Contains(m.ToolOutput, "Process exited with code 1") &&
					(m.Success == nil || *m.Success) {
					t.Errorf("failed command's result success = %v, want false", m.Success)
				}
			}
			// Outputs are stored once, in tool.output, which the transcript
			// shows (the body is a summary)
			for _, m := range got.Messages {
				if m.Role == "tool_result" && m.OutputSize > 0 && m.ToolOutput == "" {
					t.Errorf("tool_result of %s without its output (content %q)", m.ToolName, m.Content)
				}
				if m.Role == "tool_result" && m.ToolName == "exec" && !strings.HasPrefix(m.ToolOutput, "ok  \tdemo") {
					t.Errorf("exec cell's output %q, want the file's", m.ToolOutput)
				}
			}
			// Every log got its message key at insert, on both paths
			var unkeyed int
			if err := store.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM otel_logs WHERE MessageKey IS NULL").Scan(&unkeyed); err != nil || unkeyed != 0 {
				t.Errorf("%d logs without message key (%v)", unkeyed, err)
			}
			for _, key := range []string{"tool_use exec_command", "tool_result exec_command"} {
				if uses[key] != 2 {
					t.Errorf("%d %s messages, want 2 (exec-1 and call_cmd1)", uses[key], key)
				}
			}
			if uses["tool_use exec"] != 1 || uses["tool_result exec"] != 1 || uses["tool_result view_image"] != 1 {
				t.Errorf("tool messages: %v", uses)
			}

			list, err := store.QuerySessions(ctx, "", time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC), time.Date(2026, 10, 27, 0, 0, 0, 0, time.UTC), storage.Page{Limit: 10})
			if err != nil {
				t.Fatal(err)
			}
			// A row per service; the OTLP events merged into the file's
			// messages count for none
			listed, rows := 0, 0
			for _, s := range list.Sessions {
				if s.SessionID == "sess-tools" {
					listed += s.MessageCount
					rows++
				}
			}
			if rows == 0 || listed != len(got.Messages) {
				t.Errorf("session list counts %d messages in %d rows, transcript shows %d", listed, rows, len(got.Messages))
			}
		})
	}
}
