package otlpgrpc

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/tobilg/ai-observer/internal/selfmetrics"
	collogspb "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	coltracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// waitCount waits until the counter of labels has grown by want: the server
// records a request when it has answered it, possibly after the client
// returned
func waitCount(t *testing.T, labels []string, before, want int64) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		got := selfmetrics.OTLPRequests.With(labels...).Value() - before
		if got == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("requests %v counted %d, want %d", labels, got, want)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// Export requests are counted by signal and result like OTLP/HTTP's, also
// those grpc-go answers itself (too large)
func TestRequestsAreCounted(t *testing.T) {
	ing, _ := storeIngester(t)
	conn := serve(t, ing, Options{Authorize: func(peerHost, token string) error {
		switch token {
		case "secret":
			return nil
		case "blocked":
			return status.Error(codes.ResourceExhausted, "too many failed authentication attempts")
		}
		return status.Error(codes.Unauthenticated, "authentication required")
	}})
	authed := func(token string) context.Context {
		return metadata.AppendToOutgoingContext(context.Background(), "authorization", "Bearer "+token)
	}
	labels := func(signal, result string) []string { return []string{selfmetrics.TransportGRPC, signal, result} }
	count := func(signal, result string) int64 {
		return selfmetrics.OTLPRequests.With(labels(signal, result)...).Value()
	}

	traces := coltracepb.NewTraceServiceClient(conn)
	logsClient := collogspb.NewLogsServiceClient(conn)
	okBefore := count("traces", "ok")
	unauthBefore, largeBefore := count("logs", "unauthorized"), count("logs", "too_large")

	if _, err := traces.Export(authed("secret"), spans(bytes.Repeat([]byte{1}, 16))); err != nil {
		t.Fatal(err)
	}
	waitCount(t, labels("traces", "ok"), okBefore, 1)

	for _, token := range []string{"wrong", "blocked"} {
		if _, err := logsClient.Export(authed(token), logs("x")); err == nil {
			t.Fatalf("token %q accepted", token)
		}
	}
	waitCount(t, labels("logs", "unauthorized"), unauthBefore, 2)

	big := logs(strings.Repeat("x", MaxMessageBytes+1))
	if _, err := logsClient.Export(authed("secret"), big, grpc.MaxCallSendMsgSize(2*MaxMessageBytes)); status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("oversized request: %v", err)
	}
	waitCount(t, labels("logs", "too_large"), largeBefore, 1)
}

func TestGRPCResult(t *testing.T) {
	for _, tt := range []struct {
		code       codes.Code
		authFailed bool
		want       string
	}{
		{codes.OK, false, "ok"},
		{codes.Unauthenticated, true, "unauthorized"},
		{codes.ResourceExhausted, true, "unauthorized"},
		{codes.ResourceExhausted, false, "too_large"},
		{codes.InvalidArgument, false, "invalid"},
		{codes.Unimplemented, false, "invalid"},
		{codes.Unavailable, false, "unavailable"},
		{codes.Internal, false, "error"},
		{codes.Canceled, false, "error"},
	} {
		if got := grpcResult(tt.code, tt.authFailed); got != tt.want {
			t.Errorf("grpcResult(%v, %v) = %q, want %q", tt.code, tt.authFailed, got, tt.want)
		}
	}
}
