package storage

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/tobilg/ai-observer/internal/api"
)

// Trace lists select the traces with spans in the range but describe each
// by all of its spans, as the trace detail does
func TestQueryTracesDescribesWholeTraces(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	ctx := context.Background()
	base := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	span := func(trace, id, parent string, at time.Duration, dur time.Duration, status string) api.Span {
		return api.Span{TraceID: trace, SpanID: id, ParentSpanID: parent, SpanName: "op " + id, ServiceName: "claude-code",
			Timestamp: base.Add(at), Duration: int64(dur), StatusCode: status}
	}
	spans := []api.Span{
		// straddle: began an hour before the range, failed before it
		span("straddle", "root", "", -time.Hour, 3*time.Hour, "OK"),
		span("straddle", "early", "root", -50*time.Minute, time.Minute, "ERROR"),
		span("straddle", "inside", "root", 30*time.Minute, time.Minute, "OK"),
		// inside: entirely in the range
		span("inside", "a", "", 10*time.Minute, time.Minute, "OK"),
		span("inside", "b", "a", 11*time.Minute, time.Minute, "OK"),
		// late: continues after the range
		span("late", "x", "", 50*time.Minute, time.Minute, "UNSET"),
		span("late", "y", "x", 2*time.Hour, time.Minute, "ERROR"),
	}
	if err := store.InsertSpans(ctx, spans); err != nil {
		t.Fatal(err)
	}
	from, to := base, base.Add(time.Hour)

	resp, err := store.QueryTraces(ctx, TraceFilter{}, from, to, Page{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Total != 3 || len(resp.Traces) != 3 {
		t.Fatalf("traces = %+v", resp)
	}
	// Ordered by the first span in the range: late (50m), straddle (30m),
	// inside (10m)
	got := map[string]api.TraceOverview{}
	for i, want := range []string{"late", "straddle", "inside"} {
		if resp.Traces[i].TraceID != want {
			t.Errorf("trace %d = %s, want %s", i, resp.Traces[i].TraceID, want)
		}
		got[resp.Traces[i].TraceID] = resp.Traces[i]
	}

	s := got["straddle"]
	if !s.StartTime.Equal(base.Add(-time.Hour)) || s.SpanCount != 3 || s.Status != "ERROR" || s.RootSpanID != "root" ||
		s.Duration != int64(3*time.Hour) {
		t.Errorf("straddling trace = %+v", s)
	}
	l := got["late"]
	if l.SpanCount != 2 || l.Status != "ERROR" || l.Duration != int64(2*time.Hour+time.Minute-50*time.Minute) {
		t.Errorf("trace continuing after the range = %+v", l)
	}

	// The list shows what the detail view shows
	for id, overview := range got {
		detail, err := store.TraceSpansPage(ctx, id, Page{Limit: 1})
		if err != nil {
			t.Fatal(err)
		}
		if *detail.Trace != overview {
			t.Errorf("%s: list %+v, detail %+v", id, overview, *detail.Trace)
		}
	}

	// Recent traces too
	recent, err := store.GetRecentTraces(ctx, 1, from, to)
	if err != nil {
		t.Fatal(err)
	}
	if len(recent.Traces) != 1 || recent.Traces[0].TraceID != "late" || recent.Traces[0].SpanCount != 2 {
		t.Errorf("recent = %+v", recent.Traces)
	}
}

// Pages follow the order of the first span in the range, also for traces
// whose true start is outside it
func TestQueryTracesPagesByFirstSpanInRange(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	ctx := context.Background()
	base := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	var spans []api.Span
	for i := range 7 {
		trace := fmt.Sprintf("t%d", i)
		// Odd traces began a day before the range
		if i%2 == 1 {
			spans = append(spans, api.Span{TraceID: trace, SpanID: trace + "-old", SpanName: "old", ServiceName: "claude-code",
				Timestamp: base.Add(-24 * time.Hour)})
		}
		spans = append(spans, api.Span{TraceID: trace, SpanID: trace + "-in", SpanName: "in", ServiceName: "claude-code",
			Timestamp: base.Add(time.Duration(i) * time.Minute)})
	}
	if err := store.InsertSpans(ctx, spans); err != nil {
		t.Fatal(err)
	}

	var seen []string
	page := Page{Limit: 3}
	for {
		resp, err := store.QueryTraces(ctx, TraceFilter{}, base, base.Add(time.Hour), page)
		if err != nil {
			t.Fatal(err)
		}
		if resp.Total != 7 {
			t.Fatalf("total = %d", resp.Total)
		}
		for _, tr := range resp.Traces {
			seen = append(seen, tr.TraceID)
		}
		if !resp.HasMore {
			break
		}
		page.Cursor = resp.NextCursor
	}
	want := []string{"t6", "t5", "t4", "t3", "t2", "t1", "t0"}
	if fmt.Sprint(seen) != fmt.Sprint(want) {
		t.Errorf("pages = %v, want %v", seen, want)
	}
}
