package watcher

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/tobilg/ai-observer/internal/importer"
)

// ToolWatcher holds configuration for watching a specific tool
type ToolWatcher struct {
	source      importer.SourceType
	watchDirs   []string
	missingDirs []string
	fileMatch   func(string) bool
	parser      IncrementalParser
	debounceMs  int
}

// DetectionResult reports what was found for a tool
type DetectionResult struct {
	Source  importer.SourceType
	Found   bool
	Paths   []string
	Missing []string
}

func (t *ToolWatcher) Detect() DetectionResult {
	result := DetectionResult{Source: t.source}
	allDirs := make([]string, 0, len(t.watchDirs)+len(t.missingDirs))
	allDirs = append(allDirs, t.watchDirs...)
	allDirs = append(allDirs, t.missingDirs...)
	t.watchDirs = nil
	t.missingDirs = nil
	for _, dir := range allDirs {
		if _, err := os.Stat(dir); err == nil {
			t.watchDirs = append(t.watchDirs, dir)
			result.Paths = append(result.Paths, dir)
			result.Found = true
		} else {
			t.missingDirs = append(t.missingDirs, dir)
			result.Missing = append(result.Missing, dir)
		}
	}
	return result
}

func (t *ToolWatcher) MatchesFile(path string) bool {
	return t.fileMatch(path)
}

func newClaudeToolWatcher() *ToolWatcher {
	paths := importer.GetClaudeWatchPaths()
	// If no paths found from config, use default paths for polling
	if len(paths) == 0 {
		homeDir, _ := os.UserHomeDir()
		if homeDir != "" {
			paths = []string{
				filepath.Join(homeDir, ".config", "claude", "projects"),
				filepath.Join(homeDir, ".claude", "projects"),
			}
		}
	}
	return &ToolWatcher{
		source:     importer.SourceClaude,
		watchDirs:  paths,
		fileMatch:  func(path string) bool { return strings.HasSuffix(path, ".jsonl") },
		parser:     &claudeIncrementalParser{},
		debounceMs: 200,
	}
}

func newCodexToolWatcher() *ToolWatcher {
	path := importer.GetCodexWatchPath()
	var paths []string
	if path != "" {
		paths = []string{path}
	} else {
		homeDir, _ := os.UserHomeDir()
		if homeDir != "" {
			paths = []string{filepath.Join(homeDir, ".codex", "sessions")}
		}
	}
	return &ToolWatcher{
		source:     importer.SourceCodex,
		watchDirs:  paths,
		fileMatch:  func(path string) bool { return strings.HasSuffix(path, ".jsonl") },
		parser:     &codexIncrementalParser{},
		debounceMs: 200,
	}
}

func newGeminiToolWatcher() *ToolWatcher {
	path := importer.GetGeminiWatchPath()
	var paths []string
	if path != "" {
		paths = []string{path}
	} else {
		homeDir, _ := os.UserHomeDir()
		if homeDir != "" {
			paths = []string{filepath.Join(homeDir, ".gemini", "tmp")}
		}
	}
	return &ToolWatcher{
		source:    importer.SourceGemini,
		watchDirs: paths,
		fileMatch: func(path string) bool {
			base := filepath.Base(path)
			return strings.HasPrefix(base, "session-") && strings.HasSuffix(base, ".json")
		},
		parser:     &geminiIncrementalParser{},
		debounceMs: 500,
	}
}
