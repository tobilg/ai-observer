package storage

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/tobilg/ai-observer/internal/api"
	"github.com/tobilg/ai-observer/internal/tools"
)

// The records below have the shapes the importers, the watcher and OTLP
// conversion produce for the same requests (see usage_overlap.go)

var overlapBase = time.Date(2026, 11, 8, 18, 0, 0, 0, time.UTC)

func float(v float64) *float64 { return &v }

// claudeFileRequest returns what an import of a Claude Code session file
// stores for one request: the claude_code.api_request log and the four
// usage points, at the entry's time
func claudeFileRequest(session, requestID string, ts time.Time, input, output float64) ([]api.LogRecord, []api.MetricDataPoint) {
	log := api.LogRecord{Timestamp: ts, ServiceName: "claude-code", Body: "api_request", LogAttributes: map[string]string{
		"event.name": "claude_code.api_request", "session.id": session, "model": "claude-opus-5-5",
		"import_source": "local_jsonl", "request_id": requestID,
	}}
	point := func(name, tokenType string, v float64) api.MetricDataPoint {
		attrs := map[string]string{"model": "claude-opus-5-5", "import_source": "local_jsonl"}
		if tokenType != "" {
			attrs["type"] = tokenType
		}
		return api.MetricDataPoint{Timestamp: ts, ServiceName: "claude-code", MetricName: name, MetricType: "sum", Value: float(v), Attributes: attrs}
	}
	return []api.LogRecord{log}, []api.MetricDataPoint{
		point("claude_code.token.usage", "input", input), point("claude_code.token.usage_user_facing", "input", input),
		point("claude_code.token.usage", "output", output), point("claude_code.token.usage_user_facing", "output", output),
		point("claude_code.cost.usage", "", 0.5), point("claude_code.cost.usage_user_facing", "", 0.5),
	}
}

// claudeOTLPRequest returns Claude Code's api_request event for a request
func claudeOTLPRequest(session, requestID string, ts time.Time) api.LogRecord {
	return api.LogRecord{Timestamp: ts, ServiceName: "claude-code", Body: "api_request", LogAttributes: map[string]string{
		"event.name": "api_request", "session.id": session, "request_id": requestID, "duration_ms": "8000",
		"input_tokens": "10", "output_tokens": "20", "cost_usd": "0.6",
	}}
}

// claudeOTLPTokens returns a delta claude_code.token.usage point of a session
func claudeOTLPTokens(session string, ts time.Time, v float64) api.MetricDataPoint {
	delta := int32(1)
	return api.MetricDataPoint{Timestamp: ts, ServiceName: "claude-code", MetricName: "claude_code.token.usage", MetricType: "sum",
		Value: float(v), AggregationTemporality: &delta,
		Attributes: map[string]string{"model": "claude-opus-5-5", "type": "input", "session.id": session}}
}

type overlapEnv struct {
	t     *testing.T
	ctx   context.Context
	store *DuckDBStore
}

func newOverlapEnv(t *testing.T) *overlapEnv {
	store, cleanup := setupTestStore(t)
	t.Cleanup(cleanup)
	return &overlapEnv{t: t, ctx: context.Background(), store: store}
}

func (e *overlapEnv) importFile(source, path string, logs []api.LogRecord, metrics []api.MetricDataPoint, replace bool) {
	e.t.Helper()
	var sel *FileRecords
	if replace {
		sel = &FileRecords{}
	}
	state := &ImportState{Source: source, FilePath: path, ImportedAt: time.Now(), SourceTracked: true}
	if err := e.store.InsertImportedFile(e.ctx, logs, metrics, nil, state, sel); err != nil {
		e.t.Fatal(err)
	}
}

func (e *overlapEnv) watchFile(source, path string, logs []api.LogRecord, metrics []api.MetricDataPoint) {
	e.t.Helper()
	state := &ImportState{Source: source, FilePath: path, SourceTracked: true}
	if err := e.store.InsertWatcherBatch(e.ctx, logs, metrics, nil, state, false); err != nil {
		e.t.Fatal(err)
	}
}

func (e *overlapEnv) otlpLogs(logs []api.LogRecord, metrics []api.MetricDataPoint) {
	e.t.Helper()
	if err := e.store.InsertTelemetry(e.ctx, logs, metrics, nil); err != nil {
		e.t.Fatal(err)
	}
}

