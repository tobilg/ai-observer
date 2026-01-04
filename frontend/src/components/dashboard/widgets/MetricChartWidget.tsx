import { useMemo, useCallback } from 'react'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { BarChart as BarChartIcon } from 'lucide-react'
import type { WidgetConfig, TimeSelection } from '@/types/dashboard'
import { isAbsoluteTimeSelection } from '@/types/dashboard'
import { MetricBarChart, CHART_COLORS } from '@/components/charts'
import { useMetricData } from '@/contexts/MetricDataContext'
import {
  getMetricMetadata,
  getSeriesLabel,
  formatMetricValue,
  getSourceDisplayName,
  getServiceDisplayName,
} from '@/lib/metricMetadata'

interface MetricChartWidgetProps {
  widgetId: string
  title: string
  config: WidgetConfig
  timeSelection: TimeSelection
  fromTime: Date
  toTime: Date
}

// Format timestamp for X-axis based on timeframe duration and interval
const formatTickLabel = (timestamp: number, durationSeconds: number, intervalSeconds: number): string => {
  const date = new Date(timestamp)
  const DAY_SECONDS = 24 * 60 * 60

  // For sub-daily intervals, always include time to distinguish bars
  const hasSubDailyInterval = intervalSeconds < DAY_SECONDS

  // Include seconds when interval is less than 60 seconds to ensure unique labels
  const needsSeconds = intervalSeconds < 60

  if (durationSeconds < DAY_SECONDS) {
    // Less than 24h: time only (with seconds if needed for uniqueness)
    return date.toLocaleTimeString([], {
      hour: '2-digit',
      minute: '2-digit',
      ...(needsSeconds && { second: '2-digit' }),
    })
  } else if (durationSeconds <= 7 * DAY_SECONDS || hasSubDailyInterval) {
    // 24h to 7 days OR any range with sub-daily intervals: date + time + year
    return (
      date.toLocaleDateString([], { month: 'short', day: 'numeric', year: 'numeric' }) +
      ' ' +
      date.toLocaleTimeString([], {
        hour: '2-digit',
        minute: '2-digit',
        ...(needsSeconds && { second: '2-digit' }),
      })
    )
  } else {
    // More than 7 days with daily+ intervals: date + year only
    return date.toLocaleDateString([], { month: 'short', day: 'numeric', year: 'numeric' })
  }
}

