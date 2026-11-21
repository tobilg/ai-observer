package storage

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tobilg/ai-observer/internal/api"
	"github.com/tobilg/ai-observer/internal/msgmerge"
)

// messageKeyLogs is a Claude Code session recorded in its file and over
// OTLP: the prompt twice, the reply with its usage event, and a log that is
// no message
func messageKeyLogs() []api.LogRecord {
	t0 := overlapBase
	log := func(ms int, body string, attrs map[string]string) api.LogRecord {
		a := map[string]string{"session.id": "s1"}
		for k, v := range attrs {
			a[k] = v
		}
		return api.LogRecord{Timestamp: t0.Add(time.Duration(ms) * time.Millisecond), ServiceName: "claude-code", Body: body, LogAttributes: a}
	}
	return []api.LogRecord{
		log(0, "Fix it", map[string]string{"event.name": "transcript.message", "message.role": "user", "message.uuid": "u1", "import_source": "local_jsonl"}),
		log(0, "claude_code.user_prompt", map[string]string{"event.name": "user_prompt", "prompt": "Fix it", "message.uuid": "u1"}),
		log(900, "Done.", map[string]string{"event.name": "transcript.message", "message.role": "assistant", "request_id": "req_1", "import_source": "local_jsonl"}),
		log(950, "claude_code.api_request", map[string]string{"event.name": "api_request", "request_id": "req_1", "input_tokens": "100", "output_tokens": "20", "cost_usd": "0.25"}),
		log(990, "claude_code.tool_decision", map[string]string{"event.name": "conversation_starts"}),
	}
}

// sessionSummary lists the session of messageKeyLogs
func sessionSummary(t *testing.T, store *DuckDBStore) api.Session {
	t.Helper()
	res, err := store.QuerySessions(context.Background(), "", overlapBase.Add(-time.Hour), overlapBase.Add(time.Hour), Page{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Sessions) != 1 {
		t.Fatalf("sessions %+v", res.Sessions)
	}
	return res.Sessions[0]
}

func checkSessionSummary(t *testing.T, s api.Session) {
	t.Helper()
	if s.MessageCount != 2 || s.InputTokens == nil || *s.InputTokens != 100 || s.OutputTokens == nil || *s.OutputTokens != 20 ||
		s.CostUSD == nil || *s.CostUSD != 0.25 || s.Title != "Fix it" {
		t.Errorf("session listed as %+v", s)
	}
}

func storedKeys(t *testing.T, store *DuckDBStore) []string {
	t.Helper()
	rows, err := store.db.Query(`SELECT COALESCE(MessageKey, '<null>') FROM otel_logs ORDER BY Timestamp, rowid`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var keys []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			t.Fatal(err)
		}
		keys = append(keys, k)
	}
	return keys
}

