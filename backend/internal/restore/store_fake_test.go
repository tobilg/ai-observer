package restore

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tobilg/ai-observer/internal/exporter"
	"github.com/tobilg/ai-observer/internal/redact"
	"github.com/tobilg/ai-observer/internal/storage"
)

// conflictStore holds records of the export's range. Restore is not
// overridden: calling it panics (the embedded nil Store).
type conflictStore struct{ Store }

func (conflictStore) InspectRestore(context.Context, storage.RestoreFiles) (*storage.RestoreInventory, error) {
	now := time.Now()
	return &storage.RestoreInventory{Rows: map[string]int64{"otel_logs": 3}, From: now.Add(-time.Hour), To: now, Services: []string{"claude-code"}}, nil
}

func (conflictStore) Redactor() *redact.Redactor { return nil }

func (conflictStore) CountRestoreConflicts(context.Context, *storage.RestoreInventory) (*storage.DeleteSummary, error) {
	return &storage.DeleteSummary{LogCount: 2}, nil
}

// Records of the export's range already stored refuse a restore without
// --replace before anything is written, also without a confirmation
func TestRunRefusesConflictWithoutReplace(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{exporter.TracesFile, exporter.LogsFile, exporter.MetricsFile} {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	var out bytes.Buffer
	err := Run(context.Background(), conflictStore{}, Options{Path: dir, SkipConfirm: true, Out: &out})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("error %v, want ErrConflict", err)
	}
	if !strings.Contains(out.String(), "--replace") {
		t.Errorf("output does not name --replace:\n%s", out.String())
	}
}
