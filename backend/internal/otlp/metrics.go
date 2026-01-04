package otlp

import (
	"github.com/tobilg/ai-observer/internal/api"
	colmetricspb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	metricspb "go.opentelemetry.io/proto/otlp/metrics/v1"
)

// MetricConversionResult contains the converted metrics and any derived metrics
type MetricConversionResult struct {
	Metrics        []api.MetricDataPoint
	DerivedMetrics []api.MetricDataPoint
}

// ConvertMetrics converts OTLP metrics to internal metric format
func ConvertMetrics(req *colmetricspb.ExportMetricsServiceRequest) MetricConversionResult {
	var metrics []api.MetricDataPoint
	var derivedMetrics []api.MetricDataPoint

	for _, rm := range req.GetResourceMetrics() {
		serviceName := extractServiceName(rm.GetResource().GetAttributes())
		resourceAttrs := convertAttributes(rm.GetResource().GetAttributes())

		for _, sm := range rm.GetScopeMetrics() {
			scopeName := sm.GetScope().GetName()
			scopeVersion := sm.GetScope().GetVersion()

			for _, m := range sm.GetMetrics() {
				baseMetric := api.MetricDataPoint{
					ServiceName:        serviceName,
					MetricName:         m.GetName(),
					MetricDescription:  m.GetDescription(),
					MetricUnit:         m.GetUnit(),
					ResourceAttributes: resourceAttrs,
					ScopeName:          scopeName,
					ScopeVersion:       scopeVersion,
				}

				switch data := m.Data.(type) {
				case *metricspb.Metric_Gauge:
					metrics = append(metrics, convertGauge(baseMetric, data.Gauge)...)
				case *metricspb.Metric_Sum:
					metrics = append(metrics, convertSum(baseMetric, data.Sum)...)
				case *metricspb.Metric_Histogram:
					metrics = append(metrics, convertHistogram(baseMetric, data.Histogram)...)
				case *metricspb.Metric_ExponentialHistogram:
					metrics = append(metrics, convertExpHistogram(baseMetric, data.ExponentialHistogram)...)
				case *metricspb.Metric_Summary:
					metrics = append(metrics, convertSummary(baseMetric, data.Summary)...)
				}
			}
		}
	}

	// Derive Gemini cost metrics from token usage metrics
	for _, m := range metrics {
		if derived := DeriveGeminiCostMetric(m); derived != nil {
			derivedMetrics = append(derivedMetrics, *derived)
		}
	}

	// Derive user-facing metrics for Claude Code token usage
	// (filters to only include API calls that have cache activity)
	userFacingMetrics := DeriveClaudeUserFacingMetrics(metrics)
	derivedMetrics = append(derivedMetrics, userFacingMetrics...)

	return MetricConversionResult{Metrics: metrics, DerivedMetrics: derivedMetrics}
}

func convertGauge(base api.MetricDataPoint, gauge *metricspb.Gauge) []api.MetricDataPoint {
	var metrics []api.MetricDataPoint
	for _, dp := range gauge.GetDataPoints() {
		m := base
		m.Timestamp = nanosToTime(dp.GetTimeUnixNano())
		m.Attributes = convertAttributes(dp.GetAttributes())
		m.MetricType = "gauge"
		value := getNumberValue(dp)
		m.Value = &value
		metrics = append(metrics, m)
	}
	return metrics
}

func convertSum(base api.MetricDataPoint, sum *metricspb.Sum) []api.MetricDataPoint {
	var metrics []api.MetricDataPoint
	aggregationTemp := int32(sum.GetAggregationTemporality())
	isMonotonic := sum.GetIsMonotonic()

	for _, dp := range sum.GetDataPoints() {
		m := base
		m.Timestamp = nanosToTime(dp.GetTimeUnixNano())
		m.Attributes = convertAttributes(dp.GetAttributes())
		m.MetricType = "sum"
		value := getNumberValue(dp)
		m.Value = &value
		m.AggregationTemporality = &aggregationTemp
		m.IsMonotonic = &isMonotonic
		metrics = append(metrics, m)
	}
	return metrics
}

