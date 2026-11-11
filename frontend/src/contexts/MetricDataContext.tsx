import { useMemo, useCallback, type ReactNode } from 'react'
import type { MetricQuery } from '@/lib/api'
import { browserTimeZone } from '@/lib/timeZone'
import { errorText } from '@/lib/errors'
import { queryKeys, useBatchMetricSeries } from '@/lib/queries'
import {
  timeRangeSource,
  timeSelectionDurationSeconds,
  timeSelectionIntervalSeconds,
} from '@/lib/timeUtils'
import { isAbsoluteTimeSelection, type DashboardWidget, type TimeSelection } from '@/types/dashboard'
import { useTelemetryStore } from '@/stores/telemetryStore'
import { useLiveInvalidate } from '@/hooks/useLiveInvalidate'
import { MetricDataContext, metricWidgetQueries, type MetricData } from '@/contexts/metricData'

interface MetricDataProviderProps {
  widgets: DashboardWidget[]
  timeSelection: TimeSelection
  children: ReactNode
}

const NOT_CONFIGURED: MetricData = { series: [], loading: false, refreshing: false, error: null }

/**
 * Loads the series of a dashboard's metric widgets in one batch request and
 * hands each widget its result (useMetricData). The batch is refreshed on
 * new metrics and as a relative window moves; widgets keep showing their
 * series while it reloads.
 */
export function MetricDataProvider({ widgets, timeSelection, children }: MetricDataProviderProps) {
  const queries = useMemo(() => metricWidgetQueries(widgets), [widgets])
  // Same content, same object: a widget moved or renamed does not refetch
  const queriesKey = JSON.stringify(queries)
  const intervalSeconds = timeSelectionIntervalSeconds(timeSelection)
  // Buckets follow the browser's clock: a day bar is a local day
  const timeZone = browserTimeZone()
  const params = useMemo(
    () => ({ queries: JSON.parse(queriesKey) as MetricQuery[], intervalSeconds, timeZone }),
    [queriesKey, intervalSeconds, timeZone],
  )
  const range = useMemo(() => timeRangeSource(timeSelection), [timeSelection])
  const batch = useBatchMetricSeries(params, range)

  const metricsUpdateCount = useTelemetryStore((state) => state.metricsUpdateCount)
  const durationSeconds = timeSelectionDurationSeconds(timeSelection)
  useLiveInvalidate({
    queryKey: queryKeys.metrics.batchSeries(params, range),
    // Absolute ranges show fixed historical data
    enabled: !isAbsoluteTimeSelection(timeSelection) && params.queries.length > 0,
    intervalMs: Math.min((durationSeconds * 1000) / 10, 60_000),
    updateCount: metricsUpdateCount,
  })

  const { data, error, isError, isFetching, refetch } = batch
  const results = useMemo(() => new Map((data?.results ?? []).map((r) => [r.id, r])), [data])
  const batchError = isError ? errorText(error, 'Failed to load the metric') : null

  const getMetricData = useCallback(
    (widgetId: string): MetricData => {
      if (!params.queries.some((q) => q.id === widgetId)) {
        return NOT_CONFIGURED
      }
      const result = results.get(widgetId)
      if (!result) {
        // Not in the shown batch: loading, or the batch failed
        return { series: [], loading: !batchError, refreshing: false, error: batchError }
      }
      return {
        series: result.success ? (result.series ?? []) : [],
        loading: false,
        refreshing: isFetching,
        error: result.success ? null : result.error || 'Failed to load the metric',
        stat: result.stat,
        metricType: result.metricType,
      }
    },
    [params.queries, results, batchError, isFetching],
  )

  const refreshAll = useCallback(() => {
    void refetch()
  }, [refetch])

  const value = useMemo(() => ({ getMetricData, refreshAll }), [getMetricData, refreshAll])

  return <MetricDataContext.Provider value={value}>{children}</MetricDataContext.Provider>
}
