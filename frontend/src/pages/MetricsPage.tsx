import { useEffect, useState, useRef, useMemo, useCallback } from 'react'
import { useSearchParams } from 'react-router-dom'
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from '@/components/ui/card'
import { Select } from '@/components/ui/select'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { DateRangePicker } from '@/components/ui/date-range-picker'
import { Layers, BarChart3 } from 'lucide-react'
import { api } from '@/lib/api'
import type { TimeSeries } from '@/types/metrics'
import { MetricBarChart, CHART_COLORS } from '@/components/charts'
import { useTelemetryStore } from '@/stores/telemetryStore'
import {
  getMetricMetadata,
  getSeriesLabel,
  formatMetricValue,
  getSourceDisplayName,
  getServiceDisplayName,
  METRIC_CATALOG,
  type SourceTool,
} from '@/lib/metricMetadata'
import { toast } from 'sonner'
import {
  TIMEFRAME_OPTIONS,
  isAbsoluteTimeSelection,
  getTimeSelectionLabel,
  type TimeSelection,
} from '@/types/dashboard'
import { formatIntervalSeconds } from '@/lib/utils'
import { getLocalStorageValue } from '@/hooks/useLocalStorage'
import { calculateInterval, calculateTickInterval } from '@/lib/timeUtils'

const DEFAULT_TIMEFRAME = '15m'
const TIME_SELECTION_STORAGE_KEY = 'ai-observer-metrics-timeselection'

