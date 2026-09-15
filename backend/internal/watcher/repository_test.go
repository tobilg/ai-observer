package watcher

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/tobilg/ai-observer/internal/api"
	"github.com/tobilg/ai-observer/internal/importer"
	"github.com/tobilg/ai-observer/internal/storage"
)

const repositoryCodexMeta = `{"timestamp":"2026-09-01T12:00:00Z","type":"session_meta","payload":{"id":"s1","cwd":"/missing/project","model":"gpt-4o","git":{"repository_url":"https://credential@github.com/acme/project.git","branch":"main"}}}`
const repositoryCodexTokens = `{"timestamp":"2026-09-01T12:00:01Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":100,"output_tokens":50}}}}`
const repositoryCodexNextTokens = `{"timestamp":"2026-09-01T12:00:02Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":150,"output_tokens":60}}}}`
const repositoryClaudeMeta = `{"type":"user","timestamp":"2026-09-01T12:00:00Z","sessionId":"s1","cwd":"/missing/project","gitBranch":"main","message":{"role":"user","content":[{"type":"text","text":"https://github.com/acme/project/pull/7"}]}}`
const repositoryClaudeTokens = `{"type":"assistant","timestamp":"2026-09-01T12:00:01Z","sessionId":"s1","requestId":"r1","message":{"id":"m1","role":"assistant","model":"claude-sonnet-4-20250514","content":[{"type":"text","text":"hello"}],"usage":{"input_tokens":100,"output_tokens":50}}}`
const repositoryClaudeNextTokens = `{"type":"assistant","timestamp":"2026-09-01T12:00:02Z","sessionId":"s1","requestId":"r2","message":{"id":"m2","role":"assistant","model":"claude-sonnet-4-20250514","content":[{"type":"text","text":"next"}],"usage":{"input_tokens":50,"output_tokens":10}}}`

type repositoryParserCase struct {
	name, prefix, suffix, tokenName string
	full                            func() importer.SessionParser
	watch                           func() IncrementalParser
}

func repositoryParsers() []repositoryParserCase {
	return []repositoryParserCase{
		{"Claude", repositoryClaudeMeta + "\n" + repositoryClaudeTokens + "\n", repositoryClaudeNextTokens + "\n", importer.ClaudeTokenUsageMetric, func() importer.SessionParser { return importer.NewClaudeParser() }, func() IncrementalParser { return &claudeIncrementalParser{} }},
		{"Codex", repositoryCodexMeta + "\n" + repositoryCodexTokens + "\n", repositoryCodexNextTokens + "\n", "codex_cli_rs.token.usage", func() importer.SessionParser { return importer.NewCodexParser() }, func() IncrementalParser { return &codexIncrementalParser{} }},
	}
}

func assertRepositoryBatch(t *testing.T, metrics []api.MetricDataPoint, logs []api.LogRecord, repo string) {
	t.Helper()
	for _, metric := range metrics {
		if got := metric.Attributes["repository"]; got != repo {
			t.Errorf("%s repository = %q, want %q", metric.MetricName, got, repo)
		}
		if got := metric.Attributes["git_branch"]; got != "main" {
			t.Errorf("%s branch = %q", metric.MetricName, got)
		}
	}
	for _, log := range logs {
		if got := log.LogAttributes["repository"]; got != repo {
			t.Errorf("%s repository = %q, want %q", log.Body, got, repo)
		}
		if got := log.LogAttributes["git_branch"]; got != "main" {
			t.Errorf("%s branch = %q", log.Body, got)
		}
	}
}

func assertRepositoryTokenValue(t *testing.T, metrics []api.MetricDataPoint, name string, want float64) {
	t.Helper()
	var total float64
	for _, metric := range metrics {
		if metric.MetricName == name && metric.Attributes["type"] == "input" && metric.Value != nil {
			total += *metric.Value
		}
	}
	if total != want {
		t.Fatalf("input token total = %v, want %v", total, want)
	}
}

func appendRepositoryFile(t *testing.T, path, content string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(content); err != nil {
		t.Fatal(err)
	}
}

