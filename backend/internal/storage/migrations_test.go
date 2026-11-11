package storage

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"maps"
	"path/filepath"
	"slices"
	"strings"
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

	resp, err := store.QueryTraces(ctx, TraceFilter{}, time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC), time.Date(2025, 1, 3, 0, 0, 0, 0, time.UTC), Page{Limit: 10})
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

// Trace and span IDs that earlier versions read from OTLP/JSON as base64
// instead of hex are repaired; other IDs are kept.
func TestHexIDMigration(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "ids.duckdb")
	const (
		traceID = "5b8efff798038103d269b633813fc60c"
		spanID  = "eee19b7ec3c1b174"
		parent  = "eee19b7ec3c1b173"
	)
	// What earlier versions stored for an ID sent as hex text
	misread := func(sent string) string {
		b, err := base64.StdEncoding.DecodeString(sent)
		if err != nil {
			t.Fatal(err)
		}
		return hex.EncodeToString(b)
	}
	notSent := strings.Repeat("ff", 24) // 24 bytes whose base64 is not hex

	legacy, err := sql.Open("duckdb", dbPath)
	if err != nil {
		t.Fatalf("opening legacy database: %v", err)
	}
	for _, stmt := range []string{
		schemaTraces,
		schemaLogs,
		`INSERT INTO otel_traces (Timestamp, TraceId, SpanId, ParentSpanId, SpanName, ServiceName, Duration, "Links.TraceId", "Links.SpanId") VALUES
			('2025-01-02 10:00:00', '` + misread(traceID) + `', '` + misread(spanID) + `', '` + misread(parent) + `', 'json', 'gemini_cli', 1,
			 '["` + misread(strings.ToUpper(traceID)) + `"]', '["` + misread(parent) + `", "` + spanID + `"]'),
			('2025-01-02 10:00:01', '` + traceID + `', '` + spanID + `', NULL, 'protobuf', 'claude-code', 1, '[]', '[]'),
			('2025-01-02 10:00:02', 'not-hex-trace-id-of-48-characters-xxxxxxxxxxxxxx', 's1', 'p1', 'other', 'svc', 1, NULL, NULL),
			('2025-01-02 10:00:03', '` + notSent + `', '` + spanID + `', NULL, 'odd', 'svc', 1, NULL, NULL)`,
		`INSERT INTO otel_logs (Timestamp, TraceId, SpanId, ServiceName) VALUES
			('2025-01-02 10:00:00', '` + misread(traceID) + `', '` + misread(spanID) + `', 'gemini_cli'),
			('2025-01-02 10:00:01', NULL, NULL, 'gemini_cli')`,
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

	rows, err := store.db.QueryContext(ctx, `SELECT SpanName, TraceId, SpanId, COALESCE(ParentSpanId, ''),
		COALESCE(CAST("Links.TraceId" AS VARCHAR), ''), COALESCE(CAST("Links.SpanId" AS VARCHAR), '')
		FROM otel_traces ORDER BY Timestamp`)
	if err != nil {
		t.Fatal(err)
	}
	var got [][]string
	for rows.Next() {
		row := make([]string, 6)
		if err := rows.Scan(&row[0], &row[1], &row[2], &row[3], &row[4], &row[5]); err != nil {
			t.Fatal(err)
		}
		got = append(got, row)
	}
	rows.Close()
	want := [][]string{
		{"json", traceID, spanID, parent, `["` + traceID + `"]`, `["` + parent + `","` + spanID + `"]`},
		{"protobuf", traceID, spanID, "", "[]", "[]"},
		{"other", "not-hex-trace-id-of-48-characters-xxxxxxxxxxxxxx", "s1", "p1", "", ""},
		{"odd", notSent, spanID, "", "", ""},
	}
	if !slices.EqualFunc(got, want, slices.Equal) {
		t.Errorf("spans after migration:\n got %q\nwant %q", got, want)
	}

	var logTrace, logSpan sql.NullString
	if err := store.db.QueryRowContext(ctx, `SELECT TraceId, SpanId FROM otel_logs ORDER BY Timestamp LIMIT 1`).Scan(&logTrace, &logSpan); err != nil {
		t.Fatal(err)
	}
	if logTrace.String != traceID || logSpan.String != spanID {
		t.Errorf("log IDs = %s/%s, want %s/%s", logTrace.String, logSpan.String, traceID, spanID)
	}
	// The repaired spans are found by the IDs that were sent
	spans, err := store.TraceSpans(ctx, traceID)
	if err != nil {
		t.Fatal(err)
	}
	if len(spans) != 2 {
		t.Errorf("trace %s has %d spans, want 2", traceID, len(spans))
	}
}

