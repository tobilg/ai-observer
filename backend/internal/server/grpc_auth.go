package server

import (
	"fmt"
	"math"

	"github.com/tobilg/ai-observer/internal/otlpgrpc"
	"github.com/tobilg/ai-observer/internal/selfmetrics"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// GRPCAuthorizer returns the token check of OTLP/gRPC requests, nil when
// authentication is off. Requests must carry "authorization: Bearer <token>"
// metadata (what OTEL_EXPORTER_OTLP_HEADERS=authorization=Bearer%20<token>
// sends). As on OTLP/HTTP, wrong tokens count as failed attempts of the
// peer's address, which is blocked after too many of them.
func (a *TokenAuth) GRPCAuthorizer() otlpgrpc.Authorizer {
	if a == nil {
		return nil
	}
	return func(peerHost, token string) error {
		if wait := a.limiter.blocked(peerHost); wait > 0 {
			selfmetrics.AuthFailures.With(selfmetrics.TransportGRPC, "blocked").Inc()
			return status.Error(codes.ResourceExhausted, fmt.Sprintf(
				"too many failed authentication attempts from this address; retry in %d seconds", int(math.Ceil(wait.Seconds()))))
		}
		if a.matches(token) {
			return nil
		}
		if token != "" {
			a.limiter.fail(peerHost, token)
		}
		selfmetrics.AuthFailures.With(selfmetrics.TransportGRPC, "unauthorized").Inc()
		return status.Error(codes.Unauthenticated, "authentication required; see AI_OBSERVER_AUTH_TOKEN")
	}
}
