package restore

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tobilg/ai-observer/internal/api"
	"github.com/tobilg/ai-observer/internal/exporter"
	"github.com/tobilg/ai-observer/internal/redact"
	"github.com/tobilg/ai-observer/internal/storage"
)

// fakeKey builds an Anthropic-shaped key at runtime, so secret scanners do
// not flag the repository
func fakeKey(suffix string) string {
	return "sk-ant-api03-" + strings.Repeat("Ab3x", 10) + suffix
}

// fillSecrets stores records with secrets in every column ingestion
// redacts, in a database that does not redact, and two cumulative series
// whose attributes differ only by a secret, of which a deleted day leaves
// baselines
func fillSecrets(t *testing.T, store *storage.DuckDBStore) {
	t.Helper()
	ctx := context.Background()
	key := fakeKey("Q")
	cumulative := int32(2)
	var metrics []api.MetricDataPoint
	for day := 0; day < 2; day++ {
		for i, suffix := range []string{"A", "B"} {
			at := base.Add(time.Duration(day)*24*time.Hour + time.Duration(i)*time.Minute)
			total := float64(100*(day+1) + i)
			metrics = append(metrics, api.MetricDataPoint{Timestamp: at, ServiceName: "gemini_cli", MetricName: "gemini_cli.token.usage",
				MetricType: "sum", Value: &total, AggregationTemporality: &cumulative,
				Attributes:         map[string]string{"type": "input", "auth": fakeKey(suffix)},
				ResourceAttributes: map[string]string{"env": "token " + key}})
		}
	}
	if err := store.InsertMetrics(ctx, metrics); err != nil {
		t.Fatal(err)
	}
	at := base.Add(24*time.Hour + time.Hour)
	if err := store.InsertLogs(ctx, []api.LogRecord{{Timestamp: at, ServiceName: "claude-code", Body: "key " + key,
		LogAttributes:      map[string]string{"session.id": "secret-session", "event.name": "user_prompt", "prompt": "use " + key},
		ResourceAttributes: map[string]string{"r": key}, ScopeAttributes: map[string]string{"s": key}}}); err != nil {
		t.Fatal(err)
	}
	if err := store.InsertSpans(ctx, []api.Span{{Timestamp: at, TraceID: "secret-trace", SpanID: "1", SpanName: "chat",
		ServiceName: "copilot-chat", StatusMessage: "failed with " + key,
		SpanAttributes:     map[string]string{"gen_ai.conversation.id": "secret-chat", "input": key},
		ResourceAttributes: map[string]string{"r": key},
		Events:             []api.SpanEvent{{Timestamp: at, Name: "e", Attributes: map[string]string{"content": key}}},
		Links:              []api.SpanLink{{TraceID: "other", SpanID: "2", Attributes: map[string]string{"l": key}}}}}); err != nil {
		t.Fatal(err)
	}
	// The baselines of the first day keep the secrets of their series
	if _, err := store.DeleteAllInRange(ctx, time.Time{}, base.Add(12*time.Hour), ""); err != nil {
		t.Fatal(err)
	}
}

// countContaining counts the rows of table whose text contains s
func countContaining(t *testing.T, store *storage.DuckDBStore, table, s string) int64 {
	t.Helper()
	return countRows(t, store, "SELECT COUNT(*) FROM (SELECT * FROM "+table+") r WHERE contains(CAST(r AS VARCHAR), '"+s+"')")
}

