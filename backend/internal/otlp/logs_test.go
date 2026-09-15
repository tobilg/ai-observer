package otlp

import (
	"strings"
	"testing"
)

func TestDecodeLogs_CodexCLI(t *testing.T) {
	// Codex CLI style OTLP log payload (JSON format)
	// Simulates various Codex event types
	codexLogPayload := `{
		"resourceLogs": [{
			"resource": {
				"attributes": [
					{"key": "service.name", "value": {"stringValue": "codex-cli"}},
					{"key": "service.version", "value": {"stringValue": "0.1.0"}},
					{"key": "telemetry.sdk.name", "value": {"stringValue": "opentelemetry"}},
					{"key": "telemetry.sdk.language", "value": {"stringValue": "nodejs"}}
				]
			},
			"scopeLogs": [{
				"scope": {
					"name": "codex-cli",
					"version": "0.1.0"
				},
				"logRecords": [
					{
						"timeUnixNano": "1703500000000000000",
						"severityNumber": 9,
						"severityText": "INFO",
						"body": {"stringValue": "codex.conversation_starts"},
						"attributes": [
							{"key": "event.name", "value": {"stringValue": "codex.conversation_starts"}},
							{"key": "model", "value": {"stringValue": "gpt-4"}},
							{"key": "sandbox_mode", "value": {"boolValue": true}},
							{"key": "approval_policy", "value": {"stringValue": "auto-edit"}},
							{"key": "reasoning_enabled", "value": {"boolValue": false}},
							{"key": "conversation_id", "value": {"stringValue": "conv-abc123"}}
						]
					},
					{
						"timeUnixNano": "1703500001000000000",
						"severityNumber": 9,
						"severityText": "INFO",
						"body": {"stringValue": "codex.api_request"},
						"attributes": [
							{"key": "event.name", "value": {"stringValue": "codex.api_request"}},
							{"key": "duration_ms", "value": {"intValue": 1250}},
							{"key": "status_code", "value": {"intValue": 200}},
							{"key": "tokens.prompt", "value": {"intValue": 150}},
							{"key": "tokens.completion", "value": {"intValue": 75}},
							{"key": "tokens.total", "value": {"intValue": 225}}
						]
					},
					{
						"timeUnixNano": "1703500002000000000",
						"severityNumber": 9,
						"severityText": "INFO",
						"body": {"stringValue": "codex.user_prompt"},
						"attributes": [
							{"key": "event.name", "value": {"stringValue": "codex.user_prompt"}},
							{"key": "char_length", "value": {"intValue": 42}},
							{"key": "content", "value": {"stringValue": "Help me refactor this function"}}
						]
					},
					{
						"timeUnixNano": "1703500003000000000",
						"severityNumber": 9,
						"severityText": "INFO",
						"body": {"stringValue": "codex.tool_decision"},
						"attributes": [
							{"key": "event.name", "value": {"stringValue": "codex.tool_decision"}},
							{"key": "tool_name", "value": {"stringValue": "write_file"}},
							{"key": "approved", "value": {"boolValue": true}},
							{"key": "decision_source", "value": {"stringValue": "config"}}
						]
					},
					{
						"timeUnixNano": "1703500004000000000",
						"severityNumber": 9,
						"severityText": "INFO",
						"body": {"stringValue": "codex.tool_result"},
						"attributes": [
							{"key": "event.name", "value": {"stringValue": "codex.tool_result"}},
							{"key": "tool_name", "value": {"stringValue": "write_file"}},
							{"key": "duration_ms", "value": {"intValue": 50}},
							{"key": "success", "value": {"boolValue": true}},
							{"key": "output_preview", "value": {"stringValue": "File written successfully"}}
						]
					}
				]
			}]
		}]
	}`

	// Get JSON decoder
	decoder, err := GetDecoder("application/json")
	if err != nil {
		t.Fatalf("Failed to get decoder: %v", err)
	}

	// Decode the payload
	req, err := decoder.DecodeLogs(strings.NewReader(codexLogPayload))
	if err != nil {
		t.Fatalf("Failed to decode logs: %v", err)
	}

	// Convert to internal format
	result := ConvertLogs(req)
	logs := result.Logs

	// Verify we got all 5 log records
	if len(logs) != 5 {
		t.Errorf("Expected 5 log records, got %d", len(logs))
	}

	// Verify service name extraction
	for _, log := range logs {
		if log.ServiceName != "codex-cli" {
			t.Errorf("Expected service name 'codex-cli', got '%s'", log.ServiceName)
		}
		if log.SeverityText != "INFO" {
			t.Errorf("Expected severity 'INFO', got '%s'", log.SeverityText)
		}
		if log.ScopeName != "codex-cli" {
			t.Errorf("Expected scope name 'codex-cli', got '%s'", log.ScopeName)
		}
	}

	// Verify specific event types in body
	expectedBodies := []string{
		"codex.conversation_starts",
		"codex.api_request",
		"codex.user_prompt",
		"codex.tool_decision",
		"codex.tool_result",
	}

	for i, log := range logs {
		if log.Body != expectedBodies[i] {
			t.Errorf("Log %d: expected body '%s', got '%s'", i, expectedBodies[i], log.Body)
		}
	}

	// Verify attributes are preserved
	// First log should have model attribute
	if logs[0].LogAttributes == nil {
		t.Error("First log should have attributes")
	} else if logs[0].LogAttributes["model"] != "gpt-4" {
		t.Errorf("Expected model 'gpt-4', got '%v'", logs[0].LogAttributes["model"])
	}

	// Second log should have token counts (stored as strings in map[string]string)
	if logs[1].LogAttributes == nil {
		t.Error("Second log should have attributes")
	} else {
		tokensTotal, ok := logs[1].LogAttributes["tokens.total"]
		if !ok {
			t.Error("Expected tokens.total attribute")
		} else if tokensTotal != "225" {
			t.Errorf("Expected tokens.total = '225', got '%s'", tokensTotal)
		}
	}

	t.Logf("Successfully parsed %d Codex CLI log records", len(logs))
}

