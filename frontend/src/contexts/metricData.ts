import { createContext, useContext } from 'react'
import type { MetricQuery } from '@/lib/api'
import { getMetricMetadata, statParam } from '@/lib/metricMetadata'
import type { TimeSeries } from '@/types/metrics'
import { WIDGET_TYPES, type DashboardWidget } from '@/types/dashboard'

export interface MetricData {
  series: TimeSeries[]
  // No data for the widget yet: the first load, or the widget was just added
  loading: boolean
  // The shown series are being refreshed
  refreshing: boolean
  // Why the widget's series could not be loaded (the whole batch or this
  // widget's query failed)
  error: string | null
  // The statistic of a histogram metric's series, as the server computed
  // them (avg by default), and the type of the metric's points
  stat?: string
  metricType?: string
}

export interface MetricDataContextValue {
  getMetricData: (widgetId: string) => MetricData
  // Loads every widget's series again (Retry)
  refreshAll: () => void
}

export const MetricDataContext = createContext<MetricDataContextValue | null>(null)

export function useMetricData(widgetId: string): MetricData {
  const context = useContext(MetricDataContext)
  if (!context) {
    throw new Error('useMetricData must be used within MetricDataProvider')
  }
  return context.getMetricData(widgetId)
}

export function useRefreshMetricData(): () => void {
  const context = useContext(MetricDataContext)
  if (!context) {
    throw new Error('useRefreshMetricData must be used within MetricDataProvider')
  }
  return context.refreshAll
}

// The series queries of the metric widgets, sorted by widget ID so moving
// widgets around does not change the request (or its cache entry)
export function metricWidgetQueries(widgets: DashboardWidget[]): MetricQuery[] {
  return widgets
    .filter(
      (w) =>
        (w.widgetType === WIDGET_TYPES.METRIC_VALUE || w.widgetType === WIDGET_TYPES.METRIC_CHART) &&
        w.config?.metricName,
    )
    .map(metricWidgetQuery)
    .sort((a, b) => a.id.localeCompare(b.id))
}

/**
 * The series query of a metric widget. A chart shows a series per value of
 * its breakdown attribute (groupBy); a value widget shows one value of all
 * points (merge), which the server computes, so averages and percentiles
 * are right. A breakdown value filters the points. Histograms are asked
 * for the configured stat; without one the server's average, except for
 * token counts, whose sum is the usage (defaultHistogramStat). Stored
 * configs of earlier versions give the same queries.
 */
export function metricWidgetQuery(widget: DashboardWidget): MetricQuery {
  const { config } = widget
  const isValue = widget.widgetType === WIDGET_TYPES.METRIC_VALUE
  const query: MetricQuery = {
    id: widget.id,
    name: config.metricName!,
    service: config.service || undefined,
    aggregate: isValue,
  }
  if (isValue) {
    query.merge = true
  } else if (config.breakdownAttribute) {
    query.groupBy = [config.breakdownAttribute]
  }
  if (config.breakdownAttribute && config.breakdownValue) {
    query.filters = { [config.breakdownAttribute]: config.breakdownValue }
  }
  const stat = statParam(getMetricMetadata(query.name), config.stat)
  if (stat) query.stat = stat
  return query
}
