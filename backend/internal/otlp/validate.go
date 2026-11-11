package otlp

import (
	"fmt"
	"strings"

	colmetricspb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	coltracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	metricspb "go.opentelemetry.io/proto/otlp/metrics/v1"
)

// Rejections describes the records of an export request that are not stored
// because the OTLP specification calls them invalid, for the response's
// partial_success (https://opentelemetry.io/docs/specs/otlp/#partial-success).
// Records left out on purpose (Codex's codex.sse_event logs, the tools'
// noise spans, spans sent again) are not rejections. Warnings describe data
// that holds no records, such as a metric without data, and count nothing.
type Rejections struct {
	// Count is the number of rejected records (spans, data points, log
	// records): the response's rejected_<signal>
	Count  int64
	issues []rejectionIssue
}

type rejectionIssue struct {
	reason string
	count  int64
}

// Empty reports whether nothing was rejected or warned about
func (r *Rejections) Empty() bool {
	return len(r.issues) == 0
}

// Message is the response's error_message: each reason with its number, in
// the order they were met, e.g. "2 spans rejected: invalid trace ID"
func (r *Rejections) Message() string {
	parts := make([]string, len(r.issues))
	for i, issue := range r.issues {
		parts[i] = fmt.Sprintf("%d %s", issue.count, issue.reason)
	}
	return strings.Join(parts, "; ")
}

// reject counts a rejected record with reason
func (r *Rejections) reject(reason string) {
	r.Count++
	r.note(reason)
}

// note records reason once more without rejecting a record
func (r *Rejections) note(reason string) {
	for i := range r.issues {
		if r.issues[i].reason == reason {
			r.issues[i].count++
			return
		}
	}
	r.issues = append(r.issues, rejectionIssue{reason: reason, count: 1})
}

// Rejection reasons, phrased to follow the count in Message
const (
	reasonInvalidTraceID    = "span(s) rejected: invalid trace_id (must be 16 bytes, not all zero)"
	reasonInvalidSpanID     = "span(s) rejected: invalid span_id (must be 8 bytes, not all zero)"
	reasonNoValue           = "number data point(s) rejected: no value (as_int or as_double)"
	reasonBucketCounts      = "histogram data point(s) rejected: bucket_counts must be empty or have one more element than explicit_bounds"
	reasonMetricWithoutData = "metric(s) without data or of an unknown type ignored"
)

// DropInvalidSpans removes the spans whose trace or span ID is invalid (not
// 16 and 8 bytes, or all zero; the OTLP proto calls such IDs invalid and the
// fields required) from req and returns them as rejections. Storage would
// key them by an empty or broken ID, and every such span would replace the
// previous one.
func DropInvalidSpans(req *coltracepb.ExportTraceServiceRequest) Rejections {
	var r Rejections
	for _, rs := range req.GetResourceSpans() {
		for _, ss := range rs.GetScopeSpans() {
			kept := ss.Spans[:0]
			for _, s := range ss.GetSpans() {
				switch {
				case !validID(s.GetTraceId(), traceIDSize):
					r.reject(reasonInvalidTraceID)
				case !validID(s.GetSpanId(), spanIDSize):
					r.reject(reasonInvalidSpanID)
				default:
					kept = append(kept, s)
				}
			}
			clear(ss.Spans[len(kept):])
			ss.Spans = kept
		}
	}
	return r
}

// validID reports whether id has size bytes and is not all zero
func validID(id []byte, size int) bool {
	if len(id) != size {
		return false
	}
	for _, b := range id {
		if b != 0 {
			return true
		}
	}
	return false
}

// DropInvalidDataPoints removes the data points the OTLP proto calls invalid
// from req and returns them as rejections: number data points (gauge, sum)
// without a value, which were stored as 0 before, and histogram data points
// whose bucket_counts do not fit explicit_bounds. Metrics without data (or
// of a type this version does not know) hold no data points: they are
// ignored, as before, and reported as a warning.
func DropInvalidDataPoints(req *colmetricspb.ExportMetricsServiceRequest) Rejections {
	var r Rejections
	for _, rm := range req.GetResourceMetrics() {
		for _, sm := range rm.GetScopeMetrics() {
			for _, m := range sm.GetMetrics() {
				switch data := m.Data.(type) {
				case *metricspb.Metric_Gauge:
					data.Gauge.DataPoints = dropNumberPoints(data.Gauge.GetDataPoints(), &r)
				case *metricspb.Metric_Sum:
					data.Sum.DataPoints = dropNumberPoints(data.Sum.GetDataPoints(), &r)
				case *metricspb.Metric_Histogram:
					data.Histogram.DataPoints = dropHistogramPoints(data.Histogram.GetDataPoints(), &r)
				case *metricspb.Metric_ExponentialHistogram, *metricspb.Metric_Summary:
				default:
					r.note(reasonMetricWithoutData)
				}
			}
		}
	}
	return r
}

func dropNumberPoints(points []*metricspb.NumberDataPoint, r *Rejections) []*metricspb.NumberDataPoint {
	kept := points[:0]
	for _, dp := range points {
		if dp.Value == nil {
			r.reject(reasonNoValue)
			continue
		}
		kept = append(kept, dp)
	}
	clear(points[len(kept):])
	return kept
}

func dropHistogramPoints(points []*metricspb.HistogramDataPoint, r *Rejections) []*metricspb.HistogramDataPoint {
	kept := points[:0]
	for _, dp := range points {
		if n := len(dp.GetBucketCounts()); n != 0 && n != len(dp.GetExplicitBounds())+1 {
			r.reject(reasonBucketCounts)
			continue
		}
		kept = append(kept, dp)
	}
	clear(points[len(kept):])
	return kept
}