export function MetricsPage() {
  const [searchParams, setSearchParams] = useSearchParams()
  const [metricNames, setMetricNames] = useState<string[]>([])
  const [selectedMetric, setSelectedMetric] = useState<string>('')
  const [series, setSeries] = useState<TimeSeries[]>([])
  const [services, setServices] = useState<string[]>([])
  const [selectedService, setSelectedService] = useState<string>('')
  const [loading, setLoading] = useState(false)
  const [lastUpdate, setLastUpdate] = useState<Date>(new Date())
  const [stacked, setStacked] = useState(true)

  // Get time selection from URL (for shareable links), then localStorage, then default
  const getInitialTimeSelection = (): TimeSelection => {
    // Check for absolute date range in URL
    const fromParam = searchParams.get('from')
    const toParam = searchParams.get('to')
    if (fromParam && toParam) {
      const from = new Date(fromParam)
      const to = new Date(toParam)
      if (!isNaN(from.getTime()) && !isNaN(to.getTime())) {
        const intervalSeconds = calculateInterval(from, to)
        const rangeDays = (to.getTime() - from.getTime()) / (1000 * 60 * 60 * 24)
        const bucketCount = Math.ceil((rangeDays * 86400) / intervalSeconds)
        return {
          type: 'absolute',
          range: {
            from,
            to,
            intervalSeconds,
            tickInterval: calculateTickInterval(bucketCount),
          },
        }
      }
    }

    // Check for relative timeframe in URL or localStorage
    const storedSelection = getLocalStorageValue<{ type: string; timeframeValue: string } | null>(TIME_SELECTION_STORAGE_KEY, null)
    const timeframeParam = searchParams.get('timeframe')
      || storedSelection?.timeframeValue
      || DEFAULT_TIMEFRAME
    const timeframe = TIMEFRAME_OPTIONS.find((t) => t.value === timeframeParam)
      || TIMEFRAME_OPTIONS.find((t) => t.value === DEFAULT_TIMEFRAME)!
    return { type: 'relative', timeframe }
  }

  const [timeSelection, setTimeSelectionState] = useState<TimeSelection>(getInitialTimeSelection)

  const isAbsoluteRange = isAbsoluteTimeSelection(timeSelection)

  const handleTimeSelectionChange = (selection: TimeSelection) => {
    setTimeSelectionState(selection)

    // Update URL params
    const newParams = new URLSearchParams(searchParams)

    if (isAbsoluteTimeSelection(selection)) {
      // Absolute range: set from/to, remove timeframe
      newParams.set('from', selection.range.from.toISOString())
      newParams.set('to', selection.range.to.toISOString())
      newParams.delete('timeframe')
    } else {
      // Relative range: set timeframe, remove from/to
      newParams.delete('from')
      newParams.delete('to')
      if (selection.timeframe.value === DEFAULT_TIMEFRAME) {
        newParams.delete('timeframe')
      } else {
        newParams.set('timeframe', selection.timeframe.value)
      }
      // Persist to localStorage
      try {
        localStorage.setItem(
          TIME_SELECTION_STORAGE_KEY,
          JSON.stringify({ type: 'relative', timeframeValue: selection.timeframe.value })
        )
      } catch {
        // Ignore storage errors
      }
    }

    setSearchParams(newParams)
  }

  // Derive time range values from selection
  const { fromTime, toTime, intervalSeconds, tickInterval } = useMemo(() => {
    if (isAbsoluteTimeSelection(timeSelection)) {
      return {
        fromTime: timeSelection.range.from,
        toTime: timeSelection.range.to,
        intervalSeconds: timeSelection.range.intervalSeconds,
        tickInterval: timeSelection.range.tickInterval,
      }
    }
    const now = new Date()
    const from = new Date(now.getTime() - timeSelection.timeframe.durationSeconds * 1000)
    return {
      fromTime: from,
      toTime: now,
      intervalSeconds: timeSelection.timeframe.intervalSeconds,
      tickInterval: timeSelection.timeframe.tickInterval,
    }
  }, [timeSelection])

  // Real-time metrics from WebSocket
  const recentMetrics = useTelemetryStore((state) => state.recentMetrics)
  const prevMetricsCountRef = useRef(0)

  // Map service name to source tool
  const getSourceFromService = useCallback((service: string): SourceTool | null => {
    const normalized = service.toLowerCase().replace(/-/g, '_')
    if (normalized.includes('claude')) return 'claude_code'
    if (normalized.includes('gemini')) return 'gemini_cli'
    if (normalized.includes('codex')) return 'codex_cli_rs'
    return null
  }, [])

  // Fetch services on mount
  useEffect(() => {
    const fetchServices = async () => {
      try {
        const servicesData = await api.getServices()
        setServices(servicesData.services ?? [])
      } catch (err) {
        console.error('Failed to fetch services:', err)
        toast.error('Failed to fetch services')
      }
    }
    fetchServices()
  }, [])

  // Fetch metric names when service selection changes
  useEffect(() => {
    const abortController = new AbortController()

    const fetchMetricNames = async () => {
      try {
        const namesData = await api.getMetricNames(selectedService || undefined, { signal: abortController.signal })
        const names = namesData.names ?? []
        setMetricNames(names)
      } catch (err) {
        if (err instanceof Error && err.name === 'AbortError') {
          return // Ignore abort errors
        }
        console.error('Failed to fetch metric names:', err)
        toast.error('Failed to fetch metric names')
      }
    }
    fetchMetricNames()

    return () => abortController.abort()
  }, [selectedService])

  // Reset selected metric when service changes and current metric is not valid
  useEffect(() => {
    if (!selectedService) return // No filtering when no service selected

    const filterSource = getSourceFromService(selectedService)
    if (!filterSource) return

    // Check if current metric belongs to the selected service's source
    if (selectedMetric) {
      const meta = getMetricMetadata(selectedMetric)
      if (meta.source !== filterSource && meta.source !== 'unknown') {
        setSelectedMetric('')
      }
    }
  }, [selectedService, selectedMetric, getSourceFromService])

  // Auto-refresh at the X-axis tick interval rate (disabled for absolute ranges)
  useEffect(() => {
    // Skip auto-refresh for absolute date ranges (static historical data)
    if (isAbsoluteRange) {
      return
    }

    const refreshMs = intervalSeconds * 1000 * (tickInterval + 1)
    const interval = setInterval(() => {
      setLastUpdate(new Date())
    }, refreshMs)
    return () => clearInterval(interval)
  }, [intervalSeconds, tickInterval, isAbsoluteRange])

  // Also refresh when new metrics arrive via WebSocket (disabled for absolute ranges)
  useEffect(() => {
    // Skip WebSocket refresh for absolute date ranges
    if (isAbsoluteRange) {
      return
    }

    if (recentMetrics.length > prevMetricsCountRef.current) {
      const timer = setTimeout(() => {
        setLastUpdate(new Date())
      }, 500)
      prevMetricsCountRef.current = recentMetrics.length
      return () => clearTimeout(timer)
    }
  }, [recentMetrics.length, isAbsoluteRange])

  useEffect(() => {
    if (!selectedMetric) {
      setSeries([])
      return
    }

    const abortController = new AbortController()

    const fetchSeries = async () => {
      // Only show loading on initial load, not on refreshes
      const isInitialLoad = series.length === 0
      if (isInitialLoad) {
        setLoading(true)
      }
      try {
        // Use computed time range (fromTime/toTime for absolute, fresh calculation for relative)
        let fetchFrom: Date
        let fetchTo: Date

        if (isAbsoluteRange) {
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

        const data = await api.getMetricSeries({
          name: selectedMetric,
          service: selectedService || undefined,
          from: fetchFrom.toISOString(),
          to: fetchTo.toISOString(),
          intervalSeconds,
        }, { signal: abortController.signal })
        setSeries(data.series ?? [])
      } catch (err) {
        if (err instanceof Error && err.name === 'AbortError') {
          return // Ignore abort errors
        }
        console.error('Failed to fetch metric series:', err)
        toast.error('Failed to fetch metric series')
      } finally {
        if (!abortController.signal.aborted && isInitialLoad) {
          setLoading(false)
        }
      }
    }
    fetchSeries()

    return () => abortController.abort()
  }, [selectedMetric, selectedService, lastUpdate, timeSelection, fromTime, toTime, intervalSeconds, isAbsoluteRange])

  // Get metadata for selected metric
  const metadata = useMemo(
    () => (selectedMetric ? getMetricMetadata(selectedMetric) : null),
    [selectedMetric]
  )

  // Group metric names by source - filter by service if selected
  const groupedMetrics = useMemo(() => {
    const groups: Record<SourceTool | 'other', string[]> = {
      claude_code: [],
      gemini_cli: [],
      codex_cli_rs: [],
      unknown: [],
      other: [],
    }

    // Determine which source to filter by based on selected service
    const filterSource = selectedService ? getSourceFromService(selectedService) : null

    // Start with catalog metrics (filtered by source if service selected)
    const allMetricNames = new Set<string>()
    for (const m of METRIC_CATALOG) {
      if (!filterSource || m.source === filterSource) {
        allMetricNames.add(m.name)
      }
    }

    // Add metrics from the database (already filtered by service via API)
    for (const name of metricNames) {
      allMetricNames.add(name)
    }

    // Group by source
    for (const name of allMetricNames) {
      const meta = getMetricMetadata(name)
      if (meta.source === 'unknown') {
        groups.other.push(name)
      } else {
        groups[meta.source].push(name)
      }
    }

    // Sort each group alphabetically
    for (const key of Object.keys(groups) as (SourceTool | 'other')[]) {
      groups[key].sort()
    }

    return groups
  }, [metricNames, selectedService, getSourceFromService])

  // Helper to get series key from labels (prefer type if available, else service)
  const getSeriesKey = useCallback((labels?: Record<string, string>) => {
    if (labels?.type) {
      return labels.service ? `${labels.service}:${labels.type}` : labels.type
    }
    return labels?.service || 'value'
  }, [])

  // Helper to get human-readable series label
  const getDisplayLabel = useCallback((labels?: Record<string, string>) => {
    if (!metadata || !labels) return labels?.service || 'value'
    return getSeriesLabel(labels, metadata)
  }, [metadata])

  // Format Y-axis values
  const formatYAxis = useCallback((value: number) => {
    if (!metadata) return value.toString()
    return formatMetricValue(value, metadata.unit)
  }, [metadata])

  // Custom tooltip formatter
  const tooltipFormatter = useCallback((value: number | undefined, name: string | undefined): [string, string] => {
    if (value === undefined) return ['—', name || 'value']
    // Find the series to get its labels
    const seriesItem = series.find(s => getSeriesKey(s.labels) === name)
    const displayLabel = seriesItem ? getDisplayLabel(seriesItem.labels) : (name || 'value')
    const formattedValue = metadata ? formatMetricValue(value, metadata.unit) : value.toString()
    return [formattedValue, displayLabel]
  }, [metadata, series, getSeriesKey, getDisplayLabel])

  // Calculate duration in seconds for formatting
  const durationSeconds = useMemo(() => {
    return (toTime.getTime() - fromTime.getTime()) / 1000
  }, [fromTime, toTime])

  // Format timestamp for X-axis based on duration and interval
  const formatTickLabel = useCallback((timestamp: number): string => {
    const date = new Date(timestamp)
    const DAY_SECONDS = 24 * 60 * 60

    // For sub-daily intervals, always include time to distinguish bars
    const hasSubDailyInterval = intervalSeconds < DAY_SECONDS

    if (durationSeconds < DAY_SECONDS) {
      // Less than 24h: time only
      return date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
    } else if (durationSeconds <= 7 * DAY_SECONDS || hasSubDailyInterval) {
      // 24h to 7 days OR any range with sub-daily intervals: date + time + year
      return (
        date.toLocaleDateString([], { month: 'short', day: 'numeric', year: 'numeric' }) +
        ' ' +
        date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
      )
    } else {
      // More than 7 days with daily+ intervals: date + year only
      return date.toLocaleDateString([], { month: 'short', day: 'numeric', year: 'numeric' })
    }
  }, [durationSeconds, intervalSeconds])

  // Generate chart data directly from backend data (backend fills missing buckets)
  const sortedData = useMemo(() => {
    if (series.length === 0) return []

    // Collect all unique timestamps from backend
    const allTimestamps = new Set<number>()
    for (const s of series) {
      for (const [timestamp] of s.datapoints) {
        allTimestamps.add(timestamp)
      }
    }

    // Sort timestamps
    const sortedTimestamps = Array.from(allTimestamps).sort((a, b) => a - b)

    // Get all series keys
    const seriesKeys = series.map((s) => getSeriesKey(s.labels))

    // Build lookup map: seriesKey -> timestamp -> value
    const dataMap = new Map<string, Map<number, number>>()
    for (const s of series) {
      const key = getSeriesKey(s.labels)
      const timestampMap = new Map<number, number>()
      for (const [timestamp, value] of s.datapoints) {
        timestampMap.set(timestamp, value)
      }
      dataMap.set(key, timestampMap)
    }

    // Create chart data from backend timestamps
    return sortedTimestamps.map((timestamp) => {
      const point: Record<string, number | string> = {
        timestamp,
        time: formatTickLabel(timestamp),
      }
      for (const key of seriesKeys) {
        const value = dataMap.get(key)?.get(timestamp)
        point[key] = value ?? 0
      }
      return point
    })
  }, [series, getSeriesKey, formatTickLabel])

  // Create deterministic color map based on sorted series keys (memoized)
  const colorMap = useMemo(() => {
    const keys = series.map((s) => getSeriesKey(s.labels)).sort()
    const map = new Map<string, string>()
    keys.forEach((key, i) => {
      map.set(key, CHART_COLORS[i % CHART_COLORS.length])
    })
    return map
  }, [series, getSeriesKey])

  // Convert series to ChartSeries format for the chart component
  const chartSeries = useMemo(() => {
    return series.map((s) => ({
      key: getSeriesKey(s.labels),
      label: getDisplayLabel(s.labels),
    }))
  }, [series, getSeriesKey, getDisplayLabel])

  // Use time selection-specific tick interval for X-axis labels
  const xAxisTickInterval = tickInterval

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-3xl font-bold tracking-tight">Metrics</h1>
        <p className="text-muted-foreground">
          View and analyze metric time series data
        </p>
      </div>

      {/* Filters */}
      <Card>
        <CardContent className="pt-6">
          <div className="flex gap-4">
            <div className="flex-1">
              <label className="text-sm font-medium mb-2 block">Service</label>
              <Select
                value={selectedService}
                onChange={(e) => setSelectedService(e.target.value)}
              >
                <option value="">All Services</option>
                {services.map((s) => (
                  <option key={s} value={s}>
                    {getServiceDisplayName(s)}
                  </option>
                ))}
              </Select>
            </div>
            <div className="flex-1">
              <label className="text-sm font-medium mb-2 block">Metric</label>
              <Select
                value={selectedMetric}
                onChange={(e) => setSelectedMetric(e.target.value)}
              >
                <option value="">Select a metric</option>
                {groupedMetrics.claude_code.length > 0 && (
                  <optgroup label={getSourceDisplayName('claude_code')}>
                    {groupedMetrics.claude_code.map((name) => (
                      <option key={name} value={name}>{getMetricMetadata(name).displayName}</option>
                    ))}
                  </optgroup>
                )}
                {groupedMetrics.gemini_cli.length > 0 && (
                  <optgroup label={getSourceDisplayName('gemini_cli')}>
                    {groupedMetrics.gemini_cli.map((name) => (
                      <option key={name} value={name}>{getMetricMetadata(name).displayName}</option>
                    ))}
                  </optgroup>
                )}
                {groupedMetrics.codex_cli_rs.length > 0 && (
                  <optgroup label={getSourceDisplayName('codex_cli_rs')}>
                    {groupedMetrics.codex_cli_rs.map((name) => (
                      <option key={name} value={name}>{getMetricMetadata(name).displayName}</option>
                    ))}
                  </optgroup>
                )}
                {groupedMetrics.other.length > 0 && (
                  <optgroup label="Other">
                    {groupedMetrics.other.map((name) => (
                      <option key={name} value={name}>{name}</option>
                    ))}
                  </optgroup>
                )}
              </Select>
            </div>
            <div className="flex-1">
              <label className="text-sm font-medium mb-2 block">Timeframe</label>
              <DateRangePicker
                value={timeSelection}
                onChange={handleTimeSelectionChange}
              />
            </div>
          </div>
        </CardContent>
      </Card>

      {/* Chart */}
      <Card>
        <CardHeader>
          <div className="flex items-center justify-between">
            <div>
              <div className="flex items-center gap-2">
                <CardTitle>{metadata?.displayName || selectedMetric || 'Select a metric'}</CardTitle>
                {metadata && (
                  <Badge variant="outline" className="text-xs">
                    {metadata.unit.displayUnit}
                  </Badge>
                )}
              </div>
              <CardDescription>
                {metadata?.description || `${getTimeSelectionLabel(timeSelection)}, ${formatIntervalSeconds(intervalSeconds)} intervals`}
              </CardDescription>
              {metadata && (
                <p className="text-xs text-muted-foreground mt-1">
                  {getTimeSelectionLabel(timeSelection)}, {formatIntervalSeconds(intervalSeconds)} intervals
                </p>
              )}
            </div>
            <div className="flex items-center gap-2">
              {recentMetrics.length > 0 && (
                <Badge variant="secondary" className="text-xs">
                  {recentMetrics.length} real-time updates
                </Badge>
              )}
              <div className="flex border rounded-md">
                <Button
                  variant={stacked ? "secondary" : "ghost"}
                  size="sm"
                  className="h-8 px-2 rounded-r-none"
                  onClick={() => setStacked(true)}
                  title="Stacked bars"
                >
                  <Layers className="h-4 w-4" />
                </Button>
                <Button
                  variant={!stacked ? "secondary" : "ghost"}
                  size="sm"
                  className="h-8 px-2 rounded-l-none"
                  onClick={() => setStacked(false)}
                  title="Grouped bars"
                >
                  <BarChart3 className="h-4 w-4" />
                </Button>
              </div>
            </div>
          </div>
        </CardHeader>
        <CardContent>
          {loading ? (
            <div className="h-80 flex items-center justify-center text-muted-foreground">
              Loading...
            </div>
          ) : sortedData.length === 0 ? (
            <div className="h-80 flex items-center justify-center text-muted-foreground">
              {selectedMetric
                ? 'No data available for this metric'
                : 'Select a metric to view data'}
            </div>
          ) : (
            <div className="h-80">
              <MetricBarChart
                data={sortedData}
                series={chartSeries}
                colorMap={colorMap}
                formatYAxis={formatYAxis}
                tooltipFormatter={tooltipFormatter}
                xAxisInterval={xAxisTickInterval}
                showLegend={true}
                stacked={stacked}
              />
            </div>
          )}
        </CardContent>
      </Card>

      {/* Metric Info */}
      {metricNames.length === 0 && (
        <Card>
          <CardContent className="pt-6">
            <p className="text-muted-foreground text-center">
              No metrics data available yet. Configure your AI coding tool to send metrics telemetry.
            </p>
          </CardContent>
        </Card>
      )}
    </div>
  )
}
