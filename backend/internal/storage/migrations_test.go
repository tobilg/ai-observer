package storage

import (
	"context"
	"database/sql"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/tobilg/ai-observer/internal/api"
	"github.com/tobilg/ai-observer/internal/tools"
)

// A database created before the session ID columns existed gets them filled
// from the stored attributes, once.
func TestSessionIDMigrationBackfillsExistingRows(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "legacy.duckdb")

	// Schema and rows as written by earlier versions
	legacy, err := sql.Open("duckdb", dbPath)
	if err != nil {
		t.Fatalf("opening legacy database: %v", err)
	}
	for _, stmt := range []string{
		schemaLogs, schemaTraces,
		`INSERT INTO otel_logs (Timestamp, ServiceName, LogAttributes) VALUES
			('2025-01-02 10:00:00', 'copilot-chat', '{"session.id": "s1", "gen_ai.conversation.id": "alias-1"}'),
			('2025-01-02 10:01:00', 'claude-code', '{"session.id": ""}'),
			('2025-01-02 10:02:00', 'claude-code', '{"event.name": "x"}')`,
		`INSERT INTO otel_traces (Timestamp, TraceId, SpanId, SpanName, ServiceName, SpanAttributes) VALUES
			('2025-01-02 10:00:00', 't1', 'sp1', 'chat', 'copilot-chat', '{"copilot_chat.session_id": "s2"}')`,
	} {
		if _, err := legacy.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	legacy.Close()

	store, err := NewDuckDBStore(dbPath)
	if err != nil {
		t.Fatalf("opening store: %v", err)
	}

	type row struct {
		sessionID sql.NullString
		ids       []any
	}
	scanRows := func(query string) []row {
		t.Helper()
		rows, err := store.db.QueryContext(ctx, query)
		if err != nil {
			t.Fatalf("%s: %v", query, err)
		}
		defer rows.Close()
		var result []row
		for rows.Next() {
			var r row
			var ids any
			if err := rows.Scan(&r.sessionID, &ids); err != nil {
				t.Fatalf("scanning: %v", err)
			}
			if ids != nil {
				r.ids = ids.([]any)
			}
			result = append(result, r)
		}
		return result
	}

	logs := scanRows("SELECT SessionId, SessionIds FROM otel_logs ORDER BY Timestamp")
	if len(logs) != 3 {
		t.Fatalf("got %d logs, want 3", len(logs))
	}
	if logs[0].sessionID.String != "s1" || len(logs[0].ids) != 2 {
		t.Errorf("aliased log = %+v, want SessionId s1 and two ids", logs[0])
	}
	for _, r := range logs[1:] {
		if r.sessionID.Valid || r.ids != nil {
			t.Errorf("log without session ID got %+v", r)
		}
	}
	spans := scanRows("SELECT SessionId, SessionIds FROM otel_traces")
	if len(spans) != 1 || spans[0].sessionID.String != "s2" {
		t.Errorf("spans = %+v, want SessionId s2", spans)
	}

	// The alias finds the session too
	aliasLogs, err := store.SessionLogs(ctx, "alias-1")
	if err != nil {
		t.Fatalf("SessionLogs: %v", err)
	}
	if len(aliasLogs) != 1 || aliasLogs[0].ServiceName != "copilot-chat" {
		t.Errorf("logs of alias-1 = %+v, want the copilot-chat log", aliasLogs)
	}

	// Reopening must not run the migration again
	store.Close()
	store, err = NewDuckDBStore(dbPath)
	if err != nil {
		t.Fatalf("reopening store: %v", err)
	}
	defer store.Close()
	var applied int
	if err := store.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM schema_migrations").Scan(&applied); err != nil {
		t.Fatalf("counting migrations: %v", err)
	}
	if applied != len(migrations) {
		t.Errorf("schema_migrations has %d rows, want %d", applied, len(migrations))
	}
}

