import { useMemo } from 'react'
import { Card } from '@/components/ui/card'
import { Gauge } from 'lucide-react'
import { useMetricData } from '@/contexts/metricData'
import { getMetricMetadata, formatMetricValue, getSourceDisplayName, getServiceDisplayName } from '@/lib/metricMetadata'
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
  const { series, loading, error } = useMetricData(widgetId)

  // Get metadata for the configured metric (for display formatting only)
  const metadata = useMemo(
    () => (config.metricName ? getMetricMetadata(config.metricName) : null),
    [config.metricName]
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

  // Get breakdown display value from metadata
  const breakdownDisplayValue = useMemo(() => {
    if (!config.breakdownAttribute || !config.breakdownValue || !metadata) return null
    const breakdown = metadata.breakdowns?.find(
      (b) => b.attributeKey === config.breakdownAttribute
    )
    return breakdown?.knownValues?.[config.breakdownValue] || config.breakdownValue
  }, [config.breakdownAttribute, config.breakdownValue, metadata])

  // Calculate total value from all series
  // If breakdownAttribute and breakdownValue are set, filter to only matching series
  // Each series has a single datapoint with [0, aggregated_value]
  const value = useMemo(() => {
    if (series.length === 0) return null

    // If both breakdownAttribute and breakdownValue are set, filter series
    if (config.breakdownAttribute && config.breakdownValue) {
      const filteredSeries = series.filter((s) => {
        // Check if series labels match the breakdown criteria
        const breakdownKey = config.breakdownAttribute || 'type'
        return s.labels?.[breakdownKey] === config.breakdownValue
      })

      if (filteredSeries.length === 0) return null
      return filteredSeries.reduce((acc, s) => acc + (s.datapoints[0]?.[1] || 0), 0)
    }

    // Default: sum all series
    return series.reduce((acc, s) => acc + (s.datapoints[0]?.[1] || 0), 0)
  }, [series, config.breakdownAttribute, config.breakdownValue])

  // Format value using metadata unit
  const formattedValue = useMemo(() => {
    if (value === null) return '—'
    if (metadata) {
      return formatMetricValue(value, metadata.unit)
    }
    // Fallback formatting
    if (Math.abs(value) >= 1000000) return `${(value / 1000000).toFixed(1)}M`
    if (Math.abs(value) >= 1000) return `${(value / 1000).toFixed(1)}K`
    return Number.isInteger(value) ? value.toString() : value.toFixed(2)
  }, [value, metadata])

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
              {breakdownDisplayValue && (
                <span className="text-xs text-muted-foreground truncate mt-0.5">
                  ({breakdownDisplayValue})
                </span>
              )}
            </span>
          </div>
          <Gauge className="h-4 w-4 text-muted-foreground flex-shrink-0" />
        </div>
        {/* Value - centered in remaining space */}
        <div className="flex-1 flex flex-col justify-center">
          {loading ? (
            <div className="text-2xl @[140px]:text-4xl font-bold text-muted-foreground">...</div>
          ) : error ? (
            <div className="text-sm text-destructive">
              {error}
            </div>
          ) : !config.metricName ? (
            <div className="text-sm text-muted-foreground">Not configured</div>
          ) : (
            <div className="text-2xl @[140px]:text-4xl font-bold">{formattedValue}</div>
          )}
          {/* Spacer matching subtext height in StatsWidget */}
          <div className="h-4" />
        </div>
      </div>
    </Card>
  )
}
