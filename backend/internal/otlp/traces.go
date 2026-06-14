package otlp

import (
	"encoding/hex"
	"encoding/json"
	"strconv"
	"time"

	"github.com/tobilg/ai-observer/internal/api"
	coltracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
)

// ConvertTraces converts OTLP traces to internal span format
func ConvertTraces(req *coltracepb.ExportTraceServiceRequest) []api.Span {
	var spans []api.Span

	for _, rs := range req.GetResourceSpans() {
		serviceName := extractServiceName(rs.GetResource().GetAttributes())
		resourceAttrs := convertAttributes(rs.GetResource().GetAttributes())

		for _, ss := range rs.GetScopeSpans() {
			scopeName := ss.GetScope().GetName()
			scopeVersion := ss.GetScope().GetVersion()

			for _, s := range ss.GetSpans() {
				span := api.Span{
					Timestamp:          nanosToTime(s.GetStartTimeUnixNano()),
					TraceID:            bytesToHex(s.GetTraceId()),
					SpanID:             bytesToHex(s.GetSpanId()),
					ParentSpanID:       bytesToHex(s.GetParentSpanId()),
					TraceState:         s.GetTraceState(),
					SpanName:           s.GetName(),
					SpanKind:           spanKindToString(s.GetKind()),
					ServiceName:        serviceName,
					ResourceAttributes: resourceAttrs,
					ScopeName:          scopeName,
					ScopeVersion:       scopeVersion,
					SpanAttributes:     convertAttributes(s.GetAttributes()),
					Duration:           int64(s.GetEndTimeUnixNano() - s.GetStartTimeUnixNano()),
					StatusCode:         statusCodeToString(s.GetStatus().GetCode()),
					StatusMessage:      s.GetStatus().GetMessage(),
					Events:             convertEvents(s.GetEvents()),
					Links:              convertLinks(s.GetLinks()),
				}
				spans = append(spans, span)
			}
		}
	}

	return spans
}

func extractServiceName(attrs []*commonpb.KeyValue) string {
	for _, kv := range attrs {
		if kv.GetKey() == "service.name" {
			return anyValueToString(kv.GetValue())
		}
	}
	return "unknown"
}

func convertAttributes(attrs []*commonpb.KeyValue) map[string]string {
	result := make(map[string]string)
	for _, kv := range attrs {
		result[kv.GetKey()] = anyValueToString(kv.GetValue())
	}
	return result
}

func anyValueToString(v *commonpb.AnyValue) string {
	if v == nil {
		return ""
	}
	switch val := v.Value.(type) {
	case *commonpb.AnyValue_StringValue:
		return val.StringValue
	case *commonpb.AnyValue_IntValue:
		return strconv.FormatInt(val.IntValue, 10)
	case *commonpb.AnyValue_DoubleValue:
		return strconv.FormatFloat(val.DoubleValue, 'f', -1, 64)
	case *commonpb.AnyValue_BoolValue:
		if val.BoolValue {
			return "true"
		}
		return "false"
	case *commonpb.AnyValue_ArrayValue:
		data, _ := json.Marshal(convertArrayValue(val.ArrayValue))
		return string(data)
	case *commonpb.AnyValue_KvlistValue:
		data, _ := json.Marshal(convertKvListValue(val.KvlistValue))
		return string(data)
	case *commonpb.AnyValue_BytesValue:
		return string(val.BytesValue)
	default:
		return ""
	}
}

func convertEvents(events []*tracepb.Span_Event) []api.SpanEvent {
	result := make([]api.SpanEvent, len(events))
	for i, e := range events {
		result[i] = api.SpanEvent{
			Timestamp:  nanosToTime(e.GetTimeUnixNano()),
			Name:       e.GetName(),
			Attributes: convertAttributes(e.GetAttributes()),
		}
	}
	return result
}

func convertLinks(links []*tracepb.Span_Link) []api.SpanLink {
	result := make([]api.SpanLink, len(links))
	for i, l := range links {
		result[i] = api.SpanLink{
			TraceID:    bytesToHex(l.GetTraceId()),
			SpanID:     bytesToHex(l.GetSpanId()),
			TraceState: l.GetTraceState(),
			Attributes: convertAttributes(l.GetAttributes()),
		}
	}
	return result
}

func nanosToTime(nanos uint64) time.Time {
	return time.Unix(0, int64(nanos))
}

func bytesToHex(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	return hex.EncodeToString(b)
}

func spanKindToString(kind tracepb.Span_SpanKind) string {
	switch kind {
	case tracepb.Span_SPAN_KIND_INTERNAL:
		return "INTERNAL"
	case tracepb.Span_SPAN_KIND_SERVER:
		return "SERVER"
	case tracepb.Span_SPAN_KIND_CLIENT:
		return "CLIENT"
	case tracepb.Span_SPAN_KIND_PRODUCER:
		return "PRODUCER"
	case tracepb.Span_SPAN_KIND_CONSUMER:
		return "CONSUMER"
	default:
		return "UNSPECIFIED"
	}
}

func statusCodeToString(code tracepb.Status_StatusCode) string {
	switch code {
	case tracepb.Status_STATUS_CODE_OK:
		return "OK"
	case tracepb.Status_STATUS_CODE_ERROR:
		return "ERROR"
	default:
		return "UNSET"
	}
}
