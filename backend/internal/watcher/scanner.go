package watcher

import (
	"os"
	"path/filepath"
	"time"

	"github.com/tobilg/ai-observer/internal/logger"
)

// addDirectoryRecursive adds a directory and all subdirectories to the fsnotify watcher
func (w *Watcher) addDirectoryRecursive(dir string) error {
	return filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip errors, continue walking
		}
		if info.IsDir() {
			if addErr := w.fsWatcher.Add(path); addErr != nil {
				logger.Debug("Failed to watch subdirectory", "path", path, "error", addErr)
			}
		}
		return nil
	})
}

// pollMissingDirectories periodically checks for missing directories and adds them when found
func (w *Watcher) pollMissingDirectories() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-w.ctx.Done():
			return
		case <-ticker.C:
			for _, tw := range w.tools {
				var stillMissing []string
				for _, dir := range tw.missingDirs {
					if _, err := os.Stat(dir); err == nil {
						// Directory now exists
						logger.Info("Directory appeared, adding to watcher",
							"tool", string(tw.source), "dir", shortenPath(dir))
						tw.watchDirs = append(tw.watchDirs, dir)
						if err := w.addDirectoryRecursive(dir); err != nil {
							logger.Warn("Failed to add new directory to watcher", "dir", dir, "error", err)
						}
					} else {
						stillMissing = append(stillMissing, dir)
					}
				}
				tw.missingDirs = stillMissing
			}
		}
	}
}

// initialScan walks all watched directories and processes existing files
func (w *Watcher) initialScan(backfill bool) {
	for _, tw := range w.tools {
		for _, dir := range tw.watchDirs {
			err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
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
						if err := w.store.SetWatchFields(w.ctx, string(tw.source), path, fileSize, 0, ""); err != nil {
							logger.Error("Failed to set initial watch fields", "path", path, "error", err)
						}
						logger.Debug("Skipping existing file (no backfill)", "path", shortenPath(path), "size", fileSize)
					} else {
						// Backfill from beginning
						logger.Debug("Backfilling file", "path", shortenPath(path))
						w.processFile(path)
					}
					return nil
				}

				// State exists from a prior full import (ByteOffset==0, FileHash!="")
				if state.ByteOffset == 0 && state.FileHash != "" {
					if err := w.store.SetWatchFields(w.ctx, string(tw.source), path, fileSize, state.RecordCount, ""); err != nil {
						logger.Error("Failed to update watch fields for imported file", "path", path, "error", err)
					}
					logger.Debug("Resuming from end of previously imported file", "path", shortenPath(path), "size", fileSize)
					return nil
				}

				// State exists with ByteOffset > 0 - check if file grew
				if state.ByteOffset > 0 && fileSize > state.ByteOffset {
					logger.Debug("File grew since last check, processing", "path", shortenPath(path),
						"lastOffset", state.ByteOffset, "currentSize", fileSize)
					w.processFile(path)
				}

				return nil
			})
			if err != nil {
				logger.Error("Error during initial scan", "dir", dir, "error", err)
			}
		}
	}
}

// isDir returns true if path is a directory
func isDir(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.IsDir()
}
