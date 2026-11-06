import {
  useEffect,
  useState,
  useMemo,
  useCallback,
  type ReactNode,
} from 'react'
import { api } from '@/lib/api'
import { isAbortError } from '@/lib/errors'
import { useDashboardStore } from '@/stores/dashboardStore'
import { useShallow } from 'zustand/react/shallow'
import { WIDGET_TYPES, isAbsoluteTimeSelection } from '@/types/dashboard'
import { useTelemetryStore } from '@/stores/telemetryStore'
import { useLiveRefresh } from '@/hooks/useLiveRefresh'
import { MetricDataContext, type MetricData } from '@/contexts/metricData'

interface MetricDataProviderProps {
  children: ReactNode
}

const EMPTY_RESULTS = new Map<string, MetricData>()

export function MetricDataProvider({ children }: MetricDataProviderProps) {
  const {
    widgets,
    timeSelection,
    fromTime,
    toTime,
    intervalSeconds,
    isAbsoluteRange,
  } = useDashboardStore(
    useShallow((s) => ({
      widgets: s.widgets,
      timeSelection: s.timeSelection,
      fromTime: s.fromTime,
      toTime: s.toTime,
      intervalSeconds: s.intervalSeconds,
      isAbsoluteRange: s.isAbsoluteRange,
    }))
  )
  const metricsUpdateCount = useTelemetryStore((state) => state.metricsUpdateCount)

  // Store results by widget ID
  const [results, setResults] = useState<Map<string, MetricData>>(new Map())
  const [loading, setLoading] = useState(false)
  const [refreshTrigger, setRefreshTrigger] = useState(0)

  // Extract metric widgets that need data
  const metricWidgets = useMemo(() => {
    return widgets.filter(
      (w) =>
        (w.widgetType === WIDGET_TYPES.METRIC_VALUE ||
          w.widgetType === WIDGET_TYPES.METRIC_CHART) &&
        w.config?.metricName
    )
  }, [widgets])

  // Build queries from widgets
  const queries = useMemo(() => {
    return metricWidgets.map((widget) => ({
      id: widget.id,
      name: widget.config.metricName!,
      service: widget.config.service,
      aggregate: widget.widgetType === WIDGET_TYPES.METRIC_VALUE,
    }))
  }, [metricWidgets])

  // Refresh on new metrics and as the time window moves (not for absolute
  // ranges, which show fixed historical data)
  const durationSeconds = isAbsoluteTimeSelection(timeSelection)
    ? (toTime.getTime() - fromTime.getTime()) / 1000
    : timeSelection.timeframe.durationSeconds
  const liveRefreshKey = useLiveRefresh({
    enabled: !isAbsoluteRange,
    intervalMs: Math.min((durationSeconds * 1000) / 10, 60000),
    updateCount: metricsUpdateCount,
  })

  // Fetch batch data when queries or time selection change
  useEffect(() => {
    if (queries.length === 0) {
      return
    }

    const controller = new AbortController()

    const fetchData = async () => {
      setLoading(true)

      // Compute time range based on selection type
      let fetchFrom: Date
      let fetchTo: Date

      if (isAbsoluteRange) {
        // Use fixed dates for absolute ranges
        fetchFrom = fromTime
        fetchTo = toTime
      } else {
        // Compute fresh time range for relative ranges
        const now = new Date()
        const durationSeconds = isAbsoluteTimeSelection(timeSelection)
          ? (toTime.getTime() - fromTime.getTime()) / 1000
          : timeSelection.timeframe.durationSeconds
        fetchFrom = new Date(now.getTime() - durationSeconds * 1000)
        fetchTo = now
      }

      try {
        const response = await api.getBatchMetricSeries(
          {
            from: fetchFrom.toISOString(),
            to: fetchTo.toISOString(),
            intervalSeconds,
            queries,
          },
          { signal: controller.signal }
        )

        const newResults = new Map<string, MetricData>()
        for (const result of response.results) {
          newResults.set(result.id, {
            series: result.success ? result.series || [] : [],
            loading: false,
            error: result.success ? null : result.error || 'Unknown error',
          })
        }
        setResults(newResults)
      } catch (error) {
        if (isAbortError(error)) {
          return
        }
        console.error('Failed to fetch batch metrics:', error)
        // Set error state for all widgets
        const errorResults = new Map<string, MetricData>()
        for (const query of queries) {
          errorResults.set(query.id, {
            series: [],
            loading: false,
            error: error instanceof Error ? error.message : 'Failed to fetch',
          })
        }
        setResults(errorResults)
      } finally {
        setLoading(false)
      }
    }

    fetchData()

    return () => controller.abort()
  }, [queries, timeSelection, fromTime, toTime, intervalSeconds, isAbsoluteRange, refreshTrigger, liveRefreshKey])

  const getMetricData = useCallback(
    (widgetId: string): MetricData => {
      const activeResults = queries.length === 0 ? EMPTY_RESULTS : results
      const data = activeResults.get(widgetId)
      if (data) {
        return data
      }
      // Return loading state if not yet fetched
      return { series: [], loading: loading, error: null }
    },
    [queries.length, results, loading]
  )

  const refreshAll = useCallback(() => {
    setRefreshTrigger((prev) => prev + 1)
  }, [])

  const value = useMemo(
    () => ({
      getMetricData,
      refreshAll,
    }),
    [getMetricData, refreshAll]
  )

  return (
    <MetricDataContext.Provider value={value}>
      {children}
    </MetricDataContext.Provider>
  )
}
