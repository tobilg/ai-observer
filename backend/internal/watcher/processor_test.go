package watcher

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tobilg/ai-observer/internal/api"
	"github.com/tobilg/ai-observer/internal/importer"
	"github.com/tobilg/ai-observer/internal/storage"
)

func TestProcessFileDoesNotAdvanceStateWhenBatchInsertFails(t *testing.T) {
	path := writeProcessorTestFile(t)
	store := &fakeWatcherStore{batchErr: errors.New("insert failed")}
	parser := &fakeIncrementalParser{
		result: &IncrementalResult{
			Logs: []api.LogRecord{{
				Timestamp:   time.Now(),
				ServiceName: importer.SourceCodex.ServiceName(),
				Body:        "hello",
			}},
		},
		offset: 123,
	}
	w := newProcessorTestWatcher(store, parser, filepath.Dir(path))

	w.processFile(path)

	if got := store.batchCalls.Load(); got != 1 {
		t.Fatalf("batch calls = %d, want 1", got)
	}
	if got := store.watchFieldCalls.Load(); got != 0 {
		t.Fatalf("SetWatchFields calls = %d, want 0", got)
	}
	if store.persistedState != nil {
		t.Fatal("state was persisted despite batch failure")
	}
}

func TestProcessFilePersistsStateThroughBatchOnSuccess(t *testing.T) {
	path := writeProcessorTestFile(t)
	store := &fakeWatcherStore{}
	parser := &fakeIncrementalParser{
		result: &IncrementalResult{
			Logs: []api.LogRecord{{
				Timestamp:   time.Now(),
				ServiceName: importer.SourceCodex.ServiceName(),
				Body:        "hello",
			}},
		},
		offset: 456,
	}
	w := newProcessorTestWatcher(store, parser, filepath.Dir(path))

	w.processFile(path)

	if got := store.batchCalls.Load(); got != 1 {
		t.Fatalf("batch calls = %d, want 1", got)
	}
	if got := store.watchFieldCalls.Load(); got != 0 {
		t.Fatalf("SetWatchFields calls = %d, want 0", got)
	}
	if store.persistedState == nil {
		t.Fatal("expected persisted state")
	}
	if got := store.persistedState.ByteOffset; got != 456 {
		t.Fatalf("persisted ByteOffset = %d, want 456", got)
	}
}

func TestWatcherStopCancelsPendingDebounceTimer(t *testing.T) {
	path := writeProcessorTestFile(t)
	store := &fakeWatcherStore{}
	parser := &fakeIncrementalParser{
		result: &IncrementalResult{
			Logs: []api.LogRecord{{
				Timestamp:   time.Now(),
				ServiceName: importer.SourceCodex.ServiceName(),
				Body:        "hello",
			}},
		},
		offset: 789,
	}
	w := newProcessorTestWatcher(store, parser, filepath.Dir(path))
	w.tools[0].debounceMs = 50

	w.scheduleProcess(path, w.tools[0])
	w.Stop()
	time.Sleep(75 * time.Millisecond)

	if got := parser.calls.Load(); got != 0 {
		t.Fatalf("parser calls = %d, want 0", got)
	}
}

func newProcessorTestWatcher(store *fakeWatcherStore, parser IncrementalParser, dir string) *Watcher {
	ctx, cancel := context.WithCancel(context.Background())
	return &Watcher{
		store:  store,
		timers: make(map[string]*time.Timer),
		ctx:    ctx,
		cancel: cancel,
		tools: []*ToolWatcher{{
			source:     importer.SourceCodex,
			watchDirs:  []string{dir},
			fileMatch:  func(path string) bool { return strings.HasSuffix(path, ".jsonl") },
			parser:     parser,
			debounceMs: 1,
		}},
	}
}

func writeProcessorTestFile(t *testing.T) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(path, []byte("test"), 0644); err != nil {
		t.Fatalf("writing test file: %v", err)
	}
	return path
}

type fakeIncrementalParser struct {
	result *IncrementalResult
	err    error
	offset int64
	calls  atomic.Int64
}

func (p *fakeIncrementalParser) ParseIncremental(_ context.Context, _ string, state *storage.ImportState) (*IncrementalResult, error) {
	p.calls.Add(1)
	if p.err != nil {
		return nil, p.err
	}
	state.ByteOffset = p.offset
	state.MessageCount = 1
	state.ParserState = `{"ok":true}`
	return p.result, nil
}

type fakeWatcherStore struct {
	state           *storage.ImportState
	persistedState  *storage.ImportState
	batchErr        error
	batchCalls      atomic.Int64
	watchFieldCalls atomic.Int64
}

func (s *fakeWatcherStore) GetImportState(_ context.Context, _, _ string) (*storage.ImportState, error) {
	if s.state == nil {
		return nil, nil
	}
	copy := *s.state
	return &copy, nil
}

func (s *fakeWatcherStore) SetWatchFields(_ context.Context, source, filePath string, byteOffset int64, messageCount int, parserState string) error {
	s.watchFieldCalls.Add(1)
	s.persistedState = &storage.ImportState{
		Source:       source,
		FilePath:     filePath,
		ByteOffset:   byteOffset,
		MessageCount: messageCount,
		ParserState:  parserState,
	}
	return nil
}

func (s *fakeWatcherStore) InsertWatcherBatch(_ context.Context, _ []api.LogRecord, _ []api.MetricDataPoint, _ []api.Span, state *storage.ImportState) error {
	s.batchCalls.Add(1)
	if s.batchErr != nil {
		return s.batchErr
	}
	copy := *state
	s.persistedState = &copy
	return nil
}
