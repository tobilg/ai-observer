// Package otlpgrpc serves OTLP/gRPC: the collector's TraceService,
// MetricsService and LogsService, whose exports run through the same
// ingestion pipeline as OTLP/HTTP (handlers.Handlers.Ingest*).
//
// Contract, as the OTLP specification describes it
// (https://opentelemetry.io/docs/specs/otlp/#otlpgrpc):
//   - Requests may be gzip-compressed (grpc-encoding: gzip); other encodings
//     are answered Unimplemented by grpc-go.
//   - A message over MaxMessageBytes, also after decompression, is answered
//     ResourceExhausted without retry information, so exporters do not retry
//     it (the 413 of OTLP/HTTP).
//   - A request that does not decode is answered InvalidArgument (not
//     retried; the 400 of OTLP/HTTP).
//   - A failure to store is answered Unavailable, which exporters retry;
//     nothing of the request was stored (the 503 of OTLP/HTTP).
//   - Invalid records are reported in the response's partial_success.
//   - With a token, requests must carry "authorization: Bearer <token>"
//     metadata (Unauthenticated otherwise).
//
// With Options.TLS the server speaks gRPC over TLS (HTTP/2 negotiated by
// ALPN), otherwise in clear text.
//
// Browser pages cannot send exports: in clear text the server speaks only
// HTTP/2 with prior knowledge, which no browser sends. Over TLS a browser can
// connect, but a page's request is rejected before any handler runs: gRPC
// accepts only POST with content-type application/grpc, which is not a CORS
// "simple" request, so the browser sends an OPTIONS preflight first, which
// gRPC answers 405 without CORS headers (and a simple text/plain POST is
// answered 415). gRPC-Web, the browser variant, is not served. So the
// cross-site (CSRF) and DNS rebinding attacks that the OTLP/HTTP port's
// Origin check stops cannot send telemetry here.
package otlpgrpc

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"strings"

	"github.com/tobilg/ai-observer/internal/handlers"
	"github.com/tobilg/ai-observer/internal/logger"
	collogspb "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	colmetricspb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	coltracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	_ "google.golang.org/grpc/encoding/gzip" // accepts gzip-compressed requests
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// MaxMessageBytes bounds a request message, after decompression: the
// 10 MB limit of OTLP/HTTP (middleware.MaxPayloadBytes)
const MaxMessageBytes = 10 << 20

// Ingester runs the ingestion pipeline of each signal (handlers.Handlers).
// Errors are *handlers.StoreError.
type Ingester interface {
	IngestTraces(ctx context.Context, req *coltracepb.ExportTraceServiceRequest) (*coltracepb.ExportTraceServiceResponse, error)
	IngestMetrics(ctx context.Context, req *colmetricspb.ExportMetricsServiceRequest, key handlers.BatchKey) (*colmetricspb.ExportMetricsServiceResponse, error)
	IngestLogs(ctx context.Context, req *collogspb.ExportLogsServiceRequest, key handlers.BatchKey) (*collogspb.ExportLogsServiceResponse, error)
}

// Authorizer checks the bearer token a request presents ("" when it sent
// none) from the peer's host. The error it returns, a gRPC status, is the
// request's answer.
type Authorizer func(peerHost, token string) error

// Options configure the server
type Options struct {
	// Authorize, if set, is asked for every request
	Authorize Authorizer
	// TLS, if set, makes the server speak gRPC over TLS with this
	// configuration (its certificates); the server adds h2 to its ALPN
	// protocols
	TLS *tls.Config
}

// NewServer returns a gRPC server with the OTLP collector services
func NewServer(ing Ingester, opts Options) *grpc.Server {
	serverOpts := []grpc.ServerOption{
		grpc.MaxRecvMsgSize(MaxMessageBytes),
		// Stop and GracefulStop return only when every export has returned,
		// so storage is not closed while one still writes
		grpc.WaitForHandlers(true),
		grpc.ChainUnaryInterceptor(recoverPanics, authorize(opts.Authorize)),
		grpc.StatsHandler(requestMetrics{}),
	}
	if opts.TLS != nil {
		serverOpts = append(serverOpts, grpc.Creds(credentials.NewTLS(opts.TLS)))
	}
	srv := grpc.NewServer(serverOpts...)
	register(srv, ing)
	return srv
}

