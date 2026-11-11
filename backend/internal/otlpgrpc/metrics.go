package otlpgrpc

import (
	"context"
	"sync/atomic"

	"github.com/tobilg/ai-observer/internal/selfmetrics"
	collogspb "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	colmetricspb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	coltracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/stats"
	"google.golang.org/grpc/status"
)

// requestMetrics counts the server's export requests in the self-metrics
// (selfmetrics.OTLPRequests, transport grpc), as server.OTLPMetrics counts
// those of OTLP/HTTP. It is a stats handler rather than an interceptor so
// that it also counts what grpc-go answers itself without running a
// handler: messages over MaxMessageBytes and unsupported compression.
type requestMetrics struct{}

// rpcKey is the context key of a request's *rpcInfo
type rpcKey struct{}

// rpcInfo is what requestMetrics knows about a request
type rpcInfo struct {
	signal string
	// authFailed is set by the authorize interceptor when it refused the
	// request: its ResourceExhausted then means a blocked address, not a
	// message too large
	authFailed atomic.Bool
}

// signals are the signals of the services' Export methods
var signals = map[string]string{
	"/" + coltracepb.TraceService_ServiceDesc.ServiceName + "/Export":     "traces",
	"/" + colmetricspb.MetricsService_ServiceDesc.ServiceName + "/Export": "metrics",
	"/" + collogspb.LogsService_ServiceDesc.ServiceName + "/Export":       "logs",
}

func (requestMetrics) TagRPC(ctx context.Context, info *stats.RPCTagInfo) context.Context {
	signal, ok := signals[info.FullMethodName]
	if !ok {
		return ctx
	}
	return context.WithValue(ctx, rpcKey{}, &rpcInfo{signal: signal})
}

func (requestMetrics) HandleRPC(ctx context.Context, s stats.RPCStats) {
	end, ok := s.(*stats.End)
	if !ok {
		return
	}
	if info, ok := ctx.Value(rpcKey{}).(*rpcInfo); ok {
		selfmetrics.OTLPRequests.With(selfmetrics.TransportGRPC, info.signal, grpcResult(status.Code(end.Error), info.authFailed.Load())).Inc()
	}
}

func (requestMetrics) TagConn(ctx context.Context, _ *stats.ConnTagInfo) context.Context { return ctx }

func (requestMetrics) HandleConn(context.Context, stats.ConnStats) {}

// markAuthFailed records that the authorize interceptor refused the request
// of ctx
func markAuthFailed(ctx context.Context) {
	if info, ok := ctx.Value(rpcKey{}).(*rpcInfo); ok {
		info.authFailed.Store(true)
	}
}

// grpcResult is the result label of a request answered with code, as
// selfmetrics.OTLPResult labels HTTP statuses
func grpcResult(code codes.Code, authFailed bool) string {
	switch code {
	case codes.OK:
		return "ok"
	case codes.Unauthenticated, codes.PermissionDenied:
		return "unauthorized"
	case codes.ResourceExhausted:
		if authFailed {
			return "unauthorized"
		}
		return "too_large"
	case codes.InvalidArgument, codes.Unimplemented:
		return "invalid"
	case codes.Unavailable:
		return "unavailable"
	default:
		return "error"
	}
}
