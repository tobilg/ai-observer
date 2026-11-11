// The metric types of the API, generated from the Go types (api.gen.ts)
import type { TimeSeries } from './api.gen'

export type {
  MetricType,
  HistogramStat,
  MetricDataPoint,
  MetricsResponse,
  TimeSeries,
  TimeSeriesResponse,
  MetricAttributeKeysResponse,
  MetricNamesResponse,
  BreakdownValuesResponse,
  MetricQuery,
  MetricQueryErrorCode,
  MetricQueryResult,
  BatchMetricSeriesRequest,
  BatchMetricSeriesResponse,
} from './api.gen'

// A point of a series: [timestamp (Unix ms), value]. A value is null where a
// bucket has none: averages, extremes and percentiles of histograms, and
// gauges, in buckets without points.
export type SeriesPoint = TimeSeries['datapoints'][number]
