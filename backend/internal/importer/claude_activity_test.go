package importer

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tobilg/ai-observer/internal/storage"
)

type activityJSON = map[string]any

func activityAssistant(id, req string, content []any, usage any) activityJSON {
	return activityJSON{"type": "assistant", "timestamp": "2026-09-01T23:59:00Z", "sessionId": "review-session", "requestId": req, "costUSD": 0.05,
		"message": activityJSON{"id": id, "model": "claude-sonnet-4-20250514", "role": "assistant", "content": content, "usage": usage}}
}

func activityTool(id, name string, input activityJSON) activityJSON {
	return activityJSON{"type": "tool_use", "id": id, "name": name, "input": input}
}

func activityResult(id, content string, failed bool) activityJSON {
	return activityJSON{"type": "user", "timestamp": "2026-09-01T23:59:01Z", "sessionId": "review-session",
		"message": activityJSON{"role": "user", "content": []any{activityJSON{"type": "tool_result", "tool_use_id": id, "content": content, "is_error": failed}}}}
}

func activityFile(t *testing.T, entries ...activityJSON) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "review-session.jsonl")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	enc := json.NewEncoder(f)
	for _, entry := range entries {
		if err := enc.Encode(entry); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func activityParse(t *testing.T, entries ...activityJSON) *ImportResult {
	t.Helper()
	r, err := NewClaudeParser().ParseFile(context.Background(), activityFile(t, entries...))
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func activitySum(r *ImportResult, name, kind string) float64 {
	var sum float64
	for _, m := range r.Metrics {
		if m.MetricName == name && (kind == "" || m.Attributes["type"] == kind) && m.Value != nil {
			sum += *m.Value
		}
	}
	return sum
}

func TestClaudeActivityLateUsage(t *testing.T) {
	r := activityParse(t,
		activityAssistant("msg-1", "req-1", []any{activityJSON{"type": "text", "text": "Working"}}, nil),
		activityAssistant("msg-1", "req-1", nil, activityJSON{"input_tokens": 100, "output_tokens": 50}),
	)
	if got := activitySum(r, "claude_code.token.usage", ""); got != 150 {
		t.Errorf("token total: got %v, want 150", got)
	}
	if got := activitySum(r, "claude_code.cost.usage", ""); got <= 0 {
		t.Errorf("cost: got %v, want positive", got)
	}
}

func TestClaudeActivityDistinctToolBlocksSameRequest(t *testing.T) {
	r := activityParse(t,
		activityAssistant("msg-1", "req-1", []any{activityTool("tool-1", "Write", activityJSON{"file_path": "/project/a.go", "content": "a"})}, nil),
		activityAssistant("msg-1", "req-1", []any{activityTool("tool-2", "Write", activityJSON{"file_path": "/project/b.go", "content": "b"})}, nil),
		activityResult("tool-1", "File created successfully", false),
		activityResult("tool-2", "File created successfully", false),
	)
	if got := activitySum(r, "claude_code.lines_of_code.count", "added"); got != 2 {
		t.Errorf("two distinct one-line writes: got %v added, want 2", got)
	}
}

func TestClaudeActivityCommitDetection(t *testing.T) {
	for _, tc := range []struct {
		name, command, result string
		failed                bool
		want                  float64
	}{
		{"echo", "echo 'git commit'", "git commit", false, 0},
		{"failed", "git commit -m test", "nothing to commit, working tree clean", true, 0},
		{"git_option", "git -C /project commit -m test", "[main abc1234] test", false, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := activityParse(t, activityAssistant("msg-1", "req-1", []any{activityTool("tool-1", "Bash", activityJSON{"command": tc.command})}, nil), activityResult("tool-1", tc.result, tc.failed))
			if got := activitySum(r, "claude_code.commit.count", ""); got != tc.want {
				t.Errorf("%q: got %v commits, want %v", tc.command, got, tc.want)
			}
		})
	}
}

func TestClaudeActivityFailedEdit(t *testing.T) {
	r := activityParse(t,
		activityAssistant("msg-1", "req-1", []any{activityTool("tool-1", "Edit", activityJSON{"file_path": "/project/a.go", "old_string": "old", "new_string": "new"})}, nil),
		activityResult("tool-1", "String to replace not found in file", true),
	)
	if got := activitySum(r, "claude_code.lines_of_code.count", ""); got != 0 {
		t.Errorf("failed edit: got %v changed lines, want 0", got)
	}
}

func TestClaudeActivityLOCDelta(t *testing.T) {
	t.Run("unchanged_context", func(t *testing.T) {
		r := activityParse(t,
			activityAssistant("msg-1", "req-1", []any{activityTool("tool-1", "Edit", activityJSON{"file_path": "/project/a.go", "old_string": "keep\nold\nkeep", "new_string": "keep\nnew\nkeep"})}, nil),
			activityResult("tool-1", "The file has been updated successfully", false),
		)
		for _, kind := range []string{"added", "removed"} {
			if got := activitySum(r, "claude_code.lines_of_code.count", kind); got != 1 {
				t.Errorf("one-line replacement: got %v %s, want 1", got, kind)
			}
		}
	})
	t.Run("trailing_newline", func(t *testing.T) {
		r := activityParse(t,
			activityAssistant("msg-1", "req-1", []any{activityTool("tool-1", "Write", activityJSON{"file_path": "/project/a.go", "content": "a\nb\n"})}, nil),
			activityResult("tool-1", "File created successfully", false),
		)
		if got := activitySum(r, "claude_code.lines_of_code.count", "added"); got != 2 {
			t.Errorf("two-line file: got %v added, want 2", got)
		}
	})
}

func TestClaudeActivityMultiplePRs(t *testing.T) {
	r := activityParse(t,
		activityJSON{"type": "pr-link", "timestamp": "2026-09-01T12:00:00Z", "prNumber": 7, "prRepository": "acme/app", "prUrl": "https://github.com/acme/app/pull/7"},
		activityJSON{"type": "pr-link", "timestamp": "2026-09-01T12:00:01Z", "prNumber": 8, "prRepository": "acme/app", "prUrl": "https://github.com/acme/app/pull/8"},
		activityJSON{"type": "pr-link", "timestamp": "2026-09-01T12:00:02Z", "prNumber": 7, "prRepository": "acme/app", "prUrl": "https://github.com/acme/app/pull/7"},
	)
	if got := activitySum(r, "claude_code.pull_request.count", ""); got != 2 {
		t.Errorf("two distinct PRs plus repeated link: got %v, want 2", got)
	}
}

func TestClaudeActivityPRDateRange(t *testing.T) {
	file := activityFile(t,
		activityAssistant("msg-1", "req-1", []any{activityJSON{"type": "text", "text": "Opening a PR"}}, nil),
		activityJSON{"type": "pr-link", "timestamp": "2026-09-02T00:01:00Z", "prNumber": 7, "prRepository": "acme/app", "prUrl": "https://github.com/acme/app/pull/7"},
	)
	p := NewClaudeParser()
	p.configPaths = []string{filepath.Dir(file)}
	r, err := p.ParseFile(context.Background(), file)
	if err != nil {
		t.Fatal(err)
	}
	if got := activitySum(r, "claude_code.pull_request.count", ""); got != 1 {
		t.Fatalf("fixture PR count = %v", got)
	}
	store, err := storage.NewDuckDBStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	i := NewImporter(store, false)
	i.RegisterParser(p)
	from, _ := time.Parse(time.RFC3339, "2026-09-02T00:00:00Z")
	if err := i.Import(context.Background(), []SourceType{SourceClaude}, Options{FromDate: &from, SkipConfirm: true}); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := store.DB().QueryRow("SELECT count(*) FROM otel_metrics WHERE MetricName = 'claude_code.pull_request.count'").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("PR inside selected date range: imported %d, want 1 (parser LastTime=%s)", count, r.LastTime)
	}
}

