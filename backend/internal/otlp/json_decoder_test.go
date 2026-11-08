package otlp

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"strings"
	"testing"
)

const (
	testTraceID = "5b8efff798038103d269b633813fc60c"
	testSpanID  = "eee19b7ec3c1b174"
	testParent  = "eee19b7ec3c1b173"
)

func TestJSONDecoderReadsHexTraceIDs(t *testing.T) {
	body := `{"resourceSpans":[{"scopeSpans":[{"spans":[
		{"traceId":"` + testTraceID + `","spanId":"` + testSpanID + `","parentSpanId":"` + testParent + `","name":"hex",
		 "links":[{"traceId":"` + strings.ToUpper(testTraceID) + `","spanId":"` + testParent + `"}]},
		{"traceId":"` + base64.StdEncoding.EncodeToString(mustHex(t, testTraceID)) + `","spanId":"` + base64.StdEncoding.EncodeToString(mustHex(t, testSpanID)) + `","name":"base64"},
		{"traceId":"` + testTraceID + `","spanId":"` + testSpanID + `","parentSpanId":"","name":"root"}
	]}]}]}`
	req, err := (&JSONDecoder{}).DecodeTraces(strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	spans := req.GetResourceSpans()[0].GetScopeSpans()[0].GetSpans()
	for _, s := range spans {
		if got := hex.EncodeToString(s.GetTraceId()); got != testTraceID {
			t.Errorf("%s: trace ID %s, want %s", s.GetName(), got, testTraceID)
		}
		if got := hex.EncodeToString(s.GetSpanId()); got != testSpanID {
			t.Errorf("%s: span ID %s, want %s", s.GetName(), got, testSpanID)
		}
	}
	if got := hex.EncodeToString(spans[0].GetParentSpanId()); got != testParent {
		t.Errorf("parent span ID %s, want %s", got, testParent)
	}
	if len(spans[2].GetParentSpanId()) != 0 {
		t.Errorf("empty parent span ID read as %x", spans[2].GetParentSpanId())
	}
	link := spans[0].GetLinks()[0]
	if hex.EncodeToString(link.GetTraceId()) != testTraceID || hex.EncodeToString(link.GetSpanId()) != testParent {
		t.Errorf("link = %x/%x", link.GetTraceId(), link.GetSpanId())
	}
}

func TestJSONDecoderReadsHexLogAndExemplarIDs(t *testing.T) {
	logs, err := (&JSONDecoder{}).DecodeLogs(strings.NewReader(`{"resourceLogs":[{"scopeLogs":[{"logRecords":[
		{"traceId":"` + testTraceID + `","spanId":"` + testSpanID + `","body":{"stringValue":"x"}},
		{"body":{"stringValue":"no trace context"}}
	]}]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	records := logs.GetResourceLogs()[0].GetScopeLogs()[0].GetLogRecords()
	if hex.EncodeToString(records[0].GetTraceId()) != testTraceID || hex.EncodeToString(records[0].GetSpanId()) != testSpanID {
		t.Errorf("log IDs = %x/%x", records[0].GetTraceId(), records[0].GetSpanId())
	}
	if len(records[1].GetTraceId()) != 0 || len(records[1].GetSpanId()) != 0 {
		t.Errorf("log without trace context has IDs %x/%x", records[1].GetTraceId(), records[1].GetSpanId())
	}

	metrics, err := (&JSONDecoder{}).DecodeMetrics(strings.NewReader(`{"resourceMetrics":[{"scopeMetrics":[{"metrics":[
		{"name":"m","sum":{"dataPoints":[{"asInt":"1","exemplars":[{"asInt":"1","traceId":"` + testTraceID + `","spanId":"` + testSpanID + `"}]}]}}
	]}]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	e := metrics.GetResourceMetrics()[0].GetScopeMetrics()[0].GetMetrics()[0].GetSum().GetDataPoints()[0].GetExemplars()[0]
	if hex.EncodeToString(e.GetTraceId()) != testTraceID || hex.EncodeToString(e.GetSpanId()) != testSpanID {
		t.Errorf("exemplar IDs = %x/%x", e.GetTraceId(), e.GetSpanId())
	}
}

func TestHexIDKeepsOtherIDs(t *testing.T) {
	// 24 bytes whose base64 is not hex: not a misread hex ID
	notHex := bytes.Repeat([]byte{0xff}, 24)
	for _, id := range [][]byte{nil, mustHex(t, testTraceID), notHex, bytes.Repeat([]byte{1}, 20)} {
		if got := hexID(id, traceIDSize); !bytes.Equal(got, id) {
			t.Errorf("hexID(%x) = %x, want it unchanged", id, got)
		}
	}
}

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
