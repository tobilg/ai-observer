package server

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	appMiddleware "github.com/tobilg/ai-observer/internal/middleware"
)

func newMCPServer(t *testing.T, token string) *Server {
	t.Helper()
	cfg := getTestConfig(t)
	cfg.MCPEnabled = true
	cfg.AuthToken = token
	server, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	t.Cleanup(func() { server.storage.Close() })
	return server
}

const mcpInitialize = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}`

func mcpRequest(token, host string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(mcpInitialize))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if host != "" {
		req.Host = host
	}
	return req
}

// An MCP client lists and calls the tools over HTTP
func TestMCPEndpointOverHTTP(t *testing.T) {
	server := newMCPServer(t, "")
	ts := httptest.NewServer(server.apiRouter)
	defer ts.Close()

	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	cs, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: ts.URL + "/mcp"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	tools, err := cs.ListTools(context.Background(), nil)
	if err != nil || len(tools.Tools) < 9 {
		t.Fatalf("tools = %v, %v", tools, err)
	}
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "list_sessions", Arguments: map[string]any{}})
	if err != nil || res.IsError {
		t.Fatalf("list_sessions = %+v, %v", res, err)
	}
}

func TestMCPRequiresTokenAndKnownHost(t *testing.T) {
	server := newMCPServer(t, "s3cret")
	if rec := serve(server.apiRouter, mcpRequest("", "")); rec.Code != http.StatusUnauthorized {
		t.Errorf("without token: %d, want 401", rec.Code)
	}
	if rec := serve(server.apiRouter, mcpRequest("wrong", "")); rec.Code != http.StatusUnauthorized {
		t.Errorf("wrong token: %d, want 401", rec.Code)
	}
	if rec := serve(server.apiRouter, mcpRequest("s3cret", "")); rec.Code != http.StatusOK {
		t.Errorf("with token: %d (%s), want 200", rec.Code, rec.Body)
	}

	// DNS rebinding: a foreign host name is rejected before MCP
	open := newMCPServer(t, "")
	req := mcpRequest("", "")
	rec := httptest.NewRecorder()
	req.Host = "evil.example.com"
	open.apiRouter.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Errorf("foreign host: %d, want rejected", rec.Code)
	}
}

func TestMCPDisabledByDefault(t *testing.T) {
	server := newAuthServer(t)
	rec := serve(server.apiRouter, mcpRequest("s3cret", ""))
	if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "AI_OBSERVER_MCP") {
		t.Errorf("disabled MCP: %d %s, want 404 with a hint", rec.Code, rec.Body)
	}
}

// MCP requests run without the API's request timeout
func TestMCPHasNoRequestTimeout(t *testing.T) {
	deadlines := map[string]bool{}
	handler := exceptPath("/mcp", appMiddleware.DefaultContextTimeoutMiddleware)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, ok := r.Context().Deadline()
		deadlines[r.URL.Path] = ok
	}))
	for _, path := range []string{"/mcp", "/api/logs"} {
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, path, nil))
	}
	if deadlines["/mcp"] || !deadlines["/api/logs"] {
		t.Errorf("deadlines = %v, want none for /mcp only", deadlines)
	}
}

// MCP requests have no timeout, so shutdown cancels them: otherwise a long
// search would hold up closing the database
func TestMCPRequestsEndOnStop(t *testing.T) {
	stopping, stop := context.WithCancel(context.Background())
	started, ended := make(chan struct{}), make(chan error, 1)
	handler := endOnStop(stopping, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
		ended <- r.Context().Err()
	}))
	go handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/mcp", nil))
	<-started
	stop()
	select {
	case err := <-ended:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("request context ended with %v, want cancelled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the request was not cancelled")
	}
}

func TestLocalURL(t *testing.T) {
	cases := map[string]string{
		"0.0.0.0:8080":   "http://localhost:8080/mcp",
		"[::]:8080":      "http://localhost:8080/mcp",
		"127.0.0.1:8080": "http://127.0.0.1:8080/mcp",
		"[::1]:9000":     "http://[::1]:9000/mcp",
	}
	for addr, want := range cases {
		tcp, err := net.ResolveTCPAddr("tcp", addr)
		if err != nil {
			t.Fatal(err)
		}
		if got := localURL(tcp, "/mcp"); got != want {
			t.Errorf("localURL(%s) = %s, want %s", addr, got, want)
		}
	}
}