func (e *overlapEnv) otlpMetrics(metrics ...api.MetricDataPoint) {
	e.t.Helper()
	if err := e.store.InsertMetrics(e.ctx, metrics); err != nil {
		e.t.Fatal(err)
	}
}

// fileUsage counts the stored usage points read from files, per metric
func (e *overlapEnv) fileUsage(service string) map[string]int {
	e.t.Helper()
	rows, err := e.store.db.QueryContext(e.ctx, `SELECT MetricName, COUNT(*) FROM otel_metrics
		WHERE ServiceName = ? AND json_extract_string(Attributes, '$.import_source') IS NOT NULL GROUP BY MetricName`, service)
	if err != nil {
		e.t.Fatal(err)
	}
	defer rows.Close()
	counts := map[string]int{}
	for rows.Next() {
		var name string
		var n int
		if err := rows.Scan(&name, &n); err != nil {
			e.t.Fatal(err)
		}
		counts[name] = n
	}
	return counts
}

// fileUsageAt counts the file usage points of a service at ts
func (e *overlapEnv) fileUsageAt(service string, ts time.Time) int {
	e.t.Helper()
	var n int
	err := e.store.db.QueryRowContext(e.ctx, `SELECT COUNT(*) FROM otel_metrics WHERE ServiceName = ? AND Timestamp = ?::TIMESTAMP
		AND json_extract_string(Attributes, '$.import_source') IS NOT NULL`, service, formatTimeForDB(ts)).Scan(&n)
	if err != nil {
		e.t.Fatal(err)
	}
	return n
}

// count returns the number of rows of a table
func (e *overlapEnv) count(table string) int {
	e.t.Helper()
	var n int
	if err := e.store.db.QueryRowContext(e.ctx, "SELECT COUNT(*) FROM "+table).Scan(&n); err != nil {
		e.t.Fatal(err)
	}
	return n
}

// claudeSession is a Claude Code session of two requests (at t0, t0+1m) and
// its file
type claudeSession struct {
	id, file string
	t0       time.Time
}

func (s claudeSession) req(i int) string   { return fmt.Sprintf("req_%s_%d", s.id, i) }
func (s claudeSession) at(i int) time.Time { return s.t0.Add(time.Duration(i) * time.Minute) }
func (s claudeSession) fileRecords(n int) ([]api.LogRecord, []api.MetricDataPoint) {
	var logs []api.LogRecord
	var metrics []api.MetricDataPoint
	for i := range n {
		l, m := claudeFileRequest(s.id, s.req(i), s.at(i), 100, 200)
		logs, metrics = append(logs, l...), append(metrics, m...)
	}
	return logs, metrics
}

// otlp returns the OTLP records of requests from..to-1: their events (5 s
// after the file entry) and a token point of the session after them
func (s claudeSession) otlp(from, to int) ([]api.LogRecord, api.MetricDataPoint) {
	var logs []api.LogRecord
	for i := from; i < to; i++ {
		logs = append(logs, claudeOTLPRequest(s.id, s.req(i), s.at(i).Add(5*time.Second)))
	}
	return logs, claudeOTLPTokens(s.id, s.at(to-1).Add(30*time.Second), 300)
}

func TestClaudeUsageOTLPFirstThenFile(t *testing.T) {
	e := newOverlapEnv(t)
	s := claudeSession{id: "s1", file: "/claude/s1.jsonl", t0: overlapBase}
	logs, tokens := s.otlp(0, 2)
	e.otlpLogs(logs, nil)
	e.otlpMetrics(tokens)

	fileLogs, fileMetrics := s.fileRecords(3) // request 2 is in the file only
	e.importFile("claude-code", s.file, fileLogs, fileMetrics, false)

	if e.fileUsageAt("claude-code", s.at(0)) != 0 || e.fileUsageAt("claude-code", s.at(1)) != 0 {
		t.Error("file usage of requests OTLP recorded was stored")
	}
	if got := e.fileUsageAt("claude-code", s.at(2)); got != 6 {
		t.Errorf("file usage of the request only the file records = %d points, want 6", got)
	}
	// The file's api_request logs and transcript stay
	if got := e.count("otel_logs"); got != 2+3 {
		t.Errorf("logs = %d, want the 2 OTLP events and the file's 3 request logs", got)
	}
}

