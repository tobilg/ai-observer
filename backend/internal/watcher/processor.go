package watcher

import (
	"os"

	"github.com/tobilg/ai-observer/internal/logger"
	"github.com/tobilg/ai-observer/internal/storage"
	"github.com/tobilg/ai-observer/internal/websocket"
)

// processFile finds the owning tool, parses incrementally, stores results, and broadcasts
func (w *Watcher) processFile(filePath string) {
	tw := w.findToolForFile(filePath)
	if tw == nil {
		logger.Debug("No tool watcher matched file", "path", filePath)
		return
	}

	// Load existing state
	state, err := w.store.GetImportState(w.ctx, string(tw.source), filePath)
	if err != nil {
		logger.Error("Failed to get import state", "path", filePath, "error", err)
		return
	}
	if state == nil {
		state = &storage.ImportState{
			Source:   string(tw.source),
			FilePath: filePath,
		}
	}

	// A file shorter than what was read was truncated or replaced, and an
	// import of a date range that left out later records did not read it to
	// the end: read it again from the start and replace its stored records.
	// Records an earlier version stored without the file's path are replaced
	// too (InsertWatcherBatch).
	replace := false
	if rewritten(tw, filePath, state) || startsOver(state) {
		logger.Info("Reading session file again from the start", "path", shortenPath(filePath))
		state = &storage.ImportState{Source: state.Source, FilePath: filePath, SourceTracked: state.SourceTracked}
		replace = true
	}

	// Parse incrementally
	result, err := tw.parser.ParseIncremental(w.ctx, filePath, state)
	if err != nil {
		if w.ctx.Err() != nil {
			logStoppedDuring(filePath)
			return
		}
		logger.Error("Failed to parse file incrementally", "path", filePath, "error", err)
		return
	}

	if result == nil {
		return
	}

	hasData := len(result.Logs) > 0 || len(result.Metrics) > 0 || len(result.Spans) > 0
	if !hasData && !replace {
		// Still update state (byte offset may have changed)
		if err := w.store.SetWatchFields(w.ctx, string(tw.source), filePath, state.ByteOffset, state.MessageCount, state.ParserState); err != nil {
			logger.Error("Failed to update watch fields", "path", filePath, "error", err)
		}
		return
	}

	// Store results and advance state in one transaction. If this fails, leave
	// the offset unchanged so the next file event can retry the same data.
	if err := w.store.InsertWatcherBatch(w.ctx, result.Logs, result.Metrics, result.Spans, state, replace); err != nil {
		if w.ctx.Err() != nil {
			logStoppedDuring(filePath)
			return
		}
		logger.Error("Failed to store watcher batch", "path", filePath, "error", err,
			"logs", len(result.Logs), "metrics", len(result.Metrics), "spans", len(result.Spans))
		return
	}

	// Broadcast via WebSocket if hub is available
	if w.hub != nil {
		if len(result.Logs) > 0 {
			w.hub.Broadcast(websocket.LogsStored(result.Logs))
		}
		if len(result.Metrics) > 0 {
			w.hub.Broadcast(websocket.MetricsStored(result.Metrics))
		}
		if len(result.Spans) > 0 {
			w.hub.Broadcast(websocket.SpansStored(result.Spans))
		}
	}

	logger.Debug("Processed file", "path", shortenPath(filePath),
		"tool", string(tw.source),
		"logs", len(result.Logs),
		"metrics", len(result.Metrics),
		"spans", len(result.Spans))
}

// startsOver reports whether an import of a date range left out records
// after the range, so watch has to read the whole file (ImportState.Partial)
func startsOver(state *storage.ImportState) bool {
	return state.Partial && state.ByteOffset == 0 && state.MessageCount == 0
}

// rewritten reports whether the file now ends before the position already
// read: fewer messages for parsers that resume by message count, fewer bytes
// for the others
func rewritten(tw *ToolWatcher, path string, state *storage.ImportState) bool {
	if counter, ok := tw.parser.(endMessageCounter); ok {
		count, err := counter.EndMessageCount(path)
		return err == nil && count < state.MessageCount
	}
	size := fileSize(path)
	return size >= 0 && size < state.ByteOffset
}

// fileSize returns the size of the file at path, or -1 if it cannot be read
func fileSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return -1
	}
	return info.Size()
}

// logStoppedDuring reports a file whose processing the watcher's shutdown
// interrupted. Nothing of it was stored, and its state was not advanced.
func logStoppedDuring(filePath string) {
	logger.Info("Stopped while processing a file; it is read again on the next start", "path", shortenPath(filePath))
}
