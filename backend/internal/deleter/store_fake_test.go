package deleter

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/tobilg/ai-observer/internal/storage"
)

// chunkStore deletes 1 log per chunk and fails the chunk starting at
// failAt. Calls it does not override panic (the embedded nil Store).
type chunkStore struct {
	Store
	failAt  time.Time
	deleted []time.Time
}

func (s *chunkStore) DeleteAllInRange(_ context.Context, from, _ time.Time, _ string) (*storage.DeleteSummary, error) {
	if from.Equal(s.failAt) {
		return nil, errors.New("disk full")
	}
	s.deleted = append(s.deleted, from)
	return &storage.DeleteSummary{LogCount: 1}, nil
}

// A failing chunk stops a chunked deletion; the summary counts the chunks
// deleted before it, and later chunks are not attempted
func TestExecuteChunkedStopsAtFailingChunk(t *testing.T) {
	start := time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC)
	store := &chunkStore{failAt: start.Add(48 * time.Hour)}
	summary, err := ExecuteChunked(context.Background(), store,
		Options{Scope: ScopeAll, From: start, To: start.Add(96 * time.Hour)}, 24*time.Hour)
	if err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Fatalf("error %v, want the store's", err)
	}
	if summary.LogCount != 2 || len(store.deleted) != 2 {
		t.Fatalf("summary %+v after chunks %v, want the 2 chunks before the failure", summary, store.deleted)
	}
}

// failingCounter fails the preview's count
type failingCounter struct{ Store }

func (failingCounter) CountAllInRange(context.Context, time.Time, time.Time, string) (*storage.DeleteSummary, error) {
	return nil, errors.New("database is locked")
}

// A failed preview deletes nothing and asks nothing
func TestRunPreviewFailure(t *testing.T) {
	var out bytes.Buffer
	err := Run(context.Background(), failingCounter{}, Options{Scope: ScopeAll, Out: &out, In: strings.NewReader("y\n")})
	if err == nil || !strings.Contains(err.Error(), "preview failed") {
		t.Fatalf("error %v, want the preview's", err)
	}
	if strings.Contains(out.String(), "Continue?") {
		t.Fatalf("asked for confirmation after a failed preview: %s", out.String())
	}
}
