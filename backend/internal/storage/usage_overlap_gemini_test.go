package storage

import (
	"strconv"
	"testing"
	"time"

	"github.com/tobilg/ai-observer/internal/api"
)

// geminiCounts are the token counts of one Gemini CLI response
type geminiCounts struct{ input, output, cached, thoughts, tool float64 }

// geminiFileResponse returns the token and cost points an import of a
// Gemini CLI session file stores for one response (importer.ConvertGeminiMessages):
// a point per token type with a count, at the message's time
func geminiFileResponse(session string, ts time.Time, tk geminiCounts) []api.MetricDataPoint {
	var points []api.MetricDataPoint
	for _, t := range []struct {
		tokenType string
		v         float64
	}{{"input", tk.input}, {"output", tk.output}, {"cache", tk.cached}, {"thought", tk.thoughts}, {"tool", tk.tool}} {
		if t.v > 0 {
			points = append(points, api.MetricDataPoint{Timestamp: ts, ServiceName: "gemini_cli", MetricName: "gemini_cli.token.usage",
				MetricType: "sum", Value: float(t.v), Attributes: map[string]string{
					"type": t.tokenType, "model": "gemini-2.5-pro", "import_source": "local_jsonl", "session.id": session}})
		}
	}
	return append(points, api.MetricDataPoint{Timestamp: ts, ServiceName: "gemini_cli", MetricName: "gemini_cli.cost.usage",
		MetricType: "sum", Value: float(0.02), Attributes: map[string]string{
			"model": "gemini-2.5-pro", "import_source": "local_jsonl", "session.id": session}})
}

// geminiOTLPResponse returns Gemini CLI's api_response event of a response
func geminiOTLPResponse(session string, ts time.Time, tk geminiCounts) api.LogRecord {
	n := func(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }
	return api.LogRecord{Timestamp: ts, ServiceName: "gemini_cli", Body: "API response from gemini-2.5-pro. Status: 200. Duration: 900ms.",
		LogAttributes: map[string]string{
			"event.name": "gemini_cli.api_response", "session.id": session, "model": "gemini-2.5-pro", "duration_ms": "900",
			"input_token_count": n(tk.input), "output_token_count": n(tk.output), "cached_content_token_count": n(tk.cached),
			"thoughts_token_count": n(tk.thoughts), "tool_token_count": n(tk.tool), "prompt_id": session + "########1",
		}}
}

// geminiOTLPTokens returns a cumulative gemini_cli.token.usage point of a
// session: its running total of input tokens
func geminiOTLPTokens(session string, ts time.Time, total float64) api.MetricDataPoint {
	cumulative := int32(2)
	return api.MetricDataPoint{Timestamp: ts, ServiceName: "gemini_cli", MetricName: "gemini_cli.token.usage", MetricType: "sum",
		Value: float(total), AggregationTemporality: &cumulative,
		Attributes: map[string]string{"model": "gemini-2.5-pro", "type": "input", "session.id": session, "installation.id": "i1"}}
}

// A Gemini CLI session of three responses: the first two recorded over OTLP
// and in the file, the third (sent while AI Observer was down) only in the
// file. The file records a response after its event: a text response
// milliseconds after, a response of tool calls only once its calls ran.
type geminiSession struct {
	id, file string
	t0       time.Time
}

var geminiResponses = []geminiCounts{
	{7777, 90, 3114, 260, 0},
	{7874, 26, 6080, 29, 0},
	{8407, 80, 3076, 255, 12},
}

func (s geminiSession) eventAt(i int) time.Time { return s.t0.Add(time.Duration(i) * time.Minute) }
func (s geminiSession) fileAt(i int) time.Time {
	if i == 1 {
		return s.eventAt(i).Add(40 * time.Second) // tool calls only
	}
	return s.eventAt(i).Add(30 * time.Millisecond)
}

func (s geminiSession) fileRecords(from, to int) []api.MetricDataPoint {
	var points []api.MetricDataPoint
	for i := from; i < to; i++ {
		points = append(points, geminiFileResponse(s.id, s.fileAt(i), geminiResponses[i])...)
	}
	return points
}

// otlp returns the events of responses 0 and 1 and the session's next
// cumulative token point after them
func (s geminiSession) otlp() ([]api.LogRecord, api.MetricDataPoint) {
	logs := []api.LogRecord{geminiOTLPResponse(s.id, s.eventAt(0), geminiResponses[0]), geminiOTLPResponse(s.id, s.eventAt(1), geminiResponses[1])}
	return logs, geminiOTLPTokens(s.id, s.eventAt(1).Add(10*time.Second), geminiResponses[0].input+geminiResponses[1].input)
}

func (e *overlapEnv) checkGemini(s geminiSession) {
	e.t.Helper()
	for i, want := range []int{0, 0, 6} {
		if got := e.fileUsageAt("gemini_cli", s.fileAt(i)); got != want {
			e.t.Errorf("file usage points of response %d = %d, want %d", i, got, want)
		}
	}
}

