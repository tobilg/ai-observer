package restore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tobilg/ai-observer/internal/api"
	"github.com/tobilg/ai-observer/internal/exporter"
	"github.com/tobilg/ai-observer/internal/importer"
	"github.com/tobilg/ai-observer/internal/redact"
	"github.com/tobilg/ai-observer/internal/storage"
	"github.com/tobilg/ai-observer/internal/watcher"
)

// sessionFiles are synthesized session files of the three file-backed
// tools under root, which the importers and the watcher read (the tools'
// path settings point at it)
type sessionFiles struct {
	root                  string
	claude, codex, gemini string
}

func newSessionFiles(t *testing.T, root string) *sessionFiles {
	t.Helper()
	f := &sessionFiles{
		root:   root,
		claude: filepath.Join(root, "claude", "-home-user-project", "claude-session.jsonl"),
		codex:  filepath.Join(root, "codex", "2025", "01", "02", "rollout-2025-01-02T11-00-00-codex-session.jsonl"),
		gemini: filepath.Join(root, "gemini", "project", "chats", "session-gemini.json"),
	}
	f.use(t)
	return f
}

// use points the importers and the watcher at the files
func (f *sessionFiles) use(t *testing.T) {
	t.Setenv("AI_OBSERVER_CLAUDE_PATH", filepath.Join(f.root, "claude"))
	t.Setenv("AI_OBSERVER_CODEX_PATH", filepath.Join(f.root, "codex"))
	t.Setenv("AI_OBSERVER_GEMINI_PATH", filepath.Join(f.root, "gemini"))
}

