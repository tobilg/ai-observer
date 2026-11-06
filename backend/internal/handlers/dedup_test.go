package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/tobilg/ai-observer/internal/storage"
	"github.com/tobilg/ai-observer/pkg/compression"
)

func testKey(s string) batchKey {
	h := newBatchHasher("logs")
	h.Write([]byte(s))
	return h.key()
}

func TestBatchDedupClaimAndRelease(t *testing.T) {
	d := newBatchDedup()
	ctx := context.Background()
	key := testKey("a")

	if claimed, _ := d.claim(ctx, key); !claimed {
		t.Fatal("first claim = false, want true")
	}
	d.release(key, true)
	if claimed, _ := d.claim(ctx, key); claimed {
		t.Error("claim of a stored batch = true, want false")
	}
	if claimed, _ := d.claim(ctx, testKey("b")); !claimed {
		t.Error("claim of another batch = false, want true")
	}
}

func TestBatchDedupForgetsFailedBatches(t *testing.T) {
	d := newBatchDedup()
	ctx := context.Background()
	key := testKey("a")

	d.claim(ctx, key)
	d.release(key, false)
	if claimed, _ := d.claim(ctx, key); !claimed {
		t.Error("claim after a failed store = false, want true: the retry must be stored")
	}
}

func TestBatchDedupExpires(t *testing.T) {
	d := newBatchDedup()
	now := time.Now()
	d.now = func() time.Time { return now }
	ctx := context.Background()
	key := testKey("a")

	d.claim(ctx, key)
	d.release(key, true)
	now = now.Add(dedupTTL + time.Second)
	if claimed, _ := d.claim(ctx, key); !claimed {
		t.Error("claim after the TTL = false, want true")
	}
}

func TestBatchDedupIsBounded(t *testing.T) {
	d := newBatchDedup()
	d.max = 2
	ctx := context.Background()
	for _, s := range []string{"a", "b", "c"} {
		d.claim(ctx, testKey(s))
		d.release(testKey(s), true)
	}
	if len(d.entries) != 2 || d.stored.Len() != 2 {
		t.Fatalf("entries = %d, stored = %d, want 2 and 2", len(d.entries), d.stored.Len())
	}
	if claimed, _ := d.claim(ctx, testKey("a")); !claimed {
		t.Error("the oldest batch must have been forgotten")
	}
}

// A request for a batch that is being stored waits for the outcome
func TestBatchDedupConcurrentClaimWaits(t *testing.T) {
	d := newBatchDedup()
	ctx := context.Background()
	key := testKey("a")
	d.claim(ctx, key)

	result := make(chan bool)
	go func() {
		claimed, _ := d.claim(ctx, key)
		result <- claimed
	}()
	select {
	case <-result:
		t.Fatal("second claim returned while the first was pending")
	case <-time.After(20 * time.Millisecond):
	}
	d.release(key, true)
	if <-result {
		t.Error("second claim = true after the batch was stored, want false")
	}

	ctx, cancel := context.WithCancel(context.Background())
	other := testKey("b")
	d.claim(ctx, other)
	cancel()
	if _, err := d.claim(ctx, other); err == nil {
		t.Error("claim with a cancelled context while pending: want an error")
	}
}

