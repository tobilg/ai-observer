import { useMemo } from 'react'
import { Card } from '@/components/ui/card'
import { Gauge } from 'lucide-react'
import { useMetricData, useRefreshMetricData } from '@/contexts/metricData'
import { getMetricMetadata, formatMetricValue, getSourceDisplayName, statUnit } from '@/lib/metricMetadata'
import { useServiceNames } from '@/hooks/useServiceNames'
import { WidgetError, WidgetLoading } from './WidgetStatus'
import type { WidgetConfig } from '@/types/dashboard'

interface MetricValueWidgetProps {
  widgetId: string
  title: string
  config: WidgetConfig
}

export function MetricValueWidget({
  widgetId,
  title,
  config,
}: MetricValueWidgetProps) {
  // Get data from context (batched fetch)
  const { series, loading, refreshing, error, stat } = useMetricData(widgetId)
  const refreshAll = useRefreshMetricData()
  const { displayName } = useServiceNames()

  // Get metadata for the configured metric (for display formatting only)
  const metadata = useMemo(
    () => (config.metricName ? getMetricMetadata(config.metricName) : null),
    [config.metricName]
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

  // Get breakdown display value from metadata
  const breakdownDisplayValue = useMemo(() => {
    if (!config.breakdownAttribute || !config.breakdownValue || !metadata) return null
    const breakdown = metadata.breakdowns?.find(
      (b) => b.attributeKey === config.breakdownAttribute
    )
    return breakdown?.knownValues?.[config.breakdownValue] || config.breakdownValue
  }, [config.breakdownAttribute, config.breakdownValue, metadata])

  // The server merges all points (filtered to the breakdown value) into one
  // series of one point: [0, value]. No points in the range: no series; no
  // value (an average without values): null.
  const value = series[0]?.datapoints[0]?.[1] ?? null

  // Format value using metadata unit
  const formattedValue = useMemo(() => {
    if (value === null) return '—'
    if (metadata) {
      return formatMetricValue(value, statUnit(metadata.unit, stat))
    }
    // Fallback formatting
    if (Math.abs(value) >= 1000000) return `${(value / 1000000).toFixed(1)}M`
    if (Math.abs(value) >= 1000) return `${(value / 1000).toFixed(1)}K`
    return Number.isInteger(value) ? value.toString() : value.toFixed(2)
  }, [value, metadata, stat])

  return (
    <Card className="@container border-0 shadow-none h-full">
      <div className="h-full flex flex-col p-2 @[140px]:p-3">
        {/* Header */}
        <div className="flex flex-row items-start justify-between">
          <div className="flex flex-col min-w-0 flex-1">
            <span className="text-xs text-muted-foreground truncate">
              {serviceName || '\u00A0'}
            </span>
            <span className="flex items-center gap-1 truncate">
              <span className="text-sm @[140px]:text-base font-medium truncate">
                {title}
              </span>
              {(breakdownDisplayValue || stat) && (
                <span className="text-xs text-muted-foreground truncate mt-0.5">
                  ({[breakdownDisplayValue, stat].filter(Boolean).join(', ')})
                </span>
              )}
            </span>
          </div>
          <Gauge className="h-4 w-4 text-muted-foreground flex-shrink-0" aria-hidden="true" />
        </div>
        {/* Value - centered in remaining space */}
        <div className="flex-1 flex flex-col justify-center">
          {!config.metricName ? (
            <div className="text-sm text-muted-foreground">Not configured</div>
          ) : loading ? (
            <WidgetLoading className="h-10" />
          ) : error ? (
            <WidgetError message={error} onRetry={refreshAll} retrying={refreshing} />
          ) : (
            <div className="text-2xl @[140px]:text-4xl font-bold tabular-nums">{formattedValue}</div>
          )}
          {/* Spacer matching subtext height in StatsWidget */}
          <div className="h-4" />
        </div>
      </div>
    </Card>
  )
}
