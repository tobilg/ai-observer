package storage

import (
	"context"
	"encoding/json"
	"math"
	"testing"
	"time"

	"github.com/tobilg/ai-observer/internal/api"
)

// Appenders marshal values written to JSON columns; serialized documents must
// be passed as json.RawMessage or they end up stored as JSON strings, which
// breaks every json_extract query. This guards all JSON columns.
func TestAppendedJSONColumnsHoldJSONValues(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	ctx := context.Background()
	now := time.Now()
	value := 1.5

	if err := store.InsertLogs(ctx, []api.LogRecord{{
		Timestamp:          now,
		ServiceName:        "svc",
		ResourceAttributes: map[string]string{"service.name": "svc"},
		LogAttributes:      map[string]string{"session.id": "s1", "cmd": "a<b && c>d"},
	}}); err != nil {
		t.Fatalf("InsertLogs failed: %v", err)
	}
	if err := store.InsertMetrics(ctx, []api.MetricDataPoint{{
		Timestamp:      now,
		ServiceName:    "svc",
		MetricName:     "m",
		MetricType:     "histogram",
		Value:          &value,
		Attributes:     map[string]string{"type": "input"},
		BucketCounts:   []uint64{1, 2},
		ExplicitBounds: []float64{0.5},
	}}); err != nil {
		t.Fatalf("InsertMetrics failed: %v", err)
	}
	if err := store.InsertSpans(ctx, []api.Span{{
		Timestamp:      now,
		TraceID:        "t1",
		SpanID:         "s1",
		SpanName:       "span",
		ServiceName:    "svc",
		SpanAttributes: map[string]string{"gen_ai.conversation.id": "c1"},
		Events:         []api.SpanEvent{{Timestamp: now, Name: "e", Attributes: map[string]string{"k": "v"}}},
	}}); err != nil {
		t.Fatalf("InsertSpans failed: %v", err)
	}

	checks := []struct {
		query string
		want  string
	}{
		{"SELECT json_type(LogAttributes) FROM otel_logs", "OBJECT"},
		{"SELECT json_extract_string(LogAttributes, '$.cmd') FROM otel_logs", "a<b && c>d"},
		{"SELECT json_type(ResourceAttributes) FROM otel_logs", "OBJECT"},
		{"SELECT json_extract_string(Attributes, '$.type') FROM otel_metrics", "input"},
		{"SELECT json_type(BucketCounts) FROM otel_metrics", "ARRAY"},
		{"SELECT json_extract_string(SpanAttributes, '$.\"gen_ai.conversation.id\"') FROM otel_traces", "c1"},
		{"SELECT json_extract_string(\"Events.Name\", '$[0]') FROM otel_traces", "e"},
		{"SELECT json_type(\"Links.TraceId\") FROM otel_traces", "ARRAY"},
	}
	for _, c := range checks {
		var got string
		if err := store.db.QueryRowContext(ctx, c.query).Scan(&got); err != nil {
			t.Errorf("%s: %v", c.query, err)
			continue
		}
		if got != c.want {
			t.Errorf("%s = %q, want %q", c.query, got, c.want)
		}
	}
}

func TestInsertSpans_SkipsSpansStoredByEarlierBatches(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	ctx := context.Background()
	now := time.Now()

	span := func(service, spanID string) api.Span {
		return api.Span{Timestamp: now, TraceID: "trace-1", SpanID: spanID, SpanName: "op", ServiceName: service}
	}

	if err := store.InsertSpans(ctx, []api.Span{span("svc-a", "span-1")}); err != nil {
		t.Fatalf("first InsertSpans failed: %v", err)
	}
	// A retried span, a new span, and the same span ID from another service
	if err := store.InsertSpans(ctx, []api.Span{span("svc-a", "span-1"), span("svc-a", "span-2"), span("svc-b", "span-1")}); err != nil {
		t.Fatalf("second InsertSpans failed: %v", err)
	}

	var count int
	if err := store.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM otel_traces").Scan(&count); err != nil {
		t.Fatalf("counting spans: %v", err)
	}
	if count != 3 {
		t.Errorf("expected 3 stored spans, got %d", count)
	}
}

// Reads must not wait for writes: a large import holds the write lock for the
// whole transaction, and dashboards should keep loading meanwhile.
func TestReadsAreNotBlockedByWrites(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	store.mu.Lock() // simulate a long-running write transaction
	defer store.mu.Unlock()

	done := make(chan error, 1)
	go func() {
		_, err := store.QueryLogs(context.Background(), "", "", "", "", time.Now().Add(-time.Hour), time.Now(), Page{Limit: 10})
		done <- err
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("QueryLogs failed: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("read blocked while the write lock was held")
	}
}

func TestFloat64ArrayToStringKeepsPrecision(t *testing.T) {
	got := float64ArrayToString([]float64{1e-7, 2.5, 1234567.891, math.NaN(), math.Inf(1)})
	want := "[1e-07, 2.5, 1234567.891, null, null]"
	if got != want {
		t.Errorf("float64ArrayToString = %s, want %s", got, want)
	}
	if !json.Valid([]byte(got)) {
		t.Errorf("%s is not valid JSON", got)
	}
}

func TestHistogramWithNaNQuantileIsStored(t *testing.T) {
	store, err := NewDuckDBStore(":memory:")
	if err != nil {
		t.Fatalf("creating store: %v", err)
	}
	defer store.Close()
	ctx := context.Background()

	err = store.InsertMetrics(ctx, []api.MetricDataPoint{{
		Timestamp:      time.Now(),
		ServiceName:    "svc",
		MetricName:     "latency",
		MetricType:     "histogram",
		ExplicitBounds: []float64{1e-7, 0.5},
		QuantileValues: []float64{math.NaN()},
	}})
	if err != nil {
		t.Fatalf("InsertMetrics: %v", err)
	}

	var first float64
	if err := store.db.QueryRowContext(ctx, "SELECT CAST(ExplicitBounds->>0 AS DOUBLE) FROM otel_metrics").Scan(&first); err != nil {
		t.Fatalf("reading bounds: %v", err)
	}
	if first != 1e-7 {
		t.Errorf("first bound = %v, want 1e-7", first)
	}
}
