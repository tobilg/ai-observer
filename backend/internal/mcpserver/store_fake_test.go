package mcpserver

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/tobilg/ai-observer/internal/api"
	"github.com/tobilg/ai-observer/internal/redact"
	"github.com/tobilg/ai-observer/internal/storage"
)

// failingStore is a Store whose queries fail with err. Calls it does not
// override panic (the embedded nil Store).
type failingStore struct {
	Store
	err error
}

func (f failingStore) Redactor() *redact.Redactor { return redact.New(redact.Secrets) }
func (f failingStore) LocalTimeZone() string      { return "UTC" }
func (f failingStore) ValidateTimeZone(string) error {
	return nil
}

func (f failingStore) QuerySessions(context.Context, string, time.Time, time.Time, storage.Page) (*api.SessionsResponse, error) {
	return nil, f.err
}

func (f failingStore) QueryLogs(context.Context, string, string, string, string, time.Time, time.Time, storage.Page) (*api.LogsResponse, error) {
	return nil, f.err
}

func (f failingStore) QuerySeries(context.Context, api.MetricQuery, storage.SeriesRange) (*api.TimeSeriesResponse, error) {
	return nil, f.err
}

// fakeSession connects a client to New over store
func fakeSession(t *testing.T, store Store) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	if _, err := New(store, "test").Connect(ctx, serverTransport, nil); err != nil {
		t.Fatal(err)
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs
}

// A failing query is a tool error (isError) the agent can read, not a
// protocol error; invalid cursors and metric arguments are explained in the
// tools' words
func TestToolErrorsWithFailingStore(t *testing.T) {
	tests := []struct {
		name string
		err  error
		tool string
		args map[string]any
		want string
	}{
		{"store failure", errors.New("IO Error: disk full"), "list_sessions", nil, "disk full"},
		{"invalid cursor", storage.ErrInvalidCursor, "list_sessions", map[string]any{"cursor": "x"}, "pass the next_cursor"},
		{"timeout", context.DeadlineExceeded, "search_logs", map[string]any{"query": "x"}, "deadline exceeded"},
		{"invalid group_by", &storage.MetricQueryError{Code: api.MetricErrorInvalidGroupBy, Message: `invalid groupBy "service"`},
			"query_metrics", map[string]any{"name": "m", "group_by": []string{"service"}}, `invalid group_by "service"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cs := fakeSession(t, failingStore{err: tt.err})
			var out map[string]any
			msg := call(t, cs, tt.tool, tt.args, &out)
			if msg == "" {
				t.Fatalf("%s succeeded with a failing store: %v", tt.tool, out)
			}
			if !strings.Contains(msg, tt.want) {
				t.Errorf("error %q, want it to contain %q", msg, tt.want)
			}
		})
	}
}
