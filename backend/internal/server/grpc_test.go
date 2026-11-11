package server

import (
	"bytes"
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/tobilg/ai-observer/internal/selfmetrics"
	"github.com/tobilg/ai-observer/internal/watcher"
	coltracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func freeTCPPort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// startServer runs srv's ListenAndServe until the test ends and waits until
// the API answers
func startServer(t *testing.T, srv *Server) {
	t.Helper()
	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe() }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(ctx)
	})
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case err := <-errCh:
			t.Fatalf("server stopped: %v", err)
		default:
		}
		if resp, err := http.Get("http://127.0.0.1:18080/health"); err == nil {
			resp.Body.Close()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("server did not start")
}

func traceClient(t *testing.T, port int) coltracepb.TraceServiceClient {
	t.Helper()
	conn, err := grpc.NewClient(net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return coltracepb.NewTraceServiceClient(conn)
}

func oneSpan() *coltracepb.ExportTraceServiceRequest {
	return &coltracepb.ExportTraceServiceRequest{ResourceSpans: []*tracepb.ResourceSpans{{
		ScopeSpans: []*tracepb.ScopeSpans{{Spans: []*tracepb.Span{{
			Name: "span", TraceId: bytes.Repeat([]byte{1}, 16), SpanId: bytes.Repeat([]byte{2}, 8),
			StartTimeUnixNano: 1_700_000_000_000_000_000, EndTimeUnixNano: 1_700_000_001_000_000_000,
		}}}},
	}}}
}

// serve listens for OTLP/gRPC beside OTLP/HTTP and reports it in its status
func TestServerServesOTLPGRPC(t *testing.T) {
	cfg := getTestConfig(t)
	cfg.OTLPGRPCPort = freeTCPPort(t)
	srv, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	startServer(t, srv)

	if _, err := traceClient(t, cfg.OTLPGRPCPort).Export(context.Background(), oneSpan()); err != nil {
		t.Fatalf("export over gRPC: %v", err)
	}
	stats, err := srv.storage.GetStats(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if stats.SpanCount != 1 {
		t.Errorf("stored spans = %d, want 1", stats.SpanCount)
	}
	info := srv.serverInfo()
	if info.Mode != ModeServe || !info.OTLPEnabled || !info.OTLPGRPCEnabled || info.OTLPGRPCPort != cfg.OTLPGRPCPort {
		t.Errorf("status = %+v", info)
	}
}

// Port 0 turns OTLP/gRPC off
func TestServerWithoutOTLPGRPC(t *testing.T) {
	cfg := getTestConfig(t)
	cfg.OTLPGRPCPort = 0
	srv, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	startServer(t, srv)
	if info := srv.serverInfo(); info.OTLPGRPCEnabled || info.OTLPGRPCPort != 0 {
		t.Errorf("status = %+v, want gRPC off", info)
	}
}

// A busy gRPC port fails startup like the other ports do
func TestServerFailsWhenOTLPGRPCPortBusy(t *testing.T) {
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()
	cfg := getTestConfig(t)
	cfg.OTLPGRPCPort = busy.Addr().(*net.TCPAddr).Port
	srv, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Shutdown(context.Background())

	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe() }()
	select {
	case err := <-errCh:
		if err == nil || err == http.ErrServerClosed {
			t.Fatalf("expected a startup error, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ListenAndServe kept running although the gRPC port was in use")
	}
	// The OTLP/HTTP port opened before was released
	if l, err := net.Listen("tcp", "127.0.0.1:14318"); err != nil {
		t.Errorf("OTLP/HTTP port still in use: %v", err)
	} else {
		l.Close()
	}
}

// With AI_OBSERVER_AUTH_TOKEN, gRPC exports need "authorization: Bearer"
func TestServerOTLPGRPCRequiresToken(t *testing.T) {
	cfg := getTestConfig(t)
	cfg.OTLPGRPCPort = freeTCPPort(t)
	cfg.AuthToken = "grpc-test-token"
	srv, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe() }()
	defer srv.Shutdown(context.Background())
	client := traceClient(t, cfg.OTLPGRPCPort)
	unauthorized := selfmetrics.AuthFailures.With(selfmetrics.TransportGRPC, "unauthorized").Value()

	var lastErr error
	for range 100 { // until the server listens
		_, lastErr = client.Export(context.Background(), oneSpan())
		if status.Code(lastErr) != codes.Unavailable {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if status.Code(lastErr) != codes.Unauthenticated {
		t.Errorf("without token: %v, want Unauthenticated", lastErr)
	}
	if got := selfmetrics.AuthFailures.With(selfmetrics.TransportGRPC, "unauthorized").Value(); got != unauthorized+1 {
		t.Errorf("gRPC authentication failures = %d, want %d", got, unauthorized+1)
	}
	ctx := metadata.AppendToOutgoingContext(context.Background(), "authorization", "Bearer grpc-test-token")
	if _, err := client.Export(ctx, oneSpan()); err != nil {
		t.Errorf("with token: %v", err)
	}
}

// serve --watch runs the OTLP servers and the file watcher: mode serve+watch
func TestServerModeServeWatch(t *testing.T) {
	for _, env := range []string{"AI_OBSERVER_CLAUDE_PATH", "AI_OBSERVER_CODEX_PATH", "AI_OBSERVER_GEMINI_PATH"} {
		t.Setenv(env, t.TempDir())
	}
	cfg := getTestConfig(t)
	srv, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.StartWatcher(watcher.Config{}); err != nil {
		t.Fatal(err)
	}
	if info := srv.serverInfo(); info.Mode != ModeWatch {
		t.Errorf("mode before the OTLP server started = %q, want watch", info.Mode)
	}
	startServer(t, srv)
	info := srv.serverInfo()
	if info.Mode != ModeServeWatch || !info.OTLPEnabled {
		t.Errorf("mode = %q, OTLP %v; want serve+watch with OTLP", info.Mode, info.OTLPEnabled)
	}
	if !slices.Equal(info.WatchedTools, []string{"claude-code", "codex", "gemini"}) {
		t.Errorf("watched tools = %v", info.WatchedTools)
	}
}

// With AI_OBSERVER_TLS_CERT/KEY, OTLP/gRPC speaks TLS too, and clear-text
// clients are refused
func TestServerOTLPGRPCOverTLS(t *testing.T) {
	certFile, keyFile, pool := selfSignedCert(t, t.TempDir())
	cfg := getTestConfig(t)
	cfg.TLSCert, cfg.TLSKey = certFile, keyFile
	cfg.OTLPGRPCPort = freeTCPPort(t)
	srv, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if got := srv.grpcProtocol(); got != "gRPC (TLS)" {
		t.Errorf("protocol = %q", got)
	}
	go func() { _ = srv.ListenAndServe() }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(ctx)
	})

	addr := net.JoinHostPort("localhost", strconv.Itoa(cfg.OTLPGRPCPort))
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{RootCAs: pool})))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	client := coltracepb.NewTraceServiceClient(conn)
	for range 100 { // until the server listens
		if _, err = client.Export(context.Background(), oneSpan()); status.Code(err) != codes.Unavailable {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("export over TLS: %v", err)
	}

	plain := traceClient(t, cfg.OTLPGRPCPort)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := plain.Export(ctx, oneSpan()); err == nil {
		t.Error("a clear-text export was accepted on the TLS port")
	}
}