func TestClaudeActivityRepositoryAttribution(t *testing.T) {
	dir := t.TempDir()
	for _, args := range [][]string{{"init", "--quiet", dir}, {"-C", dir, "config", "remote.origin.url", "https://github.com/acme/local-project.git"}} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git fixture: %v: %s", err, out)
		}
	}
	a := activityAssistant("msg-1", "req-1", nil, activityJSON{"input_tokens": 100})
	a["cwd"] = dir
	r := activityParse(t, a, activityJSON{"type": "pr-link", "timestamp": "2026-09-02T00:01:00Z", "prNumber": 7, "prRepository": "other/external-project", "prUrl": "https://github.com/other/external-project/pull/7"})
	for _, m := range r.Metrics {
		if m.MetricName == "claude_code.token.usage" && m.Attributes["repository"] != "acme/local-project" {
			t.Errorf("usage in local git repository attributed to %q, want acme/local-project", m.Attributes["repository"])
		}
	}
}

func TestClaudeActivitySuccessfulResultsAndDeduplication(t *testing.T) {
	for _, tc := range []struct {
		name   string
		result activityJSON
		want   float64
	}{
		{"missing", nil, 0},
		{"successful", activityResult("tool-1", "File created successfully", false), 1},
		{"failed", activityResult("tool-1", "Permission denied", true), 0},
		{"interrupted", func() activityJSON {
			r := activityResult("tool-1", "File created successfully", false)
			r["toolUseResult"] = activityJSON{"interrupted": true}
			return r
		}(), 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			call := activityAssistant("msg-1", "req-1", []any{activityTool("tool-1", "Write", activityJSON{"file_path": "/project/a.go", "content": "a"})}, activityJSON{"input_tokens": 100})
			entries := []activityJSON{call, call}
			if tc.result != nil {
				entries = append(entries, tc.result, tc.result, call, tc.result)
			}
			r := activityParse(t, entries...)
			if got := activitySum(r, claudeLinesOfCodeMetric, "added"); got != tc.want {
				t.Errorf("added = %v, want %v", got, tc.want)
			}
			if got := activitySum(r, ClaudeTokenUsageMetric, "input"); got != 100 {
				t.Errorf("duplicate usage: got %v, want 100", got)
			}
		})
	}
}