func TestDecodeLogs_CodexCLI_Protobuf(t *testing.T) {
	// Test that we can get a protobuf decoder (Codex uses protocol = "binary")
	decoder, err := GetDecoder("application/x-protobuf")
	if err != nil {
		t.Fatalf("Failed to get protobuf decoder: %v", err)
	}

	if decoder == nil {
		t.Error("Protobuf decoder should not be nil")
	}

	t.Log("Protobuf decoder available for Codex CLI binary protocol")
}

func TestDecodeLogs_IntegerBody(t *testing.T) {
	// Test that integer body values are correctly converted to string
	payload := `{
		"resourceLogs": [{
			"resource": {
				"attributes": [
					{"key": "service.name", "value": {"stringValue": "test-service"}}
				]
			},
			"scopeLogs": [{
				"scope": {"name": "test"},
				"logRecords": [
					{
						"timeUnixNano": "1703500000000000000",
						"severityNumber": 9,
						"body": {"intValue": 12345}
					},
					{
						"timeUnixNano": "1703500001000000000",
						"severityNumber": 9,
						"body": {"doubleValue": 123.456}
					}
				]
			}]
		}]
	}`

	decoder, err := GetDecoder("application/json")
	if err != nil {
		t.Fatalf("Failed to get decoder: %v", err)
	}

	req, err := decoder.DecodeLogs(strings.NewReader(payload))
	if err != nil {
		t.Fatalf("Failed to decode logs: %v", err)
	}

	result := ConvertLogs(req)
	logs := result.Logs

	if len(logs) != 2 {
		t.Fatalf("Expected 2 log records, got %d", len(logs))
	}

	// Verify integer body is correctly converted
	if logs[0].Body != "12345" {
		t.Errorf("Expected integer body '12345', got '%s'", logs[0].Body)
	}

	// Verify double body is correctly converted
	if logs[1].Body != "123.456" {
		t.Errorf("Expected double body '123.456', got '%s'", logs[1].Body)
	}

	t.Logf("Integer body: %s, Double body: %s", logs[0].Body, logs[1].Body)
}

func TestCodexEventTypes(t *testing.T) {
	// Verify all documented Codex event types
	eventTypes := map[string]string{
		"codex.conversation_starts": "Model, reasoning config, sandbox mode, approval policy",
		"codex.api_request":         "Duration, HTTP status, token counts",
		"codex.sse_event":           "Streamed response metrics and timing",
		"codex.user_prompt":         "Character length, content (if enabled)",
		"codex.tool_decision":       "Approval/denial status, decision source",
		"codex.tool_result":         "Execution duration, success status, output preview",
	}

	for eventType, description := range eventTypes {
		t.Run(eventType, func(t *testing.T) {
			if !strings.HasPrefix(eventType, "codex.") {
				t.Errorf("Event type should have 'codex.' prefix: %s", eventType)
			}
			t.Logf("%s: %s", eventType, description)
		})
	}
}

