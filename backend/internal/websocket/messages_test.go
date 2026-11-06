package websocket

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/tobilg/ai-observer/internal/api"
)

func TestNewTracesMessage(t *testing.T) {
	payload := map[string]string{"trace": "data"}
	before := time.Now()
	msg := NewTracesMessage(payload)
	after := time.Now()

	if msg.Type != MessageTypeTraces {
		t.Errorf("Type = %q, want %q", msg.Type, MessageTypeTraces)
	}
	if msg.Timestamp.Before(before) || msg.Timestamp.After(after) {
		t.Errorf("Timestamp not in expected range")
	}
	if msg.Payload == nil {
		t.Error("Payload is nil")
	}
}

func TestNewMetricsMessage(t *testing.T) {
	payload := []int{1, 2, 3}
	msg := NewMetricsMessage(payload)

	if msg.Type != MessageTypeMetrics {
		t.Errorf("Type = %q, want %q", msg.Type, MessageTypeMetrics)
	}
	if msg.Payload == nil {
		t.Error("Payload is nil")
	}
}

func TestNewLogsMessage(t *testing.T) {
	payload := "log entry"
	msg := NewLogsMessage(payload)

	if msg.Type != MessageTypeLogs {
		t.Errorf("Type = %q, want %q", msg.Type, MessageTypeLogs)
	}
	if msg.Payload != payload {
		t.Errorf("Payload = %v, want %v", msg.Payload, payload)
	}
}

func TestMessageTypes(t *testing.T) {
	if MessageTypeTraces != "traces" {
		t.Errorf("MessageTypeTraces = %q, want %q", MessageTypeTraces, "traces")
	}
	if MessageTypeMetrics != "metrics" {
		t.Errorf("MessageTypeMetrics = %q, want %q", MessageTypeMetrics, "metrics")
	}
	if MessageTypeLogs != "logs" {
		t.Errorf("MessageTypeLogs = %q, want %q", MessageTypeLogs, "logs")
	}
}

func TestStoredNotificationsCarryNoContent(t *testing.T) {
	ts := time.Date(2025, 1, 2, 10, 0, 0, 0, time.UTC)
	msg := LogsStored([]api.LogRecord{
		{Timestamp: ts, ServiceName: "claude-code", Body: "secret prompt"},
		{Timestamp: ts.Add(time.Second), ServiceName: "claude-code", Body: "more"},
		{Timestamp: ts.Add(2 * time.Second), ServiceName: "gemini_cli"},
	})

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(data), "secret") {
		t.Errorf("notification contains record content: %s", data)
	}

	n, ok := msg.Payload.(Notification)
	if !ok {
		t.Fatalf("payload is %T, want Notification", msg.Payload)
	}
	if n.Count != 3 || len(n.Timestamps) != 3 || n.Timestamps[1] != ts.Add(time.Second).UnixMilli() {
		t.Errorf("notification = %+v", n)
	}
	if len(n.Services) != 2 || n.Services[0] != "claude-code" || n.Services[1] != "gemini_cli" {
		t.Errorf("services = %v, want claude-code and gemini_cli", n.Services)
	}
}