func countRows(t *testing.T, store *storage.DuckDBStore, table string) int {
	t.Helper()
	var n int
	if err := store.DB().QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func postOTLP(handler http.Handler, path string, body []byte, gzipped bool) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if gzipped {
		req.Header.Set("Content-Encoding", "gzip")
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

// An exporter that did not get the response sends the batch again; it is
// answered with success and stored once
func TestOTLPRetriedBatchIsStoredOnce(t *testing.T) {
	h, cleanup := setupTestHandlers(t)
	defer cleanup()

	logs, _ := json.Marshal(createLogsPayload())
	metrics, _ := json.Marshal(createMetricsPayload())
	compressed, err := gzipCompress(logs)
	if err != nil {
		t.Fatal(err)
	}
	logsHandler := compression.GzipDecompressMiddleware(http.HandlerFunc(h.HandleLogs))
	metricsHandler := http.HandlerFunc(h.HandleMetrics)

	if rec := postOTLP(logsHandler, "/v1/logs", logs, false); rec.Code != http.StatusOK {
		t.Fatalf("first logs export: status %d: %s", rec.Code, rec.Body)
	}
	storedLogs := countRows(t, h.store, "otel_logs")
	// The same batch again, once compressed: the decompressed body is compared
	for _, gzipped := range []bool{false, true} {
		body := logs
		if gzipped {
			body = compressed
		}
		if rec := postOTLP(logsHandler, "/v1/logs", body, gzipped); rec.Code != http.StatusOK {
			t.Fatalf("retried logs export (gzip %v): status %d: %s", gzipped, rec.Code, rec.Body)
		}
	}
	if got := countRows(t, h.store, "otel_logs"); got != storedLogs {
		t.Errorf("otel_logs rows = %d after retries, want %d", got, storedLogs)
	}

	postOTLP(metricsHandler, "/v1/metrics", metrics, false)
	storedMetrics := countRows(t, h.store, "otel_metrics")
	postOTLP(metricsHandler, "/v1/metrics", metrics, false)
	if got := countRows(t, h.store, "otel_metrics"); got != storedMetrics {
		t.Errorf("otel_metrics rows = %d after a retry, want %d", got, storedMetrics)
	}
}

// The same bytes sent for another signal are another batch
func TestBatchKeyIncludesSignal(t *testing.T) {
	logs, metrics := newBatchHasher("logs"), newBatchHasher("metrics")
	logs.Write([]byte("{}"))
	metrics.Write([]byte("{}"))
	if logs.key() == metrics.key() {
		t.Error("logs and metrics batches with the same body have the same key")
	}
}

func TestOTLPDistinctBatchesAreStored(t *testing.T) {
	h, cleanup := setupTestHandlers(t)
	defer cleanup()

	first := createLogsPayload()
	second := createLogsPayload()
	second.ResourceLogs[0].ScopeLogs[0].LogRecords[0].Body.StringValue = "another message"
	for _, payload := range []otlpLogsRequest{first, second} {
		body, _ := json.Marshal(payload)
		if rec := postOTLP(http.HandlerFunc(h.HandleLogs), "/v1/logs", body, false); rec.Code != http.StatusOK {
			t.Fatalf("status %d: %s", rec.Code, rec.Body)
		}
	}
	if got := countRows(t, h.store, "otel_logs"); got != 2 {
		t.Errorf("otel_logs rows = %d, want 2", got)
	}
}

// A batch that could not be stored is stored by the exporter's retry
func TestOTLPBatchFailedThenRetriedIsStoredOnce(t *testing.T) {
	h, cleanup := setupTestHandlers(t)
	defer cleanup()
	working := h.store

	broken, err := storage.NewDuckDBStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer broken.Close()
	if _, err := broken.DB().Exec("DROP TABLE otel_logs"); err != nil {
		t.Fatal(err)
	}

	body, _ := json.Marshal(createLogsPayload())
	h.store = broken
	if rec := postOTLP(http.HandlerFunc(h.HandleLogs), "/v1/logs", body, false); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status with a failing store = %d, want 503", rec.Code)
	}
	h.store = working
	if rec := postOTLP(http.HandlerFunc(h.HandleLogs), "/v1/logs", body, false); rec.Code != http.StatusOK {
		t.Fatalf("retry: status %d: %s", rec.Code, rec.Body)
	}
	if got := countRows(t, working, "otel_logs"); got != 1 {
		t.Errorf("otel_logs rows = %d, want 1", got)
	}
}

func TestOTLPConcurrentIdenticalBatchesAreStoredOnce(t *testing.T) {
	h, cleanup := setupTestHandlers(t)
	defer cleanup()
	body, _ := json.Marshal(createLogsPayload())

	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			if rec := postOTLP(http.HandlerFunc(h.HandleLogs), "/v1/logs", body, false); rec.Code != http.StatusOK {
				t.Errorf("status %d: %s", rec.Code, rec.Body)
			}
		})
	}
	wg.Wait()
	if got := countRows(t, h.store, "otel_logs"); got != 1 {
		t.Errorf("otel_logs rows = %d, want 1", got)
	}
}
