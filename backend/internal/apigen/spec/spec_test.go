package spec

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/tobilg/ai-observer/internal/api"
	"github.com/tobilg/ai-observer/internal/apigen"
	"github.com/tobilg/ai-observer/internal/otlp"
	"github.com/tobilg/ai-observer/internal/storage"
	"github.com/tobilg/ai-observer/internal/websocket"
	colmetricspb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	metricspb "go.opentelemetry.io/proto/otlp/metrics/v1"
)

// The committed TypeScript types and OpenAPI document are what the Go
// types say (the drift check)
func TestGeneratedFilesAreUpToDate(t *testing.T) {
	files, err := Files(".")
	if err != nil {
		t.Fatal(err)
	}
	root, err := RepoRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	for path, want := range files {
		got, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
		if err != nil || string(got) != string(want) {
			t.Errorf("%s is out of date with the Go types of the API: run %s and commit the result", path, RegenerateCommand)
		}
	}
}

func buildREST(t *testing.T) *apigen.Model {
	t.Helper()
	m, err := apigen.Build(REST(), ".")
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// WebSocket messages match their types, also for empty batches
func TestWebSocketMessagesMatchTypes(t *testing.T) {
	m := buildREST(t)
	now := time.Now()
	messages := []websocket.Message{
		websocket.SpansStored([]api.Span{{ServiceName: "a", Timestamp: now}, {ServiceName: "b", Timestamp: now}}),
		websocket.LogsStored([]api.LogRecord{{ServiceName: "a", Timestamp: now}}),
		websocket.MetricsStored(nil),
	}
	for _, msg := range messages {
		data, err := json.Marshal(msg)
		if err != nil {
			t.Fatal(err)
		}
		for _, problem := range m.Validate("WebSocketMessage", data) {
			t.Errorf("%s: %s", data, problem)
		}
	}
}

// The values the code writes are members of the enums the types declare
func TestEnumsCoverWrittenValues(t *testing.T) {
	m := buildREST(t)
	members := func(name string) []string {
		var values []string
		for _, member := range m.Enum(name).Members {
			values = append(values, member.Value)
		}
		return values
	}
	for _, status := range []string{storage.TraceStatus(1, 1), storage.TraceStatus(0, 1), storage.TraceStatus(0, 0)} {
		if !slices.Contains(members("TraceStatus"), status) {
			t.Errorf("trace status %q is not a TraceStatus", status)
		}
	}

	// Each type of OTLP metric becomes a MetricType
	attrs := []*metricspb.Metric{
		{Name: "g", Data: &metricspb.Metric_Gauge{Gauge: &metricspb.Gauge{DataPoints: []*metricspb.NumberDataPoint{{}}}}},
		{Name: "s", Data: &metricspb.Metric_Sum{Sum: &metricspb.Sum{DataPoints: []*metricspb.NumberDataPoint{{}}}}},
		{Name: "h", Data: &metricspb.Metric_Histogram{Histogram: &metricspb.Histogram{DataPoints: []*metricspb.HistogramDataPoint{{}}}}},
		{Name: "e", Data: &metricspb.Metric_ExponentialHistogram{ExponentialHistogram: &metricspb.ExponentialHistogram{
			DataPoints: []*metricspb.ExponentialHistogramDataPoint{{}}}}},
		{Name: "q", Data: &metricspb.Metric_Summary{Summary: &metricspb.Summary{DataPoints: []*metricspb.SummaryDataPoint{{}}}}},
	}
	req := &colmetricspb.ExportMetricsServiceRequest{ResourceMetrics: []*metricspb.ResourceMetrics{{
		ScopeMetrics: []*metricspb.ScopeMetrics{{Metrics: attrs}},
	}}}
	converted := otlp.ConvertMetrics(req).Metrics
	if len(converted) != len(attrs) {
		t.Fatalf("converted %d points, want %d", len(converted), len(attrs))
	}
	seen := map[string]bool{}
	for _, p := range converted {
		seen[p.MetricType] = true
		if !slices.Contains(members("MetricType"), p.MetricType) {
			t.Errorf("metric type %q is not a MetricType", p.MetricType)
		}
	}
	if len(seen) != len(members("MetricType")) {
		t.Errorf("metric types written %v, declared %v", seen, members("MetricType"))
	}
}

// The MCP configuration builds (its outputs are checked against real tool
// results by the server's TestMCPOutputsMatchTypes)
func TestMCPConfig(t *testing.T) {
	cfg, err := MCP()
	if err != nil {
		t.Fatal(err)
	}
	m, err := apigen.Build(cfg, ".")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"UsageSummary", "MetricsResult", "TraceResult", "SessionsResult", "SessionRow", "TraceSpan", "MetricPoint"} {
		if m.TypeNamed(name) == nil {
			t.Errorf("no MCP type %s", name)
		}
	}
}

// Every reference of the OpenAPI document names a schema it declares, and
// every operation declares the parameters of its path
func TestOpenAPIIsConsistent(t *testing.T) {
	data, err := buildREST(t).OpenAPI(Routes())
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	schemas := doc["components"].(map[string]any)["schemas"].(map[string]any)
	var walk func(v any, path string)
	walk = func(v any, path string) {
		switch v := v.(type) {
		case map[string]any:
			if ref, ok := v["$ref"].(string); ok {
				name, found := strings.CutPrefix(ref, "#/components/schemas/")
				if !found || schemas[name] == nil {
					t.Errorf("%s: unresolved $ref %s", path, ref)
				}
			}
			for k, child := range v {
				walk(child, path+"/"+k)
			}
		case []any:
			for _, child := range v {
				walk(child, path)
			}
		}
	}
	walk(doc, "")
	ids := map[string]string{}
	for path, item := range doc["paths"].(map[string]any) {
		for method, op := range item.(map[string]any) {
			op := op.(map[string]any)
			id := op["operationId"].(string)
			if prev, dup := ids[id]; dup {
				t.Errorf("operationId %s of %s %s is also that of %s", id, method, path, prev)
			}
			ids[id] = method + " " + path
			declared := map[string]bool{}
			params, _ := op["parameters"].([]any)
			for _, p := range params {
				p := p.(map[string]any)
				if p["in"] == "path" {
					declared[p["name"].(string)] = true
				}
			}
			for _, name := range apigen.PathParams(path) {
				if !declared[name] {
					t.Errorf("%s %s does not declare the path parameter %s", method, path, name)
				}
			}
		}
	}
}
