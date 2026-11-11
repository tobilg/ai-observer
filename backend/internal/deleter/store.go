package deleter

import (
	"context"
	"time"

	"github.com/tobilg/ai-observer/internal/storage"
)

// Counter counts the records a deletion would delete (Preview):
// storage.DuckDBStore
type Counter interface {
	CountLogsInRange(ctx context.Context, from, to time.Time, service string) (int64, error)
	CountMetricsInRange(ctx context.Context, from, to time.Time, service string) (int64, error)
	CountTracesInRange(ctx context.Context, from, to time.Time, service string) (traces int64, spans int64, err error)
	CountAllInRange(ctx context.Context, from, to time.Time, service string) (*storage.DeleteSummary, error)
}

// Store is the storage a deletion uses: storage.DuckDBStore
type Store interface {
	Counter
	DeleteLogsInRange(ctx context.Context, from, to time.Time, service string) (int64, error)
	DeleteMetricsInRange(ctx context.Context, from, to time.Time, service string) (int64, error)
	DeleteTracesInRange(ctx context.Context, from, to time.Time, service string) (int64, error)
	DeleteAllInRange(ctx context.Context, from, to time.Time, service string) (*storage.DeleteSummary, error)
	// Checkpoint and DatabaseSize report the space a deletion freed
	Checkpoint(ctx context.Context) error
	DatabaseSize(ctx context.Context) (storage.DatabaseSize, error)
}

var _ Store = (*storage.DuckDBStore)(nil)
