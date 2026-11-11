package importer

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tobilg/ai-observer/internal/api"
	"github.com/tobilg/ai-observer/internal/otlp"
	"github.com/tobilg/ai-observer/internal/storage"
	"github.com/tobilg/ai-observer/internal/transcript"
)

// geminiOverlapSession is a Gemini CLI session file as the chat recording
// service writes it (gemini-cli packages/core/src/services/
// chatRecordingService.ts; shape and values from a recorded session)
const geminiOverlapSession = `{
  "sessionId": "8b65451f-31e9-4d19-82ff-fa85ff0b09c4",
  "projectHash": "2d31728c50b3f98484aa0f7d2751307228b51966dfbfeea574b5353ff683beb6",
  "startTime": "2025-12-26T11:06:10.579Z",
  "lastUpdated": "2025-12-26T11:07:18.426Z",
  "messages": [
    {
      "id": "61613736-2a84-4696-80fb-a0fba4bba704",
      "timestamp": "2025-12-26T11:07:05.964Z",
      "type": "user",
      "content": "make yourself familiar with the existing codebase"
    },
    {
      "id": "473cb884-0d51-47c9-9a7e-2049f0c02040",
      "timestamp": "2025-12-26T11:07:11.648Z",
      "type": "gemini",
      "content": "I will start by examining the README.md file.",
      "thoughts": [{"subject": "Beginning the Analysis", "description": "...", "timestamp": "2025-12-26T11:07:10.112Z"}],
      "tokens": {"input": 7777, "output": 90, "cached": 3114, "thoughts": 260, "tool": 0, "total": 8127},
      "model": "gemini-2.5-pro",
      "toolCalls": [
        {
          "id": "read_file-1766747231641-63d9ab01f7dc4",
          "name": "read_file",
          "args": {
            "file_path": "README.md"
          },
          "result": [{"functionResponse": {"id": "read_file-1766747231641-63d9ab01f7dc4", "name": "read_file", "response": {"output": "# ai-observer"}}}],
          "status": "success",
          "timestamp": "2025-12-26T11:07:11.681Z",
          "displayName": "ReadFile"
        }
      ]
    },
    {
      "id": "ae5594cd-bb95-4a93-8556-44b49c529620",
      "timestamp": "2025-12-26T11:07:14.003Z",
      "type": "gemini",
      "content": "The README.md is minimal. I'll proceed by examining the Makefile.",
      "tokens": {"input": 7874, "output": 26, "cached": 6080, "thoughts": 29, "tool": 0, "total": 7929},
      "model": "gemini-2.5-pro"
    },
    {
      "id": "ba3106ff-4e07-4f57-8995-e9445a3f00b0",
      "timestamp": "2025-12-26T11:07:18.426Z",
      "type": "gemini",
      "content": "The Makefile reveals a Go backend and a TypeScript frontend.",
      "tokens": {"input": 8407, "output": 80, "cached": 3076, "thoughts": 255, "tool": 0, "total": 8742},
      "model": "gemini-2.5-pro"
    }
  ]
}`

const geminiOverlapSessionID = "8b65451f-31e9-4d19-82ff-fa85ff0b09c4"

var geminiOverlapTimes = []time.Time{
	time.Date(2025, 12, 26, 11, 7, 11, 648_000_000, time.UTC),
	time.Date(2025, 12, 26, 11, 7, 14, 3_000_000, time.UTC),
	time.Date(2025, 12, 26, 11, 7, 18, 426_000_000, time.UTC),
}

