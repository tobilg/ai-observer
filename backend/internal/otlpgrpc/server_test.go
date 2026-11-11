package otlpgrpc

import (
	"bytes"
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/tobilg/ai-observer/internal/handlers"
	"github.com/tobilg/ai-observer/internal/storage"
	collogspb "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	colmetricspb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	coltracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
	metricspb "go.opentelemetry.io/proto/otlp/metrics/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/encoding/gzip"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// serve starts a server for ing on a free local port and returns a client
// connection to it
func serve(t *testing.T, ing Ingester, opts Options) *grpc.ClientConn {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := NewServer(ing, opts)
	go srv.Serve(lis)
	t.Cleanup(srv.Stop)
	conn, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

// storeIngester returns the real ingestion pipeline over an in-memory store
func storeIngester(t *testing.T) (*handlers.Handlers, *storage.DuckDBStore) {
	t.Helper()
	store, err := storage.NewDuckDBStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	return handlers.New(store, nil), store
}

func resource() *resourcepb.Resource {
	return &resourcepb.Resource{Attributes: []*commonpb.KeyValue{{
		Key: "service.name", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "grpc-test"}},
	}}}
}

func spans(traceIDs ...[]byte) *coltracepb.ExportTraceServiceRequest {
	var list []*tracepb.Span
	for i, id := range traceIDs {
		list = append(list, &tracepb.Span{Name: "span", TraceId: id, SpanId: []byte{1, 2, 3, 4, 5, 6, 7, byte(i + 1)},
			StartTimeUnixNano: 1_700_000_000_000_000_000, EndTimeUnixNano: 1_700_000_001_000_000_000})
	}
	return &coltracepb.ExportTraceServiceRequest{ResourceSpans: []*tracepb.ResourceSpans{{
		Resource: resource(), ScopeSpans: []*tracepb.ScopeSpans{{Spans: list}},
	}}}
}

func logs(body string) *collogspb.ExportLogsServiceRequest {
	return &collogspb.ExportLogsServiceRequest{ResourceLogs: []*logspb.ResourceLogs{{
		Resource: resource(),
		ScopeLogs: []*logspb.ScopeLogs{{LogRecords: []*logspb.LogRecord{{
			TimeUnixNano: 1_700_000_000_000_000_000,
			Body:         &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: body}},
		}}}},
	}}}
}

func gauge(values ...float64) *colmetricspb.ExportMetricsServiceRequest {
	var points []*metricspb.NumberDataPoint
	for _, v := range values {
		points = append(points, &metricspb.NumberDataPoint{TimeUnixNano: 1_700_000_000_000_000_000, Value: &metricspb.NumberDataPoint_AsDouble{AsDouble: v}})
	}
	points = append(points, &metricspb.NumberDataPoint{TimeUnixNano: 1_700_000_000_000_000_000}) // no value: rejected
	return &colmetricspb.ExportMetricsServiceRequest{ResourceMetrics: []*metricspb.ResourceMetrics{{
		Resource: resource(),
		ScopeMetrics: []*metricspb.ScopeMetrics{{Metrics: []*metricspb.Metric{{
			Name: "test.gauge", Data: &metricspb.Metric_Gauge{Gauge: &metricspb.Gauge{DataPoints: points}},
		}}}},
	}}}
}