func TestRepositoryFullAndWatchParity(t *testing.T) {
	for _, tc := range repositoryParsers() {
		t.Run(tc.name, func(t *testing.T) {
			path := writeWatcherTestFile(t, tc.prefix)
			full, err := tc.full().ParseFile(context.Background(), path)
			if err != nil {
				t.Fatal(err)
			}
			state := &storage.ImportState{FilePath: path}
			watched, err := tc.watch().ParseIncremental(context.Background(), path, state)
			if err != nil {
				t.Fatal(err)
			}
			assertRepositoryBatch(t, full.Metrics, full.Logs, "acme/project")
			assertRepositoryBatch(t, watched.Metrics, watched.Logs, "acme/project")
			assertRepositoryTokenValue(t, full.Metrics, tc.tokenName, 100)
			assertRepositoryTokenValue(t, watched.Metrics, tc.tokenName, 100)
			if len(full.Logs) != len(watched.Logs) {
				t.Fatalf("full/watch log counts differ: %d/%d", len(full.Logs), len(watched.Logs))
			}
			for _, metric := range full.Metrics {
				if metric.MetricName == "claude_code.pull_request.count" || metric.MetricName == "claude_code.commit.count" {
					t.Errorf("text reference created activity: %s", metric.MetricName)
				}
			}
			if strings.Contains(state.ParserState, "credential") {
				t.Fatal("repository state persisted URL credentials")
			}
		})
	}
}

func TestRepositoryPersistsAcrossRestart(t *testing.T) {
	for _, tc := range repositoryParsers() {
		t.Run(tc.name, func(t *testing.T) {
			path := writeWatcherTestFile(t, tc.prefix)
			state := &storage.ImportState{FilePath: path}
			if _, err := tc.watch().ParseIncremental(context.Background(), path, state); err != nil {
				t.Fatal(err)
			}
			// Round-trip the persisted state and instantiate a new parser.
			data, err := json.Marshal(state)
			if err != nil {
				t.Fatal(err)
			}
			var restored storage.ImportState
			if err := json.Unmarshal(data, &restored); err != nil {
				t.Fatal(err)
			}
			appendRepositoryFile(t, path, tc.suffix)
			result, err := tc.watch().ParseIncremental(context.Background(), path, &restored)
			if err != nil {
				t.Fatal(err)
			}
			assertRepositoryBatch(t, result.Metrics, result.Logs, "acme/project")
			assertRepositoryTokenValue(t, result.Metrics, tc.tokenName, 50)
			if restored.ByteOffset != int64(len(tc.prefix+tc.suffix)) {
				t.Fatalf("offset = %d", restored.ByteOffset)
			}
			empty, err := tc.watch().ParseIncremental(context.Background(), path, &restored)
			if err != nil {
				t.Fatal(err)
			}
			if len(empty.Logs)+len(empty.Metrics) != 0 {
				t.Fatal("replayed data with no append")
			}
		})
	}
}

func TestRepositoryRecoversLegacyState(t *testing.T) {
	for _, tc := range repositoryParsers() {
		for _, oldState := range []string{"", `{"messageIndex":9,"seenRequests":{"m1:r1":true},"sessionId":"s1","currentModel":"gpt-4o","lastTokenCount":{"input_tokens":100,"output_tokens":50}}`} {
			t.Run(tc.name+"/"+oldState, func(t *testing.T) {
				path := writeWatcherTestFile(t, tc.prefix+tc.suffix)
				state := &storage.ImportState{FilePath: path, ByteOffset: int64(len(tc.prefix)), MessageCount: 9, ParserState: oldState}
				result, err := tc.watch().ParseIncremental(context.Background(), path, state)
				if err != nil {
					t.Fatal(err)
				}
				assertRepositoryBatch(t, result.Metrics, result.Logs, "acme/project")
				assertRepositoryTokenValue(t, result.Metrics, tc.tokenName, 50)
				for _, log := range result.Logs {
					if log.Body == "hello" || log.Body == "conversation_starts" {
						t.Errorf("re-emitted prefix log: %s", log.Body)
					}
					if log.LogAttributes["event.name"] == "transcript.message" && log.LogAttributes["message.index"] != "9" {
						t.Errorf("message index was reset: %v", log.LogAttributes)
					}
				}
				if !strings.Contains(state.ParserState, `"repository"`) {
					t.Fatal("recovered context not persisted")
				}
			})
		}
	}
}

