package selfmetrics

import (
	"strings"
	"sync"
	"testing"
)

func TestRegistryValuesAndExposition(t *testing.T) {
	r := NewRegistry()
	requests := r.NewCounter("test_requests_total", "Requests\nby result", "signal", "result")
	clients := r.NewGauge("test_clients", "Clients")
	latency := r.NewHistogram("test_seconds", "Latency", []float64{0.1, 1}, "route")

	var wg sync.WaitGroup
	for range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			requests.With("logs", "ok").Inc()
			latency.With("/api/x").Observe(0.05)
		}()
	}
	wg.Wait()
	requests.With("logs", "ok").Add(-3) // ignored
	requests.With(`tr"aces`, "error").Add(2)
	clients.With().Add(3)
	clients.With().Add(-1)
	latency.With("/api/x").Observe(0.5)
	latency.With("/api/x").Observe(5)

	if got := requests.With("logs", "ok").Value(); got != 50 {
		t.Errorf("counter = %d", got)
	}
	if got := latency.With("/api/x").Count(); got != 52 {
		t.Errorf("histogram count = %d", got)
	}

	snap := r.Snapshot()
	if len(snap.Metrics) != 3 || snap.Metrics[0].Name != "test_requests_total" || len(snap.Metrics[0].Series) != 2 {
		t.Fatalf("snapshot = %+v", snap)
	}
	h := snap.Metrics[2].Series[0]
	if len(h.Buckets) != 3 || h.Buckets[0].Count != 50 || h.Buckets[1].Count != 51 || h.Buckets[2].LE != "+Inf" || h.Buckets[2].Count != 52 {
		t.Errorf("buckets = %+v", h.Buckets)
	}

	var b strings.Builder
	if err := r.WritePrometheus(&b); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	for _, want := range []string{
		"# HELP test_requests_total Requests\\nby result\n# TYPE test_requests_total counter\n",
		`test_requests_total{signal="logs",result="ok"} 50` + "\n",
		`test_requests_total{signal="tr\"aces",result="error"} 2` + "\n",
		"# TYPE test_clients gauge\ntest_clients 2\n",
		`test_seconds_bucket{route="/api/x",le="0.1"} 50` + "\n",
		`test_seconds_bucket{route="/api/x",le="+Inf"} 52` + "\n",
		`test_seconds_count{route="/api/x"} 52` + "\n",
		`test_seconds_sum{route="/api/x"} 7.99`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("exposition lacks %q:\n%s", want, out)
		}
	}
}

func TestGaugeFunc(t *testing.T) {
	r := NewRegistry()
	n := int64(4)
	r.NewGaugeFunc("test_func", "Read when exposed", func() int64 { return n })
	n = 7
	snap := r.Snapshot()
	if v := snap.Metrics[0].Series[0].Value; v == nil || *v != 7 {
		t.Errorf("gauge func = %v", v)
	}
	SetWebSocketClients(func() int { return 3 })
	var b strings.Builder
	Default.WritePrometheus(&b)
	if !strings.Contains(b.String(), "\nai_observer_websocket_clients 3\n") {
		t.Errorf("websocket clients missing:\n%s", b.String())
	}
}

func TestRegistryRejectsMistakes(t *testing.T) {
	r := NewRegistry()
	r.NewCounter("dup", "")
	for name, fn := range map[string]func(){
		"duplicate":    func() { r.NewCounter("dup", "") },
		"label count":  func() { r.NewCounter("c", "", "a").With() },
		"bucket order": func() { r.NewHistogram("h", "", []float64{2, 1}) },
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s: no panic", name)
				}
			}()
			fn()
		}()
	}
}

func TestOTLPResult(t *testing.T) {
	for status, want := range map[int]string{200: "ok", 400: "invalid", 415: "invalid", 401: "unauthorized", 429: "unauthorized",
		413: "too_large", 503: "unavailable", 500: "error"} {
		if got := OTLPResult(status); got != want {
			t.Errorf("%d: %q, want %q", status, got, want)
		}
	}
}

func TestWriteTimer(t *testing.T) {
	waits, txs := WriteLockWaitSeconds.With().Count(), WriteTransactionSeconds.With().Count()
	func() {
		wt := StartWrite()
		defer wt.Locked().Done()
	}()
	if WriteLockWaitSeconds.With().Count() != waits+1 || WriteTransactionSeconds.With().Count() != txs+1 {
		t.Error("write not measured")
	}
}
