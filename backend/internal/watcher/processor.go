package watcher

import (
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

	// Parse incrementally
	result, err := tw.parser.ParseIncremental(w.ctx, filePath, state)
	if err != nil {
		logger.Error("Failed to parse file incrementally", "path", filePath, "error", err)
		return
	}

	if result == nil {
		return
	}

	hasData := len(result.Logs) > 0 || len(result.Metrics) > 0 || len(result.Spans) > 0
	if !hasData {
		// Still update state (byte offset may have changed)
		if err := w.store.SetWatchFields(w.ctx, string(tw.source), filePath, state.ByteOffset, state.MessageCount, state.ParserState); err != nil {
			logger.Error("Failed to update watch fields", "path", filePath, "error", err)
		}
		return
	}

	// Store results and advance state in one transaction. If this fails, leave
	// the offset unchanged so the next file event can retry the same data.
	if err := w.store.InsertWatcherBatch(w.ctx, result.Logs, result.Metrics, result.Spans, state); err != nil {
		logger.Error("Failed to store watcher batch", "path", filePath, "error", err,
			"logs", len(result.Logs), "metrics", len(result.Metrics), "spans", len(result.Spans))
		return
	}

	// Broadcast via WebSocket if hub is available
	if w.hub != nil {
		if len(result.Logs) > 0 {
			w.hub.Broadcast(websocket.NewLogsMessage(result.Logs))
		}
		if len(result.Metrics) > 0 {
			w.hub.Broadcast(websocket.NewMetricsMessage(result.Metrics))
		}
		if len(result.Spans) > 0 {
			w.hub.Broadcast(websocket.NewTracesMessage(result.Spans))
		}
	}

	logger.Debug("Processed file", "path", shortenPath(filePath),
		"tool", string(tw.source),
		"logs", len(result.Logs),
		"metrics", len(result.Metrics),
		"spans", len(result.Spans))
}
