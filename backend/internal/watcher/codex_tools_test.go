package watcher

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tobilg/ai-observer/internal/api"
	"github.com/tobilg/ai-observer/internal/importer"
	"github.com/tobilg/ai-observer/internal/storage"
)

// The importer's fixture of every kind of Codex tool call
const codexToolsFixture = "../importer/testdata/codex_tools.jsonl"

func readCodexToolsFixture(t *testing.T) []string {
	t.Helper()
	data, err := os.ReadFile(codexToolsFixture)
	if err != nil {
		t.Fatal(err)
	}
	return strings.SplitAfter(strings.TrimSuffix(string(data), "\n"), "\n")
}

// comparable returns what import and watch must store alike of a log
func comparableLog(l api.LogRecord) string {
	attrs := maps.Clone(l.LogAttributes)
	delete(attrs, "import_source")
	data, _ := json.Marshal(attrs)
	return fmt.Sprintf("%s|%s|%s", l.Timestamp.UTC().Format("15:04:05.000"), l.Body, data)
}

func comparableLogs(logs []api.LogRecord) []string {
	out := make([]string, len(logs))
	for i, l := range logs {
		out[i] = comparableLog(l)
	}
	return out
}

// watchInParts writes the file in parts, one per cut (byte positions), and
// runs the incremental parser after each, keeping the state between runs as
// the watcher stores it
func watchInParts(t *testing.T, content string, cuts []int, state *storage.ImportState) []api.LogRecord {
	t.Helper()
	path := state.FilePath
	var logs []api.LogRecord
	for _, cut := range append(cuts, len(content)) {
		if err := os.WriteFile(path, []byte(content[:cut]), 0o644); err != nil {
			t.Fatal(err)
		}
		result, err := (&codexIncrementalParser{}).ParseIncremental(context.Background(), path, state)
		if err != nil {
			t.Fatal(err)
		}
		logs = append(logs, result.Logs...)
		// The watcher stores the state as JSON between runs
		roundTrip := *state
		var ps codexParserState
		if err := json.Unmarshal([]byte(state.ParserState), &ps); err != nil {
			t.Fatal(err)
		}
		data, _ := json.Marshal(ps)
		roundTrip.ParserState = string(data)
		*state = roundTrip
	}
	return logs
}

// Watch stores the same tool calls as import however the file's appends
// split calls, outputs and executions, also within a line
func TestCodexWatchStoresToolCallsLikeImport(t *testing.T) {
	lines := readCodexToolsFixture(t)
	content := strings.Join(lines, "")
	path := filepath.Join(t.TempDir(), "rollout-2026-10-26T07-33-20-sess-tools.jsonl")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	imported, err := importer.NewCodexParser().ParseFile(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	want := comparableLogs(imported.Logs)

	offset := 0
	for i, line := range lines[:len(lines)-1] {
		offset += len(line)
		for _, cuts := range [][]int{
			{offset},                       // between two lines
			{offset - len(line)/2},         // within a line
			{offset - len(line)/2, offset}, // within a line, then after it
		} {
			state := &storage.ImportState{Source: string(importer.SourceCodex), FilePath: path}
			got := comparableLogs(watchInParts(t, content, cuts, state))
			if strings.Join(got, "\n") != strings.Join(want, "\n") {
				t.Fatalf("appends cut at %v (line %d): watch stored\n%s\nimport stored\n%s", cuts, i, strings.Join(got, "\n"), strings.Join(want, "\n"))
			}
			if state.MessageCount != imported.MessageCount {
				t.Fatalf("cut at %v: message count %d, import %d", cuts, state.MessageCount, imported.MessageCount)
			}
		}
	}
}

// Watch continues where an import stopped reading: the calls the import read
// whose outputs or executions follow are rebuilt from the file
func TestCodexWatchAfterImportPairsToolCalls(t *testing.T) {
	lines := readCodexToolsFixture(t)
	content := strings.Join(lines, "")
	dir := t.TempDir()
	full := filepath.Join(dir, "full", "rollout-2026-10-26T07-33-20-sess-tools.jsonl")
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	imported, err := importer.NewCodexParser().ParseFile(context.Background(), full)
	if err != nil {
		t.Fatal(err)
	}
	want := comparableLogs(imported.Logs)

	head := 0
	for i, line := range lines[:len(lines)-1] {
		head += len(line)
		path := filepath.Join(dir, fmt.Sprintf("part%d", i), filepath.Base(full))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content[:head]), 0o644); err != nil {
			t.Fatal(err)
		}
		first, err := importer.NewCodexParser().ParseFile(context.Background(), path)
		if err != nil {
			t.Fatal(err)
		}
		// The import's state: where it stopped reading, no parser state
		state := &storage.ImportState{
			Source: string(importer.SourceCodex), FilePath: path,
			ByteOffset: first.ParsedBytes, MessageCount: first.MessageCount,
		}
		rest := watchInParts(t, content, nil, state)
		got := comparableLogs(append(first.Logs, rest...))
		if strings.Join(got, "\n") != strings.Join(want, "\n") {
			t.Fatalf("import of %d lines, then watch: stored\n%s\nimport of all stored\n%s", i+1, strings.Join(got, "\n"), strings.Join(want, "\n"))
		}
	}
}

// A parser state of an earlier version, which kept no tool calls: watch
// rebuilds them from the part of the file it read, so an execution after the
// offset does not become a call of its own
func TestCodexWatchRebuildsToolCallsOfEarlierState(t *testing.T) {
	lines := []string{
		`{"timestamp":"2026-10-26T07:33:20.000Z","type":"session_meta","payload":{"id":"s1"}}` + "\n",
		`{"timestamp":"2026-10-26T07:33:21.000Z","type":"response_item","payload":{"type":"function_call","name":"exec_command","arguments":"{\"cmd\":\"ls\"}","call_id":"c1"}}` + "\n",
		`{"timestamp":"2026-10-26T07:33:22.000Z","type":"event_msg","payload":{"type":"item_completed","item":{"type":"CommandExecution","id":"c1","command":["ls"],"cwd":"/w","status":"completed","exit_code":0,"duration":{"secs":1,"nanos":0},"aggregated_output":"a\n"}}}` + "\n",
		`{"timestamp":"2026-10-26T07:33:23.000Z","type":"response_item","payload":{"type":"function_call_output","call_id":"c1","output":"a\n"}}` + "\n",
	}
	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	head := lines[0] + lines[1]
	// What an earlier version kept after reading the first two lines
	earlier, _ := json.Marshal(map[string]any{
		"repository": map[string]any{}, "sessionId": "s1", "messageIndex": 1,
	})
	state := &storage.ImportState{
		Source: string(importer.SourceCodex), FilePath: path,
		ByteOffset: int64(len(head)), MessageCount: 1, ParserState: string(earlier),
	}
	logs := watchInParts(t, strings.Join(lines, ""), nil, state)
	if len(logs) != 1 || logs[0].LogAttributes["message.role"] != "tool_result" || logs[0].LogAttributes["duration_ms"] != "1000" ||
		logs[0].LogAttributes["tool.name"] != "exec_command" {
		t.Errorf("watch stored %d messages after the offset: %v, want the call's result with its execution's duration", len(logs), comparableLogs(logs))
	}
}
