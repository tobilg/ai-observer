import {
  createContext,
  useContext,
  useEffect,
  useState,
  useMemo,
  useCallback,
  useRef,
  type ReactNode,
} from 'react'
import { api } from '@/lib/api'
import { useDashboardStore } from '@/stores/dashboardStore'
import { WIDGET_TYPES, isAbsoluteTimeSelection } from '@/types/dashboard'
import type { TimeSeries } from '@/types/metrics'
import { useTelemetryStore } from '@/stores/telemetryStore'

interface MetricData {
  series: TimeSeries[]
  loading: boolean
  error: string | null
}

interface MetricDataContextValue {
  getMetricData: (widgetId: string) => MetricData
  refreshAll: () => void
}

const MetricDataContext = createContext<MetricDataContextValue | null>(null)

export function useMetricData(widgetId: string): MetricData {
  const context = useContext(MetricDataContext)
  if (!context) {
    throw new Error('useMetricData must be used within MetricDataProvider')
  }
  return context.getMetricData(widgetId)
}

interface MetricDataProviderProps {
  children: ReactNode
}

export function MetricDataProvider({ children }: MetricDataProviderProps) {
  const { widgets, timeSelection, fromTime, toTime, intervalSeconds, isAbsoluteRange } = useDashboardStore()
  const metricsUpdateCount = useTelemetryStore((state) => state.metricsUpdateCount)
  const prevMetricsCountRef = useRef(0)

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

  // Auto-refresh based on timeframe (disabled for absolute ranges)
  useEffect(() => {
    // Skip auto-refresh for absolute date ranges (static historical data)
    if (isAbsoluteRange) {
      return
    }

    const durationSeconds = isAbsoluteTimeSelection(timeSelection)
      ? (toTime.getTime() - fromTime.getTime()) / 1000
      : timeSelection.timeframe.durationSeconds

    const refreshMs = Math.min((durationSeconds * 1000) / 10, 60000)
    const interval = setInterval(() => {
      setRefreshTrigger((prev) => prev + 1)
    }, refreshMs)
    return () => clearInterval(interval)
  }, [timeSelection, fromTime, toTime, isAbsoluteRange])

  // Refresh when new metrics arrive via WebSocket (disabled for absolute ranges)
  useEffect(() => {
    // Skip WebSocket refresh for absolute date ranges
    if (isAbsoluteRange) {
      return
    }

    if (metricsUpdateCount > prevMetricsCountRef.current) {
      const timer = setTimeout(() => {
        setRefreshTrigger((prev) => prev + 1)
      }, 500)
      prevMetricsCountRef.current = metricsUpdateCount
      return () => clearTimeout(timer)
    }
  }, [metricsUpdateCount, isAbsoluteRange])

  // Fetch batch data when queries or time selection change
  useEffect(() => {
    if (queries.length === 0) {
      setResults(new Map())
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
        if (error instanceof Error && error.name === 'AbortError') {
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
  }, [queries, timeSelection, fromTime, toTime, intervalSeconds, isAbsoluteRange, refreshTrigger])

  const getMetricData = useCallback(
    (widgetId: string): MetricData => {
      const data = results.get(widgetId)
      if (data) {
        return data
      }
      // Return loading state if not yet fetched
      return { series: [], loading: loading, error: null }
    },
    [results, loading]
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