func TestClaudeActivityRecordedEdits(t *testing.T) {
	for _, tc := range []struct {
		name, tool     string
		input, result  activityJSON
		added, removed float64
	}{
		{"write_create", "Write", activityJSON{"content": "a\nb\n"}, activityJSON{"type": "create", "originalFile": nil}, 2, 0},
		{"write_overwrite", "Write", activityJSON{"content": "keep\nnew\nkeep\n"}, activityJSON{"type": "update", "originalFile": "keep\nold\nkeep\n"}, 1, 1},
		{"write_unknown_original", "Write", activityJSON{"content": "a\nb\n"}, activityJSON{"type": "update"}, 0, 0},
		{"replace_all_patch", "Edit", activityJSON{"old_string": "old", "new_string": "new", "replace_all": true}, activityJSON{"structuredPatch": []any{activityJSON{"lines": []string{" context", "-old", "+new", " more", "-old", "+new"}}}}, 2, 2},
		{"replace_all_missing_patch", "Edit", activityJSON{"old_string": "old", "new_string": "new", "replace_all": true}, nil, 0, 0},
		{"no_change", "Edit", activityJSON{"old_string": "same\n", "new_string": "same\n"}, nil, 0, 0},
		{"multiedit", "MultiEdit", activityJSON{"edits": []any{activityJSON{"old_string": "line1\nline2", "new_string": "line1\nline2\nline3"}, activityJSON{"old_string": "foo", "new_string": ""}}}, nil, 1, 1},
		{"wrong_file_patch", "Edit", activityJSON{"old_string": "old", "new_string": "new"}, activityJSON{"filePath": "/different/file.go", "structuredPatch": []any{activityJSON{"lines": []string{"-old", "+new"}}}}, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.input["file_path"] = "/project/file.go"
			result := activityResult("tool-1", "The file has been updated successfully", false)
			result["toolUseResult"] = tc.result
			r := activityParse(t, activityAssistant("msg", "req", []any{activityTool("tool-1", tc.tool, tc.input)}, nil), result)
			if a, d := activitySum(r, claudeLinesOfCodeMetric, "added"), activitySum(r, claudeLinesOfCodeMetric, "removed"); a != tc.added || d != tc.removed {
				t.Errorf("got +%v/-%v, want +%v/-%v", a, d, tc.added, tc.removed)
			}
		})
	}
}

