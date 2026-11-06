package storage

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tobilg/ai-observer/internal/api"
)

// Benchmarks of the query paths on a generated database. The database is
// built once per process and shared by all benchmarks. Its size is set by
// AI_OBSERVER_BENCH_ROWS (log records; spans and metric points are derived
// from it), e.g.:
//
//	AI_OBSERVER_BENCH_ROWS=2000000 go test -run '^$' -bench . ./internal/storage

const (
	benchDefaultRows = 200_000
	benchSessions    = 200
	benchDays        = 30
	benchInsertBatch = 5_000
)

var benchServices = []string{"claude-code", "codex_cli_rs", "gemini_cli", "copilot-chat"}

type benchDB struct {
	store *DuckDBStore
	dir   string
	end   time.Time // newest record
	// sessionID of a session in the middle of the range
	sessionID string
}

var (
	benchOnce   sync.Once
	benchShared *benchDB
	benchErr    error
)

// sharedBenchDB returns the generated database, creating it on first use
func sharedBenchDB(b *testing.B) *benchDB {
	b.Helper()
	benchOnce.Do(func() {
		rows := benchDefaultRows
		if v := os.Getenv("AI_OBSERVER_BENCH_ROWS"); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil || n <= 0 {
				benchErr = fmt.Errorf("invalid AI_OBSERVER_BENCH_ROWS %q", v)
				return
			}
			rows = n
		}
		benchShared, benchErr = seedBenchDB(rows)
	})
	if benchErr != nil {
		b.Fatalf("creating benchmark database: %v", benchErr)
	}
	return benchShared
}

// closeBenchDB removes the shared benchmark database, if one was created
func closeBenchDB() {
	if benchShared != nil {
		benchShared.store.Close()
		os.RemoveAll(benchShared.dir)
	}
}

// seedBenchDB writes rows log records plus rows/2 spans and rows metric
// points in time order, as OTLP ingestion would. Sessions follow each other
// over benchDays days, three of them running at the same time.
func seedBenchDB(rows int) (*benchDB, error) {
	dir, err := os.MkdirTemp("", "ai-observer-bench-")
	if err != nil {
		return nil, err
	}
	store, err := NewDuckDBStore(filepath.Join(dir, "bench.duckdb"))
	if err != nil {
		os.RemoveAll(dir)
		return nil, err
	}

	ctx := context.Background()
	end := time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC)
	start := end.Add(-benchDays * 24 * time.Hour)
	step := end.Sub(start) / time.Duration(rows)
	body := strings.Repeat("The quick brown fox jumps over the lazy dog. ", 50) // ~2 KB

	db := &benchDB{store: store, dir: dir, end: end}
	middle := benchSessions / 2

	cumulative := int32(2)
	delta := int32(1)
	totals := make(map[string]float64) // running totals of the cumulative series

	var logs []api.LogRecord
	var spans []api.Span
	var metrics []api.MetricDataPoint
	flush := func() error {
		err := store.InsertTelemetry(ctx, logs, metrics, spans)
		logs, spans, metrics = logs[:0], spans[:0], metrics[:0]
		return err
	}

	for i := 0; i < rows; i++ {
		ts := start.Add(time.Duration(i) * step)
		session := (i*benchSessions/rows + i%3) % benchSessions
		sessionID := fmt.Sprintf("session-%04d", session)
		service := benchServices[session%len(benchServices)]
		model := []string{"model-a", "model-b"}[session%2]
		if session == middle && db.sessionID == "" {
			db.sessionID = sessionID
		}

		event := []string{"user_prompt", "api_request", "tool_result"}[i%3]
		logs = append(logs, api.LogRecord{
			Timestamp:    ts,
			ServiceName:  service,
			SeverityText: []string{"INFO", "INFO", "WARN", "ERROR"}[i%4],
			Body:         fmt.Sprintf("%s %d %s", event, i, body),
			LogAttributes: map[string]string{
				"event.name": event,
				"session.id": sessionID,
				"model":      model,
				"prompt":     fmt.Sprintf("prompt %d", i),
			},
		})

		if i%2 == 0 {
			trace := fmt.Sprintf("trace-%08d", i/20)
			parent := ""
			if i%20 != 0 {
				parent = fmt.Sprintf("span-%08d", i-i%20)
			}
			spans = append(spans, api.Span{
				Timestamp:      ts,
				TraceID:        trace,
				SpanID:         fmt.Sprintf("span-%08d", i),
				ParentSpanID:   parent,
				SpanName:       "chat " + model,
				ServiceName:    service,
				Duration:       int64(250 * time.Millisecond),
				StatusCode:     []string{"OK", "OK", "OK", "ERROR"}[i%4],
				SpanAttributes: map[string]string{"session.id": sessionID, "gen_ai.request.model": model},
			})
		}

		tokenType := []string{"input", "output", "cache"}[i%3]
		key := sessionID + "/" + tokenType
		totals[key] += float64(100 + i%50)
		tokens := totals[key]
		cost := float64(i%50) / 1000
		metrics = append(metrics,
			api.MetricDataPoint{
				Timestamp:              ts,
				ServiceName:            "gemini_cli",
				MetricName:             "gemini_cli.token.usage",
				MetricType:             "sum",
				Value:                  &tokens,
				AggregationTemporality: &cumulative,
				Attributes:             map[string]string{"type": tokenType, "model": model, "session.id": sessionID},
			},
		)
		if i%2 == 0 {
			metrics = append(metrics, api.MetricDataPoint{
				Timestamp:              ts,
				ServiceName:            "claude-code",
				MetricName:             "claude_code.cost.usage",
				MetricType:             "sum",
				Value:                  &cost,
				AggregationTemporality: &delta,
				Attributes:             map[string]string{"model": model, "session.id": sessionID},
			})
		}

		if len(logs) >= benchInsertBatch {
			if err := flush(); err != nil {
				store.Close()
				os.RemoveAll(dir)
				return nil, err
			}
		}
	}
	if err := flush(); err != nil {
		store.Close()
		os.RemoveAll(dir)
		return nil, err
	}
	return db, nil
}