// Logs store their message keys; keys that are missing (restored records)
// or of another version are computed from the attributes by the session
// list, and refilled at startup
func TestMessageKeys(t *testing.T) {
	t.Setenv("AI_OBSERVER_SKIP_MIGRATION_BACKUP", "true")
	dbPath := filepath.Join(t.TempDir(), "keys.duckdb")
	store, err := NewDuckDBStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.InsertLogs(context.Background(), messageKeyLogs()); err != nil {
		t.Fatal(err)
	}
	keys := storedKeys(t, store)
	for i, k := range keys {
		message := i < 4
		if message != strings.HasPrefix(k, msgmerge.KeyPrefix()) || !message && k != msgmerge.NotMessage() {
			t.Errorf("log %d stores key %q", i, k)
		}
	}
	checkSessionSummary(t, sessionSummary(t, store))

	stale := strings.Replace(keys[2], msgmerge.KeyVersion, "0.00000000", 1)
	if _, err := store.db.Exec(`UPDATE otel_logs SET MessageKey = CASE
		WHEN Body = 'Fix it' THEN NULL WHEN Body = 'Done.' THEN ? ELSE MessageKey END`, stale); err != nil {
		t.Fatal(err)
	}
	checkSessionSummary(t, sessionSummary(t, store))
	store.Close()

	store, err = NewDuckDBStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	// Opening the store leaves them to the refill
	if got := storedKeys(t, store); got[0] != "<null>" || got[2] != stale {
		t.Errorf("keys after opening %q", got)
	}
	r, err := store.RefillMessageKeys(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if r.Logs != 2 || r.Chunks != 1 {
		t.Errorf("refill %+v, want 2 logs in 1 transaction", r)
	}
	if got := storedKeys(t, store); strings.Join(got, "|") != strings.Join(keys, "|") {
		t.Errorf("keys after the refill\n got %q\nwant %q", got, keys)
	}
	checkSessionSummary(t, sessionSummary(t, store))
	if r, err := store.RefillMessageKeys(context.Background()); err != nil || r.Logs != 0 || r.Chunks != 0 {
		t.Errorf("second refill %+v, %v", r, err)
	}
}

// Migration 9 adds the message key column without filling it.
// DuckDB cannot drop a column of an indexed table, so the logs here lose
// their keys instead. Lists are right without the keys, and the refill
// sets them
func TestMessageKeysMigration(t *testing.T) {
	t.Setenv("AI_OBSERVER_SKIP_MIGRATION_BACKUP", "true")
	dbPath := filepath.Join(t.TempDir(), "keys.duckdb")
	store, err := NewDuckDBStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.InsertLogs(context.Background(), messageKeyLogs()); err != nil {
		t.Fatal(err)
	}
	want := storedKeys(t, store)
	for _, stmt := range []string{`UPDATE otel_logs SET MessageKey = NULL`, `DELETE FROM schema_migrations WHERE version >= 9`} {
		if _, err := store.db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	store.Close()

	store, err = NewDuckDBStore(dbPath)
	if err != nil {
		t.Fatalf("migrating: %v", err)
	}
	defer store.Close()
	for i, k := range storedKeys(t, store) {
		if k != "<null>" {
			t.Errorf("log %d has key %q after migrating", i, k)
		}
	}
	checkSessionSummary(t, sessionSummary(t, store))
	if _, err := store.RefillMessageKeys(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := storedKeys(t, store); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("keys after the refill\n got %q\nwant %q", got, want)
	}
	checkSessionSummary(t, sessionSummary(t, store))
}

// The refill sets keys a chunk of row IDs per write transaction, writes of
// others run between its chunks, and Close stops it
func TestMessageKeyRefillChunks(t *testing.T) {
	t.Setenv("AI_OBSERVER_SKIP_MIGRATION_BACKUP", "true")
	defer func(n int64) { messageKeyRefillChunk = n }(messageKeyRefillChunk)
	messageKeyRefillChunk = 2
	dbPath := filepath.Join(t.TempDir(), "keys.duckdb")
	store, err := NewDuckDBStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.InsertLogs(context.Background(), messageKeyLogs()); err != nil {
		t.Fatal(err)
	}
	want := storedKeys(t, store)
	if _, err := store.db.Exec(`UPDATE otel_logs SET MessageKey = NULL`); err != nil {
		t.Fatal(err)
	}
	r, err := store.RefillMessageKeys(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if r.Logs != 5 || r.Chunks != 3 || r.Longest <= 0 || r.Duration < r.Longest {
		t.Errorf("refill %+v, want 5 logs in 3 transactions", r)
	}
	if got := storedKeys(t, store); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("keys after the refill\n got %q\nwant %q", got, want)
	}

	// A cancelled refill stops between chunks
	if _, err := store.db.Exec(`UPDATE otel_logs SET MessageKey = NULL`); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := store.RefillMessageKeys(ctx); err == nil {
		t.Error("cancelled refill returned no error")
	}

	// In the background until done; Close waits for it
	store.StartMessageKeyRefill()
	store.StartMessageKeyRefill() // once only
	deadline := time.Now().Add(10 * time.Second)
	for strings.Join(storedKeys(t, store), "|") != strings.Join(want, "|") {
		if time.Now().After(deadline) {
			t.Fatalf("background refill left keys %q", storedKeys(t, store))
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store.StartMessageKeyRefill() // after Close: nothing
}
