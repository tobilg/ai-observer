export interface MetricDataPoint {
  timestamp: string
  serviceName: string
  metricName: string
  metricDescription?: string
  metricUnit?: string
  resourceAttributes?: Record<string, string>
  scopeName?: string
  scopeVersion?: string
  attributes?: Record<string, string>
  metricType: 'gauge' | 'sum' | 'histogram' | 'exponential_histogram' | 'summary'
  value?: number
  aggregationTemporality?: number
  isMonotonic?: boolean
  count?: number
  sum?: number
  bucketCounts?: number[]
  explicitBounds?: number[]
  scale?: number
  zeroCount?: number
  positiveOffset?: number
  positiveBucketCounts?: number[]
  negativeOffset?: number
  negativeBucketCounts?: number[]
  quantileValues?: number[]
  quantileQuantiles?: number[]
  min?: number
  max?: number
}

export interface MetricsResponse {
  metrics: MetricDataPoint[]
  total: number
  hasMore: boolean
  // Requests the next page (cursor parameter) when hasMore
  nextCursor?: string
}

export interface TimeSeries {
  name: string
  labels?: Record<string, string>
  datapoints: [number, number][]
}

export interface TimeSeriesResponse {
  series: TimeSeries[]
}

export interface MetricNamesResponse {
  names: string[]
}
