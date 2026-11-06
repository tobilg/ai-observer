package storage

import (
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestTelemetryIndexMigrationKeepsOnlyLookupIndexes(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "indexes.duckdb")

	store, err := NewDuckDBStore(dbPath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	// Indexes created by earlier versions
	for _, stmt := range []string{
		"CREATE INDEX idx_traces_service_timestamp ON otel_traces(ServiceName, Timestamp)",
		"CREATE INDEX idx_logs_timestamp ON otel_logs(Timestamp)",
		"CREATE INDEX idx_metrics_name_timestamp ON otel_metrics(MetricName, Timestamp)",
	} {
		if _, err := store.db.Exec(stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	if err := store.Close(); err != nil {
		t.Fatalf("failed to close store: %v", err)
	}

	reopened, err := NewDuckDBStore(dbPath)
	if err != nil {
		t.Fatalf("failed to reopen store: %v", err)
	}
	defer reopened.Close()

	rows, err := reopened.db.Query(`
		SELECT index_name FROM duckdb_indexes()
		WHERE table_name IN ('otel_traces', 'otel_logs', 'otel_metrics')
	`)
	if err != nil {
		t.Fatalf("failed to query indexes: %v", err)
	}
	defer rows.Close()

	var got []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("failed to scan index: %v", err)
		}
		got = append(got, name)
	}
	sort.Strings(got)

	want := []string{"idx_logs_trace_id", "idx_traces_span_id", "idx_traces_trace_id_v2"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("telemetry indexes = %v, want %v", got, want)
	}
}