func TestRepositoryRecoveryCancellationPreservesState(t *testing.T) {
	for _, tc := range repositoryParsers() {
		t.Run(tc.name, func(t *testing.T) {
			path := writeWatcherTestFile(t, tc.prefix+tc.suffix)
			state := storage.ImportState{FilePath: path, ByteOffset: int64(len(tc.prefix)), MessageCount: 9}
			before := state
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if _, err := tc.watch().ParseIncremental(ctx, path, &state); err != context.Canceled {
				t.Fatalf("error = %v", err)
			}
			if !reflect.DeepEqual(before, state) {
				t.Fatal("cancellation changed persisted state")
			}
		})
	}
}

func TestCodexAllLogsReceiveRepository(t *testing.T) {
	lines := []string{
		repositoryCodexMeta,
		`{"timestamp":"2026-09-01T12:00:01Z","type":"event_msg","payload":{"type":"user_message"}}`,
		`{"timestamp":"2026-09-01T12:00:02Z","type":"event_msg","payload":{"type":"agent_message"}}`,
		`{"timestamp":"2026-09-01T12:00:03Z","type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"message"}]}}`,
		`{"timestamp":"2026-09-01T12:00:04Z","type":"response_item","payload":{"type":"function_call","name":"exec_command","arguments":"{\"cmd\":\"echo hello\"}","call_id":"c1"}}`,
		`{"timestamp":"2026-09-01T12:00:05Z","type":"response_item","payload":{"type":"function_call_output","output":"hello","call_id":"c1"}}`,
		`{"timestamp":"2026-09-01T12:00:06Z","type":"response_item","payload":{"type":"reasoning","summary":[{"type":"summary_text","text":"reasoning"}]}}`,
		repositoryCodexTokens,
	}
	path := writeWatcherTestFile(t, strings.Join(lines, "\n")+"\n")
	full, err := importer.NewCodexParser().ParseFile(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	watched, err := (&codexIncrementalParser{}).ParseIncremental(context.Background(), path, &storage.ImportState{})
	if err != nil {
		t.Fatal(err)
	}
	if len(full.Logs) != 7 || len(watched.Logs) != 7 {
		t.Fatalf("log counts = %d/%d, want 7/7", len(full.Logs), len(watched.Logs))
	}
	assertRepositoryBatch(t, full.Metrics, full.Logs, "acme/project")
	assertRepositoryBatch(t, watched.Metrics, watched.Logs, "acme/project")
}

func TestRepositoryPartialMetadataAndLateReference(t *testing.T) {
	// A late reference enriches the current batch, without changing records
	// returned by an earlier call. Incomplete JSON contributes no evidence.
	meta := strings.Replace(repositoryCodexMeta, `"git":{"repository_url":"https://credential@github.com/acme/project.git","branch":"main"}`, `"git":{"branch":"main"}`, 1)
	path := writeWatcherTestFile(t, meta+"\n"+repositoryCodexTokens+"\n")
	state := &storage.ImportState{}
	first, err := (&codexIncrementalParser{}).ParseIncremental(context.Background(), path, state)
	if err != nil {
		t.Fatal(err)
	}
	assertRepositoryBatch(t, first.Metrics, first.Logs, "project")
	reference := `{"timestamp":"2026-09-01T12:00:03Z","type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"https://github.com/acme/project"}]}}`
	appendRepositoryFile(t, path, reference[:len(reference)-4])
	offset := state.ByteOffset
	partial, err := (&codexIncrementalParser{}).ParseIncremental(context.Background(), path, state)
	if err != nil {
		t.Fatal(err)
	}
	if state.ByteOffset != offset || len(partial.Logs)+len(partial.Metrics) != 0 {
		t.Fatal("incomplete metadata was committed")
	}
	appendRepositoryFile(t, path, reference[len(reference)-4:]+"\n"+repositoryCodexNextTokens+"\n")
	last, err := (&codexIncrementalParser{}).ParseIncremental(context.Background(), path, state)
	if err != nil {
		t.Fatal(err)
	}
	assertRepositoryBatch(t, last.Metrics, last.Logs, "acme/project")
	assertRepositoryBatch(t, first.Metrics, first.Logs, "project")
	assertRepositoryTokenValue(t, last.Metrics, "codex_cli_rs.token.usage", 50)
}
