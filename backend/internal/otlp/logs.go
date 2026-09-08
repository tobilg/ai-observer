package otlp

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/tobilg/ai-observer/internal/api"
	collogspb "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
)

// LogConversionResult contains converted logs and any derived metrics
type LogConversionResult struct {
	Logs           []api.LogRecord
	DerivedMetrics []api.MetricDataPoint
}

// ConvertLogs converts OTLP logs to internal log format and extracts derived metrics
func ConvertLogs(req *collogspb.ExportLogsServiceRequest) LogConversionResult {
	var logs []api.LogRecord
	var derivedMetrics []api.MetricDataPoint

	for _, rl := range req.GetResourceLogs() {
		serviceName := extractServiceName(rl.GetResource().GetAttributes())
		resourceAttrs := convertAttributes(rl.GetResource().GetAttributes())
		resourceSchemaURL := rl.GetSchemaUrl()

		for _, sl := range rl.GetScopeLogs() {
			scopeName := sl.GetScope().GetName()
			scopeVersion := sl.GetScope().GetVersion()
			scopeAttrs := convertAttributes(sl.GetScope().GetAttributes())
			scopeSchemaURL := sl.GetSchemaUrl()

			for _, lr := range sl.GetLogRecords() {
				logAttrs := convertAttributes(lr.GetAttributes())
				if eventName := strings.TrimSpace(lr.GetEventName()); eventName != "" && logAttrs["event.name"] == "" {
					logAttrs["event.name"] = eventName
				}

				// Resolve timestamp with fallbacks before creating log record
				timestamp := nanosToTime(lr.GetTimeUnixNano())
				if timestamp.IsZero() || timestamp.Unix() == 0 {
					// Try event.timestamp attribute (used by tracing crate's OpenTelemetryTracingBridge)
					if eventTimestamp, ok := logAttrs["event.timestamp"]; ok {
						if t, err := time.Parse(time.RFC3339, eventTimestamp); err == nil {
							timestamp = t
						} else if t, err := time.Parse(time.RFC3339Nano, eventTimestamp); err == nil {
							timestamp = t
						}
					}
					// Fallback to observed timestamp if still zero
					if timestamp.IsZero() || timestamp.Unix() == 0 {
						if observedNanos := lr.GetObservedTimeUnixNano(); observedNanos > 0 {
							timestamp = nanosToTime(observedNanos)
						}
					}
				}

				log := api.LogRecord{
					Timestamp:          timestamp,
					TraceID:            bytesToHex(lr.GetTraceId()),
					SpanID:             bytesToHex(lr.GetSpanId()),
					TraceFlags:         lr.GetFlags(),
					SeverityText:       lr.GetSeverityText(),
					SeverityNumber:     int32(lr.GetSeverityNumber()),
					ServiceName:        serviceName,
					Body:               anyValueToBody(lr.GetBody()),
					ResourceSchemaURL:  resourceSchemaURL,
					ResourceAttributes: resourceAttrs,
					ScopeSchemaURL:     scopeSchemaURL,
					ScopeName:          scopeName,
					ScopeVersion:       scopeVersion,
					ScopeAttributes:    scopeAttrs,
					LogAttributes:      logAttrs,
				}

				// If severity text is empty, derive from severity number
				if log.SeverityText == "" {
					log.SeverityText = severityNumberToText(lr.GetSeverityNumber())
				}

				// Handle tracing crate's OpenTelemetryTracingBridge format:
				// - event.name in attributes → Body
				eventName, hasEventName := logAttrs["event.name"]

				// Handle codex.sse_event events from Codex CLI:
				// - Extract token/cost metrics from response.completed events
				// - Filter out the raw log (too noisy to store)
				if hasEventName && eventName == "codex.sse_event" && serviceName == "codex_cli_rs" {
					// Extract metrics from response.completed events
					if metrics := ExtractCodexMetrics(logAttrs, log.Timestamp, serviceName, resourceAttrs, log.TraceID, log.SpanID); len(metrics) > 0 {
						derivedMetrics = append(derivedMetrics, metrics...)
					}
					continue // Skip storing raw log
				}

				if log.Body == "" && hasEventName {
					log.Body = eventName
				}

				logs = append(logs, log)
			}
		}
	}

	return LogConversionResult{Logs: logs, DerivedMetrics: derivedMetrics}
}

// anyValueToBody converts an AnyValue to a string body
// For complex values, it serializes to JSON
func anyValueToBody(v *commonpb.AnyValue) string {
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

func convertArrayValue(arr *commonpb.ArrayValue) []interface{} {
	if arr == nil {
		return nil
	}
	result := make([]interface{}, len(arr.Values))
	for i, v := range arr.Values {
		result[i] = anyValueToInterface(v)
	}
	return result
}

func convertKvListValue(kvl *commonpb.KeyValueList) map[string]interface{} {
	if kvl == nil {
		return nil
	}
	result := make(map[string]interface{})
	for _, kv := range kvl.Values {
		result[kv.Key] = anyValueToInterface(kv.Value)
	}
	return result
}

func anyValueToInterface(v *commonpb.AnyValue) interface{} {
	if v == nil {
		return nil
	}
	switch val := v.Value.(type) {
	case *commonpb.AnyValue_StringValue:
		return val.StringValue
	case *commonpb.AnyValue_IntValue:
		return val.IntValue
	case *commonpb.AnyValue_DoubleValue:
		return val.DoubleValue
	case *commonpb.AnyValue_BoolValue:
		return val.BoolValue
	case *commonpb.AnyValue_ArrayValue:
		return convertArrayValue(val.ArrayValue)
	case *commonpb.AnyValue_KvlistValue:
		return convertKvListValue(val.KvlistValue)
	case *commonpb.AnyValue_BytesValue:
		return val.BytesValue
	default:
		return nil
	}
}

func severityNumberToText(sn logspb.SeverityNumber) string {
	switch {
	case sn >= logspb.SeverityNumber_SEVERITY_NUMBER_FATAL:
		return "FATAL"
	case sn >= logspb.SeverityNumber_SEVERITY_NUMBER_ERROR:
		return "ERROR"
	case sn >= logspb.SeverityNumber_SEVERITY_NUMBER_WARN:
		return "WARN"
	case sn >= logspb.SeverityNumber_SEVERITY_NUMBER_INFO:
		return "INFO"
	case sn >= logspb.SeverityNumber_SEVERITY_NUMBER_DEBUG:
		return "DEBUG"
	case sn >= logspb.SeverityNumber_SEVERITY_NUMBER_TRACE:
		return "TRACE"
	default:
		return ""
	}
}