func TestClaudeActivityToolResultContentBlocks(t *testing.T) {
	result := activityResult("tool-1", "", false)
	result["message"].(activityJSON)["content"].([]any)[0].(activityJSON)["content"] = []any{activityJSON{"type": "text", "text": "[main (root-commit) abc1234] test"}, activityJSON{"type": "image", "source": activityJSON{"type": "base64", "data": ""}}}
	r := activityParse(t, activityAssistant("msg", "req", []any{activityTool("tool-1", "Bash", activityJSON{"command": "git -c user.name=test commit -m test"})}, nil), result)
	if got := activitySum(r, claudeCommitMetric, ""); got != 1 {
		t.Errorf("commit count = %v, want 1", got)
	}
	for _, log := range r.Logs {
		if log.LogAttributes["message.role"] == "tool_result" && strings.Contains(log.Body, "abc1234") {
			return
		}
	}
	t.Fatal("array tool result missing from transcript")
}

func TestClaudeActivityCommitCommands(t *testing.T) {
	for _, command := range []string{
		"git add . && git commit -m 'test'", "git -C '/some dir' -c user.name=Test commit -m test",
		"git --git-dir=/repo/.git --work-tree=/repo commit -m test", "'git' \"commit\" -m test",
	} {
		t.Run(command, func(t *testing.T) {
			r := activityParse(t, activityAssistant("msg", "req", []any{activityTool("tool", "Bash", activityJSON{"command": command})}, nil), activityResult("tool", "[main abc1234] one\n[main def5678] two\n[main abc1234] repeated", false))
			if got := activitySum(r, claudeCommitMetric, ""); got != 2 {
				t.Errorf("commits = %v, want 2", got)
			}
		})
	}
	for _, command := range []string{"echo 'git commit'", "cat <<'EOF'\ngit commit\nEOF", "git status # git commit", "f() { git commit; }", "git log --format='git commit'", "echo git commit", "git status; echo 'unterminated"} {
		t.Run(command, func(t *testing.T) {
			r := activityParse(t, activityAssistant("msg", "req", []any{activityTool("tool", "Bash", activityJSON{"command": command})}, nil), activityResult("tool", "[main abc1234] text", false))
			if got := activitySum(r, claudeCommitMetric, ""); got != 0 {
				t.Errorf("commits = %v, want 0", got)
			}
		})
	}
}

func TestClaudeActivityPRIdentityAndTimeBounds(t *testing.T) {
	r := activityParse(t,
		activityJSON{"type": "pr-link", "timestamp": "invalid", "prNumber": 7, "prRepository": "acme/app"},
		activityJSON{"type": "pr-link", "timestamp": "2026-09-01T12:00:00Z", "prNumber": 7, "prRepository": "acme/app"},
		activityJSON{"type": "pr-link", "timestamp": "2026-09-02T12:00:00Z", "prNumber": 7, "prRepository": "other/app"},
		activityJSON{"type": "pr-link", "timestamp": "2026-09-03T12:00:00Z", "prNumber": 7, "prUrl": "https://github.com/acme/app/pull/7"},
	)
	if got := activitySum(r, claudePullRequestMetric, ""); got != 2 {
		t.Errorf("PR count = %v, want 2", got)
	}
	if r.FirstTime.Format(time.RFC3339) != "2026-09-01T12:00:00Z" || r.LastTime.Format(time.RFC3339) != "2026-09-02T12:00:00Z" {
		t.Errorf("wrong PR-only bounds: %s to %s", r.FirstTime, r.LastTime)
	}
	repos := map[string]bool{}
	for _, m := range r.Metrics {
		if m.MetricName == claudePullRequestMetric {
			repos[m.Attributes["repository"]] = true
		}
	}
	if !repos["acme/app"] || !repos["other/app"] {
		t.Fatalf("PR repositories: %v", repos)
	}
}