func TestClaudeUsageFileFirstThenOTLP(t *testing.T) {
	e := newOverlapEnv(t)
	s := claudeSession{id: "s1", file: "/claude/s1.jsonl", t0: overlapBase}
	fileLogs, fileMetrics := s.fileRecords(2)
	e.watchFile("claude-code", s.file, fileLogs, fileMetrics)

	// Events arrive first (every 5 s), the session's metrics after them
	// (every minute): until then the file's usage is the only one
	logs, tokens := s.otlp(0, 2)
	e.otlpLogs(logs, nil)
	if got := e.fileUsage("claude-code")["claude_code.token.usage"]; got != 4 {
		t.Fatalf("file token points = %d before OTLP sent metrics, want 4", got)
	}
	e.otlpMetrics(tokens)
	if got := e.fileUsage("claude-code"); len(got) != 0 {
		t.Errorf("file usage left after OTLP recorded the requests: %v", got)
	}

	// A later request: its event arrives after the session's metrics
	l, m := claudeFileRequest(s.id, s.req(2), s.at(2), 1, 1)
	e.watchFile("claude-code", s.file, l, m)
	e.otlpLogs([]api.LogRecord{claudeOTLPRequest(s.id, s.req(2), s.at(2).Add(3*time.Second))}, nil)
	if got := e.fileUsage("claude-code"); len(got) != 0 {
		t.Errorf("file usage of a request whose event arrived after the metrics = %v", got)
	}
}

func TestClaudeUsageReimportAndWatchKeepRule(t *testing.T) {
	e := newOverlapEnv(t)
	s := claudeSession{id: "s1", file: "/claude/s1.jsonl", t0: overlapBase}
	logs, tokens := s.otlp(0, 1)
	e.otlpLogs(logs, []api.MetricDataPoint{})
	e.otlpMetrics(tokens)

	fileLogs, fileMetrics := s.fileRecords(2)
	e.importFile("claude-code", s.file, fileLogs, fileMetrics, false)
	// A forced or changed re-import replaces the file's records
	e.importFile("claude-code", s.file, fileLogs, fileMetrics, true)
	if e.fileUsageAt("claude-code", s.at(0)) != 0 || e.fileUsageAt("claude-code", s.at(1)) != 6 {
		t.Errorf("after re-import: file usage at request 0 = %d (want 0), request 1 = %d (want 6)",
			e.fileUsageAt("claude-code", s.at(0)), e.fileUsageAt("claude-code", s.at(1)))
	}

	// Watch appends a request OTLP recorded and one it did not
	e.otlpLogs([]api.LogRecord{claudeOTLPRequest(s.id, s.req(2), s.at(2).Add(time.Second))}, nil)
	l2, m2 := claudeFileRequest(s.id, s.req(2), s.at(2), 1, 1)
	l3, m3 := claudeFileRequest(s.id, s.req(3), s.at(3), 1, 1)
	e.watchFile("claude-code", s.file, append(l2, l3...), append(m2, m3...))
	if e.fileUsageAt("claude-code", s.at(2)) != 0 || e.fileUsageAt("claude-code", s.at(3)) != 6 {
		t.Errorf("after watch: file usage at request 2 = %d (want 0), request 3 = %d (want 6)",
			e.fileUsageAt("claude-code", s.at(2)), e.fileUsageAt("claude-code", s.at(3)))
	}
}

// Sessions seen through one path, and requests of other sessions, keep
// their usage
func TestClaudeUsageOnePathUnaffected(t *testing.T) {
	e := newOverlapEnv(t)
	fileOnly := claudeSession{id: "file-only", file: "/claude/a.jsonl", t0: overlapBase}
	otlpOnly := claudeSession{id: "otlp-only", file: "/claude/b.jsonl", t0: overlapBase}
	fileLogs, fileMetrics := fileOnly.fileRecords(2)
	e.importFile("claude-code", fileOnly.file, fileLogs, fileMetrics, false)
	logs, tokens := otlpOnly.otlp(0, 2)
	e.otlpLogs(logs, nil)
	e.otlpMetrics(tokens)

	if got := e.fileUsage("claude-code")["claude_code.token.usage"]; got != 4 {
		t.Errorf("file token points = %d, want 4", got)
	}
	var otlpPoints int
	e.store.db.QueryRow(`SELECT COUNT(*) FROM otel_metrics WHERE SourceFile IS NULL`).Scan(&otlpPoints)
	if otlpPoints != 1 {
		t.Errorf("OTLP points = %d, want 1", otlpPoints)
	}
}

