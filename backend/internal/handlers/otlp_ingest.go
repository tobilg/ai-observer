package handlers

import (
	"context"
	"fmt"

	"github.com/tobilg/ai-observer/internal/logger"
	"github.com/tobilg/ai-observer/internal/otlp"
	"github.com/tobilg/ai-observer/internal/selfmetrics"
	"github.com/tobilg/ai-observer/internal/websocket"
	collogspb "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	colmetricspb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	coltracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/protobuf/proto"
)

// The ingestion pipeline of each signal, shared by OTLP/HTTP (HandleTraces,
// HandleMetrics, HandleLogs) and OTLP/gRPC (internal/otlpgrpc). A transport
// decodes the request and passes it here: invalid records are dropped and
// reported as partial success (otlp.DropInvalid*), the rest converted,
// retried log and metric batches skipped (batchDedup), the records stored
// and a notification broadcast. The response carries the partial success;
// an error is always a *StoreError.

// StoreError reports that received telemetry could not be stored. Nothing of
// the export was stored, so the exporter's retry duplicates nothing:
// OTLP/HTTP answers 503 with Retry-After and OTLP/gRPC Unavailable, both of
// which exporters retry (a 500 would make them drop the batch).
type StoreError struct {
	Signal string
	Err    error
}

func (e *StoreError) Error() string { return "failed to store " + e.Signal + ": " + e.Err.Error() }

func (e *StoreError) Unwrap() error { return e.Err }

func storeError(signal string, err error) error {
	logger.Error("Failed to store "+signal, "error", err)
	return &StoreError{Signal: signal, Err: err}
}

// BatchKey identifies the content of a log or metric export, so that an
// exporter's retry of a stored batch is recognized (batchDedup)
type BatchKey = batchKey

// MessageBatchKey returns the BatchKey of a decoded export request: the
// hash of its deterministic protobuf encoding. OTLP/gRPC uses it, since its
// requests arrive decoded; a retry carries the same message, so it has the
// same encoding. Compute it before the request is passed to an Ingest
// function, which drops invalid records from it.
func MessageBatchKey(signal string, msg proto.Message) (BatchKey, error) {
	data, err := proto.MarshalOptions{Deterministic: true}.Marshal(msg)
	if err != nil {
		return BatchKey{}, fmt.Errorf("encoding %s request: %w", signal, err)
	}
	h := newBatchHasher(signal)
	h.Write(data)
	return h.key(), nil
}

// IngestTraces stores the spans of an export and the metrics derived from
// them (Copilot token usage and cost) together; spans sent again are skipped
// and get no second set of metrics, so traces need no batch key
func (h *Handlers) IngestTraces(ctx context.Context, req *coltracepb.ExportTraceServiceRequest) (*coltracepb.ExportTraceServiceResponse, error) {
	rejections := otlp.DropInvalidSpans(req)
	resp := &coltracepb.ExportTraceServiceResponse{}
	if !rejections.Empty() {
		resp.PartialSuccess = &coltracepb.ExportTracePartialSuccess{
			RejectedSpans: rejections.Count, ErrorMessage: rejections.Message(),
		}
		logger.Warn("Rejected invalid spans", "rejected", rejections.Count, "reason", rejections.Message())
		selfmetrics.RecordsRejected.With("spans").Add(int(rejections.Count))
	}

	spans, derivedMetrics, err := h.store.InsertTraces(ctx, otlp.ConvertTraces(req), otlp.DeriveCopilotMetricsFromSpans)
	if err != nil {
		return nil, storeError("traces", err)
	}
	if h.hub != nil && len(spans) > 0 {
		h.hub.Broadcast(websocket.SpansStored(spans))
	}
	if h.hub != nil && len(derivedMetrics) > 0 {
		h.hub.Broadcast(websocket.MetricsStored(derivedMetrics))
	}
	logger.Debug("Received spans", "count", len(spans), "derived_metrics", len(derivedMetrics))
	selfmetrics.RecordsStored.With("spans").Add(len(spans))
	selfmetrics.RecordsStored.With("metrics").Add(len(derivedMetrics))
	return resp, nil
}