func TestClaudeActivitySessionAndActiveTime(t *testing.T) {
	message := activityAssistant("msg", "req", []any{activityJSON{"type": "text", "text": "hello"}}, nil)
	message["gitBranch"] = "feature"
	r := activityParse(t, message,
		activityJSON{"type": "system", "subtype": "turn_duration", "timestamp": "2026-09-02T00:01:00Z", "durationMs": 30000},
		activityJSON{"type": "system", "subtype": "turn_duration", "timestamp": "2026-09-02T00:02:00Z", "durationMs": 15000},
		activityJSON{"type": "system", "subtype": "turn_duration", "timestamp": "2026-09-02T00:03:00Z", "durationMs": -1},
	)
	if got := activitySum(r, claudeSessionMetric, ""); got != 1 {
		t.Errorf("session count = %v", got)
	}
	if got := activitySum(r, claudeActiveTimeMetric, "cli"); got != 45 {
		t.Errorf("CLI active seconds = %v, want 45", got)
	}
	for _, m := range r.Metrics {
		if m.Attributes["git_branch"] != "feature" {
			t.Errorf("missing branch: %v", m.Attributes)
		}
		if m.MetricName == claudeActiveTimeMetric && m.MetricUnit != "s" {
			t.Errorf("active time unit = %q", m.MetricUnit)
		}
	}
	for _, log := range r.Logs {
		if log.LogAttributes["git_branch"] != "feature" {
			t.Errorf("missing transcript branch: %v", log.LogAttributes)
		}
		if log.LogAttributes["pr_number"] != "" {
			t.Error("session transcript should not inherit an unrelated PR")
		}
	}
}

func TestClaudeActivityDiffBoundaries(t *testing.T) {
	for _, tc := range []struct {
		old, new       string
		added, removed int
	}{
		{"", "a\nb\n", 2, 0}, {"a\n", "", 0, 1}, {"a\nold\nkeep\nold2\nz", "a\nnew\nkeep\nnew2\nz", 2, 2}, {"same\n", "same", 0, 0}, {"\n", "", 0, 1},
	} {
		a, r, ok := changedLines(tc.old, tc.new)
		if !ok || a != tc.added || r != tc.removed {
			t.Errorf("diff %q -> %q: %d/%d/%v", tc.old, tc.new, a, r, ok)
		}
	}
	if _, _, ok := changedLines(strings.Repeat("a\n", 2500), strings.Repeat("b\n", 2500)); ok {
		t.Error("large diff should be omitted")
	}
}

func TestCodexRepositoryFromSessionMetadata(t *testing.T) {
	for _, remote := range []string{"https://github.com/acme/project.git", "git@github.com:acme/project.git", "https://example-token@github.com/acme/project.git"} {
		t.Run(remote, func(t *testing.T) {
			file := activityFile(t,
				activityJSON{"type": "session_meta", "timestamp": "2026-09-01T12:00:00Z", "payload": activityJSON{"id": "codex-session", "cwd": "/missing/unrelated", "model": "gpt-4o", "git": activityJSON{"repository_url": remote}}},
				activityJSON{"type": "event_msg", "timestamp": "2026-09-01T12:01:00Z", "payload": activityJSON{"type": "token_count", "info": activityJSON{"total_token_usage": activityJSON{"input_tokens": 100, "output_tokens": 50}}}},
			)
			r, err := NewCodexParser().ParseFile(context.Background(), file)
			if err != nil {
				t.Fatal(err)
			}
			if len(r.Metrics) == 0 {
				t.Fatal("no Codex usage metrics")
			}
			for _, m := range r.Metrics {
				if m.Attributes["repository"] != "acme/project" {
					t.Errorf("repository = %q", m.Attributes["repository"])
				}
			}
		})
	}
}