// geminiOverlapOTLP returns what Gemini CLI sent over OTLP for the first two
// responses of geminiOverlapSession (AI Observer was down for the third):
// the prompt, each response's api_response event and operation details
// record (a few ms before the file records it), the tool call, and the
// session's cumulative token counter with the cost AI Observer derives
func geminiOverlapOTLP() ([]api.LogRecord, []api.MetricDataPoint) {
	session := geminiOverlapSessionID
	event := func(ts time.Time, name, body string, attrs map[string]string) api.LogRecord {
		a := map[string]string{"event.name": name, "session.id": session, "installation.id": "i-1", "interactive": "true"}
		for k, v := range attrs {
			a[k] = v
		}
		return api.LogRecord{Timestamp: ts, ServiceName: "gemini_cli", Body: body, LogAttributes: a}
	}
	response := func(ts time.Time, input, output, cached, thoughts string) []api.LogRecord {
		return []api.LogRecord{
			event(ts, "gemini_cli.api_response", "API response from gemini-2.5-pro. Status: 200. Duration: 2100ms.", map[string]string{
				"model": "gemini-2.5-pro", "duration_ms": "2100", "input_token_count": input, "output_token_count": output,
				"cached_content_token_count": cached, "thoughts_token_count": thoughts, "tool_token_count": "0",
				"prompt_id": session + "########1", "status_code": "200"}),
			event(ts.Add(time.Millisecond), "gen_ai.client.inference.operation.details", "GenAI operation details from gemini-2.5-pro.", map[string]string{
				"gen_ai.usage.input_tokens": input, "gen_ai.usage.output_tokens": output,
				"gen_ai.output.messages": `[{"role":"assistant","parts":[{"type":"text","content":"..."}]}]`}),
		}
	}
	logs := []api.LogRecord{event(time.Date(2025, 12, 26, 11, 7, 5, 950_000_000, time.UTC), "gemini_cli.user_prompt", "User prompt. Length: 49.",
		map[string]string{"prompt": "make yourself familiar with the existing codebase", "prompt_length": "49", "prompt_id": session + "########1"})}
	logs = append(logs, response(geminiOverlapTimes[0].Add(-30*time.Millisecond), "7777", "90", "3114", "260")...)
	logs = append(logs, event(geminiOverlapTimes[0].Add(25*time.Millisecond), "gemini_cli.tool_call", "Tool call: read_file. Success: true. Duration: 7ms.",
		map[string]string{"function_name": "read_file", "function_args": "{\n  \"file_path\": \"README.md\"\n}", "duration_ms": "7", "success": "true",
			"prompt_id": session + "########1", "tool_type": "native"}))
	logs = append(logs, response(geminiOverlapTimes[1].Add(-20*time.Millisecond), "7874", "26", "6080", "29")...)

	cumulative := int32(2)
	monotonic := true
	var points []api.MetricDataPoint
	for _, t := range []struct {
		tokenType string
		v         float64
	}{{"input", 7777 + 7874}, {"output", 90 + 26}, {"cache", 3114 + 6080}, {"thought", 260 + 29}} {
		v := t.v
		points = append(points, api.MetricDataPoint{Timestamp: geminiOverlapTimes[1].Add(10 * time.Second), ServiceName: "gemini_cli",
			MetricName: "gemini_cli.token.usage", MetricType: "sum", Value: &v, AggregationTemporality: &cumulative, IsMonotonic: &monotonic,
			Attributes: map[string]string{"session.id": session, "installation.id": "i-1", "interactive": "true", "model": "gemini-2.5-pro", "type": t.tokenType}})
	}
	return logs, append(points, otlp.DeriveGeminiCostMetrics(points)...)
}

