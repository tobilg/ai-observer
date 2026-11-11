import { useMemo, useCallback } from 'react'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { BarChart as BarChartIcon } from 'lucide-react'
import type { WidgetConfig, TimeSelection } from '@/types/dashboard'
import { MetricBarChart, buildChartData, formatTickLabel } from '@/components/charts'
import { useMetricData, useRefreshMetricData } from '@/contexts/metricData'
import {
  getMetricMetadata,
  getSeriesLabel,
  formatMetricValue,
  getSourceDisplayName,
  isAdditiveStat,
  statUnit,
} from '@/lib/metricMetadata'
import { timeSelectionDurationSeconds, timeSelectionIntervalSeconds } from '@/lib/timeUtils'
import { useServiceNames } from '@/hooks/useServiceNames'
import { WidgetError, WidgetLoading } from './WidgetStatus'

interface MetricChartWidgetProps {
  widgetId: string
  title: string
  config: WidgetConfig
  timeSelection: TimeSelection
}

export function MetricChartWidget({
  widgetId,
  title,
  config,
  timeSelection,
}: MetricChartWidgetProps) {
  const durationSeconds = timeSelectionDurationSeconds(timeSelection)
  const intervalSeconds = timeSelectionIntervalSeconds(timeSelection)
  // Get data from context (batched fetch)
  const { series, loading, refreshing, error, stat } = useMetricData(widgetId)
  const refreshAll = useRefreshMetricData()
  const { displayName } = useServiceNames()

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

  // Format Y-axis values using metadata; a histogram's count is a number
  const unit = useMemo(() => (metadata ? statUnit(metadata.unit, stat) : null), [metadata, stat])
  const formatYAxis = useCallback(
    (value: number) => {
      if (!unit) return value.toString()
      return formatMetricValue(value, unit)
    },
    [unit]
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
      const formattedValue = unit ? formatMetricValue(value, unit) : value.toString()
      return [formattedValue, displayLabel]
    },
    [unit, keyToLabelMap]
  )

  // Get service name from config, series labels, or fall back to source display name
  const serviceName = (() => {
    if (config.service) return displayName(config.service)
    for (const s of series) {
      if (s.labels?.service) return displayName(s.labels.service)
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
            {stat && <span className="ml-1 text-xs font-normal text-muted-foreground">({stat})</span>}
          </CardTitle>
        </div>
        <BarChartIcon className="h-4 w-4 text-muted-foreground shrink-0" aria-hidden="true" />
      </CardHeader>
      <CardContent className="flex-1 p-3 pt-2">
        {!config.metricName ? (
          <div className="h-32 flex items-center justify-center text-muted-foreground text-sm">Not configured</div>
        ) : loading ? (
          <WidgetLoading className="h-32" />
        ) : error ? (
          <div className="h-32 flex items-center justify-center px-2">
            <WidgetError message={error} onRetry={refreshAll} retrying={refreshing} />
          </div>
        ) : chartData.length === 0 ? (
          <div className="h-32 flex items-center justify-center text-muted-foreground text-sm">No data in the selected range</div>
        ) : (
          <div className="h-32">
            <MetricBarChart
              data={chartData}
              series={chartSeries}
              colorMap={colorMap}
              formatYAxis={formatYAxis}
              tooltipFormatter={tooltipFormatter}
              compact={true}
              // Averages and percentiles do not add up
              stacked={isAdditiveStat(stat) && (config.chartStacked ?? true)}
            />
          </div>
        )}
      </CardContent>
    </Card>
  )
}
