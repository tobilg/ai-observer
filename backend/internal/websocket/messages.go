package websocket

import (
	"time"

	"github.com/tobilg/ai-observer/internal/api"
)

type MessageType string

const (
	MessageTypeTraces  MessageType = "traces"
	MessageTypeMetrics MessageType = "metrics"
	MessageTypeLogs    MessageType = "logs"
)

type Message struct {
	Type      MessageType `json:"type"`
	Timestamp time.Time   `json:"timestamp"`
	Payload   interface{} `json:"payload"`
}

func NewTracesMessage(payload interface{}) Message {
	return Message{
		Type:      MessageTypeTraces,
		Timestamp: time.Now(),
		Payload:   payload,
	}
}

func NewMetricsMessage(payload interface{}) Message {
	return Message{
		Type:      MessageTypeMetrics,
		Timestamp: time.Now(),
		Payload:   payload,
	}
}

func NewLogsMessage(payload interface{}) Message {
	return Message{
		Type:      MessageTypeLogs,
		Timestamp: time.Now(),
		Payload:   payload,
	}
}

// Notification is the payload of a message: it tells clients that new records
// were stored, without their content. Clients re-query the API for the data,
// so prompts, code and tool output are not pushed to every open browser, and
// large ingestion batches do not turn into large WebSocket frames.
type Notification struct {
	Count      int      `json:"count"`
	Services   []string `json:"services"`
	Timestamps []int64  `json:"timestamps"` // Unix milliseconds of each record
}

func newNotification(n int, record func(i int) (service string, ts time.Time)) Notification {
	notification := Notification{Count: n, Services: []string{}, Timestamps: make([]int64, n)}
	seen := make(map[string]bool)
	for i := 0; i < n; i++ {
		service, ts := record(i)
		notification.Timestamps[i] = ts.UnixMilli()
		if !seen[service] {
			seen[service] = true
			notification.Services = append(notification.Services, service)
		}
	}
	return notification
}

// SpansStored announces stored spans
func SpansStored(spans []api.Span) Message {
	return NewTracesMessage(newNotification(len(spans), func(i int) (string, time.Time) {
		return spans[i].ServiceName, spans[i].Timestamp
	}))
}

// LogsStored announces stored log records
func LogsStored(logs []api.LogRecord) Message {
	return NewLogsMessage(newNotification(len(logs), func(i int) (string, time.Time) {
		return logs[i].ServiceName, logs[i].Timestamp
	}))
}

// MetricsStored announces stored metric data points
func MetricsStored(metrics []api.MetricDataPoint) Message {
	return NewMetricsMessage(newNotification(len(metrics), func(i int) (string, time.Time) {
		return metrics[i].ServiceName, metrics[i].Timestamp
	}))
}
