package storage

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/tobilg/ai-observer/internal/api"
	"github.com/tobilg/ai-observer/internal/redact"
)

// Telemetry text is redacted when it is stored, whatever path stores it
func TestStoredTelemetryIsRedacted(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	store.SetRedactor(redact.New(redact.Secrets))
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	secret := "gh" + "p_" + strings.Repeat("Ab3x", 9)
	value := 1.0

	logs := []api.LogRecord{{
		Timestamp: now, ServiceName: "claude-code", Body: "pushed with " + secret,
		LogAttributes: map[string]string{"session.id": "s1", "tool_input": "git push https://" + secret + "@github.com/o/r"},
	}}
	spans := []api.Span{{
		Timestamp: now, TraceID: "t1", SpanID: "sp1", SpanName: "chat", ServiceName: "copilot-chat",
		SpanAttributes: map[string]string{"gen_ai.conversation.id": "s2", "gen_ai.input.messages": "token " + secret},
		Events:         []api.SpanEvent{{Timestamp: now, Name: "e", Attributes: map[string]string{"x": secret}}},
		StatusMessage:  "failed: " + secret,
	}}
	metrics := []api.MetricDataPoint{{
		Timestamp: now, ServiceName: "claude-code", MetricName: "m", MetricType: "sum", Value: &value,
		Attributes: map[string]string{"note": secret},
	}}
	if err := store.InsertTelemetry(ctx, logs, metrics, spans); err != nil {
		t.Fatal(err)
	}

	for _, q := range []string{
		"SELECT COUNT(*) FROM otel_logs WHERE CAST(LogAttributes AS VARCHAR) LIKE '%' || ? || '%' OR Body LIKE '%' || ? || '%'",
		"SELECT COUNT(*) FROM otel_traces WHERE CAST(SpanAttributes AS VARCHAR) LIKE '%' || ? || '%' OR StatusMessage LIKE '%' || ? || '%'",
		"SELECT COUNT(*) FROM otel_traces WHERE CAST(\"Events.Attributes\" AS VARCHAR) LIKE '%' || ? || '%' OR ? = ''",
		"SELECT COUNT(*) FROM otel_metrics WHERE CAST(Attributes AS VARCHAR) LIKE '%' || ? || '%' OR ? = ''",
	} {
		var n int
		if err := store.DB().QueryRow(q, secret, secret).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Errorf("the secret was stored: %s", q)
		}
	}

	// Content and session IDs stay usable
	sessionLogs, err := store.SessionLogs(ctx, "s1")
	if err != nil || len(sessionLogs) != 1 {
		t.Fatalf("session logs = %v, %v", sessionLogs, err)
	}
	if got := sessionLogs[0].Attrs["tool_input"]; got != "git push https://[REDACTED:github_token]@github.com/o/r" {
		t.Errorf("tool_input = %q", got)
	}
	sessionSpans, err := store.SessionSpans(ctx, "s2")
	if err != nil || len(sessionSpans) != 1 || !strings.Contains(sessionSpans[0].SpanAttributes["gen_ai.input.messages"], "[REDACTED:github_token]") {
		t.Errorf("session spans = %+v, %v", sessionSpans, err)
	}
}

// Without a redactor (AI_OBSERVER_REDACT=off) telemetry is stored as it is
func TestTelemetryWithoutRedactor(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	secret := "gh" + "p_" + strings.Repeat("Ab3x", 9)
	if err := store.InsertLogs(context.Background(), []api.LogRecord{{Timestamp: time.Now(), ServiceName: "s", Body: secret}}); err != nil {
		t.Fatal(err)
	}
	var body string
	if err := store.DB().QueryRow("SELECT Body FROM otel_logs").Scan(&body); err != nil || body != secret {
		t.Errorf("body = %q, %v", body, err)
	}
}

// The records of an export share their resource attributes: they are
// redacted once per transaction, and every record gets the redacted copy
func TestSharedAttributesAreRedactedOnce(t *testing.T) {
	secret := "gh" + "p_" + strings.Repeat("Ab3x", 9)
	resource := map[string]string{"service.name": "claude-code", "token": "token " + secret}
	w := &writeTx{redact: redact.New(redact.Secrets)}

	first, second := w.redactShared(resource), w.redactShared(resource)
	if strings.Contains(first["token"], secret) || first["token"] != "token [REDACTED:github_token]" {
		t.Errorf("redacted = %v", first)
	}
	if len(w.shared) != 1 || reflect.ValueOf(first).UnsafePointer() != reflect.ValueOf(second).UnsafePointer() {
		t.Errorf("the map was redacted %d times, want once", len(w.shared))
	}
	if !strings.Contains(resource["token"], secret) {
		t.Error("the record's own map was changed")
	}
	// Without a redactor nothing is cached
	plain := &writeTx{}
	if got := plain.redactShared(resource); got["token"] != resource["token"] || plain.shared != nil {
		t.Errorf("without a redactor = %v", got)
	}
}
