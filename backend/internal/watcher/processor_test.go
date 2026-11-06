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

func (s *fakeWatcherStore) InsertWatcherBatch(_ context.Context, _ []api.LogRecord, _ []api.MetricDataPoint, _ []api.Span, state *storage.ImportState, _ bool) error {
	s.batchCalls.Add(1)
	if s.batchErr != nil {
		return s.batchErr
	}
	copy := *state
	s.persistedState = &copy
	return nil
}

// blockingParser blocks its first parse until release is closed and records
// how many parses ran at the same time
type blockingParser struct {
	fakeIncrementalParser
	started  chan struct{}
	release  chan struct{}
	active   atomic.Int64
	maxSeen  atomic.Int64
	blockOne atomic.Bool
}

func (p *blockingParser) ParseIncremental(ctx context.Context, path string, state *storage.ImportState) (*IncrementalResult, error) {
	n := p.active.Add(1)
	defer p.active.Add(-1)
	for {
		seen := p.maxSeen.Load()
		if n <= seen || p.maxSeen.CompareAndSwap(seen, n) {
			break
		}
	}
	if p.blockOne.CompareAndSwap(false, true) {
		close(p.started)
		<-p.release
	}
	return p.fakeIncrementalParser.ParseIncremental(ctx, path, state)
}

// Events for a file that is being processed must not start a second,
// concurrent run: it would parse from the same offset and store duplicates.
func TestWatcherDoesNotProcessSameFileConcurrently(t *testing.T) {
	path := writeProcessorTestFile(t)
	store := &fakeWatcherStore{}
	parser := &blockingParser{
		fakeIncrementalParser: fakeIncrementalParser{
			result: &IncrementalResult{Logs: []api.LogRecord{{Timestamp: time.Now(), ServiceName: "codex_cli_rs"}}},
			offset: 1,
		},
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	w := newProcessorTestWatcher(store, parser, filepath.Dir(path))

	w.scheduleProcess(path, w.tools[0])
	<-parser.started

	// Two more events while the first run is blocked
	w.scheduleProcess(path, w.tools[0])
	time.Sleep(10 * time.Millisecond)
	w.scheduleProcess(path, w.tools[0])
	time.Sleep(10 * time.Millisecond)

	close(parser.release)
	deadline := time.Now().Add(2 * time.Second)
	for parser.calls.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	time.Sleep(20 * time.Millisecond) // no further runs may follow
	w.Stop()

	if got := parser.maxSeen.Load(); got != 1 {
		t.Errorf("max concurrent parses = %d, want 1", got)
	}
	if got := parser.calls.Load(); got != 2 {
		t.Errorf("parses = %d, want 2 (the queued events coalesce into one re-run)", got)
	}
}
