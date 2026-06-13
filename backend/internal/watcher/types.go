package watcher

import (
	"context"

	"github.com/tobilg/ai-observer/internal/api"
	"github.com/tobilg/ai-observer/internal/storage"
)

// Store is the storage surface the watcher needs. DuckDBStore implements this,
// and tests can provide fakes for failure-path coverage.
type Store interface {
	GetImportState(ctx context.Context, source, filePath string) (*storage.ImportState, error)
	SetWatchFields(ctx context.Context, source, filePath string, byteOffset int64, messageCount int, parserState string) error
	InsertWatcherBatch(ctx context.Context, logs []api.LogRecord, metrics []api.MetricDataPoint, spans []api.Span, state *storage.ImportState) error
}

// IncrementalParser processes new data from a file since the last known state.
type IncrementalParser interface {
	ParseIncremental(ctx context.Context, filePath string, state *storage.ImportState) (*IncrementalResult, error)
}

// IncrementalResult contains the parsed records from an incremental parse.
type IncrementalResult struct {
	Logs    []api.LogRecord
	Metrics []api.MetricDataPoint
	Spans   []api.Span
}
