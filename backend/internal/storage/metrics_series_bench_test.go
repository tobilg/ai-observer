package storage

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/tobilg/ai-observer/internal/api"
)

// BenchmarkSeriesOnCopy measures series queries on a copy of a real
// database, which AI_OBSERVER_BENCH_DB_COPY names (it is migrated in place,
// so never pass the live database). The range is the 3 days before
// AI_OBSERVER_BENCH_DB_END (RFC 3339, default now).
//
//	AI_OBSERVER_BENCH_DB_COPY=/tmp/copy.duckdb go test -run '^$' -bench SeriesOnCopy ./internal/storage
func BenchmarkSeriesOnCopy(b *testing.B) {
	path := os.Getenv("AI_OBSERVER_BENCH_DB_COPY")
	if path == "" {
		b.Skip("AI_OBSERVER_BENCH_DB_COPY is not set")
	}
	end := time.Now()
	if v := os.Getenv("AI_OBSERVER_BENCH_DB_END"); v != "" {
		var err error
		if end, err = time.Parse(time.RFC3339, v); err != nil {
			b.Fatal(err)
		}
	}
	store, err := NewDuckDBStore(path)
	if err != nil {
		b.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	from := end.Add(-3 * 24 * time.Hour)

	cases := []struct {
		name string
		q    api.MetricQuery
		zone string
	}{
		{"sum_delta_53k", api.MetricQuery{Name: "codex.websocket.event"}, ""},
		{"sum_delta_53k_berlin", api.MetricQuery{Name: "codex.websocket.event"}, "Europe/Berlin"},
		{"per_event_12k", api.MetricQuery{Name: "codex_cli_rs.token.usage"}, ""},
		{"per_event_12k_aggregate", api.MetricQuery{Name: "codex_cli_rs.token.usage", Aggregate: true}, ""},
		{"per_event_12k_by_model", api.MetricQuery{Name: "codex_cli_rs.token.usage", GroupBy: []string{"model"}}, ""},
		{"per_event_12k_by_model_type", api.MetricQuery{Name: "codex_cli_rs.token.usage", GroupBy: []string{"model", "type"}}, ""},
		{"per_event_12k_filtered", api.MetricQuery{Name: "codex_cli_rs.token.usage", Filters: map[string]string{"type": "input"}}, ""},
		{"gauge_36k", api.MetricQuery{Name: "exec_server_connections_active"}, ""},
		{"histogram_delta_53k_sum", api.MetricQuery{Name: "codex.websocket.event.duration_ms", Stat: StatSum}, ""},
		{"histogram_delta_53k_avg", api.MetricQuery{Name: "codex.websocket.event.duration_ms"}, ""},
		{"histogram_delta_53k_p95", api.MetricQuery{Name: "codex.websocket.event.duration_ms", Stat: StatP95}, ""},
		{"histogram_delta_53k_p95_by_kind", api.MetricQuery{Name: "codex.websocket.event.duration_ms", Stat: StatP95, GroupBy: []string{"kind"}}, ""},
		{"histogram_cumulative_8k_sum", api.MetricQuery{Name: "gen_ai.client.operation.duration", Stat: StatSum}, ""},
		{"histogram_cumulative_8k_avg", api.MetricQuery{Name: "gen_ai.client.operation.duration"}, ""},
		{"histogram_cumulative_8k_p95", api.MetricQuery{Name: "gen_ai.client.operation.duration", Stat: StatP95}, ""},
	}
	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			r := SeriesRange{From: from, To: end, Interval: 3600, TimeZone: c.zone}
			for b.Loop() {
				if _, err := store.QuerySeries(ctx, c.q, r); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