// Migration 10 drops the "tool" tokens (Codex's total), names the imported
// cache types like OTLP, and derives the usage of the codex.sse_event logs
// stored for codex-app-server, which it deletes.
func TestCodexTokenTypesMigration(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "codex.duckdb")
	legacy, err := sql.Open("duckdb", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		schemaMetrics,
		schemaLogs,
		`INSERT INTO otel_metrics (Timestamp, ServiceName, MetricName, Attributes, MetricType, Value) VALUES
			('2026-11-01 10:00:00', 'codex_cli_rs', 'codex_cli_rs.token.usage', '{"model": "gpt-5", "type": "input"}', 'sum', 1000),
			('2026-11-01 10:00:00', 'codex_cli_rs', 'codex_cli_rs.token.usage', '{"model": "gpt-5", "type": "cacheRead"}', 'sum', 200),
			('2026-11-01 10:00:00', 'codex_cli_rs', 'codex_cli_rs.token.usage', '{"model": "gpt-5", "type": "output"}', 'sum', 10),
			('2026-11-01 10:00:00', 'codex_cli_rs', 'codex_cli_rs.token.usage', '{"model": "gpt-5", "type": "tool"}', 'sum', 1010),
			('2026-11-02 10:00:00', 'codex_cli_rs', 'codex_cli_rs.token.usage', '{"model": "gpt-5", "type": "cache_read", "import_source": "local_jsonl"}', 'sum', 300),
			('2026-11-02 10:00:00', 'codex_cli_rs', 'codex_cli_rs.token.usage', '{"model": "gpt-5", "type": "cache_creation", "import_source": "local_jsonl"}', 'sum', 5),
			('2026-11-02 10:00:00', 'gemini_cli', 'gemini_cli.token.usage', '{"type": "tool"}', 'sum', 7)`,
		`INSERT INTO otel_logs (Timestamp, ServiceName, Body, ResourceAttributes, LogAttributes) VALUES
			('2026-11-08 18:00:00', 'codex-app-server', 'codex.sse_event', '{"service.name": "codex-app-server"}',
			 '{"event.name": "codex.sse_event", "event.kind": "response.completed", "conversation.id": "c1", "model": "gpt-6-astra",
			   "originator": "Codex_Desktop", "input_token_count": "1000", "cached_token_count": "400", "cache_write_token_count": "100",
			   "output_token_count": "20", "reasoning_token_count": "5", "tool_token_count": "1020"}'),
			('2026-11-08 18:00:01', 'codex-app-server', 'codex.user_prompt', '{}', '{"event.name": "codex.user_prompt", "conversation.id": "c1"}'),
			('2026-11-08 18:00:02', 'other-service', 'codex.sse_event', '{}', '{"event.name": "codex.sse_event", "event.kind": "response.completed", "input_token_count": "9"}')`,
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

	tokens := func(service string) map[string]float64 {
		t.Helper()
		rows, err := store.db.QueryContext(ctx, `SELECT json_extract_string(Attributes, '$.type'), SUM(Value) FROM otel_metrics
			WHERE MetricName = 'codex_cli_rs.token.usage' AND ServiceName = ? GROUP BY 1`, service)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		got := make(map[string]float64)
		for rows.Next() {
			var tokenType string
			var value float64
			if err := rows.Scan(&tokenType, &value); err != nil {
				t.Fatal(err)
			}
			got[tokenType] = value
		}
		return got
	}
	if got, want := tokens("codex_cli_rs"), map[string]float64{"input": 1000, "cacheRead": 500, "cacheWrite": 5, "output": 10}; !maps.Equal(got, want) {
		t.Errorf("codex_cli_rs tokens = %v, want %v", got, want)
	}
	if got, want := tokens("codex-app-server"), map[string]float64{"input": 1000, "cacheRead": 400, "cacheWrite": 100, "output": 20, "reasoning": 5}; !maps.Equal(got, want) {
		t.Errorf("codex-app-server tokens = %v, want %v", got, want)
	}
	if got := countRows(t, store, `SELECT COUNT(*) FROM otel_metrics WHERE MetricName = 'gemini_cli.token.usage'`); got != 1 {
		t.Errorf("Gemini's tool tokens were changed: %d points", got)
	}

	// The cost, with the originator, which tells the Codex app from the CLI
	var cost float64
	var originator string
	if err := store.db.QueryRowContext(ctx, `SELECT Value, json_extract_string(Attributes, '$.originator') FROM otel_metrics
		WHERE MetricName = 'codex_cli_rs.cost.usage' AND ServiceName = 'codex-app-server'`).Scan(&cost, &originator); err != nil {
		t.Fatalf("cost of the app server's response: %v", err)
	}
	if cost <= 0 || originator != "Codex_Desktop" {
		t.Errorf("cost = %v, originator %q", cost, originator)
	}

	// The SSE event is gone, other logs are kept
	rows, err := store.db.QueryContext(ctx, `SELECT ServiceName, Body FROM otel_logs ORDER BY Timestamp`)
	if err != nil {
		t.Fatal(err)
	}
	var logs []string
	for rows.Next() {
		var service, body string
		if err := rows.Scan(&service, &body); err != nil {
			t.Fatal(err)
		}
		logs = append(logs, service+" "+body)
	}
	rows.Close()
	if want := []string{"codex-app-server codex.user_prompt", "other-service codex.sse_event"}; !slices.Equal(logs, want) {
		t.Errorf("logs = %q, want %q", logs, want)
	}
	if names, err := store.GetMetricNames(ctx, "codex-app-server"); err != nil || strings.Join(names, ",") != "codex_cli_rs.cost.usage,codex_cli_rs.token.usage" {
		t.Errorf("metric names of codex-app-server = %v (%v)", names, err)
	}
}
