package server

import (
	"context"
	"fmt"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/tobilg/ai-observer/internal/api"
	appMiddleware "github.com/tobilg/ai-observer/internal/middleware"
	"github.com/tobilg/ai-observer/internal/storage"
)

// fillDays stores days of logs ending now, 130,000 a day: more than a
// DuckDB row group (122,880 rows), whose blocks a checkpoint frees only
// when most of its rows are deleted
func fillDays(t *testing.T, s *Server, days int, now time.Time) {
	t.Helper()
	ctx := context.Background()
	// Text that does not compress, like real prompts and tool output
	rng := rand.New(rand.NewPCG(1, 2))
	for d := 0; d < days; d++ {
		logs := make([]api.LogRecord, 130_000)
		for i := range logs {
			logs[i] = api.LogRecord{
				Timestamp:   now.Add(-time.Duration(d)*24*time.Hour - time.Duration(i)*500*time.Millisecond),
				ServiceName: "claude-code", Body: fmt.Sprintf("log %d %d %016x%016x", d, i, rng.Uint64(), rng.Uint64()),
			}
		}
		if err := s.storage.InsertLogs(ctx, logs); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.storage.Checkpoint(ctx); err != nil {
		t.Fatal(err)
	}
}

func newTestServer(t *testing.T) *Server {
	t.Helper()
	s, err := New(getTestConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.storage.Close() })
	return s
}