// A tool that sends events but no metrics: the file's usage is the only one
func TestClaudeUsageKeptWithoutOTLPMetrics(t *testing.T) {
	e := newOverlapEnv(t)
	s := claudeSession{id: "s1", file: "/claude/s1.jsonl", t0: overlapBase}
	logs, _ := s.otlp(0, 2)
	e.otlpLogs(logs, nil)
	// Another session's metrics do not count
	e.otlpMetrics(claudeOTLPTokens("other", s.at(1), 5))
	fileLogs, fileMetrics := s.fileRecords(2)
	e.importFile("claude-code", s.file, fileLogs, fileMetrics, false)
	if got := e.fileUsage("claude-code")["claude_code.token.usage"]; got != 4 {
		t.Errorf("file token points = %d, want 4 when the session sent no metrics", got)
	}
}

// Two requests logged at the same time in a file: their points cannot be
// told apart, so they are only removed when OTLP recorded both
func TestClaudeUsageAmbiguousTimestampKept(t *testing.T) {
	e := newOverlapEnv(t)
	s := claudeSession{id: "s1", file: "/claude/s1.jsonl", t0: overlapBase}
	l1, m1 := claudeFileRequest(s.id, "req_a", s.t0, 1, 1)
	l2, m2 := claudeFileRequest(s.id, "req_b", s.t0, 2, 2)
	e.importFile("claude-code", s.file, append(l1, l2...), append(m1, m2...), false)
	e.otlpLogs([]api.LogRecord{claudeOTLPRequest(s.id, "req_a", s.t0.Add(time.Second))}, nil)
	e.otlpMetrics(claudeOTLPTokens(s.id, s.t0.Add(time.Minute), 3))
	if got := e.fileUsageAt("claude-code", s.t0); got != 12 {
		t.Errorf("file usage = %d points, want all 12 while one of the two requests is not recorded by OTLP", got)
	}
	e.otlpLogs([]api.LogRecord{claudeOTLPRequest(s.id, "req_b", s.t0.Add(2*time.Second))}, nil)
	if got := e.fileUsageAt("claude-code", s.t0); got != 0 {
		t.Errorf("file usage = %d points, want 0 once OTLP recorded both", got)
	}
}

