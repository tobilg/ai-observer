package restore

import (
	"context"

	"github.com/tobilg/ai-observer/internal/redact"
	"github.com/tobilg/ai-observer/internal/storage"
)

// Store is the storage a restore writes to: storage.DuckDBStore
type Store interface {
	InspectRestore(ctx context.Context, files storage.RestoreFiles) (*storage.RestoreInventory, error)
	CountRestoreConflicts(ctx context.Context, inv *storage.RestoreInventory) (*storage.DeleteSummary, error)
	Restore(ctx context.Context, files storage.RestoreFiles, inv *storage.RestoreInventory, replace bool) (*storage.RestoreResult, error)
	// Checkpoint returns the blocks of replaced records for reuse
	Checkpoint(ctx context.Context) error
	// Redactor redacts the restored text
	Redactor() *redact.Redactor
}

var _ Store = (*storage.DuckDBStore)(nil)