func convertHistogram(base api.MetricDataPoint, hist *metricspb.Histogram) []api.MetricDataPoint {
	var metrics []api.MetricDataPoint
	aggregationTemp := int32(hist.GetAggregationTemporality())

	for _, dp := range hist.GetDataPoints() {
		m := base
		m.Timestamp = nanosToTime(dp.GetTimeUnixNano())
		m.Attributes = convertAttributes(dp.GetAttributes())
		m.MetricType = "histogram"
		count := dp.GetCount()
		m.Count = &count
		if dp.Sum != nil {
			sum := dp.GetSum()
			m.Sum = &sum
		}
		m.BucketCounts = dp.GetBucketCounts()
		m.ExplicitBounds = dp.GetExplicitBounds()
		m.AggregationTemporality = &aggregationTemp
		if dp.Min != nil {
			min := dp.GetMin()
			m.Min = &min
		}
		if dp.Max != nil {
			max := dp.GetMax()
			m.Max = &max
		}
		metrics = append(metrics, m)
	}
	return metrics
}

func convertExpHistogram(base api.MetricDataPoint, hist *metricspb.ExponentialHistogram) []api.MetricDataPoint {
	var metrics []api.MetricDataPoint
	aggregationTemp := int32(hist.GetAggregationTemporality())

	for _, dp := range hist.GetDataPoints() {
		m := base
		m.Timestamp = nanosToTime(dp.GetTimeUnixNano())
		m.Attributes = convertAttributes(dp.GetAttributes())
		m.MetricType = "exponential_histogram"
		count := dp.GetCount()
		m.Count = &count
		if dp.Sum != nil {
			sum := dp.GetSum()
			m.Sum = &sum
		}
		scale := dp.GetScale()
		m.Scale = &scale
		zeroCount := dp.GetZeroCount()
		m.ZeroCount = &zeroCount

		if pos := dp.GetPositive(); pos != nil {
			offset := pos.GetOffset()
			m.PositiveOffset = &offset
			m.PositiveBucketCounts = pos.GetBucketCounts()
		}
		if neg := dp.GetNegative(); neg != nil {
			offset := neg.GetOffset()
			m.NegativeOffset = &offset
			m.NegativeBucketCounts = neg.GetBucketCounts()
		}

		m.AggregationTemporality = &aggregationTemp
		if dp.Min != nil {
			min := dp.GetMin()
			m.Min = &min
		}
		if dp.Max != nil {
			max := dp.GetMax()
			m.Max = &max
		}
		metrics = append(metrics, m)
	}
	return metrics
}

func convertSummary(base api.MetricDataPoint, summary *metricspb.Summary) []api.MetricDataPoint {
	var metrics []api.MetricDataPoint
	for _, dp := range summary.GetDataPoints() {
		m := base
		m.Timestamp = nanosToTime(dp.GetTimeUnixNano())
		m.Attributes = convertAttributes(dp.GetAttributes())
		m.MetricType = "summary"
		count := dp.GetCount()
		m.Count = &count
		sum := dp.GetSum()
		m.Sum = &sum

		quantiles := dp.GetQuantileValues()
		m.QuantileQuantiles = make([]float64, len(quantiles))
		m.QuantileValues = make([]float64, len(quantiles))
		for i, q := range quantiles {
			m.QuantileQuantiles[i] = q.GetQuantile()
			m.QuantileValues[i] = q.GetValue()
		}
		metrics = append(metrics, m)
	}
	return metrics
}

// getNumberValue extracts the numeric value from a NumberDataPoint
func getNumberValue(dp *metricspb.NumberDataPoint) float64 {
	switch v := dp.Value.(type) {
	case *metricspb.NumberDataPoint_AsDouble:
		return v.AsDouble
	case *metricspb.NumberDataPoint_AsInt:
		return float64(v.AsInt)
	default:
		return 0
	}
}