func TestGeminiUsageBothOrders(t *testing.T) {
	s := geminiSession{id: "8b65451f-31e9-4d19-82ff-fa85ff0b09c4", file: "/gemini/tmp/p/chats/session-8b65451f.json", t0: overlapBase}
	t.Run("OTLP first, then import", func(t *testing.T) {
		e := newOverlapEnv(t)
		logs, point := s.otlp()
		e.otlpLogs(logs, []api.MetricDataPoint{point})
		e.importFile("gemini", s.file, nil, s.fileRecords(0, 3), false)
		e.checkGemini(s)
	})
	t.Run("import first, then events, then metrics", func(t *testing.T) {
		e := newOverlapEnv(t)
		e.importFile("gemini", s.file, nil, s.fileRecords(0, 3), false)
		logs, point := s.otlp()
		e.otlpLogs(logs, nil)
		if got := e.fileUsageAt("gemini_cli", s.fileAt(0)); got != 5 {
			t.Errorf("file usage before the session's OTLP metrics arrived = %d points, want 5 (kept)", got)
		}
		e.otlpMetrics(point)
		e.checkGemini(s)
	})
	t.Run("watch appends, then re-import", func(t *testing.T) {
		e := newOverlapEnv(t)
		logs, point := s.otlp()
		e.otlpLogs(logs, []api.MetricDataPoint{point})
		e.watchFile("gemini", s.file, nil, s.fileRecords(0, 1))
		e.watchFile("gemini", s.file, nil, s.fileRecords(1, 3))
		e.checkGemini(s)
		e.importFile("gemini", s.file, nil, s.fileRecords(0, 3), true)
		e.checkGemini(s)
	})
}

// A file response is only taken as recorded over OTLP with an event of the
// same session and equal counts of every token type within the window, and
// OTLP token metrics of the session at or after the event
func TestGeminiUsageNoFalseMatches(t *testing.T) {
	t0 := overlapBase
	tk := geminiResponses[0]
	other := tk
	other.thoughts++
	noThoughts := tk
	noThoughts.thoughts = 0
	tests := []struct {
		name   string
		logs   []api.LogRecord
		points []api.MetricDataPoint
	}{
		{"other session", []api.LogRecord{geminiOTLPResponse("g2", t0, tk)}, []api.MetricDataPoint{geminiOTLPTokens("g2", t0.Add(time.Second), 1)}},
		{"other counts", []api.LogRecord{geminiOTLPResponse("g1", t0, other)}, []api.MetricDataPoint{geminiOTLPTokens("g1", t0.Add(time.Second), 1)}},
		{"missing token type", []api.LogRecord{geminiOTLPResponse("g1", t0, noThoughts)}, []api.MetricDataPoint{geminiOTLPTokens("g1", t0.Add(time.Second), 1)}},
		{"outside the window", []api.LogRecord{geminiOTLPResponse("g1", t0.Add(-2*time.Hour), tk)}, []api.MetricDataPoint{geminiOTLPTokens("g1", t0.Add(-2*time.Hour+time.Second), 1)}},
		{"events without metrics", []api.LogRecord{geminiOTLPResponse("g1", t0, tk)}, nil},
		{"metrics only before the event", []api.LogRecord{geminiOTLPResponse("g1", t0, tk)}, []api.MetricDataPoint{geminiOTLPTokens("g1", t0.Add(-time.Second), 1)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newOverlapEnv(t)
			e.importFile("gemini", "/gemini/session-g1.json", nil, geminiFileResponse("g1", t0.Add(30*time.Millisecond), tk), false)
			e.otlpLogs(tt.logs, tt.points)
			if got := e.fileUsageAt("gemini_cli", t0.Add(30*time.Millisecond)); got != 5 {
				t.Errorf("file usage = %d points, want all 5 kept", got)
			}
		})
	}
}

// Sessions seen through one path keep their usage
func TestGeminiUsageOnePathUnaffected(t *testing.T) {
	e := newOverlapEnv(t)
	s := geminiSession{id: "g1", file: "/gemini/session-g1.json", t0: overlapBase}
	e.importFile("gemini", s.file, nil, s.fileRecords(0, 3), false)
	o := geminiSession{id: "g2", t0: overlapBase}
	logs, point := o.otlp()
	e.otlpLogs(logs, []api.MetricDataPoint{point})
	if got := e.fileUsage("gemini_cli"); got["gemini_cli.token.usage"] != 13 || got["gemini_cli.cost.usage"] != 3 {
		t.Errorf("file usage = %v, want all kept", got)
	}
	if got := e.count("otel_metrics"); got != 17 {
		t.Errorf("metric points = %d, want 16 file and 1 OTLP", got)
	}
}