// No session file of the tool was ever read: OTLP inserts skip the
// reconciliation (and nothing changes)
func TestUsageOverlapNeedsImportState(t *testing.T) {
	e := newOverlapEnv(t)
	s := claudeSession{id: "s1", file: "/claude/s1.jsonl", t0: overlapBase}
	logs, tokens := s.otlp(0, 1)
	e.otlpLogs(logs, nil)
	e.otlpMetrics(tokens)
	has := false
	if err := e.store.inWriteTx(e.ctx, func(w *writeTx) error {
		var err error
		has, err = hasFileRecords(e.ctx, w, tools.Claude)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if has {
		t.Error("hasFileRecords = true without imports")
	}
}

// codexResponse returns the token and cost points of one response as the
// session file (fromFile) or OTLP (derived from response.completed) records it
func codexResponse(fromFile bool, session string, ts time.Time, input, cached, output, reasoning float64) []api.MetricDataPoint {
	service := "codex-app-server"
	base := map[string]string{"model": "gpt-6-astra"}
	if fromFile {
		service = "codex_cli_rs"
		base["import_source"] = "local_jsonl"
	} else {
		base["originator"] = "codex-tui"
	}
	if session != "" {
		base["session.id"] = session
	}
	var points []api.MetricDataPoint
	for _, t := range []struct {
		typ string
		v   float64
	}{{"input", input}, {"cacheRead", cached}, {"output", output}, {"reasoning", reasoning}} {
		if t.v == 0 {
			continue
		}
		attrs := map[string]string{"type": t.typ}
		for k, v := range base {
			attrs[k] = v
		}
		points = append(points, api.MetricDataPoint{Timestamp: ts, ServiceName: service, MetricName: "codex_cli_rs.token.usage",
			MetricType: "sum", Value: float(t.v), Attributes: attrs})
	}
	cost := map[string]string{}
	for k, v := range base {
		cost[k] = v
	}
	return append(points, api.MetricDataPoint{Timestamp: ts, ServiceName: service, MetricName: "codex_cli_rs.cost.usage",
		MetricType: "sum", Value: float(0.01), Attributes: cost})
}

func TestCodexUsageBothOrders(t *testing.T) {
	ms := time.Millisecond
	t0 := overlapBase
	file := func() []api.MetricDataPoint {
		return append(append(codexResponse(true, "c1", t0, 23049, 20000, 300, 64),
			codexResponse(true, "c1", t0.Add(10*time.Second), 25979, 23000, 120, 0)...),
			codexResponse(true, "c1", t0.Add(20*time.Second), 27957, 25000, 80, 10)...) // only in the file
	}
	otlp := func() []api.MetricDataPoint {
		return append(codexResponse(false, "c1", t0.Add(-2*ms), 23049, 20000, 300, 64),
			codexResponse(false, "c1", t0.Add(10*time.Second-12*ms), 25979, 23000, 120, 0)...)
	}
	check := func(t *testing.T, e *overlapEnv) {
		t.Helper()
		if got := e.fileUsageAt("codex_cli_rs", t0) + e.fileUsageAt("codex_cli_rs", t0.Add(10*time.Second)); got != 0 {
			t.Errorf("file usage of responses OTLP recorded = %d points, want 0", got)
		}
		if got := e.fileUsageAt("codex_cli_rs", t0.Add(20*time.Second)); got != 5 {
			t.Errorf("file usage of the response only the file records = %d points, want 5", got)
		}
		if got := e.count("otel_metrics") - e.fileUsageAt("codex_cli_rs", t0.Add(20*time.Second)); got != 4+1+3+1 {
			t.Errorf("OTLP points = %d, want 9", got)
		}
	}
	t.Run("OTLP first, then import", func(t *testing.T) {
		e := newOverlapEnv(t)
		e.otlpLogs(nil, otlp())
		e.importFile("codex", "/codex/rollout-c1.jsonl", nil, file(), false)
		check(t, e)
	})
	t.Run("watch first, then OTLP", func(t *testing.T) {
		e := newOverlapEnv(t)
		e.watchFile("codex", "/codex/rollout-c1.jsonl", nil, file())
		e.otlpLogs(nil, otlp())
		check(t, e)
	})
	t.Run("re-import", func(t *testing.T) {
		e := newOverlapEnv(t)
		e.importFile("codex", "/codex/rollout-c1.jsonl", nil, file(), false)
		e.otlpLogs(nil, otlp())
		e.importFile("codex", "/codex/rollout-c1.jsonl", nil, file(), true)
		check(t, e)
	})
}

// Responses are only matched with equal counts of every token type, within
// the window, in the same session
func TestCodexUsageNoFalseMatches(t *testing.T) {
	t0 := overlapBase
	tests := []struct {
		name string
		otlp []api.MetricDataPoint
	}{
		{"other session", codexResponse(false, "c2", t0, 23049, 20000, 300, 64)},
		{"other counts", codexResponse(false, "c1", t0, 23049, 20000, 301, 64)},
		{"missing token type", codexResponse(false, "c1", t0, 23049, 20000, 300, 0)},
		{"outside the window", codexResponse(false, "c1", t0.Add(time.Minute), 23049, 20000, 300, 64)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newOverlapEnv(t)
			e.importFile("codex", "/codex/rollout-c1.jsonl", nil, codexResponse(true, "c1", t0, 23049, 20000, 300, 64), false)
			e.otlpLogs(nil, tt.otlp)
			if got := e.fileUsageAt("codex_cli_rs", t0); got != 5 {
				t.Errorf("file usage = %d points, want all 5 kept", got)
			}
		})
	}
	// OTLP usage stored before it carried session.id still matches
	e := newOverlapEnv(t)
	e.importFile("codex", "/codex/rollout-c1.jsonl", nil, codexResponse(true, "c1", t0, 23049, 20000, 300, 64), false)
	e.otlpLogs(nil, codexResponse(false, "", t0.Add(time.Millisecond), 23049, 20000, 300, 64))
	if got := e.fileUsageAt("codex_cli_rs", t0); got != 0 {
		t.Errorf("file usage = %d points, want 0 for OTLP usage without session", got)
	}
}