// register adds the three services. Their descriptions are those of the
// generated code with handlers of our own: the generated ones return a
// request that does not decode as Internal, which OTLP calls a server error;
// it is the client's (InvalidArgument).
func register(srv *grpc.Server, ing Ingester) {
	traceDesc := coltracepb.TraceService_ServiceDesc
	traceDesc.HandlerType = (*any)(nil)
	traceDesc.Methods = []grpc.MethodDesc{{MethodName: "Export", Handler: unary(
		func() *coltracepb.ExportTraceServiceRequest { return new(coltracepb.ExportTraceServiceRequest) },
		func(ctx context.Context, req *coltracepb.ExportTraceServiceRequest) (proto.Message, error) {
			return ing.IngestTraces(ctx, req)
		})}}
	srv.RegisterService(&traceDesc, ing)

	metricsDesc := colmetricspb.MetricsService_ServiceDesc
	metricsDesc.HandlerType = (*any)(nil)
	metricsDesc.Methods = []grpc.MethodDesc{{MethodName: "Export", Handler: unary(
		func() *colmetricspb.ExportMetricsServiceRequest { return new(colmetricspb.ExportMetricsServiceRequest) },
		func(ctx context.Context, req *colmetricspb.ExportMetricsServiceRequest) (proto.Message, error) {
			key, err := handlers.MessageBatchKey("metrics", req)
			if err != nil {
				return nil, status.Error(codes.InvalidArgument, err.Error())
			}
			return ing.IngestMetrics(ctx, req, key)
		})}}
	srv.RegisterService(&metricsDesc, ing)

	logsDesc := collogspb.LogsService_ServiceDesc
	logsDesc.HandlerType = (*any)(nil)
	logsDesc.Methods = []grpc.MethodDesc{{MethodName: "Export", Handler: unary(
		func() *collogspb.ExportLogsServiceRequest { return new(collogspb.ExportLogsServiceRequest) },
		func(ctx context.Context, req *collogspb.ExportLogsServiceRequest) (proto.Message, error) {
			key, err := handlers.MessageBatchKey("logs", req)
			if err != nil {
				return nil, status.Error(codes.InvalidArgument, err.Error())
			}
			return ing.IngestLogs(ctx, req, key)
		})}}
	srv.RegisterService(&logsDesc, ing)
}

// unary returns the handler of an Export method: it decodes the request
// (InvalidArgument when that fails), runs the interceptors and export, and
// maps a storage failure to Unavailable
func unary[Req proto.Message](newReq func() Req, export func(context.Context, Req) (proto.Message, error)) func(any, context.Context, func(any) error, grpc.UnaryServerInterceptor) (any, error) {
	return func(_ any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
		req := newReq()
		if err := dec(req); err != nil {
			logger.Warn("Failed to decode OTLP/gRPC request", "error", err)
			return nil, status.Error(codes.InvalidArgument, "failed to decode request: "+status.Convert(err).Message())
		}
		handle := func(ctx context.Context, r any) (any, error) {
			resp, err := export(ctx, r.(Req))
			if err != nil {
				return nil, statusOf(err)
			}
			return resp, nil
		}
		if interceptor == nil {
			return handle(ctx, req)
		}
		method, _ := grpc.Method(ctx)
		return interceptor(ctx, req, &grpc.UnaryServerInfo{FullMethod: method}, handle)
	}
}

// statusOf maps an ingestion error to its gRPC status
func statusOf(err error) error {
	if _, ok := status.FromError(err); ok {
		return err
	}
	var storeErr *handlers.StoreError
	if errors.As(err, &storeErr) {
		// Exporters retry Unavailable; nothing of the request was stored
		return status.Error(codes.Unavailable, "failed to store "+storeErr.Signal)
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return status.FromContextError(err).Err()
	}
	return status.Error(codes.Internal, err.Error())
}

// authorize returns an interceptor that asks auth about every request
func authorize(auth Authorizer) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if auth != nil {
			if err := auth(peerHost(ctx), bearerToken(ctx)); err != nil {
				markAuthFailed(ctx)
				logger.Warn("Rejected OTLP/gRPC request", "method", info.FullMethod, "error", status.Convert(err).Message())
				return nil, err
			}
		}
		return handler(ctx, req)
	}
}

// recoverPanics answers a request whose handling panicked with Internal
// instead of ending the process (what middleware.Recoverer does for HTTP)
func recoverPanics(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp any, err error) {
	defer func() {
		if p := recover(); p != nil {
			logger.Error("Panic in OTLP/gRPC handler", "method", info.FullMethod, "panic", p)
			resp, err = nil, status.Error(codes.Internal, "internal error")
		}
	}()
	return handler(ctx, req)
}

// bearerToken returns the token of the request's "authorization: Bearer"
// metadata, "" when there is none
func bearerToken(ctx context.Context) string {
	md, _ := metadata.FromIncomingContext(ctx)
	for _, value := range md.Get("authorization") {
		scheme, token, ok := strings.Cut(strings.TrimSpace(value), " ")
		if ok && strings.EqualFold(scheme, "Bearer") {
			return strings.TrimSpace(token)
		}
	}
	return ""
}

// peerHost returns the host of the request's remote address
func peerHost(ctx context.Context) string {
	p, ok := peer.FromContext(ctx)
	if !ok || p.Addr == nil {
		return ""
	}
	host, _, err := net.SplitHostPort(p.Addr.String())
	if err != nil {
		return p.Addr.String()
	}
	return host
}
