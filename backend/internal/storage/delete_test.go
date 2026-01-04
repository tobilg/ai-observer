package storage

import (
	"context"
	"testing"
	"time"

	"github.com/tobilg/ai-observer/internal/api"
)

func TestCountLogsInRange(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now()

	// Insert logs across different time ranges
	logs := []api.LogRecord{
		{Timestamp: now.Add(-2 * time.Hour), ServiceName: "svc-a", SeverityText: "INFO", Body: "old log"},
		{Timestamp: now.Add(-1 * time.Hour), ServiceName: "svc-a", SeverityText: "INFO", Body: "recent log 1"},
		{Timestamp: now.Add(-30 * time.Minute), ServiceName: "svc-b", SeverityText: "ERROR", Body: "recent log 2"},
		{Timestamp: now, ServiceName: "svc-a", SeverityText: "WARN", Body: "current log"},
	}
	store.InsertLogs(ctx, logs)

	// Count logs in the last hour
	from := now.Add(-1 * time.Hour)
	to := now.Add(1 * time.Minute)

	count, err := store.CountLogsInRange(ctx, from, to, "")
	if err != nil {
		t.Fatalf("CountLogsInRange failed: %v", err)
	}

	if count != 3 {
		t.Errorf("expected 3 logs in range, got %d", count)
	}
}

func TestCountLogsInRange_WithServiceFilter(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now()

	logs := []api.LogRecord{
		{Timestamp: now, ServiceName: "svc-a", SeverityText: "INFO", Body: "log 1"},
		{Timestamp: now, ServiceName: "svc-a", SeverityText: "INFO", Body: "log 2"},
		{Timestamp: now, ServiceName: "svc-b", SeverityText: "INFO", Body: "log 3"},
	}
	store.InsertLogs(ctx, logs)

	from := now.Add(-1 * time.Hour)
	to := now.Add(1 * time.Minute)

	count, err := store.CountLogsInRange(ctx, from, to, "svc-a")
	if err != nil {
		t.Fatalf("CountLogsInRange failed: %v", err)
	}

	if count != 2 {
		t.Errorf("expected 2 logs for svc-a, got %d", count)
	}
}

func TestCountMetricsInRange(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now()

	metrics := []api.MetricDataPoint{
		{Timestamp: now.Add(-2 * time.Hour), ServiceName: "svc", MetricName: "old_metric", MetricType: "gauge", Value: ptrFloat64(1.0)},
		{Timestamp: now, ServiceName: "svc", MetricName: "metric1", MetricType: "gauge", Value: ptrFloat64(2.0)},
		{Timestamp: now, ServiceName: "svc", MetricName: "metric2", MetricType: "gauge", Value: ptrFloat64(3.0)},
	}
	store.InsertMetrics(ctx, metrics)

	from := now.Add(-1 * time.Hour)
	to := now.Add(1 * time.Minute)

	count, err := store.CountMetricsInRange(ctx, from, to, "")
	if err != nil {
		t.Fatalf("CountMetricsInRange failed: %v", err)
	}

	if count != 2 {
		t.Errorf("expected 2 metrics in range, got %d", count)
	}
}

func TestCountTracesInRange(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now()

	spans := []api.Span{
		{TraceID: "trace-1", SpanID: "span-1", ServiceName: "svc", SpanName: "root", Timestamp: now, StatusCode: "OK"},
		{TraceID: "trace-1", SpanID: "span-2", ParentSpanID: "span-1", ServiceName: "svc", SpanName: "child", Timestamp: now, StatusCode: "OK"},
		{TraceID: "trace-2", SpanID: "span-3", ServiceName: "svc", SpanName: "root2", Timestamp: now, StatusCode: "OK"},
		{TraceID: "trace-old", SpanID: "span-old", ServiceName: "svc", SpanName: "old", Timestamp: now.Add(-2 * time.Hour), StatusCode: "OK"},
	}
	store.InsertSpans(ctx, spans)

	from := now.Add(-1 * time.Hour)
	to := now.Add(1 * time.Minute)

	traces, spans_count, err := store.CountTracesInRange(ctx, from, to, "")
	if err != nil {
		t.Fatalf("CountTracesInRange failed: %v", err)
	}

	if traces != 2 {
		t.Errorf("expected 2 traces in range, got %d", traces)
	}
	if spans_count != 3 {
		t.Errorf("expected 3 spans in range, got %d", spans_count)
	}
}