func counts(t *testing.T, store *storage.DuckDBStore) (spans, logs, metrics int64) {
	t.Helper()
	stats, err := store.GetStats(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return stats.SpanCount, stats.LogCount, stats.MetricCount
}

// The three services run the ingestion pipeline: records are stored,
// invalid ones reported as partial success, retried batches stored once
func TestExportStoresTelemetry(t *testing.T) {
	ing, store := storeIngester(t)
	conn := serve(t, ing, Options{})
	ctx := context.Background()

	traceResp, err := coltracepb.NewTraceServiceClient(conn).Export(ctx, spans(bytes.Repeat([]byte{1}, 16), []byte{0}))
	if err != nil {
		t.Fatal(err)
	}
	if ps := traceResp.GetPartialSuccess(); ps.GetRejectedSpans() != 1 || !strings.Contains(ps.GetErrorMessage(), "trace_id") {
		t.Errorf("traces partial success = %v", ps)
	}

	metricsClient := colmetricspb.NewMetricsServiceClient(conn)
	metricsResp, err := metricsClient.Export(ctx, gauge(1, 2))
	if err != nil {
		t.Fatal(err)
	}
	if ps := metricsResp.GetPartialSuccess(); ps.GetRejectedDataPoints() != 1 {
		t.Errorf("metrics partial success = %v", ps)
	}
	// An exporter's retry of the same batch is acknowledged, not stored again
	if _, err := metricsClient.Export(ctx, gauge(1, 2)); err != nil {
		t.Fatal(err)
	}

	logsClient := collogspb.NewLogsServiceClient(conn)
	logsResp, err := logsClient.Export(ctx, logs("hello"), grpc.UseCompressor(gzip.Name))
	if err != nil {
		t.Fatalf("gzip-compressed export: %v", err)
	}
	if logsResp.GetPartialSuccess() != nil {
		t.Errorf("logs partial success = %v, want none", logsResp.GetPartialSuccess())
	}
	if _, err := logsClient.Export(ctx, logs("hello")); err != nil {
		t.Fatal(err)
	}

	gotSpans, gotLogs, gotMetrics := counts(t, store)
	if gotSpans != 1 || gotLogs != 1 || gotMetrics != 2 {
		t.Errorf("stored %d spans, %d logs, %d metric points; want 1, 1, 2", gotSpans, gotLogs, gotMetrics)
	}
}

// rawCodec sends the bytes it is given as the request, to send messages
// that do not decode
type rawCodec struct{}

func (rawCodec) Marshal(v any) ([]byte, error) { return v.([]byte), nil }
func (rawCodec) Unmarshal(data []byte, v any) error {
	return proto.Unmarshal(data, v.(proto.Message))
}
func (rawCodec) Name() string { return "proto" }

func TestErrorCodes(t *testing.T) {
	ing, _ := storeIngester(t)
	conn := serve(t, ing, Options{})
	ctx := context.Background()

	t.Run("undecodable request is InvalidArgument", func(t *testing.T) {
		var resp collogspb.ExportLogsServiceResponse
		err := conn.Invoke(ctx, "/opentelemetry.proto.collector.logs.v1.LogsService/Export", []byte{0xff, 0xff, 0xff}, &resp,
			grpc.ForceCodec(rawCodec{}))
		if status.Code(err) != codes.InvalidArgument {
			t.Errorf("error = %v, want InvalidArgument", err)
		}
	})

	t.Run("oversized request is ResourceExhausted", func(t *testing.T) {
		big := logs(strings.Repeat("x", MaxMessageBytes+1))
		_, err := collogspb.NewLogsServiceClient(conn).Export(ctx, big, grpc.MaxCallSendMsgSize(2*MaxMessageBytes))
		if status.Code(err) != codes.ResourceExhausted {
			t.Errorf("error = %v, want ResourceExhausted", err)
		}
	})

	t.Run("oversized after decompression is ResourceExhausted", func(t *testing.T) {
		big := logs(strings.Repeat("x", MaxMessageBytes+1))
		_, err := collogspb.NewLogsServiceClient(conn).Export(ctx, big, grpc.MaxCallSendMsgSize(2*MaxMessageBytes), grpc.UseCompressor(gzip.Name))
		if status.Code(err) != codes.ResourceExhausted {
			t.Errorf("error = %v, want ResourceExhausted", err)
		}
	})
}

// failingIngester fails every export as storage does when a write fails
type failingIngester struct{}

func (failingIngester) IngestTraces(context.Context, *coltracepb.ExportTraceServiceRequest) (*coltracepb.ExportTraceServiceResponse, error) {
	return nil, &handlers.StoreError{Signal: "traces", Err: errors.New("disk full")}
}
func (failingIngester) IngestMetrics(context.Context, *colmetricspb.ExportMetricsServiceRequest, handlers.BatchKey) (*colmetricspb.ExportMetricsServiceResponse, error) {
	return nil, &handlers.StoreError{Signal: "metrics", Err: errors.New("disk full")}
}
func (failingIngester) IngestLogs(context.Context, *collogspb.ExportLogsServiceRequest, handlers.BatchKey) (*collogspb.ExportLogsServiceResponse, error) {
	panic("boom")
}

// A storage failure is Unavailable, which exporters retry; a panic is
// Internal and does not end the server
func TestStoreFailureIsUnavailable(t *testing.T) {
	conn := serve(t, failingIngester{}, Options{})
	ctx := context.Background()
	_, err := coltracepb.NewTraceServiceClient(conn).Export(ctx, spans(bytes.Repeat([]byte{1}, 16)))
	if status.Code(err) != codes.Unavailable {
		t.Errorf("traces: error = %v, want Unavailable", err)
	}
	_, err = colmetricspb.NewMetricsServiceClient(conn).Export(ctx, gauge(1))
	if status.Code(err) != codes.Unavailable {
		t.Errorf("metrics: error = %v, want Unavailable", err)
	}
	_, err = collogspb.NewLogsServiceClient(conn).Export(ctx, logs("x"))
	if status.Code(err) != codes.Internal {
		t.Errorf("logs: error = %v, want Internal after a panic", err)
	}
	if _, err := coltracepb.NewTraceServiceClient(conn).Export(ctx, spans(bytes.Repeat([]byte{1}, 16))); status.Code(err) != codes.Unavailable {
		t.Errorf("the server stopped serving after a panic: %v", err)
	}
}

// With an Authorizer, requests need the bearer token in their metadata
func TestAuthorization(t *testing.T) {
	ing, _ := storeIngester(t)
	var seenPeer string
	conn := serve(t, ing, Options{Authorize: func(peerHost, token string) error {
		seenPeer = peerHost
		if token != "secret" {
			return status.Error(codes.Unauthenticated, "authentication required")
		}
		return nil
	}})
	client := coltracepb.NewTraceServiceClient(conn)
	req := spans(bytes.Repeat([]byte{1}, 16))

	if _, err := client.Export(context.Background(), req); status.Code(err) != codes.Unauthenticated {
		t.Errorf("without token: error = %v, want Unauthenticated", err)
	}
	ctx := metadata.AppendToOutgoingContext(context.Background(), "authorization", "Bearer wrong")
	if _, err := client.Export(ctx, req); status.Code(err) != codes.Unauthenticated {
		t.Errorf("wrong token: error = %v, want Unauthenticated", err)
	}
	ctx = metadata.AppendToOutgoingContext(context.Background(), "authorization", "bearer secret")
	if _, err := client.Export(ctx, req); err != nil {
		t.Errorf("with token: %v", err)
	}
	if seenPeer != "127.0.0.1" {
		t.Errorf("peer host = %q, want 127.0.0.1", seenPeer)
	}
}

// GracefulStop waits for running exports (WaitForHandlers)
func TestGracefulStopWaitsForExports(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	finished := make(chan struct{})
	ing := blockingIngester{started: started, release: release, finished: finished}
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := NewServer(ing, Options{})
	go srv.Serve(lis)
	conn, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	go coltracepb.NewTraceServiceClient(conn).Export(context.Background(), spans(bytes.Repeat([]byte{1}, 16)))
	<-started

	stopped := make(chan struct{})
	go func() { srv.GracefulStop(); close(stopped) }()
	select {
	case <-stopped:
		t.Fatal("GracefulStop returned while an export was running")
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	<-stopped
	select {
	case <-finished:
	default:
		t.Error("GracefulStop returned before the export finished")
	}
}

type blockingIngester struct {
	failingIngester
	started, release, finished chan struct{}
}

func (b blockingIngester) IngestTraces(context.Context, *coltracepb.ExportTraceServiceRequest) (*coltracepb.ExportTraceServiceResponse, error) {
	close(b.started)
	<-b.release
	close(b.finished)
	return &coltracepb.ExportTraceServiceResponse{}, nil
}
