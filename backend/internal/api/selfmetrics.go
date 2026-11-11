package api

// InternalMetricsResponse is the answer of GET /api/internal/metrics: AI
// Observer's own metrics (internal/selfmetrics), also served in the
// Prometheus text format at GET /metrics
type InternalMetricsResponse struct {
	// StartedAt is when the counters started (process start), RFC 3339
	StartedAt string           `json:"startedAt"`
	Metrics   []InternalMetric `json:"metrics"`
}

// InternalMetric is a metric family: its series by label values
type InternalMetric struct {
	Name string `json:"name"`
	Help string `json:"help"`
	// Type is counter, gauge or histogram
	Type   string                 `json:"type"`
	Series []InternalMetricSeries `json:"series"`
}

// InternalMetricSeries is one series of a metric. Counters and gauges have
// Value; histograms have Count, Sum and cumulative Buckets.
type InternalMetricSeries struct {
	Labels  map[string]string `json:"labels,omitempty"`
	Value   *float64          `json:"value,omitempty"`
	Count   *uint64           `json:"count,omitempty"`
	Sum     *float64          `json:"sum,omitempty"`
	Buckets []InternalBucket  `json:"buckets,omitempty"`
}

// InternalBucket counts the observations up to LE (cumulative; the last
// bucket, LE "+Inf", counts all)
type InternalBucket struct {
	LE    string `json:"le"`
	Count uint64 `json:"count"`
}
