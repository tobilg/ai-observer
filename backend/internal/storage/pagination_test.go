package storage

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/tobilg/ai-observer/internal/api"
)

func TestNewestFirstAfter(t *testing.T) {
	sql, args := newestFirstAfter("T", []string{"a", "b"}, "2026-01-01 00:00:00", []any{"x", "y"})
	wantSQL := "(T < ?::TIMESTAMP OR (T = ?::TIMESTAMP AND (a < ? OR (a = ? AND b < ?))))"
	if sql != wantSQL {
		t.Errorf("sql = %s\nwant  %s", sql, wantSQL)
	}
	if fmt.Sprint(args) != "[2026-01-01 00:00:00 2026-01-01 00:00:00 x x y]" {
		t.Errorf("args = %v", args)
	}
}

func TestDecodeCursorRejectsInvalidCursors(t *testing.T) {
	valid := encodeCursor(pageCursor{Time: "2026-01-01 00:00:00", Keys: []string{"k"}})
	if _, err := decodeCursor(valid, 1); err != nil {
		t.Fatalf("valid cursor: %v", err)
	}
	for _, c := range []string{"not base64!", "e30", valid} { // e30 is "{}" (no time)
		if _, err := decodeCursor(c, 2); !errors.Is(err, ErrInvalidCursor) {
			t.Errorf("decodeCursor(%q) = %v, want ErrInvalidCursor", c, err)
		}
	}
}

// Walking all pages returns every log once, also when many logs share a
// timestamp, and logs stored meanwhile do not shift the following pages
func TestQueryLogsCursorPages(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	var logs []api.LogRecord
	for i := range 25 {
		// Groups of five logs with the same timestamp
		logs = append(logs, api.LogRecord{Timestamp: now.Add(-time.Duration(i/5) * time.Minute), ServiceName: "svc", Body: fmt.Sprintf("log-%02d", i)})
	}
	if err := store.InsertLogs(ctx, logs); err != nil {
		t.Fatal(err)
	}

	seen := map[string]int{}
	page := Page{Limit: 7}
	for pages := 0; ; pages++ {
		resp, err := store.QueryLogs(ctx, "", "", "", "", now.Add(-time.Hour), now.Add(time.Hour), page)
		if err != nil {
			t.Fatal(err)
		}
		if pages == 0 && resp.Total != 25 {
			t.Errorf("total = %d, want 25", resp.Total)
		}
		for _, l := range resp.Logs {
			seen[l.Body]++
		}
		if pages == 0 {
			// Newer than everything: must not appear on later pages
			if err := store.InsertLogs(ctx, []api.LogRecord{{Timestamp: now.Add(time.Minute), ServiceName: "svc", Body: "late"}}); err != nil {
				t.Fatal(err)
			}
		}
		if !resp.HasMore {
			if resp.NextCursor != "" {
				t.Error("last page has a next cursor")
			}
			break
		}
		page.Cursor = resp.NextCursor
		if pages > 10 {
			t.Fatal("pagination does not end")
		}
	}
	if len(seen) != 25 {
		t.Errorf("distinct logs = %d, want 25: %v", len(seen), seen)
	}
	for body, n := range seen {
		if n != 1 {
			t.Errorf("%s returned %d times", body, n)
		}
	}

	if _, err := store.QueryLogs(ctx, "", "", "", "", now.Add(-time.Hour), now, Page{Limit: 5, Cursor: "garbage"}); !errors.Is(err, ErrInvalidCursor) {
		t.Errorf("invalid cursor: err = %v, want ErrInvalidCursor", err)
	}
}

