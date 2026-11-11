package watcher

import (
	"context"
	"strings"
	"testing"

	"github.com/tobilg/ai-observer/internal/importer"
	"github.com/tobilg/ai-observer/internal/storage"
)

// Watch stores typed prompts, whose content is a string, like imports do
func TestClaudeIncrementalParserStoresTypedPrompts(t *testing.T) {
	line := `{"parentUuid":null,"isSidechain":false,"promptId":"p1","type":"user","message":{"role":"user","content":"Fix the flaky test"},"uuid":"u1","timestamp":"2026-11-08T17:58:24.750Z","origin":{"kind":"human"},"sessionId":"s1","version":"2.1.319","cwd":"/home/user/project"}`
	path := writeWatcherTestFile(t, line+"\n")
	state := &storage.ImportState{Source: string(importer.SourceClaude), FilePath: path}
	result, err := (&claudeIncrementalParser{}).ParseIncremental(context.Background(), path, state)
	if err != nil {
		t.Fatal(err)
	}
	var prompts []string
	for _, l := range result.Logs {
		if l.LogAttributes["message.role"] == "user" {
			prompts = append(prompts, l.Body+"|"+l.LogAttributes["message.uuid"])
		}
	}
	if strings.Join(prompts, ",") != "Fix the flaky test|u1" {
		t.Fatalf("prompts %q", prompts)
	}
}