// Restored text is redacted like ingested text, after the session IDs are
// read; baselines keep naming their (redacted) series
func TestRestoreRedacts(t *testing.T) {
	source := openStore(t, "source.duckdb")
	fillSecrets(t, source)
	if countContaining(t, source, "metric_baselines", "sk-ant-") == 0 {
		t.Fatal("the source has no baselines with secrets")
	}
	dir := export(t, source, exporter.Options{})

	target := openRedactingStore(t, "target.duckdb")
	out, err := restore(t, target, dir, false)
	if err != nil {
		t.Fatalf("restore: %v\n%s", err, out)
	}
	for _, table := range []string{"otel_logs", "otel_traces", "otel_metrics", "metric_baselines"} {
		if n := countContaining(t, target, table, "sk-ant-"); n != 0 {
			t.Errorf("%d rows of %s keep a secret", n, table)
		}
		if n := countContaining(t, target, table, "[REDACTED:anthropic_key]"); n == 0 {
			t.Errorf("no redacted rows in %s", table)
		}
	}
	for _, column := range []string{"Body", "LogAttributes", "ResourceAttributes", "ScopeAttributes"} {
		if n := countRows(t, target, "SELECT COUNT(*) FROM otel_logs WHERE contains(CAST("+column+" AS VARCHAR), '[REDACTED:')"); n != 1 {
			t.Errorf("otel_logs.%s not redacted", column)
		}
	}
	for _, column := range []string{"StatusMessage", "SpanAttributes", "ResourceAttributes", `"Events.Attributes"`, `"Links.Attributes"`} {
		if n := countRows(t, target, "SELECT COUNT(*) FROM otel_traces WHERE contains(CAST("+column+" AS VARCHAR), '[REDACTED:')"); n != 1 {
			t.Errorf("otel_traces.%s not redacted", column)
		}
	}
	if n := countRows(t, target, "SELECT COUNT(*) FROM otel_logs WHERE SessionId = 'secret-session'"); n != 1 {
		t.Error("the restored log lost its session")
	}

	// The restored database equals one that stored the same telemetry with
	// redaction: the two series are one after it, as ingestion makes them,
	// with one baseline, the later, so the series' totals agree too
	ingested := openRedactingStore(t, "ingested.duckdb")
	fillSecrets(t, ingested)
	for _, table := range []string{"otel_traces", "otel_logs", "otel_metrics", "metric_baselines", "metric_names"} {
		if got, want := tableDigest(t, target, table), tableDigest(t, ingested, table); got != want {
			t.Errorf("%s differs from a redacting ingestion: %s, want %s", table, got, want)
		}
	}
	if n := countRows(t, target, "SELECT COUNT(*) FROM metric_baselines"); n != 1 {
		t.Errorf("%d baselines, want the two series' one", n)
	}
	series := func(store *storage.DuckDBStore) string {
		total, err := store.QueryMetricSeries(context.Background(), "gemini_cli.token.usage", "", base.Add(-48*time.Hour), base.Add(72*time.Hour), 0, true)
		if err != nil {
			t.Fatal(err)
		}
		return jsonOf(t, total)
	}
	if got, want := series(target), series(ingested); got != want {
		t.Errorf("series differs from a redacting ingestion:\n got  %s\n want %s", got, want)
	}

	// Without redaction the restore is identical to the source
	plain := openStore(t, "plain.duckdb")
	plain.SetRedactor(redact.New(redact.Off))
	if out, err := restore(t, plain, dir, false); err != nil {
		t.Fatalf("restore: %v\n%s", err, out)
	}
	for _, table := range []string{"otel_traces", "otel_logs", "otel_metrics", "metric_baselines"} {
		if got, want := tableDigest(t, plain, table), tableDigest(t, source, table); got != want {
			t.Errorf("%s differs without redaction: %s, want %s", table, got, want)
		}
	}
	if got, want := series(plain), series(source); got != want {
		t.Errorf("series differs without redaction:\n got  %s\n want %s", got, want)
	}
}

// The redaction functions a restore registers are not stored in the
// database: it opens again after the restore, without them
func TestRestoredDatabaseReopens(t *testing.T) {
	source := openStore(t, "source.duckdb")
	fillSecrets(t, source)
	dir := export(t, source, exporter.Options{})

	path := filepath.Join(t.TempDir(), "target.duckdb")
	target, err := storage.NewDuckDBStore(path)
	if err != nil {
		t.Fatal(err)
	}
	target.SetRedactor(redact.New(redact.Secrets))
	if out, err := restore(t, target, dir, false); err != nil {
		t.Fatalf("restore: %v\n%s", err, out)
	}
	if err := target.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := storage.NewDuckDBStore(path)
	if err != nil {
		t.Fatalf("reopening the restored database: %v", err)
	}
	defer reopened.Close()
	if n := countRows(t, reopened, "SELECT COUNT(*) FROM otel_logs WHERE contains(Body, '[REDACTED:')"); n != 1 {
		t.Errorf("%d redacted logs after reopening, want 1", n)
	}
	if err := reopened.InsertLogs(context.Background(), []api.LogRecord{{Timestamp: base, ServiceName: "claude-code", Body: "x"}}); err != nil {
		t.Errorf("writing after reopening: %v", err)
	}
}