func BenchmarkQueryLogs(b *testing.B) {
	db := sharedBenchDB(b)
	ctx := context.Background()
	cases := []struct {
		name         string
		search       string
		window       time.Duration
		limit, offst int
	}{
		{"24h_first_page", "", 24 * time.Hour, 50, 0},
		{"30d_first_page", "", benchDays * 24 * time.Hour, 50, 0},
		{"30d_deep_page", "", benchDays * 24 * time.Hour, 50, 5000},
		{"7d_search", "prompt 12", 7 * 24 * time.Hour, 50, 0},
	}
	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			for b.Loop() {
				if _, err := db.store.QueryLogs(ctx, "", "", "", c.search, db.end.Add(-c.window), db.end, Page{Limit: c.limit, Offset: c.offst}); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkQueryTraces(b *testing.B) {
	db := sharedBenchDB(b)
	ctx := context.Background()
	for _, window := range []time.Duration{24 * time.Hour, benchDays * 24 * time.Hour} {
		b.Run(window.String(), func(b *testing.B) {
			for b.Loop() {
				if _, err := db.store.QueryTraces(ctx, "", "", db.end.Add(-window), db.end, Page{Limit: 50}); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkQuerySessions(b *testing.B) {
	db := sharedBenchDB(b)
	ctx := context.Background()
	for b.Loop() {
		if _, err := db.store.QuerySessions(ctx, "", db.end.Add(-benchDays*24*time.Hour), db.end, Page{Limit: 50}); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkSessionRecords reads a session's logs and spans as a transcript
// request does
func BenchmarkSessionRecords(b *testing.B) {
	db := sharedBenchDB(b)
	ctx := context.Background()
	for b.Loop() {
		if _, err := db.store.SessionLogs(ctx, db.sessionID); err != nil {
			b.Fatal(err)
		}
		if _, err := db.store.SessionSpans(ctx, db.sessionID); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkSearchSessions is the MCP conversation search: a substring scan of
// the session logs and spans in the range for one or several terms
func BenchmarkSearchSessions(b *testing.B) {
	db := sharedBenchDB(b)
	ctx := context.Background()
	for _, window := range []time.Duration{7 * 24 * time.Hour, benchDays * 24 * time.Hour} {
		for _, search := range []struct {
			name  string
			terms []string
		}{
			{"one_term", []string{"prompt 12"}},
			// Each term is in many records: every one is checked
			{"common_terms", []string{"user_prompt", "12", "prompt"}},
			// The rare term is the longest, so it is checked first and
			// leaves few records for the others
			{"rare_long_term", []string{"user", "prompt", "prompt 12"}},
		} {
			b.Run(fmt.Sprintf("%s_%s", window, search.name), func(b *testing.B) {
				for b.Loop() {
					if _, err := db.store.SearchSessions(ctx, search.terms, "", db.end.Add(-window), db.end, 10); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}

func BenchmarkGetStats(b *testing.B) {
	db := sharedBenchDB(b)
	ctx := context.Background()
	for b.Loop() {
		if _, err := db.store.GetStats(ctx); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkQueryMetricSeries(b *testing.B) {
	db := sharedBenchDB(b)
	ctx := context.Background()
	cases := []struct{ name, metric string }{
		{"cumulative_7d", "gemini_cli.token.usage"},
		{"delta_7d", "claude_code.cost.usage"},
	}
	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			for b.Loop() {
				if _, err := db.store.QueryMetricSeries(ctx, c.metric, "", db.end.Add(-7*24*time.Hour), db.end, 3600, false); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkInsertTelemetryParallel measures concurrent OTLP-sized inserts
// (50 logs and 20 metric points per request) into a fresh database
func BenchmarkInsertTelemetryParallel(b *testing.B) {
	store, err := NewDuckDBStore(filepath.Join(b.TempDir(), "insert.duckdb"))
	if err != nil {
		b.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()
	delta := int32(1)
	value := 1.0
	now := time.Now()
	logs := make([]api.LogRecord, 50)
	for i := range logs {
		logs[i] = api.LogRecord{
			Timestamp:     now,
			ServiceName:   "claude-code",
			Body:          "api_request",
			LogAttributes: map[string]string{"session.id": "s", "event.name": "api_request"},
		}
	}
	metrics := make([]api.MetricDataPoint, 20)
	for i := range metrics {
		metrics[i] = api.MetricDataPoint{
			Timestamp:              now,
			ServiceName:            "claude-code",
			MetricName:             "claude_code.token.usage",
			MetricType:             "sum",
			Value:                  &value,
			AggregationTemporality: &delta,
			Attributes:             map[string]string{"type": "input"},
		}
	}

	b.SetParallelism(4)
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if err := store.InsertTelemetry(ctx, logs, metrics, nil); err != nil {
				b.Error(err)
				return
			}
		}
	})
}
