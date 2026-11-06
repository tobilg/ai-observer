package watcher

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/tobilg/ai-observer/internal/storage"
)

func countServiceLogs(t *testing.T, store *storage.DuckDBStore, service string) int {
	t.Helper()
	var n int
	if err := store.DB().QueryRow("SELECT COUNT(*) FROM otel_logs WHERE ServiceName = ?", service).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// appendUntilStored appends a Codex message to path until the watcher has
// stored logs from it: a file created right after its directory can be
// written before the directory is watched, and then only later writes are seen
func appendUntilStored(t *testing.T, store *storage.DuckDBStore, path string) {
	t.Helper()
	meta := `{"timestamp":"2025-01-02T10:00:00Z","type":"session_meta","payload":{"id":"live-1","model":"gpt-5","cwd":"/tmp/p"}}` + "\n"
	if err := os.WriteFile(path, []byte(meta), 0o644); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			t.Fatal(err)
		}
		f.WriteString(mustCodexMessageLine(t, "hello") + "\n")
		f.Close()
		time.Sleep(300 * time.Millisecond) // longer than the Codex debounce
		if countServiceLogs(t, store, "codex_cli_rs") > 0 {
			return
		}
	}
	t.Fatalf("%s was not imported", path)
}

// A running watcher picks up session files in directories created after it
// started (Codex creates a directory per day)
func TestWatcherImportsFilesInNewDirectories(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AI_OBSERVER_CODEX_PATH", dir)
	store, err := storage.NewDuckDBStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	w := New(store, nil, Config{Tools: []string{"codex"}})
	if err := w.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer w.Stop()
	w.waitInitialScan()

	day := filepath.Join(dir, "2025", "01", "02")
	if err := os.MkdirAll(day, 0o755); err != nil {
		t.Fatal(err)
	}
	appendUntilStored(t, store, filepath.Join(day, "rollout-live.jsonl"))
}

// A session directory that does not exist at startup is watched once it
// appears
func TestWatcherWatchesDirectoriesThatAppearLater(t *testing.T) {
	saved := missingDirPollInterval
	missingDirPollInterval = 20 * time.Millisecond
	t.Cleanup(func() { missingDirPollInterval = saved })

	dir := filepath.Join(t.TempDir(), "sessions")
	t.Setenv("AI_OBSERVER_CODEX_PATH", dir)
	store, err := storage.NewDuckDBStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	w := New(store, nil, Config{Tools: []string{"codex"}})
	if err := w.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer w.Stop()
	w.waitInitialScan()

	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for len(w.toolWatchDirs(w.tools[0])) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := w.toolWatchDirs(w.tools[0]); len(got) != 1 || got[0] != dir {
		t.Fatalf("watched directories = %v, want %s", got, dir)
	}
	appendUntilStored(t, store, filepath.Join(dir, "rollout-late.jsonl"))
}

func TestWatchErrorHint(t *testing.T) {
	cases := map[error]string{
		syscall.EMFILE:                     "too many open files",
		syscall.ENFILE:                     "too many open files",
		syscall.ENOSPC:                     "max_user_watches",
		errors.New("permission denied"):    "",
		&os.PathError{Err: syscall.EMFILE}: "too many open files",
	}
	for err, want := range cases {
		got := watchErrorHint(err)
		if (want == "") != (got == "") || !strings.Contains(got, want) {
			t.Errorf("watchErrorHint(%v) = %q, want it to mention %q", err, got, want)
		}
	}
}