func TestQueryTracesSessionsAndMetricsCursorPages(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	value := 1.0

	var spans []api.Span
	var logs []api.LogRecord
	var metrics []api.MetricDataPoint
	for i := range 12 {
		ts := now.Add(-time.Duration(i/3) * time.Minute) // ties in groups of three
		spans = append(spans, api.Span{Timestamp: ts, TraceID: fmt.Sprintf("trace-%02d", i), SpanID: fmt.Sprintf("span-%02d", i), SpanName: "op", ServiceName: "svc"})
		logs = append(logs, api.LogRecord{Timestamp: ts, ServiceName: "svc", LogAttributes: map[string]string{"session.id": fmt.Sprintf("session-%02d", i)}})
		metrics = append(metrics, api.MetricDataPoint{Timestamp: ts, ServiceName: "svc", MetricName: "m", MetricType: "gauge", Value: &value, Attributes: map[string]string{"i": fmt.Sprint(i)}})
	}
	if err := store.InsertTelemetry(ctx, logs, metrics, spans); err != nil {
		t.Fatal(err)
	}
	from, to := now.Add(-time.Hour), now.Add(time.Hour)

	walk := func(name string, query func(Page) ([]string, bool, string, error)) {
		t.Helper()
		seen := map[string]bool{}
		page := Page{Limit: 5}
		for range 10 {
			ids, hasMore, next, err := query(page)
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			for _, id := range ids {
				if seen[id] {
					t.Errorf("%s: %s returned twice", name, id)
				}
				seen[id] = true
			}
			if !hasMore {
				break
			}
			page.Cursor = next
		}
		if len(seen) != 12 {
			t.Errorf("%s: distinct rows = %d, want 12", name, len(seen))
		}
	}

	walk("traces", func(p Page) ([]string, bool, string, error) {
		resp, err := store.QueryTraces(ctx, "", "", from, to, p)
		if err != nil {
			return nil, false, "", err
		}
		var ids []string
		for _, tr := range resp.Traces {
			ids = append(ids, tr.TraceID)
		}
		return ids, resp.HasMore, resp.NextCursor, nil
	})
	walk("sessions", func(p Page) ([]string, bool, string, error) {
		resp, err := store.QuerySessions(ctx, "", from, to, p)
		if err != nil {
			return nil, false, "", err
		}
		var ids []string
		for _, s := range resp.Sessions {
			ids = append(ids, s.SessionID)
		}
		return ids, resp.HasMore, resp.NextCursor, nil
	})
	walk("metrics", func(p Page) ([]string, bool, string, error) {
		resp, err := store.QueryMetrics(ctx, "", "m", "", from, to, p)
		if err != nil {
			return nil, false, "", err
		}
		var ids []string
		for _, m := range resp.Metrics {
			ids = append(ids, m.Attributes["i"])
		}
		return ids, resp.HasMore, resp.NextCursor, nil
	})
}

// Offset pages keep working for API clients that still use them
func TestQueryLogsOffsetPages(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	var logs []api.LogRecord
	for i := range 6 {
		logs = append(logs, api.LogRecord{Timestamp: now.Add(-time.Duration(i) * time.Second), ServiceName: "svc", Body: fmt.Sprint(i)})
	}
	if err := store.InsertLogs(ctx, logs); err != nil {
		t.Fatal(err)
	}
	resp, err := store.QueryLogs(ctx, "", "", "", "", now.Add(-time.Hour), now, Page{Limit: 4, Offset: 4})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Logs) != 2 || resp.HasMore || resp.Logs[0].Body != "4" {
		t.Errorf("offset page = %d logs (first %q), hasMore %v; want logs 4 and 5 and no more", len(resp.Logs), resp.Logs[0].Body, resp.HasMore)
	}
}

// Pages of up to maxPageSize rows (plus the one that tells whether more
// follow) read wide columns only for their own rows
func TestLateMaterializationCoversListPages(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	// A new connection of the pool must see the setting too
	conn, err := store.db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	var value string
	if err := conn.QueryRowContext(context.Background(),
		"SELECT value FROM duckdb_settings() WHERE name = 'late_materialization_max_rows'").Scan(&value); err != nil {
		t.Fatal(err)
	}
	if value != fmt.Sprint(maxPageSize+1) {
		t.Errorf("late_materialization_max_rows = %s, want %d", value, maxPageSize+1)
	}
}

