// Package selfmetrics keeps AI Observer's own metrics in memory: counters,
// gauges and histograms with labels, without a metrics library. The server
// exposes them as JSON (GET /api/internal/metrics) and in the Prometheus text
// format (GET /metrics). Instrumented code uses the metrics declared in
// metrics.go; recording is a few atomic operations.
package selfmetrics

import (
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tobilg/ai-observer/internal/api"
)

// Metric types
const (
	TypeCounter   = "counter"
	TypeGauge     = "gauge"
	TypeHistogram = "histogram"
)

// Registry holds metric families
type Registry struct {
	startedAt time.Time
	mu        sync.Mutex
	families  []*family
	names     map[string]bool
}

// NewRegistry returns an empty registry
func NewRegistry() *Registry {
	return &Registry{startedAt: time.Now(), names: make(map[string]bool)}
}

// Default is the registry of the metrics in metrics.go
var Default = NewRegistry()

type family struct {
	name, help, typ string
	labels          []string
	buckets         []float64    // histograms: upper bounds, ascending
	fn              func() int64 // gauge funcs: read when exposed

	mu     sync.RWMutex
	series map[string]*series // by joined label values
}

type series struct {
	values []string
	// counter and gauge: value; histogram: counts per bucket (the last for
	// +Inf), count and sum (float64 bits)
	value  atomic.Int64
	counts []atomic.Uint64
	count  atomic.Uint64
	sum    atomic.Uint64
}

func (r *Registry) add(name, help, typ string, buckets []float64, labels []string) *family {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.names[name] {
		panic("selfmetrics: duplicate metric " + name)
	}
	r.names[name] = true
	f := &family{name: name, help: help, typ: typ, labels: labels, buckets: buckets, series: make(map[string]*series)}
	r.families = append(r.families, f)
	return f
}

// with returns the series of the label values, creating it on first use
func (f *family) with(values []string) *series {
	if len(values) != len(f.labels) {
		panic(fmt.Sprintf("selfmetrics: %s takes %d label values, got %d", f.name, len(f.labels), len(values)))
	}
	key := strings.Join(values, "\x00")
	f.mu.RLock()
	s := f.series[key]
	f.mu.RUnlock()
	if s != nil {
		return s
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if s = f.series[key]; s == nil {
		s = &series{values: append([]string(nil), values...)}
		if f.typ == TypeHistogram {
			s.counts = make([]atomic.Uint64, len(f.buckets)+1)
		}
		f.series[key] = s
	}
	return s
}

// CounterVec is a counter with labels
type CounterVec struct{ f *family }

// Counter only increases
type Counter struct{ s *series }

// NewCounter declares a counter with the given label names
func (r *Registry) NewCounter(name, help string, labels ...string) *CounterVec {
	return &CounterVec{r.add(name, help, TypeCounter, nil, labels)}
}

// With returns the counter of the label values (in the declared order)
func (v *CounterVec) With(values ...string) Counter { return Counter{v.f.with(values)} }

// Add adds n (ignored when negative)
func (c Counter) Add(n int) {
	if n > 0 {
		c.s.value.Add(int64(n))
	}
}

// Inc adds one
func (c Counter) Inc() { c.s.value.Add(1) }

// Value returns the count
func (c Counter) Value() int64 { return c.s.value.Load() }

// GaugeVec is a gauge with labels
type GaugeVec struct{ f *family }

// Gauge goes up and down
type Gauge struct{ s *series }

// NewGauge declares a gauge with the given label names
func (r *Registry) NewGauge(name, help string, labels ...string) *GaugeVec {
	return &GaugeVec{r.add(name, help, TypeGauge, nil, labels)}
}

// With returns the gauge of the label values
func (v *GaugeVec) With(values ...string) Gauge { return Gauge{v.f.with(values)} }

// Set sets the value
func (g Gauge) Set(n int64) { g.s.value.Store(n) }

// Add adds n (negative to decrease)
func (g Gauge) Add(n int64) { g.s.value.Add(n) }

// Value returns the value
func (g Gauge) Value() int64 { return g.s.value.Load() }

// NewGaugeFunc declares a gauge without labels whose value fn returns when
// the metrics are read
func (r *Registry) NewGaugeFunc(name, help string, fn func() int64) {
	r.add(name, help, TypeGauge, nil, nil).fn = fn
}

// HistogramVec is a histogram with labels
type HistogramVec struct{ f *family }

// Histogram counts observations in buckets
type Histogram struct {
	f *family
	s *series
}

// DurationBuckets are upper bounds in seconds for request and transaction
// durations, from 1 ms to 30 s
var DurationBuckets = []float64{0.001, 0.0025, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30}

// NewHistogram declares a histogram with the given upper bounds (ascending)
// and label names
func (r *Registry) NewHistogram(name, help string, buckets []float64, labels ...string) *HistogramVec {
	if !sort.Float64sAreSorted(buckets) {
		panic("selfmetrics: buckets of " + name + " are not ascending")
	}
	return &HistogramVec{r.add(name, help, TypeHistogram, buckets, labels)}
}

// With returns the histogram of the label values
func (v *HistogramVec) With(values ...string) Histogram { return Histogram{v.f, v.f.with(values)} }

// Observe records a value
func (h Histogram) Observe(v float64) {
	i := sort.SearchFloat64s(h.f.buckets, v) // first bound >= v
	h.s.counts[i].Add(1)
	h.s.count.Add(1)
	for {
		old := h.s.sum.Load()
		if h.s.sum.CompareAndSwap(old, math.Float64bits(math.Float64frombits(old)+v)) {
			return
		}
	}
}

// ObserveSince records the seconds since start
func (h Histogram) ObserveSince(start time.Time) { h.Observe(time.Since(start).Seconds()) }

// Count returns the number of observations
func (h Histogram) Count() uint64 { return h.s.count.Load() }

// sortedSeries returns the series of f ordered by label values
func (f *family) sortedSeries() []*series {
	f.mu.RLock()
	list := make([]*series, 0, len(f.series))
	for _, s := range f.series {
		list = append(list, s)
	}
	f.mu.RUnlock()
	sort.Slice(list, func(i, j int) bool {
		return strings.Join(list[i].values, "\x00") < strings.Join(list[j].values, "\x00")
	})
	return list
}

func (r *Registry) familyList() []*family {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]*family(nil), r.families...)
}

