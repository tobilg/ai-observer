package watcher

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/tobilg/ai-observer/internal/logger"
	"github.com/tobilg/ai-observer/internal/websocket"
)

// Config configures the file watcher
type Config struct {
	Tools      []string // Tool names to watch (empty = all)
	DebounceMs int      // Override default debounce (0 = use per-tool defaults)
	Backfill   bool     // Import existing data on startup
}

// Watcher monitors AI tool session files and imports new data incrementally
type Watcher struct {
	store        Store
	hub          *websocket.Hub
	fsWatcher    *fsnotify.Watcher
	tools        []*ToolWatcher
	timers       map[string]*time.Timer
	timersMu     sync.Mutex
	processingWg sync.WaitGroup
	stopping     bool
	ctx          context.Context
	cancel       context.CancelFunc
	wg           sync.WaitGroup
	backfill     bool
}

// New creates a new file watcher
func New(store Store, hub *websocket.Hub, cfg Config) *Watcher {
	w := &Watcher{
		store:    store,
		hub:      hub,
		timers:   make(map[string]*time.Timer),
		backfill: cfg.Backfill,
	}

	// Build tool watchers based on config
	toolFactories := map[string]func() *ToolWatcher{
		"claude-code": newClaudeToolWatcher,
		"codex":       newCodexToolWatcher,
		"gemini":      newGeminiToolWatcher,
	}

	if len(cfg.Tools) == 0 {
		// Watch all tools
		for _, factory := range toolFactories {
			tw := factory()
			if cfg.DebounceMs > 0 {
				tw.debounceMs = cfg.DebounceMs
			}
			w.tools = append(w.tools, tw)
		}
	} else {
		for _, name := range cfg.Tools {
			if factory, ok := toolFactories[name]; ok {
				tw := factory()
				if cfg.DebounceMs > 0 {
					tw.debounceMs = cfg.DebounceMs
				}
				w.tools = append(w.tools, tw)
			} else {
				logger.Warn("Unknown tool for watch mode, skipping", "tool", name)
			}
		}
	}

	return w
}

// Start begins watching for file changes
func (w *Watcher) Start(ctx context.Context) error {
	w.ctx, w.cancel = context.WithCancel(ctx)

	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("creating fsnotify watcher: %w", err)
	}
	w.fsWatcher = fsw

	// Detect tools and print report
	logger.Info("File watcher starting...")
	for _, tw := range w.tools {
		result := tw.Detect()
		toolName := string(result.Source)
		for _, p := range result.Paths {
			logger.Info(fmt.Sprintf("  [%s] Watching %s (found)", toolName, shortenPath(p)))
		}
		for _, p := range result.Missing {
			logger.Info(fmt.Sprintf("  [%s] %s not found - will poll for directory creation", toolName, shortenPath(p)))
		}
		if !result.Found && len(result.Missing) == 0 {
			logger.Info(fmt.Sprintf("  [%s] No directories configured", toolName))
		}
	}

	// Add existing directories to fsnotify
	for _, tw := range w.tools {
		for _, dir := range tw.watchDirs {
			if err := w.addDirectoryRecursive(dir); err != nil {
				logger.Warn("Failed to add directory to watcher", "dir", dir, "error", err)
			}
		}
	}

	// Initial scan
	w.initialScan(w.backfill)

	// Start event loop
	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		w.run()
	}()

	// Start polling for missing directories
	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		w.pollMissingDirectories()
	}()

	return nil
}

// Stop stops the watcher gracefully
func (w *Watcher) Stop() {
	w.timersMu.Lock()
	w.stopping = true
	for filePath, timer := range w.timers {
		timer.Stop()
		delete(w.timers, filePath)
	}
	w.timersMu.Unlock()

	if w.cancel != nil {
		w.cancel()
	}
	if w.fsWatcher != nil {
		w.fsWatcher.Close()
	}
	w.wg.Wait()
	w.processingWg.Wait()
	logger.Info("File watcher stopped")
}

// run is the main event loop
func (w *Watcher) run() {
	for {
		select {
		case <-w.ctx.Done():
			return
		case event, ok := <-w.fsWatcher.Events:
			if !ok {
				return
			}
			w.handleEvent(event)
		case err, ok := <-w.fsWatcher.Errors:
			if !ok {
				return
			}
			logger.Error("File watcher error", "error", err)
		}
	}
}

func (w *Watcher) handleEvent(event fsnotify.Event) {
	// New directory created - watch it recursively
	if event.Has(fsnotify.Create) {
		if isDir(event.Name) {
			if err := w.addDirectoryRecursive(event.Name); err != nil {
				logger.Debug("Failed to watch new directory", "dir", event.Name, "error", err)
			}
			return
		}
	}

	// File created or modified - check if any tool matches
	if event.Has(fsnotify.Create) || event.Has(fsnotify.Write) {
		for _, tw := range w.tools {
			if tw.MatchesFile(event.Name) {
				w.scheduleProcess(event.Name, tw)
				return
			}
		}
	}
}

func (w *Watcher) scheduleProcess(filePath string, tw *ToolWatcher) {
	w.timersMu.Lock()
	defer w.timersMu.Unlock()

	if w.stopping {
		return
	}

	// Cancel existing timer for this file
	if timer, ok := w.timers[filePath]; ok {
		timer.Stop()
	}

	// Schedule debounced processing
	w.timers[filePath] = time.AfterFunc(time.Duration(tw.debounceMs)*time.Millisecond, func() {
		w.timersMu.Lock()
		if w.stopping {
			delete(w.timers, filePath)
			w.timersMu.Unlock()
			return
		}
		delete(w.timers, filePath)
		w.processingWg.Add(1)
		w.timersMu.Unlock()
		defer w.processingWg.Done()

		w.processFile(filePath)
	})
}

// findToolForFile returns the ToolWatcher that owns a given file path
func (w *Watcher) findToolForFile(filePath string) *ToolWatcher {
	for _, tw := range w.tools {
		if !tw.MatchesFile(filePath) {
			continue
		}
		// Check if the file is under one of this tool's watch dirs
		for _, dir := range tw.watchDirs {
			if isUnder(filePath, dir) {
				return tw
			}
		}
	}
	return nil
}

// shortenPath replaces home dir with ~
func shortenPath(path string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	if strings.HasPrefix(path, home) {
		return "~" + path[len(home):]
	}
	return path
}

// isUnder checks if filePath is under dir
func isUnder(filePath, dir string) bool {
	rel, err := filepath.Rel(dir, filePath)
	if err != nil {
		return false
	}
	return !strings.HasPrefix(rel, "..")
}