// Traces and sessions count their total in the page query. Every page
// reports the same total, also an empty one past the end.
func TestTracesAndSessionsTotalOnEveryPage(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	var spans []api.Span
	var logs []api.LogRecord
	for i := range 7 {
		ts := now.Add(-time.Duration(i) * time.Minute)
		spans = append(spans, api.Span{Timestamp: ts, TraceID: fmt.Sprintf("trace-%d", i), SpanID: fmt.Sprintf("span-%d", i), SpanName: "op", ServiceName: "svc"})
		logs = append(logs, api.LogRecord{Timestamp: ts, ServiceName: "svc", LogAttributes: map[string]string{"session.id": fmt.Sprintf("session-%d", i)}})
	}
	if err := store.InsertTelemetry(ctx, logs, nil, spans); err != nil {
		t.Fatal(err)
	}
	from, to := now.Add(-time.Hour), now.Add(time.Hour)

	pastEnd := encodeCursor(pageCursor{Time: formatTimeForDB(now.Add(-24 * time.Hour)), Keys: []string{"", ""}})
	traceCursor := encodeCursor(pageCursor{Time: formatTimeForDB(now.Add(-24 * time.Hour)), Keys: []string{""}})
	cases := []struct {
		name         string
		traces, sess Page
		tracesWant   int
		sessionsWant int
	}{
		{"first page", Page{Limit: 3}, Page{Limit: 3}, 3, 3},
		{"last offset page", Page{Limit: 3, Offset: 6}, Page{Limit: 3, Offset: 6}, 1, 1},
		{"offset past the end", Page{Limit: 3, Offset: 100}, Page{Limit: 3, Offset: 100}, 0, 0},
		{"cursor past the end", Page{Limit: 3, Cursor: traceCursor}, Page{Limit: 3, Cursor: pastEnd}, 0, 0},
	}
	for _, tc := range cases {
		traces, err := store.QueryTraces(ctx, "", "", from, to, tc.traces)
		if err != nil {
			t.Fatal(err)
		}
		if traces.Total != 7 || len(traces.Traces) != tc.tracesWant {
			t.Errorf("%s: traces = %d (total %d), want %d (total 7)", tc.name, len(traces.Traces), traces.Total, tc.tracesWant)
		}
		sessions, err := store.QuerySessions(ctx, "", from, to, tc.sess)
		if err != nil {
			t.Fatal(err)
		}
		if sessions.Total != 7 || len(sessions.Sessions) != tc.sessionsWant {
			t.Errorf("%s: sessions = %d (total %d), want %d (total 7)", tc.name, len(sessions.Sessions), sessions.Total, tc.sessionsWant)
		}
	}

	empty, err := store.QuerySessions(ctx, "", now.Add(-48*time.Hour), now.Add(-47*time.Hour), Page{Limit: 3})
	if err != nil {
		t.Fatal(err)
	}
	if empty.Total != 0 || len(empty.Sessions) != 0 {
		t.Errorf("sessions in an empty range = %d (total %d), want none", len(empty.Sessions), empty.Total)
	}
}

// Deep offsets are refused: cursor pages reach any depth without sorting
// every row before them
func TestListQueriesRefuseDeepOffsets(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	ctx := context.Background()
	now := time.Now().UTC()
	from, to := now.Add(-time.Hour), now

	deep := Page{Limit: 10, Offset: maxOffset + 1}
	queries := map[string]func(Page) error{
		"logs": func(p Page) error {
			_, err := store.QueryLogs(ctx, "", "", "", "", from, to, p)
			return err
		},
		"traces": func(p Page) error {
			_, err := store.QueryTraces(ctx, "", "", from, to, p)
			return err
		},
		"sessions": func(p Page) error {
			_, err := store.QuerySessions(ctx, "", from, to, p)
			return err
		},
		"metrics": func(p Page) error {
			_, err := store.QueryMetrics(ctx, "", "", "", from, to, p)
			return err
		},
	}
	for name, query := range queries {
		if err := query(deep); !errors.Is(err, ErrOffsetTooLarge) {
			t.Errorf("%s with offset %d: error = %v, want ErrOffsetTooLarge", name, deep.Offset, err)
		}
		if err := query(Page{Limit: 10, Offset: maxOffset}); err != nil {
			t.Errorf("%s with offset %d: %v", name, maxOffset, err)
		}
	}
}

func TestPageLimit(t *testing.T) {
	cases := []struct {
		page          Page
		limit, offset int
	}{
		{Page{}, 50, 0},
		{Page{Limit: 5000, Offset: 20}, maxPageSize, 20},
		// A cursor page ignores the offset, also one over the limit
		{Page{Limit: 10, Offset: maxOffset + 1, Cursor: "c"}, 10, 0},
	}
	for _, c := range cases {
		limit, offset, err := pageLimit(c.page)
		if err != nil || limit != c.limit || offset != c.offset {
			t.Errorf("pageLimit(%+v) = %d, %d, %v; want %d, %d", c.page, limit, offset, err, c.limit, c.offset)
		}
	}
}
