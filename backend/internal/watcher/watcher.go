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
	store     Store
	hub       *websocket.Hub
	fsWatcher *fsnotify.Watcher
	tools     []*ToolWatcher
	timers    map[string]*time.Timer
	timersMu  sync.Mutex
	// running holds the files being processed, mapped to whether another event
	// arrived meanwhile. Such an event re-runs the file after the current run
	// instead of starting a concurrent one, which would read the same
	// not-yet-committed offset and store the new records twice.
	running map[string]bool
	// dirsMu guards the tools' watchDirs and missingDirs, which
	// pollMissingDirectories updates while events are handled
	dirsMu       sync.RWMutex
	processingWg sync.WaitGroup
	stopping     bool
	ctx          context.Context
	cancel       context.CancelFunc
	wg           sync.WaitGroup
	backfill     bool
	// scanDone is closed when the startup scan of existing files has ended
	scanDone chan struct{}
	// scanPending holds the files changed during the startup scan, which are
	// processed when it ends. It is nil when no scan runs. Guarded by timersMu.
	scanPending map[string]*ToolWatcher
}

// toolFactories creates the watchers of the file-backed tools, in a fixed
// order so startup logs are deterministic
var toolFactories = []struct {
	name    string
	factory func() *ToolWatcher
}{
	{"claude-code", newClaudeToolWatcher},
	{"codex", newCodexToolWatcher},
	{"gemini", newGeminiToolWatcher},
}

// New creates a new file watcher
func New(store Store, hub *websocket.Hub, cfg Config) *Watcher {
	w := &Watcher{
		store:    store,
		hub:      hub,
		timers:   make(map[string]*time.Timer),
		backfill: cfg.Backfill,
	}

	// Build tool watchers based on config (empty = all tools)
	names := cfg.Tools
	if len(names) == 0 {
		for _, f := range toolFactories {
			names = append(names, f.name)
		}
	}
	for _, name := range names {
		tw := newToolWatcher(name)
		if tw == nil {
			logger.Warn("Unknown tool for watch mode, skipping", "tool", name)
			continue
		}
		if cfg.DebounceMs > 0 {
			tw.debounceMs = cfg.DebounceMs
		}
		w.tools = append(w.tools, tw)
	}

	return w
}

// newToolWatcher returns the watcher for the named tool, or nil
func newToolWatcher(name string) *ToolWatcher {
	for _, f := range toolFactories {
		if f.name == name {
			return f.factory()
		}
	}
	return nil
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
	watched := 0
	for _, tw := range w.tools {
		for _, dir := range tw.watchDirs {
			added, err := w.addDirectoryRecursive(dir)
			if err != nil {
				logger.Warn("Failed to add directory to watcher", "dir", dir, "error", err)
			}
			watched += added
		}
	}
	logger.Info("Watching directories", "count", watched)

	// Scan existing files and handle events in the background, so a long
	// backfill neither delays the API server nor blocks shutdown. Events are
	// read during the scan, so the kernel's event queue cannot overflow, but
	// the files they name are processed after it (scanExisting).
	w.startScan()
	w.wg.Add(2)
	go func() {
		defer w.wg.Done()
		w.run()
	}()
	go func() {
		defer w.wg.Done()
		w.scanExisting()
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
			if _, err := w.addDirectoryRecursive(event.Name); err != nil {
				logger.Debug("Failed to watch new directory", "dir", event.Name, "error", err)
			}
			return
		}
	}

	// File created or modified - check if any tool matches
	if event.Has(fsnotify.Create) || event.Has(fsnotify.Write) {
		// Claude Code and Codex both write .jsonl files, so match by directory too
		if tw := w.findToolForFile(event.Name); tw != nil {
			w.scheduleProcess(event.Name, tw)
		}
	}
}

func (w *Watcher) scheduleProcess(filePath string, tw *ToolWatcher) {
	w.timersMu.Lock()
	defer w.timersMu.Unlock()

	if w.stopping {
		return
	}
	if w.scanPending != nil {
		w.scanPending[filePath] = tw
		return
	}

	// Cancel existing timer for this file
	if timer, ok := w.timers[filePath]; ok {
		timer.Stop()
	}

	// Schedule debounced processing
	var timer *time.Timer
	timer = time.AfterFunc(time.Duration(tw.debounceMs)*time.Millisecond, func() {
		w.timersMu.Lock()
		if w.timers[filePath] == timer {
			delete(w.timers, filePath)
		}
		w.timersMu.Unlock()
		w.processExclusive(filePath)
	})
	w.timers[filePath] = timer
}

// processExclusive processes filePath unless it is being processed already,
// in which case it is processed once more after the current run. Concurrent
// runs would read the same not-yet-committed offset and store the new records
// twice. Nothing is processed once the watcher is stopping.
func (w *Watcher) processExclusive(filePath string) {
	w.timersMu.Lock()
	if w.stopping {
		w.timersMu.Unlock()
		return
	}
	if _, ok := w.running[filePath]; ok {
		w.running[filePath] = true // re-run once the current run is done
		w.timersMu.Unlock()
		return
	}
	if w.running == nil {
		w.running = make(map[string]bool)
	}
	w.running[filePath] = false
	w.processingWg.Add(1)
	w.timersMu.Unlock()
	defer w.processingWg.Done()

	for {
		w.processFile(filePath)

		w.timersMu.Lock()
		again := w.running[filePath] && !w.stopping
		if !again {
			delete(w.running, filePath)
			w.timersMu.Unlock()
			return
		}
		w.running[filePath] = false
		w.timersMu.Unlock()
	}
}

// startScan makes events wait for the startup scan
func (w *Watcher) startScan() {
	w.timersMu.Lock()
	defer w.timersMu.Unlock()
	w.scanPending = make(map[string]*ToolWatcher)
	w.scanDone = make(chan struct{})
}

// scanExisting runs the startup scan, then processes the files changed while
// it ran. Processed during the scan, a change could store a file's existing
// records before the scan records that watching starts at its end.
func (w *Watcher) scanExisting() {
	w.initialScan(w.backfill)

	w.timersMu.Lock()
	pending := w.scanPending
	w.scanPending = nil
	w.timersMu.Unlock()
	for filePath, tw := range pending {
		w.scheduleProcess(filePath, tw)
	}

	close(w.scanDone)
	if w.ctx.Err() == nil {
		logger.Info("Initial scan done; watching for changes", "changed_during_scan", len(pending))
	}
}

// waitInitialScan blocks until the startup scan of existing files has ended
func (w *Watcher) waitInitialScan() {
	<-w.scanDone
}

// findToolForFile returns the ToolWatcher that owns a given file path
func (w *Watcher) findToolForFile(filePath string) *ToolWatcher {
	w.dirsMu.RLock()
	defer w.dirsMu.RUnlock()
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

// toolWatchDirs returns a copy of the directories watched for tw
func (w *Watcher) toolWatchDirs(tw *ToolWatcher) []string {
	w.dirsMu.RLock()
	defer w.dirsMu.RUnlock()
	return append([]string(nil), tw.watchDirs...)
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
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