func setupGeminiOverlap(t *testing.T) (*storage.DuckDBStore, *Importer) {
	t.Helper()
	dir := t.TempDir()
	chats := filepath.Join(dir, "2d31728c", "chats")
	if err := os.MkdirAll(chats, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(chats, "session-2025-12-26T11-06-8b65451f.json"), []byte(geminiOverlapSession), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AI_OBSERVER_GEMINI_PATH", dir)
	store, err := storage.NewDuckDBStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	imp := NewImporter(store, false)
	imp.SetIO(&strings.Builder{}, strings.NewReader(""))
	imp.RegisterParser(NewGeminiParser())
	return store, imp
}

// checkGeminiOverlap checks that the session counts each response once:
// its usage points, its transcript and its session list row
func checkGeminiOverlap(t *testing.T, store *storage.DuckDBStore) {
	t.Helper()
	for i, want := range []bool{false, false, true} {
		if got := fileUsagePoints(t, store, geminiOverlapTimes[i]); (got > 0) != want {
			t.Errorf("file usage points of response %d = %d, want them stored: %v", i+1, got, want)
		}
	}
	ctx := context.Background()
	resp, err := transcript.Load(ctx, store, geminiOverlapSessionID)
	if err != nil {
		t.Fatal(err)
	}
	var roles []string
	for _, m := range resp.Messages {
		roles = append(roles, m.Role)
	}
	// The file's prompt, three replies and the tool call with its result;
	// every OTLP event describes one of them
	if want := "user assistant tool_use tool_result assistant assistant"; strings.Join(roles, " ") != want {
		t.Errorf("transcript roles %q, want %q", roles, want)
	}
	if tot := resp.Totals; tot.InputTokens != 7777+7874+8407 || tot.OutputTokens != 90+26+80 {
		t.Errorf("transcript totals %+v, want each response once", *tot)
	}
	if call := resp.Messages[2]; call.DurationMs != 7 || call.Success == nil || !*call.Success {
		t.Errorf("tool call not filled in from its event: %+v", call)
	}
	list, err := store.QuerySessions(ctx, "", geminiOverlapTimes[0].Add(-time.Hour), geminiOverlapTimes[0].Add(time.Hour), storage.Page{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Sessions) != 1 {
		t.Fatalf("sessions %+v", list.Sessions)
	}
	s := list.Sessions[0]
	if s.MessageCount != len(resp.Messages) || s.InputTokens == nil || *s.InputTokens != resp.Totals.InputTokens ||
		s.OutputTokens == nil || *s.OutputTokens != resp.Totals.OutputTokens || s.Title != resp.Title {
		t.Errorf("session listed as %+v, transcript has %d messages, totals %+v, title %q", s, len(resp.Messages), *resp.Totals, resp.Title)
	}
}

// A Gemini CLI session sent over OTLP and imported from its session file
// counts each response once, in either order
func TestImportGeminiSessionRecordedOverOTLP(t *testing.T) {
	ctx := context.Background()
	t.Run("OTLP first", func(t *testing.T) {
		store, imp := setupGeminiOverlap(t)
		logs, metrics := geminiOverlapOTLP()
		if err := store.InsertTelemetry(ctx, logs, metrics, nil); err != nil {
			t.Fatal(err)
		}
		if err := imp.Import(ctx, []SourceType{SourceGemini}, Options{SkipConfirm: true}); err != nil {
			t.Fatal(err)
		}
		checkGeminiOverlap(t, store)
	})
	t.Run("import first", func(t *testing.T) {
		store, imp := setupGeminiOverlap(t)
		if err := imp.Import(ctx, []SourceType{SourceGemini}, Options{SkipConfirm: true}); err != nil {
			t.Fatal(err)
		}
		logs, metrics := geminiOverlapOTLP()
		if err := store.InsertTelemetry(ctx, logs, nil, nil); err != nil {
			t.Fatal(err)
		}
		if err := store.InsertMetrics(ctx, metrics); err != nil {
			t.Fatal(err)
		}
		checkGeminiOverlap(t, store)
	})
}

// Usage imported before this version (points without session.id) stays
// counted twice until the file is imported again with --force
func TestImportGeminiForceRepairsOverlap(t *testing.T) {
	ctx := context.Background()
	store, imp := setupGeminiOverlap(t)
	logs, metrics := geminiOverlapOTLP()
	if err := store.InsertTelemetry(ctx, logs, metrics, nil); err != nil {
		t.Fatal(err)
	}
	if err := imp.Import(ctx, []SourceType{SourceGemini}, Options{SkipConfirm: true}); err != nil {
		t.Fatal(err)
	}
	// As an earlier version stored them: points without the session
	if _, err := store.DB().Exec(`UPDATE otel_metrics SET Attributes = json_object('type', json_extract_string(Attributes, '$.type'),
		'model', 'gemini-2.5-pro', 'import_source', 'local_jsonl') WHERE SourceFile IS NOT NULL`); err != nil {
		t.Fatal(err)
	}
	var file string
	if err := store.DB().QueryRow(`SELECT any_value(SourceFile) FROM otel_logs WHERE SourceFile IS NOT NULL`).Scan(&file); err != nil {
		t.Fatal(err)
	}
	parsed, err := NewGeminiParser().ParseFile(ctx, file)
	if err != nil {
		t.Fatal(err)
	}
	for i := range parsed.Metrics {
		delete(parsed.Metrics[i].Attributes, "session.id")
	}
	if err := store.InsertImportedFile(ctx, nil, parsed.Metrics, nil, &storage.ImportState{Source: "gemini", FilePath: file,
		FileHash: parsed.FileHash, ImportedAt: time.Now(), SourceTracked: true}, nil); err != nil {
		t.Fatal(err)
	}
	if got := fileUsagePoints(t, store, geminiOverlapTimes[0]); got == 0 {
		t.Fatal("legacy usage points were not stored")
	}
	if err := imp.Import(ctx, []SourceType{SourceGemini}, Options{SkipConfirm: true, Force: true}); err != nil {
		t.Fatal(err)
	}
	checkGeminiOverlap(t, store)
}

// The session file's records carry what identifies them to the OTLP records
// of the same session (internal/msgmerge, storage/usage_overlap.go)
func TestGeminiRecordsCarryIdentities(t *testing.T) {
	var session GeminiSession
	if err := json.Unmarshal([]byte(geminiOverlapSession), &session); err != nil {
		t.Fatal(err)
	}
	records, err := ConvertGeminiMessages(context.Background(), &session, 0, "local_jsonl")
	if err != nil {
		t.Fatal(err)
	}
	var reply, call, result map[string]string
	for _, l := range records.Logs {
		switch l.LogAttributes["message.role"] {
		case "assistant":
			if reply == nil {
				reply = l.LogAttributes
			}
		case "tool_use":
			call = l.LogAttributes
		case "tool_result":
			result = l.LogAttributes
		}
	}
	if reply["thoughts_token_count"] != "260" || reply["tool_token_count"] != "0" || reply["input_tokens"] != "7777" {
		t.Errorf("reply attributes %v", reply)
	}
	if call["tool.call_id"] != "read_file-1766747231641-63d9ab01f7dc4" || result["tool.call_id"] != call["tool.call_id"] {
		t.Errorf("tool call IDs %q, %q", call["tool.call_id"], result["tool.call_id"])
	}
	for _, m := range records.Metrics {
		if m.Attributes["session.id"] != geminiOverlapSessionID {
			t.Errorf("%s point without the session: %v", m.MetricName, m.Attributes)
		}
	}
}