// IngestMetrics stores the data points of an export and the metrics derived
// from them. Cumulative counters are stored as they are: metric series
// compute their per-interval increases. key identifies the export's content.
func (h *Handlers) IngestMetrics(ctx context.Context, req *colmetricspb.ExportMetricsServiceRequest, key BatchKey) (*colmetricspb.ExportMetricsServiceResponse, error) {
	rejections := otlp.DropInvalidDataPoints(req)
	resp := &colmetricspb.ExportMetricsServiceResponse{}
	if !rejections.Empty() {
		resp.PartialSuccess = &colmetricspb.ExportMetricsPartialSuccess{
			RejectedDataPoints: rejections.Count, ErrorMessage: rejections.Message(),
		}
		logger.Warn("Rejected invalid metric data points", "rejected", rejections.Count, "reason", rejections.Message())
		selfmetrics.RecordsRejected.With("metrics").Add(int(rejections.Count))
	}

	result := otlp.ConvertMetrics(req)
	allMetrics := append(result.Metrics, result.DerivedMetrics...)

	stored, err := h.claimBatch(ctx, key, "metrics")
	if err != nil {
		return nil, err
	}
	if stored {
		return resp, nil
	}
	err = h.store.InsertMetrics(ctx, allMetrics)
	h.dedup.release(key, err == nil)
	if err != nil {
		return nil, storeError("metrics", err)
	}
	if h.hub != nil && len(allMetrics) > 0 {
		h.hub.Broadcast(websocket.MetricsStored(allMetrics))
	}
	logger.Debug("Received metrics", "received", len(result.Metrics), "derived", len(result.DerivedMetrics))
	selfmetrics.RecordsStored.With("metrics").Add(len(allMetrics))
	return resp, nil
}

// IngestLogs stores the log records of an export and the metrics derived
// from them (Codex token usage and cost) together, so a failure leaves
// nothing for the exporter's retry to duplicate. key identifies the export's
// content. No log record is invalid: OTLP requires none of their fields.
func (h *Handlers) IngestLogs(ctx context.Context, req *collogspb.ExportLogsServiceRequest, key BatchKey) (*collogspb.ExportLogsServiceResponse, error) {
	resp := &collogspb.ExportLogsServiceResponse{}
	result := otlp.ConvertLogs(req)

	stored, err := h.claimBatch(ctx, key, "logs")
	if err != nil {
		return nil, err
	}
	if stored {
		return resp, nil
	}
	err = h.store.InsertTelemetry(ctx, result.Logs, result.DerivedMetrics, nil)
	h.dedup.release(key, err == nil)
	if err != nil {
		return nil, storeError("logs", err)
	}
	if h.hub != nil && len(result.DerivedMetrics) > 0 {
		h.hub.Broadcast(websocket.MetricsStored(result.DerivedMetrics))
	}
	if h.hub != nil && len(result.Logs) > 0 {
		h.hub.Broadcast(websocket.LogsStored(result.Logs))
	}
	logger.Debug("Received log records", "count", len(result.Logs), "derived_metrics", len(result.DerivedMetrics))
	selfmetrics.RecordsStored.With("logs").Add(len(result.Logs))
	selfmetrics.RecordsStored.With("metrics").Add(len(result.DerivedMetrics))
	return resp, nil
}

// claimBatch reports whether the batch with key was stored before (an
// exporter's retry), which is then answered as a success without storing it
// again. Otherwise the caller must store the batch and release the claim with
// the outcome. An error (the client went away while the same batch was being
// stored) is a *StoreError.
func (h *Handlers) claimBatch(ctx context.Context, key BatchKey, signal string) (stored bool, err error) {
	claimed, err := h.dedup.claim(ctx, key)
	if err != nil {
		return false, storeError(signal, err)
	}
	if !claimed {
		logger.Debug("Skipping a batch that was stored before (exporter retry)", "signal", signal)
		selfmetrics.BatchesDeduplicated.With(signal).Inc()
		return true, nil
	}
	return false, nil
}