func TestInsertedRecordsGetSessionIDs(t *testing.T) {
	ctx := context.Background()
	store, err := NewDuckDBStore(":memory:")
	if err != nil {
		t.Fatalf("creating store: %v", err)
	}
	defer store.Close()

	now := time.Now().UTC()
	if err := store.InsertLogs(ctx, []api.LogRecord{
		{Timestamp: now, ServiceName: "gemini_cli", LogAttributes: map[string]string{"session.id": "g1"}},
		{Timestamp: now, ServiceName: "gemini_cli", LogAttributes: map[string]string{"other": "x"}},
	}); err != nil {
		t.Fatalf("InsertLogs: %v", err)
	}

	resp, err := store.QuerySessions(ctx, "", now.Add(-time.Hour), now.Add(time.Hour), Page{Limit: 10})
	if err != nil {
		t.Fatalf("QuerySessions: %v", err)
	}
	if resp.Total != 1 || len(resp.Sessions) != 1 || resp.Sessions[0].SessionID != "g1" {
		t.Errorf("sessions = %+v, want only g1", resp)
	}
}

func TestMigrationFailureRollsBack(t *testing.T) {
	ctx := context.Background()
	store, err := NewDuckDBStore(":memory:")
	if err != nil {
		t.Fatalf("creating store: %v", err)
	}
	defer store.Close()

	err = store.migrate(ctx, []migration{{
		version: 1000,
		name:    "broken",
		stmts:   []string{"CREATE TABLE half_done (x INTEGER)", "SELECT * FROM missing_table"},
	}})
	if err == nil {
		t.Fatal("expected the broken migration to fail")
	}
	var exists bool
	if err := store.db.QueryRowContext(ctx, "SELECT COUNT(*) > 0 FROM information_schema.tables WHERE table_name = 'half_done'").Scan(&exists); err != nil {
		t.Fatalf("checking table: %v", err)
	}
	if exists {
		t.Error("a failed migration must not leave partial changes")
	}
}

func TestSpanDeduplicationMigration(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "dupes.duckdb")

	legacy, err := sql.Open("duckdb", dbPath)
	if err != nil {
		t.Fatalf("opening legacy database: %v", err)
	}
	for _, stmt := range []string{
		schemaTraces,
		// The same span stored twice (an exporter retry), plus another span
		`INSERT INTO otel_traces (Timestamp, TraceId, SpanId, SpanName, ServiceName) VALUES
			('2025-01-02 10:00:00', 't1', 's1', 'op', 'svc'),
			('2025-01-02 10:00:00', 't1', 's1', 'op', 'svc'),
			('2025-01-02 10:00:01', 't1', 's2', 'op', 'svc')`,
	} {
		if _, err := legacy.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	legacy.Close()

	store, err := NewDuckDBStore(dbPath)
	if err != nil {
		t.Fatalf("opening store: %v", err)
	}
	defer store.Close()

	var spans int
	if err := store.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM otel_traces").Scan(&spans); err != nil {
		t.Fatalf("counting spans: %v", err)
	}
	if spans != 2 {
		t.Errorf("spans = %d after migration, want 2", spans)
	}

	resp, err := store.QueryTraces(ctx, "", "", time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC), time.Date(2025, 1, 3, 0, 0, 0, 0, time.UTC), Page{Limit: 10})
	if err != nil {
		t.Fatalf("QueryTraces: %v", err)
	}
	if len(resp.Traces) != 1 || resp.Traces[0].SpanCount != 2 {
		t.Errorf("traces = %+v, want one trace with 2 spans", resp.Traces)
	}
}

// Migration 1 keeps its own copy of the session ID keys. It was taken from
// tools.SessionIDKeys; when that list changes, add a migration for the new
// keys instead of editing migration 1.
func TestMigration1SessionIDKeysMatchTheirOrigin(t *testing.T) {
	n := len(migration1SessionIDKeys)
	if len(tools.SessionIDKeys) < n || !slices.Equal(migration1SessionIDKeys, tools.SessionIDKeys[:n]) {
		t.Errorf("migration1SessionIDKeys = %v, want a prefix of tools.SessionIDKeys %v", migration1SessionIDKeys, tools.SessionIDKeys)
	}
}