export function MetricChartWidget({
  widgetId,
  title,
  config,
  timeSelection,
  fromTime,
  toTime,
}: MetricChartWidgetProps) {
  // Calculate duration and interval seconds from time selection
  const { durationSeconds, intervalSeconds } = useMemo(() => {
    if (isAbsoluteTimeSelection(timeSelection)) {
      return {
        durationSeconds: (toTime.getTime() - fromTime.getTime()) / 1000,
        intervalSeconds: timeSelection.range.intervalSeconds,
      }
    }
    return {
      durationSeconds: timeSelection.timeframe.durationSeconds,
      intervalSeconds: timeSelection.timeframe.intervalSeconds,
    }
  }, [timeSelection, fromTime, toTime])
  // Get data from context (batched fetch)
  const { series, loading, error } = useMetricData(widgetId)

  // Get metadata for the configured metric
  const metadata = useMemo(
    () => (config.metricName ? getMetricMetadata(config.metricName) : null),
    [config.metricName]
  )

  // Helper to get series key from labels (used for data keys in chart)
  const getSeriesKey = useCallback(
    (labels?: Record<string, string>) => {
      if (!labels) return 'value'
      const breakdownKey = config.breakdownAttribute || 'type'
      if (labels[breakdownKey]) {
        return labels.service
          ? `${labels.service}:${labels[breakdownKey]}`
          : labels[breakdownKey]
      }
      if (labels.type) {
        return labels.service ? `${labels.service}:${labels.type}` : labels.type
      }
      return labels.service || 'value'
    },
    [config.breakdownAttribute]
  )

  // Helper to get human-readable series label for display
  const getDisplayLabel = useCallback(
    (labels?: Record<string, string>) => {
      if (!metadata || !labels) return labels?.service || 'value'
      return getSeriesLabel(labels, metadata, config.breakdownAttribute)
    },
    [metadata, config.breakdownAttribute]
  )

  // Format Y-axis values using metadata
  const formatYAxis = useCallback(
    (value: number) => {
      if (!metadata) return value.toString()
      return formatMetricValue(value, metadata.unit)
    },
    [metadata]
  )

  // First, compute deduplicated series info (single source of truth for keys)
  const { seriesInfo, keyToLabelMap } = useMemo(() => {
    const seen = new Set<string>()
    const info: { key: string; label: string; labels?: Record<string, string> }[] = []
    const labelMap = new Map<string, string>()

    for (const s of series) {
      const key = getSeriesKey(s.labels)
      const label = getDisplayLabel(s.labels)
      labelMap.set(key, label)

      if (!seen.has(key)) {
        seen.add(key)
        info.push({ key, label, labels: s.labels })
      }
    }
    return { seriesInfo: info, keyToLabelMap: labelMap }
  }, [series, getSeriesKey, getDisplayLabel])

  // Generate chart data using the same keys as seriesInfo
  const chartData = useMemo(() => {
    if (series.length === 0) return []

    // Get the canonical keys from seriesInfo
    const seriesKeys = seriesInfo.map((s) => s.key)

    // Collect all unique timestamps from backend
    const allTimestamps = new Set<number>()
    for (const s of series) {
      for (const [timestamp] of s.datapoints) {
        allTimestamps.add(timestamp)
      }
    }

    // Sort timestamps
    const sortedTimestamps = Array.from(allTimestamps).sort((a, b) => a - b)

    // Build lookup map: seriesKey -> timestamp -> value
    // Aggregate values when multiple series have the same key (e.g., same service with different models)
    const dataMap = new Map<string, Map<number, number>>()

    for (const s of series) {
      const key = getSeriesKey(s.labels)

      if (!dataMap.has(key)) {
        dataMap.set(key, new Map<number, number>())
      }
      const timestampMap = dataMap.get(key)!

      for (const [timestamp, value] of s.datapoints) {
        // Aggregate by summing values at the same timestamp for the same key
        const existing = timestampMap.get(timestamp) || 0
        timestampMap.set(timestamp, existing + value)
      }
    }

    // Create chart data from backend timestamps
    return sortedTimestamps.map((timestamp) => {
      const point: Record<string, number | string> = {
        timestamp,
        time: formatTickLabel(timestamp, durationSeconds, intervalSeconds),
      }
      for (const key of seriesKeys) {
        const value = dataMap.get(key)?.get(timestamp)
        point[key] = value ?? 0
      }
      return point
    })
  }, [series, seriesInfo, durationSeconds, intervalSeconds, getSeriesKey])

  // Color map for series (use seriesInfo keys for consistency)
  const colorMap = useMemo(() => {
    const keys = [...seriesInfo.map((s) => s.key)].sort()
    const map = new Map<string, string>()
    keys.forEach((key, i) => {
      map.set(key, CHART_COLORS[i % CHART_COLORS.length])
    })
    return map
  }, [seriesInfo])

  // chartSeries is seriesInfo sorted by key for consistent stacking order
  // Sorting ensures bars stack in the same order regardless of backend response order
  const chartSeries = useMemo(() => {
    return [...seriesInfo]
      .sort((a, b) => a.key.localeCompare(b.key))
      .map((s) => ({ key: s.key, label: s.label }))
  }, [seriesInfo])

  // Custom tooltip formatter
  const tooltipFormatter = useCallback(
    (value: number | undefined, name: string | undefined): [string, string] => {
      if (value === undefined) return ['—', name || 'value']
      const displayLabel = name ? keyToLabelMap.get(name) || name : 'value'
      const formattedValue = metadata
        ? formatMetricValue(value, metadata.unit)
        : value.toString()
      return [formattedValue, displayLabel]
    },
    [metadata, keyToLabelMap]
  )

  // Get service name from config, series labels, or fall back to source display name
  const serviceName = useMemo(() => {
    if (config.service) return getServiceDisplayName(config.service)
    for (const s of series) {
      if (s.labels?.service) return getServiceDisplayName(s.labels.service)
    }
    // Fall back to source display name from metric metadata
    if (metadata?.source && metadata.source !== 'unknown') {
      return getSourceDisplayName(metadata.source)
    }
    return null
  }, [config.service, series, metadata])

  return (
    <Card className="border-0 shadow-none h-full flex flex-col">
      <CardHeader className="flex flex-row items-start justify-between space-y-0 p-3 pb-0">
        <div className="flex flex-col min-w-0 flex-1">
          <span className="text-xs text-muted-foreground truncate h-4">
            {serviceName || '\u00A0'}
          </span>
          <CardTitle className="text-base font-medium truncate">
            {title}
          </CardTitle>
        </div>
        <BarChartIcon className="h-4 w-4 text-muted-foreground shrink-0" />
      </CardHeader>
      <CardContent className="flex-1 p-3 pt-2">
        {loading ? (
          <div className="h-32 flex items-center justify-center text-muted-foreground text-sm">
            Loading...
          </div>
        ) : error ? (
          <div className="h-32 flex items-center justify-center text-destructive text-sm text-center px-2">
            {error}
          </div>
        ) : chartData.length === 0 || !config.metricName ? (
          <div className="h-32 flex items-center justify-center text-muted-foreground text-sm">
            {config.metricName ? 'No data' : 'Not configured'}
          </div>
        ) : (
          <div className="h-32">
            <MetricBarChart
              data={chartData}
              series={chartSeries}
              colorMap={colorMap}
              formatYAxis={formatYAxis}
              tooltipFormatter={tooltipFormatter}
              compact={true}
              stacked={config.chartStacked ?? true}
            />
          </div>
        )}
      </CardContent>
    </Card>
  )
}
