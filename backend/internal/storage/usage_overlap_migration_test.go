package storage

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/tobilg/ai-observer/internal/api"
)

// Migration 11 repairs Claude Code usage that earlier versions stored twice,
// from OTLP and from a session file, with the rule of usage_overlap.go
func TestClaudeUsageOverlapMigration(t *testing.T) {
	t.Setenv("AI_OBSERVER_SKIP_MIGRATION_BACKUP", "true")
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "overlap.duckdb")
	store, err := NewDuckDBStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t0 := overlapBase
	at := func(m int) time.Time { return t0.Add(time.Duration(m) * time.Minute) }

	// What earlier versions stored: files and OTLP side by side
	var file1Logs, file2Logs, legacyLogs []api.LogRecord
	var file1Metrics, file2Metrics, legacyMetrics []api.MetricDataPoint
	add := func(logs *[]api.LogRecord, metrics *[]api.MetricDataPoint, session, req string, ts time.Time) {
		l, m := claudeFileRequest(session, req, ts, 1, 2)
		*logs, *metrics = append(*logs, l...), append(*metrics, m...)
	}
	add(&file1Logs, &file1Metrics, "s1", "req_a", at(0))
	add(&file1Logs, &file1Metrics, "s1", "req_b", at(1))
	add(&file1Logs, &file1Metrics, "s1", "req_d", at(2)) // not recorded by OTLP
	add(&file1Logs, &file1Metrics, "s1", "req_f", at(3)) // two requests at one time,
	add(&file1Logs, &file1Metrics, "s1", "req_g", at(3)) // one not recorded by OTLP
	add(&file2Logs, &file2Metrics, "s2", "req_c", at(0)) // its session sent no metrics
	add(&legacyLogs, &legacyMetrics, "s1", "req_e", at(4))
	otlpLogs := []api.LogRecord{
		claudeOTLPRequest("s1", "req_a", at(0).Add(5*time.Second)),
		claudeOTLPRequest("s1", "req_b", at(1).Add(5*time.Second)),
		claudeOTLPRequest("s1", "req_f", at(3).Add(5*time.Second)),
		claudeOTLPRequest("s1", "req_e", at(4).Add(5*time.Second)),
		claudeOTLPRequest("s2", "req_c", at(0).Add(5*time.Second)),
	}
	err = store.inWriteTx(ctx, func(w *writeTx) error {
		for _, batch := range []struct {
			file    string
			logs    []api.LogRecord
			metrics []api.MetricDataPoint
		}{
			{"/claude/s1.jsonl", file1Logs, file1Metrics},
			{"/claude/s2.jsonl", file2Logs, file2Metrics},
			{"", legacyLogs, legacyMetrics}, // imported before records had SourceFile
			{"", otlpLogs, []api.MetricDataPoint{claudeOTLPTokens("s1", at(5), 9)}},
		} {
			if err := insertLogsTx(ctx, w, batch.logs, batch.file); err != nil {
				return err
			}
			if err := insertMetricsTx(ctx, w, batch.metrics, batch.file); err != nil {
				return err
			}
		}
		_, err := w.tx.ExecContext(ctx, `DELETE FROM schema_migrations WHERE version >= 11`)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	store.Close()

	store, err = NewDuckDBStore(dbPath)
	if err != nil {
		t.Fatalf("migrating: %v", err)
	}
	defer store.Close()
	e := &overlapEnv{t: t, ctx: ctx, store: store}
	for _, tt := range []struct {
		name string
		ts   time.Time
		want int
	}{
		{"recorded requests", at(0), 6}, // req_a's removed; s2's req_c kept (no metrics)
		{"recorded request", at(1), 0},
		{"request only in the file", at(2), 6},
		{"time with an unrecorded request", at(3), 12},
		{"legacy import", at(4), 0},
	} {
		if got := e.fileUsageAt("claude-code", tt.ts); got != tt.want {
			t.Errorf("%s: file usage points = %d, want %d", tt.name, got, tt.want)
		}
	}
	var s2 int
	store.db.QueryRow(`SELECT COUNT(*) FROM otel_metrics WHERE SourceFile = '/claude/s2.jsonl'`).Scan(&s2)
	if s2 != 6 {
		t.Errorf("points of the session without OTLP metrics = %d, want 6", s2)
	}
	if got := e.count("otel_logs"); got != len(file1Logs)+len(file2Logs)+len(legacyLogs)+len(otlpLogs) {
		t.Errorf("logs = %d: the migration must not delete logs", got)
	}
}
