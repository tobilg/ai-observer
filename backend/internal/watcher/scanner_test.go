package watcher

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/tobilg/ai-observer/internal/storage"
)

// firstCallBlockingParser blocks its first call until release is closed
type firstCallBlockingParser struct {
	started chan struct{}
	release chan struct{}
	calls   atomic.Int64
}

func (p *firstCallBlockingParser) ParseIncremental(_ context.Context, _ string, state *storage.ImportState) (*IncrementalResult, error) {
	if p.calls.Add(1) == 1 {
		close(p.started)
		<-p.release
	}
	state.ByteOffset = 1
	return &IncrementalResult{}, nil
}

// Stopping the watcher during a backfill ends the startup scan instead of
// reading the remaining files first.
func TestStopInterruptsInitialScan(t *testing.T) {
	dir := t.TempDir()
	for i := range 20 {
		path := filepath.Join(dir, fmt.Sprintf("session-%02d.jsonl", i))
		if err := os.WriteFile(path, []byte("{}\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	parser := &firstCallBlockingParser{started: make(chan struct{}), release: make(chan struct{})}
	w := newProcessorTestWatcher(&fakeWatcherStore{}, parser, dir)

	scanned := make(chan struct{})
	go func() {
		w.initialScan(true)
		close(scanned)
	}()
	<-parser.started

	stopped := make(chan struct{})
	go func() {
		w.Stop()
		close(stopped)
	}()
	// Stop waits for the file being processed
	time.Sleep(20 * time.Millisecond)
	close(parser.release)

	for _, done := range []chan struct{}{scanned, stopped} {
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("initial scan did not end after Stop")
		}
	}
	if got := parser.calls.Load(); got != 1 {
		t.Errorf("parser calls = %d, want 1: files after Stop must not be read", got)
	}
}

func TestIsUnder(t *testing.T) {
	sep := string(filepath.Separator)
	dir := filepath.Join(sep+"home", "u", ".claude")
	cases := []struct {
		path string
		want bool
	}{
		{filepath.Join(dir, "projects", "a.jsonl"), true},
		{filepath.Join(dir, "..hidden.jsonl"), true},
		{dir, true},
		{filepath.Join(sep+"home", "u", "other", "a.jsonl"), false},
		{filepath.Join(sep+"home", "u"), false},
	}
	for _, c := range cases {
		if got := isUnder(c.path, dir); got != c.want {
			t.Errorf("isUnder(%q, %q) = %v, want %v", c.path, dir, got, c.want)
		}
	}
}

// Files changed during the startup scan are processed after it, not while it
// runs: events are taken from fsnotify during the scan (so its queue cannot
// overflow on a long backfill) and remembered until the scan ends
func TestChangesDuringInitialScanAreProcessedAfterIt(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "a.jsonl")
	second := filepath.Join(dir, "b.jsonl")
	for _, path := range []string{first, second} {
		if err := os.WriteFile(path, []byte("{}\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	parser := &firstCallBlockingParser{started: make(chan struct{}), release: make(chan struct{})}
	w := newProcessorTestWatcher(&fakeWatcherStore{}, parser, dir)
	w.backfill = true
	defer w.Stop()

	w.startScan()
	go w.scanExisting()
	<-parser.started // the scan is reading the first file

	w.handleEvent(fsnotify.Event{Name: second, Op: fsnotify.Write})
	time.Sleep(20 * time.Millisecond) // longer than the debounce
	if got := parser.calls.Load(); got != 1 {
		t.Fatalf("parser calls during the scan = %d, want 1", got)
	}
	w.timersMu.Lock()
	_, pending := w.scanPending[second]
	w.timersMu.Unlock()
	if !pending {
		t.Fatal("the changed file was not remembered for after the scan")
	}

	close(parser.release)
	w.waitInitialScan()
	// The scan reads both files, then the change runs the second one again
	deadline := time.Now().Add(5 * time.Second)
	for parser.calls.Load() < 3 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if got := parser.calls.Load(); got != 3 {
		t.Errorf("parser calls = %d, want 3", got)
	}

	// Later events are processed right away
	w.handleEvent(fsnotify.Event{Name: first, Op: fsnotify.Write})
	deadline = time.Now().Add(5 * time.Second)
	for parser.calls.Load() < 4 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if got := parser.calls.Load(); got != 4 {
		t.Errorf("parser calls after the scan = %d, want 4", got)
	}
}
