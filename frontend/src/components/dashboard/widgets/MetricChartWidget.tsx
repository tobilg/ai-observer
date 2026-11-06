import { useMemo, useCallback } from 'react'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { BarChart as BarChartIcon } from 'lucide-react'
import type { WidgetConfig, TimeSelection } from '@/types/dashboard'
import { isAbsoluteTimeSelection } from '@/types/dashboard'
import { MetricBarChart, buildChartData, formatTickLabel } from '@/components/charts'
import { useMetricData } from '@/contexts/metricData'
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

  const { data: chartData, series: chartSeries, colorMap } = useMemo(
    () =>
      buildChartData(series, getSeriesKey, getDisplayLabel, (timestamp) =>
        formatTickLabel(timestamp, durationSeconds, intervalSeconds)
      ),
    [series, getSeriesKey, getDisplayLabel, durationSeconds, intervalSeconds]
  )
  const keyToLabelMap = useMemo(
    () => new Map(chartSeries.map((s) => [s.key, s.label])),
    [chartSeries]
  )

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
  const serviceName = (() => {
    if (config.service) return getServiceDisplayName(config.service)
    for (const s of series) {
      if (s.labels?.service) return getServiceDisplayName(s.labels.service)
    }
    // Fall back to source display name from metric metadata
    if (metadata?.source && metadata.source !== 'unknown') {
      return getSourceDisplayName(metadata.source)
    }
    return null
  })()

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