func TestDeleteLogsInRange(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now()

	logs := []api.LogRecord{
		{Timestamp: now.Add(-2 * time.Hour), ServiceName: "svc", SeverityText: "INFO", Body: "keep this"},
		{Timestamp: now, ServiceName: "svc", SeverityText: "INFO", Body: "delete this 1"},
		{Timestamp: now, ServiceName: "svc", SeverityText: "INFO", Body: "delete this 2"},
	}
	store.InsertLogs(ctx, logs)

	from := now.Add(-1 * time.Hour)
	to := now.Add(1 * time.Minute)

	deleted, err := store.DeleteLogsInRange(ctx, from, to, "")
	if err != nil {
		t.Fatalf("DeleteLogsInRange failed: %v", err)
	}

	if deleted != 2 {
		t.Errorf("expected 2 logs deleted, got %d", deleted)
	}

	// Verify remaining logs
	var remaining int
	store.db.QueryRow("SELECT COUNT(*) FROM otel_logs").Scan(&remaining)
	if remaining != 1 {
		t.Errorf("expected 1 log remaining, got %d", remaining)
	}
}

func TestDeleteLogsInRange_WithServiceFilter(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now()

	logs := []api.LogRecord{
		{Timestamp: now, ServiceName: "svc-a", SeverityText: "INFO", Body: "delete"},
		{Timestamp: now, ServiceName: "svc-a", SeverityText: "INFO", Body: "delete"},
		{Timestamp: now, ServiceName: "svc-b", SeverityText: "INFO", Body: "keep"},
	}
	store.InsertLogs(ctx, logs)

	from := now.Add(-1 * time.Hour)
	to := now.Add(1 * time.Minute)

	deleted, err := store.DeleteLogsInRange(ctx, from, to, "svc-a")
	if err != nil {
		t.Fatalf("DeleteLogsInRange failed: %v", err)
	}

	if deleted != 2 {
		t.Errorf("expected 2 logs deleted for svc-a, got %d", deleted)
	}

	// Verify svc-b logs remain
	var remaining int
	store.db.QueryRow("SELECT COUNT(*) FROM otel_logs WHERE ServiceName = 'svc-b'").Scan(&remaining)
	if remaining != 1 {
		t.Errorf("expected 1 svc-b log remaining, got %d", remaining)
	}
}

func TestDeleteMetricsInRange(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now()

	metrics := []api.MetricDataPoint{
		{Timestamp: now.Add(-2 * time.Hour), ServiceName: "svc", MetricName: "keep", MetricType: "gauge", Value: ptrFloat64(1.0)},
		{Timestamp: now, ServiceName: "svc", MetricName: "delete1", MetricType: "gauge", Value: ptrFloat64(2.0)},
		{Timestamp: now, ServiceName: "svc", MetricName: "delete2", MetricType: "gauge", Value: ptrFloat64(3.0)},
	}
	store.InsertMetrics(ctx, metrics)

	from := now.Add(-1 * time.Hour)
	to := now.Add(1 * time.Minute)

	deleted, err := store.DeleteMetricsInRange(ctx, from, to, "")
	if err != nil {
		t.Fatalf("DeleteMetricsInRange failed: %v", err)
	}

	if deleted != 2 {
		t.Errorf("expected 2 metrics deleted, got %d", deleted)
	}

	var remaining int
	store.db.QueryRow("SELECT COUNT(*) FROM otel_metrics").Scan(&remaining)
	if remaining != 1 {
		t.Errorf("expected 1 metric remaining, got %d", remaining)
	}
}

func TestDeleteTracesInRange(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now()

	spans := []api.Span{
		{TraceID: "trace-keep", SpanID: "span-keep", ServiceName: "svc", SpanName: "keep", Timestamp: now.Add(-2 * time.Hour), StatusCode: "OK"},
		{TraceID: "trace-del", SpanID: "span-del-1", ServiceName: "svc", SpanName: "delete1", Timestamp: now, StatusCode: "OK"},
		{TraceID: "trace-del", SpanID: "span-del-2", ParentSpanID: "span-del-1", ServiceName: "svc", SpanName: "delete2", Timestamp: now, StatusCode: "OK"},
	}
	store.InsertSpans(ctx, spans)

	from := now.Add(-1 * time.Hour)
	to := now.Add(1 * time.Minute)

	deleted, err := store.DeleteTracesInRange(ctx, from, to, "")
	if err != nil {
		t.Fatalf("DeleteTracesInRange failed: %v", err)
	}

	if deleted != 2 {
		t.Errorf("expected 2 spans deleted, got %d", deleted)
	}

	var remaining int
	store.db.QueryRow("SELECT COUNT(*) FROM otel_traces").Scan(&remaining)
	if remaining != 1 {
		t.Errorf("expected 1 span remaining, got %d", remaining)
	}
}