func usedBytes(t *testing.T, s *Server) int64 {
	t.Helper()
	size, err := s.storage.DatabaseSize(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return size.UsedBytes()
}

func oldestLog(t *testing.T, s *Server) time.Time {
	t.Helper()
	oldest, ok, err := s.storage.OldestTimestamp(context.Background())
	if err != nil || !ok {
		t.Fatalf("OldestTimestamp: %v %v", ok, err)
	}
	return oldest
}

// Above AI_OBSERVER_MAX_DB_SIZE the oldest days are deleted until the used
// blocks are within it
func TestSizeLimitDeletesOldestDays(t *testing.T) {
	s := newTestServer(t)
	now := time.Now()
	fillDays(t, s, 6, now)
	used := usedBytes(t, s)
	s.config.MaxDBSize = used * 6 / 10

	var guard sizeGuard
	s.applySizeLimit(context.Background(), &guard, now)
	if got := usedBytes(t, s); got > s.config.MaxDBSize {
		t.Errorf("used %d bytes after applying a limit of %d (before %d)", got, s.config.MaxDBSize, used)
	}
	if oldest := oldestLog(t, s); oldest.Before(now.Add(-5 * 24 * time.Hour)) {
		t.Errorf("oldest day kept: oldest record %v", oldest)
	}
	// Blocks are freed by whole row groups, so a day more may go, but not
	// the newest days (the second one spans 24 to 42 hours ago; a chunk is
	// 24 hours from the oldest record, which may reach its first records)
	recent, err := s.storage.CountLogsInRange(context.Background(), now.Add(-41*time.Hour), now, "")
	if err != nil || recent != 130_000+122_401 {
		t.Errorf("logs of the two newest days: %d, %v", recent, err)
	}
	if guard.pending {
		t.Error("deletion marked pending although its space was freed")
	}
}

// The last day is kept even above the limit
func TestSizeLimitKeepsLastDay(t *testing.T) {
	s := newTestServer(t)
	now := time.Now()
	fillDays(t, s, 3, now)
	s.config.MaxDBSize = 1
	var guard sizeGuard
	s.applySizeLimit(context.Background(), &guard, now)
	n, err := s.storage.CountLogsInRange(context.Background(), now.Add(-sizeLimitKeep), now, "")
	if err != nil || n == 0 {
		t.Fatalf("logs of the last day: %d, %v", n, err)
	}
	if oldest := oldestLog(t, s); oldest.Before(now.Add(-sizeLimitKeep)) {
		t.Errorf("records older than a day kept: %v", oldest)
	}
}

// fakeSizeStore is a database whose deletions do not show as freed space
type fakeSizeStore struct {
	used       int64
	oldest     time.Time
	deletions  int
	generation uint64
}

func (f *fakeSizeStore) Checkpoint(context.Context) error { return nil }
func (f *fakeSizeStore) DatabaseSize(context.Context) (storage.DatabaseSize, error) {
	return storage.DatabaseSize{BlockSize: 1, UsedBlocks: f.used, TotalBlocks: f.used}, nil
}
func (f *fakeSizeStore) OldestTimestamp(context.Context) (time.Time, bool, error) {
	return f.oldest, true, nil
}
func (f *fakeSizeStore) DeleteAllInRange(_ context.Context, _, to time.Time, _ string) (*storage.DeleteSummary, error) {
	f.deletions++
	f.generation++
	f.oldest = to
	return &storage.DeleteSummary{SpanCount: 50_000}, nil
}
func (f *fakeSizeStore) WriteGeneration() uint64 { return f.generation }

// When a deletion's space does not show as free, the guard deletes no
// further days on that stale measurement until new data has been written
func TestSizeLimitWaitsForFreedSpace(t *testing.T) {
	now := time.Now()
	store := &fakeSizeStore{used: 1000, oldest: now.AddDate(0, 0, -10)}
	var guard sizeGuard
	applySizeLimit(context.Background(), store, 500, &guard, now)
	if store.deletions != 1 || !guard.pending {
		t.Fatalf("%d deletions, pending %v; want 1 and pending", store.deletions, guard.pending)
	}
	applySizeLimit(context.Background(), store, 500, &guard, now)
	if store.deletions != 1 {
		t.Errorf("deleted again without new data: %d deletions", store.deletions)
	}
	store.generation++ // new data
	store.used = 400   // and the space shows as freed
	applySizeLimit(context.Background(), store, 500, &guard, now)
	if store.deletions != 1 || guard.pending {
		t.Errorf("after the space was freed: %d deletions, pending %v", store.deletions, guard.pending)
	}
	store.used = 1000
	store.generation++
	applySizeLimit(context.Background(), store, 500, &guard, now)
	if store.deletions != 2 {
		t.Errorf("over the limit again: %d deletions, want 2", store.deletions)
	}
}

// Retention checkpoints after deleting, so the write-ahead log is written
// and new data reuses the freed blocks
func TestRetentionCheckpointsAfterDeleting(t *testing.T) {
	s := newTestServer(t)
	ctx := context.Background()
	now := time.Now()
	if err := s.storage.InsertLogs(ctx, []api.LogRecord{
		{Timestamp: now.AddDate(0, 0, -40), ServiceName: "old"},
		{Timestamp: now, ServiceName: "recent"},
	}); err != nil {
		t.Fatal(err)
	}
	s.applyRetention(ctx, now.AddDate(0, 0, -30))
	size, err := s.storage.DatabaseSize(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if size.WALBytes != 0 {
		t.Errorf("write-ahead log of %d bytes after retention deleted records", size.WALBytes)
	}
}

func TestRetentionStartsForSizeLimitOnly(t *testing.T) {
	cfg := getTestConfig(t)
	cfg.MaxDBSize = 10 << 30
	s, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	s.startRetention()
	if s.stopRetention == nil {
		t.Fatal("size limit did not start the retention loop")
	}
	if err := s.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestAdminAPIRoutes(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		cfg := getTestConfig(t)
		cfg.AdminAPI = enabled
		s, err := New(cfg)
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPost, "/api/admin/checkpoint", nil)
		req.Host = "localhost:18080"
		rec := httptest.NewRecorder()
		s.apiRouter.ServeHTTP(rec, req)
		if enabled && rec.Code != http.StatusOK {
			t.Errorf("enabled checkpoint: %d %s", rec.Code, rec.Body.String())
		}
		if !enabled && (rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "AI_OBSERVER_ADMIN_API")) {
			t.Errorf("disabled checkpoint: %d %s", rec.Code, rec.Body.String())
		}
		if enabled {
			// Write endpoints only
			req = httptest.NewRequest(http.MethodGet, "/api/admin/checkpoint", nil)
			req.Host = "localhost:18080"
			rec = httptest.NewRecorder()
			s.apiRouter.ServeHTTP(rec, req)
			if rec.Code != http.StatusMethodNotAllowed {
				t.Errorf("GET checkpoint: %d", rec.Code)
			}
			// A cross-site request is rejected like other API requests
			req = httptest.NewRequest(http.MethodPost, "/api/admin/delete", strings.NewReader(`{}`))
			req.Host = "localhost:18080"
			req.Header.Set("Origin", "https://attacker.example.com")
			rec = httptest.NewRecorder()
			s.apiRouter.ServeHTTP(rec, req)
			if rec.Code != http.StatusForbidden {
				t.Errorf("cross-site delete: %d", rec.Code)
			}
		}
		s.storage.Close()
	}
}

// Admin requests run without the API's request timeout, other API requests
// with it
func TestAdminPathsHaveNoRequestTimeout(t *testing.T) {
	var deadline bool
	handler := exceptPrefix(adminPathPrefix, appMiddleware.DefaultContextTimeoutMiddleware)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, deadline = r.Context().Deadline()
		}))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/api/admin/backup", nil))
	if deadline {
		t.Error("admin request has a deadline")
	}
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/logs", nil))
	if !deadline {
		t.Error("API request without a deadline")
	}
}
