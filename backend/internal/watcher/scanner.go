package watcher

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/tobilg/ai-observer/internal/logger"
)

// addDirectoryRecursive adds a directory and all subdirectories to the
// fsnotify watcher and returns how many it added. A directory that cannot be
// watched is logged as a warning: changes in it would go unnoticed.
func (w *Watcher) addDirectoryRecursive(dir string) (int, error) {
	added := 0
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip errors, continue walking
		}
		if info.IsDir() {
			if addErr := w.fsWatcher.Add(path); addErr != nil {
				logger.Warn("Failed to watch directory; changes in it will not be imported",
					"path", shortenPath(path), "error", addErr, "hint", watchErrorHint(addErr))
			} else {
				added++
			}
		}
		return nil
	})
	return added, err
}

// watchErrorHint explains the errors that come from operating system limits.
// On macOS fsnotify opens every file in a watched directory (kqueue).
func watchErrorHint(err error) string {
	switch {
	case errors.Is(err, syscall.EMFILE), errors.Is(err, syscall.ENFILE):
		return "too many open files: raise the limit (ulimit -n) or watch fewer tools"
	case errors.Is(err, syscall.ENOSPC):
		return "watch limit reached: raise fs.inotify.max_user_watches"
	default:
		return ""
	}
}

// missingDirPollInterval is how often directories that do not exist yet are
// looked for (a variable for tests)
var missingDirPollInterval = 30 * time.Second

// pollMissingDirectories periodically checks for missing directories and adds them when found
func (w *Watcher) pollMissingDirectories() {
	ticker := time.NewTicker(missingDirPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-w.ctx.Done():
			return
		case <-ticker.C:
			w.dirsMu.Lock()
			for _, tw := range w.tools {
				var stillMissing []string
				for _, dir := range tw.missingDirs {
					if _, err := os.Stat(dir); err == nil {
						// Directory now exists
						logger.Info("Directory appeared, adding to watcher",
							"tool", string(tw.source), "dir", shortenPath(dir))
						tw.watchDirs = append(tw.watchDirs, dir)
						if _, err := w.addDirectoryRecursive(dir); err != nil {
							logger.Warn("Failed to add new directory to watcher", "dir", dir, "error", err)
						}
					} else {
						stillMissing = append(stillMissing, dir)
					}
				}
				tw.missingDirs = stillMissing
			}
			w.dirsMu.Unlock()
		}
	}
}

// initialScan walks all watched directories and processes existing files.
// It stops early when the watcher is stopped.
func (w *Watcher) initialScan(backfill bool) {
	for _, tw := range w.tools {
		for _, dir := range w.toolWatchDirs(tw) {
			if w.ctx.Err() != nil {
				return
			}
			err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
				if w.ctx.Err() != nil {
					return filepath.SkipAll
				}
				if err != nil {
					return nil
				}
				if info.IsDir() {
					return nil
				}
				if !tw.MatchesFile(path) {
					return nil
				}

				state, stateErr := w.store.GetImportState(w.ctx, string(tw.source), path)
				if stateErr != nil {
					logger.Error("Failed to get import state during initial scan", "path", path, "error", stateErr)
					return nil
				}

				fileSize := info.Size()

				if state == nil {
					// No prior state
					if !backfill {
						// Skip existing data - record current position
						if err := w.store.SetWatchFields(w.ctx, string(tw.source), path, fileSize, endMessageCount(tw, path, 0), ""); err != nil {
							logger.Error("Failed to set initial watch fields", "path", path, "error", err)
						}
						logger.Debug("Skipping existing file (no backfill)", "path", shortenPath(path), "size", fileSize)
					} else {
						// Backfill from beginning
						logger.Debug("Backfilling file", "path", shortenPath(path))
						w.processExclusive(path)
					}
					return nil
				}

				// An import of a date range that left out later records
				if startsOver(state) {
					w.processExclusive(path)
					return nil
				}

				// State from a full import of an earlier version, which did not
				// record how far it read (ByteOffset==0, FileHash!=""). Imports
				// now record it, and are resumed below like watch state.
				if state.ByteOffset == 0 && state.FileHash != "" {
					if err := w.store.SetWatchFields(w.ctx, string(tw.source), path, fileSize, endMessageCount(tw, path, state.RecordCount), ""); err != nil {
						logger.Error("Failed to update watch fields for imported file", "path", path, "error", err)
					}
					logger.Debug("Resuming from end of previously imported file", "path", shortenPath(path), "size", fileSize)
					return nil
				}

				// State exists with ByteOffset > 0 - check if the file grew,
				// or shrank because it was rewritten (processFile replaces it)
				if state.ByteOffset > 0 && fileSize != state.ByteOffset {
					logger.Debug("File changed since last check, processing", "path", shortenPath(path),
						"lastOffset", state.ByteOffset, "currentSize", fileSize)
					w.processExclusive(path)
				}

				return nil
			})
			if err != nil {
				logger.Error("Error during initial scan", "dir", dir, "error", err)
			}
		}
	}
}

// endMessageCount returns the message count at the current end of the file
// for parsers that resume by message count, and fallback for the others
func endMessageCount(tw *ToolWatcher, path string, fallback int) int {
	counter, ok := tw.parser.(endMessageCounter)
	if !ok {
		return fallback
	}
	count, err := counter.EndMessageCount(path)
	if err != nil {
		logger.Debug("Failed to count messages", "path", shortenPath(path), "error", err)
		return fallback
	}
	return count
}

// isDir returns true if path is a directory
func isDir(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.IsDir()
}