func TestCountAllInRange(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now()

	// Insert test data
	logs := []api.LogRecord{
		{Timestamp: now, ServiceName: "svc", SeverityText: "INFO", Body: "log1"},
		{Timestamp: now, ServiceName: "svc", SeverityText: "INFO", Body: "log2"},
	}
	store.InsertLogs(ctx, logs)

	metrics := []api.MetricDataPoint{
		{Timestamp: now, ServiceName: "svc", MetricName: "m1", MetricType: "gauge", Value: ptrFloat64(1.0)},
		{Timestamp: now, ServiceName: "svc", MetricName: "m2", MetricType: "gauge", Value: ptrFloat64(2.0)},
		{Timestamp: now, ServiceName: "svc", MetricName: "m3", MetricType: "gauge", Value: ptrFloat64(3.0)},
	}
	store.InsertMetrics(ctx, metrics)

	spans := []api.Span{
		{TraceID: "t1", SpanID: "s1", ServiceName: "svc", SpanName: "span1", Timestamp: now, StatusCode: "OK"},
		{TraceID: "t1", SpanID: "s2", ParentSpanID: "s1", ServiceName: "svc", SpanName: "span2", Timestamp: now, StatusCode: "OK"},
		{TraceID: "t2", SpanID: "s3", ServiceName: "svc", SpanName: "span3", Timestamp: now, StatusCode: "OK"},
	}
	store.InsertSpans(ctx, spans)

	from := now.Add(-1 * time.Hour)
	to := now.Add(1 * time.Minute)

	summary, err := store.CountAllInRange(ctx, from, to, "")
	if err != nil {
		t.Fatalf("CountAllInRange failed: %v", err)
	}

	if summary.LogCount != 2 {
		t.Errorf("expected 2 logs, got %d", summary.LogCount)
	}
	if summary.MetricCount != 3 {
		t.Errorf("expected 3 metrics, got %d", summary.MetricCount)
	}
	if summary.TraceCount != 2 {
		t.Errorf("expected 2 traces, got %d", summary.TraceCount)
	}
	if summary.SpanCount != 3 {
		t.Errorf("expected 3 spans, got %d", summary.SpanCount)
	}
}

func TestDeleteAllInRange(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now()

	// Insert test data
	logs := []api.LogRecord{
		{Timestamp: now.Add(-2 * time.Hour), ServiceName: "svc", SeverityText: "INFO", Body: "keep"},
		{Timestamp: now, ServiceName: "svc", SeverityText: "INFO", Body: "delete"},
	}
	store.InsertLogs(ctx, logs)

	metrics := []api.MetricDataPoint{
		{Timestamp: now.Add(-2 * time.Hour), ServiceName: "svc", MetricName: "keep", MetricType: "gauge", Value: ptrFloat64(1.0)},
		{Timestamp: now, ServiceName: "svc", MetricName: "delete", MetricType: "gauge", Value: ptrFloat64(2.0)},
	}
	store.InsertMetrics(ctx, metrics)

	spans := []api.Span{
		{TraceID: "t-keep", SpanID: "s-keep", ServiceName: "svc", SpanName: "keep", Timestamp: now.Add(-2 * time.Hour), StatusCode: "OK"},
		{TraceID: "t-del", SpanID: "s-del", ServiceName: "svc", SpanName: "delete", Timestamp: now, StatusCode: "OK"},
	}
	store.InsertSpans(ctx, spans)

	from := now.Add(-1 * time.Hour)
	to := now.Add(1 * time.Minute)

	summary, err := store.DeleteAllInRange(ctx, from, to, "")
	if err != nil {
		t.Fatalf("DeleteAllInRange failed: %v", err)
	}

	if summary.LogCount != 1 {
		t.Errorf("expected 1 log deleted, got %d", summary.LogCount)
	}
	if summary.MetricCount != 1 {
		t.Errorf("expected 1 metric deleted, got %d", summary.MetricCount)
	}
	if summary.SpanCount != 1 {
		t.Errorf("expected 1 span deleted, got %d", summary.SpanCount)
	}

	// Verify remaining counts
	var logCount, metricCount, spanCount int
	store.db.QueryRow("SELECT COUNT(*) FROM otel_logs").Scan(&logCount)
	store.db.QueryRow("SELECT COUNT(*) FROM otel_metrics").Scan(&metricCount)
	store.db.QueryRow("SELECT COUNT(*) FROM otel_traces").Scan(&spanCount)

	if logCount != 1 {
		t.Errorf("expected 1 log remaining, got %d", logCount)
	}
	if metricCount != 1 {
		t.Errorf("expected 1 metric remaining, got %d", metricCount)
	}
	if spanCount != 1 {
		t.Errorf("expected 1 span remaining, got %d", spanCount)
	}
}

func TestDeleteAllInRange_Empty(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now()

	from := now.Add(-1 * time.Hour)
	to := now.Add(1 * time.Minute)

	summary, err := store.DeleteAllInRange(ctx, from, to, "")
	if err != nil {
		t.Fatalf("DeleteAllInRange failed: %v", err)
	}

	if summary.LogCount != 0 || summary.MetricCount != 0 || summary.SpanCount != 0 {
		t.Errorf("expected all counts to be 0, got logs=%d, metrics=%d, spans=%d",
			summary.LogCount, summary.MetricCount, summary.SpanCount)
	}
}