// write writes the sessions with turns turns each. A later write with more
// turns appends to the JSONL files (their earlier bytes stay the same) and
// rewrites the Gemini file with more messages, as the tools do.
func (f *sessionFiles) write(t *testing.T, turns int) {
	t.Helper()
	at := func(hour, minute int) string {
		return time.Date(2025, 1, 2, hour, minute, 0, 0, time.UTC).Format("2006-01-02T15:04:05.000Z")
	}
	var claude, codex strings.Builder
	codex.WriteString(`{"timestamp":"` + at(11, 0) + `","type":"session_meta","payload":{"id":"codex-session","timestamp":"` + at(11, 0) +
		`","cwd":"/home/user/project","originator":"codex_cli_rs","cli_version":"0.50.0","model_provider":"openai","model":"gpt-5"}}` + "\n")
	var geminiMessages []string
	for i := 0; i < turns; i++ {
		fmt.Fprintf(&claude, `{"parentUuid":null,"type":"user","message":{"role":"user","content":"prompt %[1]d"},"uuid":"u-%[1]d","timestamp":"%[2]s","sessionId":"claude-session","cwd":"/home/user/project"}`+"\n",
			i, at(10, 2*i))
		fmt.Fprintf(&claude, `{"type":"assistant","timestamp":"%[2]s","sessionId":"claude-session","uuid":"a-%[1]d","requestId":"req-%[1]d","cwd":"/home/user/project","message":{"id":"msg-%[1]d","model":"claude-sonnet-4-20250514","role":"assistant","type":"message","content":[{"type":"text","text":"reply %[1]d"}],"usage":{"input_tokens":%[3]d,"output_tokens":%[4]d,"cache_read_input_tokens":10}}}`+"\n",
			i, at(10, 2*i+1), 1000+i, 100+i)
		fmt.Fprintf(&codex, `{"timestamp":"%[2]s","type":"event_msg","payload":{"type":"user_message","message":"question %[1]d"}}`+"\n", i, at(11, 2*i+1))
		fmt.Fprintf(&codex, `{"timestamp":"%[2]s","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":%[3]d,"cached_input_tokens":0,"output_tokens":%[4]d,"reasoning_output_tokens":0},"last_token_usage":{"input_tokens":500,"cached_input_tokens":0,"output_tokens":50,"reasoning_output_tokens":0}}}}`+"\n",
			i, at(11, 2*i+2), 500*(i+1), 50*(i+1))
		geminiMessages = append(geminiMessages,
			fmt.Sprintf(`{"id": "m-%[1]d-user", "timestamp": "%[2]s", "type": "user", "content": "ask %[1]d"}`, i, at(12, 2*i)),
			fmt.Sprintf(`{"id": "m-%[1]d-gemini", "timestamp": "%[2]s", "type": "gemini", "content": "answer %[1]d", "model": "gemini-2.5-flash", "tokens": {"input": 400, "output": 40, "cached": 0, "thoughts": 0, "tool": 0, "total": 440}}`, i, at(12, 2*i+1)))
	}
	gemini := `{"sessionId": "gemini-session", "projectHash": "project", "startTime": "` + at(12, 0) + `", "lastUpdated": "` + at(12, 2*turns) +
		`", "messages": [` + strings.Join(geminiMessages, ",\n") + "]}\n"
	for path, content := range map[string]string{f.claude: claude.String(), f.codex: codex.String(), f.gemini: gemini} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// openRedactingStore opens a database with the default redaction, as the
// commands do
func openRedactingStore(t *testing.T, name string) *storage.DuckDBStore {
	t.Helper()
	store := openStore(t, name)
	store.SetRedactor(redact.New(redact.Secrets))
	return store
}

// addOTLPRecords stores telemetry as OTLP ingestion does, besides the
// imported sessions
func addOTLPRecords(t *testing.T, store *storage.DuckDBStore) {
	t.Helper()
	ctx := context.Background()
	at := time.Date(2025, 1, 2, 9, 0, 0, 0, time.UTC)
	cumulative := int32(2)
	var logs []api.LogRecord
	var spans []api.Span
	var metrics []api.MetricDataPoint
	for i := 0; i < 4; i++ {
		ts := at.Add(time.Duration(i) * time.Minute)
		total := float64(100 * (i + 1))
		logs = append(logs, api.LogRecord{Timestamp: ts, ServiceName: "claude-code", Body: "claude_code.user_prompt",
			LogAttributes: map[string]string{"session.id": "otlp-session", "event.name": "user_prompt", "prompt": fmt.Sprintf("live %d", i)}})
		spans = append(spans, api.Span{Timestamp: ts, TraceID: "otlp-trace", SpanID: fmt.Sprintf("span-%d", i), SpanName: "chat",
			ServiceName: "copilot-chat", Duration: 1000,
			SpanAttributes: map[string]string{"gen_ai.conversation.id": "copilot-session", "gen_ai.operation.name": "chat"}})
		metrics = append(metrics, api.MetricDataPoint{Timestamp: ts, ServiceName: "copilot-chat", MetricName: "copilot_chat.session.count",
			MetricType: "sum", Value: &total, AggregationTemporality: &cumulative, Attributes: map[string]string{"k": "v"}})
	}
	if err := store.InsertLogs(ctx, logs); err != nil {
		t.Fatal(err)
	}
	if err := store.InsertSpans(ctx, spans); err != nil {
		t.Fatal(err)
	}
	if err := store.InsertMetrics(ctx, metrics); err != nil {
		t.Fatal(err)
	}
}

// importFiles imports the session files of all tools into store and
// returns the importer's output
func importFiles(t *testing.T, store *storage.DuckDBStore) string {
	t.Helper()
	var out bytes.Buffer
	imp := importer.NewImporter(store, false)
	imp.SetIO(&out, strings.NewReader(""))
	imp.RegisterAllParsers()
	if err := imp.Import(context.Background(), importer.AllSources(), importer.Options{SkipConfirm: true}); err != nil {
		t.Fatalf("import: %v\n%s", err, out.String())
	}
	return out.String()
}

// watchFiles runs the watcher on store until it has read the session files
// to their ends, and stops it
func watchFiles(t *testing.T, store *storage.DuckDBStore, f *sessionFiles, geminiMessages int) {
	t.Helper()
	ctx := context.Background()
	w := watcher.New(store, nil, watcher.Config{DebounceMs: 10})
	if err := w.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer w.Stop()
	size := func(path string) int64 {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		return info.Size()
	}
	done := func() bool {
		for source, path := range map[string]string{"claude-code": f.claude, "codex": f.codex} {
			state, err := store.GetImportState(ctx, source, path)
			if err != nil || state == nil || state.ByteOffset != size(path) {
				return false
			}
		}
		state, err := store.GetImportState(ctx, "gemini", f.gemini)
		return err == nil && state != nil && state.MessageCount == geminiMessages
	}
	deadline := time.Now().Add(20 * time.Second)
	for !done() {
		if time.Now().After(deadline) {
			t.Fatal("the watcher did not read the session files to their ends")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// fileState are the stored rows: all columns of all tables (SourceFile
// included), the sessions list and the import state. Without importedAt,
// the import state leaves out imported_at, the time of the last import.
func fileState(t *testing.T, store *storage.DuckDBStore, importedAt bool) map[string]string {
	t.Helper()
	results := map[string]string{}
	for _, table := range []string{"otel_traces", "otel_logs", "otel_metrics", "metric_baselines", "metric_names"} {
		results["table "+table] = digest(t, store, "SELECT * FROM "+table)
	}
	stateQuery := "SELECT * FROM import_state"
	if !importedAt {
		stateQuery = "SELECT * EXCLUDE (imported_at) FROM import_state"
	}
	results["import_state"] = digest(t, store, stateQuery)
	sessions, err := store.QuerySessions(context.Background(), "",
		time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), storage.Page{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	results["sessions"] = jsonOf(t, sessions.Sessions)
	return results
}

// digest identifies the rows of a query
func digest(t *testing.T, store *storage.DuckDBStore, query string) string {
	t.Helper()
	var count int64
	var sum *string
	if err := store.DB().QueryRow(`SELECT COUNT(*), md5(string_agg(CAST(r AS VARCHAR), '|' ORDER BY CAST(r AS VARCHAR)))
		FROM (`+query+`) r`).Scan(&count, &sum); err != nil {
		t.Fatalf("digest of %s: %v", query, err)
	}
	if sum == nil {
		return "0 rows"
	}
	return fmt.Sprintf("%d:%s", count, *sum)
}

func countRows(t *testing.T, store *storage.DuckDBStore, query string) int64 {
	t.Helper()
	var n int64
	if err := store.DB().QueryRow(query).Scan(&n); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return n
}

// sourceWithSessions is a database with imported session files of all
// three tools and OTLP records, and its export
func sourceWithSessions(t *testing.T) (*storage.DuckDBStore, *sessionFiles, string) {
	t.Helper()
	f := newSessionFiles(t, t.TempDir())
	f.write(t, 3)
	source := openRedactingStore(t, "source.duckdb")
	addOTLPRecords(t, source)
	importFiles(t, source)
	for _, table := range []string{"otel_logs", "otel_metrics"} {
		for _, path := range []string{f.claude, f.codex, f.gemini} {
			if countRows(t, source, "SELECT COUNT(*) FROM "+table+" WHERE SourceFile = '"+path+"'") == 0 {
				t.Fatalf("the source has no %s of %s", table, path)
			}
		}
	}
	if n := countRows(t, source, "SELECT COUNT(*) FROM import_state"); n != 3 {
		t.Fatalf("the source has %d import states, want 3", n)
	}
	dir := export(t, source, exporter.Options{})
	return source, f, dir
}

// Export, restore into a new database, then import the same files again:
// every record is stored once, and the records, sessions and import state
// equal the source's, also after the files grew and both imported them
func TestSessionFilesRoundTrip(t *testing.T) {
	source, f, dir := sourceWithSessions(t)
	want := fileState(t, source, true)

	m, ok, err := exporter.ReadManifest(dir)
	if err != nil || !ok || m.Format != 2 || m.Rows[exporter.ImportStateFile] != 3 {
		t.Fatalf("manifest = %+v, %v, %v; want format 2 with 3 import states", m, ok, err)
	}

	target := openRedactingStore(t, "target.duckdb")
	out, err := restore(t, target, dir, false)
	if err != nil {
		t.Fatalf("restore: %v\n%s", err, out)
	}
	for _, text := range []string{"Session files:    3 (import state)", "Text is redacted again", "Text was redacted again"} {
		if !strings.Contains(out, text) {
			t.Errorf("restore output lacks %q:\n%s", text, out)
		}
	}
	if strings.Contains(out, "not on this machine") || strings.Contains(out, "--purge") {
		t.Errorf("restore output warns about session files:\n%s", out)
	}
	compareResults(t, want, fileState(t, target, true))

	// (a) Unchanged files: the import skips them
	imported := importFiles(t, target)
	if strings.Count(imported, "Imported 0 files") != 3 {
		t.Errorf("import after restore read files again:\n%s", imported)
	}
	compareResults(t, want, fileState(t, target, true))

	// (b) Grown files: the import replaces their records, as in the source
	f.write(t, 5)
	importFiles(t, source)
	importFiles(t, target)
	grown := fileState(t, source, false)
	if grown["table otel_logs"] == want["table otel_logs"] {
		t.Fatal("importing the grown files stored nothing")
	}
	compareResults(t, grown, fileState(t, target, false))
}

// Watch resumes at the restored offsets: after the files grew, watching
// the restored database stores what watching the source stores
func TestSessionFilesWatchResumes(t *testing.T) {
	source, f, dir := sourceWithSessions(t)
	target := openRedactingStore(t, "target.duckdb")
	if out, err := restore(t, target, dir, false); err != nil {
		t.Fatalf("restore: %v\n%s", err, out)
	}

	f.write(t, 5)
	watchFiles(t, source, f, 10)
	watchFiles(t, target, f, 10)
	want := fileState(t, source, false)
	before := countRows(t, source, "SELECT COUNT(*) FROM otel_logs WHERE SourceFile IS NOT NULL")
	if before == 0 {
		t.Fatal("no file records")
	}
	compareResults(t, want, fileState(t, target, false))
}

// (c) On another machine the session files are elsewhere: the records keep
// their paths and the restore says so; --map-path rewrites them, so the
// import recognizes the files and stores nothing twice
func TestSessionFilesElsewhere(t *testing.T) {
	source, f, dir := sourceWithSessions(t)
	logs := countRows(t, source, "SELECT COUNT(*) FROM otel_logs")
	metrics := countRows(t, source, "SELECT COUNT(*) FROM otel_metrics")

	// The files move to another home directory
	moved := filepath.Join(t.TempDir(), "elsewhere")
	if err := os.Rename(f.root, moved); err != nil {
		t.Fatal(err)
	}

	unmapped := openRedactingStore(t, "unmapped.duckdb")
	out, err := restore(t, unmapped, dir, false)
	if err != nil {
		t.Fatalf("restore: %v\n%s", err, out)
	}
	if !strings.Contains(out, "3 not on this machine") || !strings.Contains(out, "--map-path") {
		t.Errorf("restore does not report the missing session files:\n%s", out)
	}
	if n := countRows(t, unmapped, "SELECT COUNT(*) FROM otel_logs WHERE starts_with(SourceFile, '"+f.root+"')"); n == 0 {
		t.Error("restored records lost their session file paths")
	}

	mapped := openRedactingStore(t, "mapped.duckdb")
	var buf bytes.Buffer
	if err := Run(context.Background(), mapped, Options{Path: dir, SkipConfirm: true, Out: &buf,
		PathMap: []storage.PathMapping{{From: f.root, To: moved}}}); err != nil {
		t.Fatalf("restore with --map-path: %v\n%s", err, buf.String())
	}
	if strings.Contains(buf.String(), "not on this machine") {
		t.Errorf("mapped paths reported missing:\n%s", buf.String())
	}
	if n := countRows(t, mapped, "SELECT COUNT(*) FROM import_state WHERE starts_with(file_path, '"+moved+"')"); n != 3 {
		t.Errorf("%d import states mapped, want 3", n)
	}
	if n := countRows(t, mapped, "SELECT COUNT(*) FROM otel_logs WHERE starts_with(SourceFile, '"+f.root+"')"); n != 0 {
		t.Errorf("%d logs keep the old path", n)
	}

	moved2 := &sessionFiles{root: moved}
	moved2.use(t)
	imported := importFiles(t, mapped)
	if strings.Count(imported, "Imported 0 files") != 3 {
		t.Errorf("import after a mapped restore read files again:\n%s", imported)
	}
	if got := countRows(t, mapped, "SELECT COUNT(*) FROM otel_logs"); got != logs {
		t.Errorf("%d logs after importing the moved files, want %d", got, logs)
	}
	if got := countRows(t, mapped, "SELECT COUNT(*) FROM otel_metrics"); got != metrics {
		t.Errorf("%d metric points after importing the moved files, want %d", got, metrics)
	}
}

// (d) The database holds import state of the export's files. The same
// state is kept; another one marks the file to be read again, and the next
// import replaces its records, whichever side stored them.
func TestSessionFilesRestoreOverImportState(t *testing.T) {
	source, f, dir := sourceWithSessions(t)
	want := fileState(t, source, true)

	// The same state: restoring the export over its own database
	out, err := restore(t, source, dir, true)
	if err != nil {
		t.Fatalf("restore: %v\n%s", err, out)
	}
	if !strings.Contains(out, "(3 with the same state kept)") || strings.Contains(out, "read again") {
		t.Errorf("restore over the same state:\n%s", out)
	}
	compareResults(t, want, fileState(t, source, true))

	// Another state: the database imported the files after they grew
	target := openRedactingStore(t, "target.duckdb")
	f.write(t, 5)
	addOTLPRecords(t, target)
	importFiles(t, target)
	grown := fileState(t, target, false)

	if _, err := restore(t, target, dir, false); !errors.Is(err, ErrConflict) {
		t.Fatalf("restore without --replace: %v", err)
	}
	out, err = restore(t, target, dir, true)
	if err != nil {
		t.Fatalf("restore --replace: %v\n%s", err, out)
	}
	if !strings.Contains(out, "3 session files the database had imported with another state are read again") {
		t.Errorf("restore does not say the files are read again:\n%s", out)
	}
	if n := countRows(t, target, "SELECT COUNT(*) FROM import_state WHERE partial AND byte_offset = 0 AND message_count = 0"); n != 3 {
		t.Errorf("%d files marked to be read again, want 3", n)
	}
	imported := importFiles(t, target)
	if strings.Contains(imported, "Imported 0 files") {
		t.Errorf("the import skipped files marked to be read again:\n%s", imported)
	}
	after := fileState(t, target, false)
	for _, key := range []string{"table otel_logs", "table otel_metrics", "table otel_traces", "sessions"} {
		if after[key] != grown[key] {
			t.Errorf("%s differs from the files' records after reading them again:\n got  %.200s\n want %.200s", key, after[key], grown[key])
		}
	}
}

// A ranged export carries the import state of the files with records in
// the range; files with records outside it are read again after a restore,
// which completes them without storing anything twice
func TestSessionFilesRangedExport(t *testing.T) {
	source, f, _ := sourceWithSessions(t)
	// Claude's records are at 10:00, Codex's at 11:00, Gemini's at 12:00:
	// the range holds Claude's first turn and all of Codex's records
	from := time.Date(2025, 1, 2, 10, 0, 0, 0, time.UTC)
	to := time.Date(2025, 1, 2, 11, 59, 0, 0, time.UTC)
	dir := export(t, source, exporter.Options{FromDate: &from, ToDate: &to, Source: exporter.SourceAll})

	target := openRedactingStore(t, "target.duckdb")
	if out, err := restore(t, target, dir, false); err != nil {
		t.Fatalf("restore: %v\n%s", err, out)
	}
	states := map[string]bool{}
	rows, err := target.DB().Query("SELECT file_path, partial AND byte_offset = 0 FROM import_state")
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var path string
		var reread bool
		if err := rows.Scan(&path, &reread); err != nil {
			t.Fatal(err)
		}
		states[path] = reread
	}
	rows.Close()
	if reread, ok := states[f.claude]; !ok || reread {
		// Claude's records at 10:00 to 10:05 are all in the range
		t.Errorf("claude state = %v (present %v), want restored as imported", reread, ok)
	}
	if reread, ok := states[f.codex]; !ok || reread {
		t.Errorf("codex state = %v (present %v), want restored as imported", reread, ok)
	}
	if _, ok := states[f.gemini]; ok {
		t.Error("the state of a file without records in the range was exported")
	}

	// A range that cuts a file: its state is marked to be read again
	to = time.Date(2025, 1, 2, 10, 2, 0, 0, time.UTC)
	cut := export(t, source, exporter.Options{FromDate: &from, ToDate: &to, Source: exporter.SourceClaude})
	partial := openRedactingStore(t, "partial.duckdb")
	if out, err := restore(t, partial, cut, false); err != nil {
		t.Fatalf("restore: %v\n%s", err, out)
	}
	if n := countRows(t, partial, "SELECT COUNT(*) FROM import_state WHERE partial AND byte_offset = 0 AND message_count = 0 AND source = 'claude-code'"); n != 1 {
		t.Fatalf("%d cut files marked to be read again, want 1", n)
	}
	// The import completes the file's records and stores none twice
	importFiles(t, partial)
	for _, table := range []string{"otel_logs", "otel_metrics"} {
		query := "SELECT * EXCLUDE (SourceFile) FROM " + table + " WHERE SourceFile = '" + f.claude + "'"
		if got, want := digest(t, partial, query), digest(t, source, query); got != want {
			t.Errorf("%s of the cut file after the import: %s, want %s", table, got, want)
		}
	}
}

func TestRestoreRemapsCodexUsageMatch(t *testing.T) {
	ctx := context.Background()
	src := openStore(t, "source.duckdb")
	value := 100.0
	metric := api.MetricDataPoint{Timestamp: base, ServiceName: "codex_cli_rs", MetricName: "codex_cli_rs.token.usage",
		MetricType: "sum", Value: &value, Attributes: map[string]string{"type": "input", "import_source": "local_jsonl", "session.id": "s"}}
	state := &storage.ImportState{Source: "codex", FilePath: "/old/s.jsonl", ImportedAt: base, SourceTracked: true}
	if err := src.InsertImportedFile(ctx, nil, []api.MetricDataPoint{metric}, nil, state, nil); err != nil {
		t.Fatal(err)
	}
	otlp := metric
	otlp.ServiceName = "codex-app-server"
	otlp.Attributes = map[string]string{"type": "input", "session.id": "s"}
	if err := src.InsertMetrics(ctx, []api.MetricDataPoint{otlp}); err != nil {
		t.Fatal(err)
	}
	dir := export(t, src, exporter.Options{})
	dst := openStore(t, "target.duckdb")
	if err := Run(ctx, dst, Options{Path: dir, SkipConfirm: true, Out: &bytes.Buffer{}, PathMap: []storage.PathMapping{{From: "/old", To: "/new"}}}); err != nil {
		t.Fatal(err)
	}
	if n := countRows(t, dst, "SELECT COUNT(*) FROM otel_metrics WHERE CodexMatch.file_path = '/new/s.jsonl'"); n != 1 {
		t.Fatalf("restored %d remapped matches, want 1", n)
	}
	state.FilePath = "/new/s.jsonl"
	metric.Timestamp = base.Add(time.Second)
	if err := dst.InsertWatcherBatch(ctx, nil, []api.MetricDataPoint{metric}, nil, state, false); err != nil {
		t.Fatal(err)
	}
	if n := countRows(t, dst, "SELECT COUNT(*) FROM otel_metrics"); n != 2 {
		t.Fatalf("stored %d responses after new file event, want 2", n)
	}
}