func formatBound(b float64) string {
	if math.IsInf(b, 1) {
		return "+Inf"
	}
	return strconv.FormatFloat(b, 'g', -1, 64)
}

// Snapshot returns the current values of all metrics
func (r *Registry) Snapshot() api.InternalMetricsResponse {
	resp := api.InternalMetricsResponse{StartedAt: r.startedAt.UTC().Format(time.RFC3339), Metrics: []api.InternalMetric{}}
	for _, f := range r.familyList() {
		m := api.InternalMetric{Name: f.name, Help: f.help, Type: f.typ, Series: []api.InternalMetricSeries{}}
		if f.fn != nil {
			v := float64(f.fn())
			m.Series = append(m.Series, api.InternalMetricSeries{Value: &v})
		}
		for _, s := range f.sortedSeries() {
			out := api.InternalMetricSeries{}
			if len(f.labels) > 0 {
				out.Labels = make(map[string]string, len(f.labels))
				for i, l := range f.labels {
					out.Labels[l] = s.values[i]
				}
			}
			if f.typ == TypeHistogram {
				var cumulative uint64
				for i := range s.counts {
					cumulative += s.counts[i].Load()
					bound := math.Inf(1)
					if i < len(f.buckets) {
						bound = f.buckets[i]
					}
					out.Buckets = append(out.Buckets, api.InternalBucket{LE: formatBound(bound), Count: cumulative})
				}
				count, sum := s.count.Load(), math.Float64frombits(s.sum.Load())
				out.Count, out.Sum = &count, &sum
			} else {
				v := float64(s.value.Load())
				out.Value = &v
			}
			m.Series = append(m.Series, out)
		}
		resp.Metrics = append(resp.Metrics, m)
	}
	return resp
}

// WritePrometheus writes all metrics in the Prometheus text exposition
// format (version 0.0.4)
func (r *Registry) WritePrometheus(w io.Writer) error {
	var b strings.Builder
	snapshot := r.Snapshot()
	families := r.familyList()
	for n, m := range snapshot.Metrics {
		f := families[n]
		fmt.Fprintf(&b, "# HELP %s %s\n# TYPE %s %s\n", m.Name, escapeHelp(m.Help), m.Name, m.Type)
		for _, s := range m.Series {
			if m.Type != TypeHistogram {
				fmt.Fprintf(&b, "%s%s %s\n", m.Name, labelText(f.labels, s.Labels, ""), strconv.FormatFloat(*s.Value, 'g', -1, 64))
				continue
			}
			for _, bucket := range s.Buckets {
				fmt.Fprintf(&b, "%s_bucket%s %d\n", m.Name, labelText(f.labels, s.Labels, bucket.LE), bucket.Count)
			}
			fmt.Fprintf(&b, "%s_sum%s %s\n", m.Name, labelText(f.labels, s.Labels, ""), strconv.FormatFloat(*s.Sum, 'g', -1, 64))
			fmt.Fprintf(&b, "%s_count%s %d\n", m.Name, labelText(f.labels, s.Labels, ""), *s.Count)
		}
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// labelText formats labels in declaration order, with le last when set
func labelText(names []string, values map[string]string, le string) string {
	var parts []string
	for _, name := range names {
		parts = append(parts, name+`="`+escapeLabel(values[name])+`"`)
	}
	if le != "" {
		parts = append(parts, `le="`+le+`"`)
	}
	if len(parts) == 0 {
		return ""
	}
	return "{" + strings.Join(parts, ",") + "}"
}

var (
	labelEscaper = strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`)
	helpEscaper  = strings.NewReplacer(`\`, `\\`, "\n", `\n`)
)

func escapeLabel(s string) string { return labelEscaper.Replace(s) }
func escapeHelp(s string) string  { return helpEscaper.Replace(s) }
