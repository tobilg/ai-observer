package importer

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// Session metadata found late in a file (here the git branch) attributes the
// records before it, although the file is read only once
func TestClaudeParseFileAttributesEarlierRecordsWithLateMetadata(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s1.jsonl")
	content := `{"type":"assistant","timestamp":"2025-01-02T10:00:00.000Z","sessionId":"s1","requestId":"req-1","message":{"id":"msg-1","model":"claude-sonnet-4-20250514","role":"assistant","type":"message","content":[{"type":"text","text":"hi"}],"usage":{"input_tokens":100,"output_tokens":50}}}
{"type":"user","timestamp":"2025-01-02T10:01:00.000Z","sessionId":"s1","gitBranch":"feature/late","message":{"role":"user","content":[{"type":"text","text":"thanks"}]}}
{"type":"pr-link","timestamp":"2025-01-02T10:02:00.000Z","sessionId":"s1","prNumber":7,"prUrl":"https://github.com/acme/widgets/pull/7","prRepository":"acme/widgets"}
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := NewClaudeParser().ParseFile(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Logs) == 0 {
		t.Fatal("no logs parsed")
	}
	for _, log := range result.Logs {
		if got := log.LogAttributes["git_branch"]; got != "feature/late" {
			t.Errorf("log %q git_branch = %q, want feature/late", log.Body, got)
		}
	}
	tokens := 0
	for _, m := range result.Metrics {
		if m.MetricName == ClaudeTokenUsageMetric {
			tokens++
			if got := m.Attributes["git_branch"]; got != "feature/late" {
				t.Errorf("token metric git_branch = %q, want feature/late", got)
			}
		}
	}
	if tokens == 0 {
		t.Error("no token metrics parsed")
	}
	if len(result.Metrics) == 0 || result.Metrics[0].MetricName != claudePullRequestMetric {
		t.Errorf("first metric = %+v, want the PR metric", result.Metrics[0])
	} else if got := result.Metrics[0].Attributes["repository"]; got != "acme/widgets" {
		t.Errorf("PR metric repository = %q, want acme/widgets", got)
	}
}
