import { useMemo } from 'react'
import { getMetricMetadata, isHistogramMetric, isHistogramStat } from '@/lib/metricMetadata'
import { useMetricNames } from '@/lib/queries'
import { WIDGET_TYPES, type WidgetConfig } from '@/types/dashboard'

// The settings of a metric widget while they are edited
export interface MetricFieldsValue {
  service: string
  // null: not chosen yet, the first stored metric is used
  metric: string | null
  breakdown: string
  breakdownValue: string
  stacked: boolean
  // Statistic of a histogram metric; '' follows the metric's default
  stat: string
}

export function metricFieldsFromConfig(config: WidgetConfig | undefined): MetricFieldsValue {
  return {
    service: config?.service ?? '',
    metric: config?.metricName ?? null,
    breakdown: config?.breakdownAttribute ?? '',
    breakdownValue: config?.breakdownValue ?? '',
    stacked: config?.chartStacked ?? true,
    stat: config?.stat ?? '',
  }
}

/** The complete widget configuration (an update replaces the stored one) */
export function metricWidgetConfig(widgetType: string, value: MetricFieldsValue, metric: string): WidgetConfig {
  return {
    service: value.service || undefined,
    metricName: metric,
    breakdownAttribute: value.breakdown || undefined,
    breakdownValue: widgetType === WIDGET_TYPES.METRIC_VALUE && value.breakdown && value.breakdownValue
      ? value.breakdownValue
      : undefined,
    chartStacked: widgetType === WIDGET_TYPES.METRIC_CHART ? value.stacked : undefined,
    stat: isHistogramMetric(getMetricMetadata(metric)) && isHistogramStat(value.stat) ? value.stat : undefined,
  }
}

/** The metric the fields name: the chosen one, or the first stored one */
export function useResolvedMetric(value: MetricFieldsValue): { metric: string; names: string[] } {
  const metricNames = useMetricNames(value.service || undefined)
  const names = useMemo(() => metricNames.data ?? [], [metricNames.data])
  return { metric: value.metric ?? names[0] ?? '', names }
}