func TestConvertLogs_CodexSSEEvent_ExtractsMetrics(t *testing.T) {
	// Test that codex.sse_event with response.completed extracts metrics
	payload := `{
		"resourceLogs": [{
			"resource": {
				"attributes": [
					{"key": "service.name", "value": {"stringValue": "codex_cli_rs"}}
				]
			},
			"scopeLogs": [{
				"scope": {"name": "codex_otel"},
				"logRecords": [
					{
						"timeUnixNano": "1703500000000000000",
						"severityNumber": 9,
						"body": {"stringValue": "codex.sse_event"},
						"attributes": [
							{"key": "event.name", "value": {"stringValue": "codex.sse_event"}},
							{"key": "event.kind", "value": {"stringValue": "response.completed"}},
							{"key": "input_token_count", "value": {"stringValue": "1000"}},
							{"key": "output_token_count", "value": {"stringValue": "500"}},
							{"key": "cached_token_count", "value": {"stringValue": "200"}},
							{"key": "model", "value": {"stringValue": "gpt-5"}}
						]
					}
				]
			}]
		}]
	}`

	decoder, err := GetDecoder("application/json")
	if err != nil {
		t.Fatalf("Failed to get decoder: %v", err)
	}

	req, err := decoder.DecodeLogs(strings.NewReader(payload))
	if err != nil {
		t.Fatalf("Failed to decode logs: %v", err)
	}

	result := ConvertLogs(req)

	// SSE events should NOT be stored as logs
	if len(result.Logs) != 0 {
		t.Errorf("Expected 0 log records (SSE events filtered), got %d", len(result.Logs))
	}

	// But metrics should be extracted
	if len(result.DerivedMetrics) == 0 {
		t.Fatal("Expected derived metrics to be extracted from SSE event")
	}

	// Should have token metrics + cost metric
	// 3 token types (input, output, cacheRead) + 1 cost = 4 metrics
	expectedMetrics := 4
	if len(result.DerivedMetrics) != expectedMetrics {
		t.Errorf("Expected %d derived metrics, got %d", expectedMetrics, len(result.DerivedMetrics))
	}

	// Verify we have both token and cost metrics
	hasTokenMetric := false
	hasCostMetric := false
	for _, m := range result.DerivedMetrics {
		if m.MetricName == CodexTokenUsageMetric {
			hasTokenMetric = true
		}
		if m.MetricName == CodexCostUsageMetric {
			hasCostMetric = true
		}
	}

	if !hasTokenMetric {
		t.Error("Expected token usage metric")
	}
	if !hasCostMetric {
		t.Error("Expected cost usage metric")
	}
}

func TestConvertLogs_CodexSSEEvent_NonResponseCompleted_NoMetrics(t *testing.T) {
	// Test that non-response.completed SSE events don't extract metrics
	payload := `{
		"resourceLogs": [{
			"resource": {
				"attributes": [
					{"key": "service.name", "value": {"stringValue": "codex_cli_rs"}}
				]
			},
			"scopeLogs": [{
				"scope": {"name": "codex_otel"},
				"logRecords": [
					{
						"timeUnixNano": "1703500000000000000",
						"severityNumber": 9,
						"body": {"stringValue": "codex.sse_event"},
						"attributes": [
							{"key": "event.name", "value": {"stringValue": "codex.sse_event"}},
							{"key": "event.kind", "value": {"stringValue": "chunk"}}
						]
					}
				]
			}]
		}]
	}`

	decoder, err := GetDecoder("application/json")
	if err != nil {
		t.Fatalf("Failed to get decoder: %v", err)
	}

	req, err := decoder.DecodeLogs(strings.NewReader(payload))
	if err != nil {
		t.Fatalf("Failed to decode logs: %v", err)
	}

	result := ConvertLogs(req)

	// SSE events should NOT be stored as logs
	if len(result.Logs) != 0 {
		t.Errorf("Expected 0 log records, got %d", len(result.Logs))
	}

	// And no metrics for non-response.completed
	if len(result.DerivedMetrics) != 0 {
		t.Errorf("Expected 0 derived metrics for chunk event, got %d", len(result.DerivedMetrics))
	}
}

func TestConvertLogs_PreservesProtoEventName(t *testing.T) {
	payload := `{
		"resourceLogs": [{
			"resource": {
				"attributes": [
					{"key": "service.name", "value": {"stringValue": "copilot-chat"}}
				]
			},
			"scopeLogs": [{
				"scope": {"name": "copilot-chat"},
				"logRecords": [
					{
						"timeUnixNano": "1703500000000000000",
						"severityNumber": 9,
						"eventName": "copilot_chat.session.start",
						"body": {"stringValue": "copilot_chat.session.start"},
						"attributes": [
							{"key": "session.id", "value": {"stringValue": "conv-123"}}
						]
					}
				]
			}]
		}]
	}`

	decoder, err := GetDecoder("application/json")
	if err != nil {
		t.Fatalf("Failed to get decoder: %v", err)
	}

	req, err := decoder.DecodeLogs(strings.NewReader(payload))
	if err != nil {
		t.Fatalf("Failed to decode logs: %v", err)
	}

	result := ConvertLogs(req)
	if len(result.Logs) != 1 {
		t.Fatalf("Expected 1 log record, got %d", len(result.Logs))
	}
	if got := result.Logs[0].LogAttributes["event.name"]; got != "copilot_chat.session.start" {
		t.Fatalf("expected event.name from proto EventName, got %q", got)
	}
	if got := result.Logs[0].LogAttributes["session.id"]; got != "conv-123" {
		t.Fatalf("expected session.id conv-123, got %q", got)
	}
}
