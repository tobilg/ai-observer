import { useEffect, useState, useMemo, useCallback } from 'react'
import { useSearchParams } from 'react-router-dom'
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from '@/components/ui/card'
import { Select } from '@/components/ui/select'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { DateRangePicker } from '@/components/common/date-range-picker'
import { Layers, BarChart3 } from 'lucide-react'
import { api } from '@/lib/api'
import { isAbortError } from '@/lib/errors'
import type { TimeSeries } from '@/types/metrics'
import { MetricBarChart, buildChartData, formatTickLabel } from '@/components/charts'
import { MetricOptions } from '@/components/common/metric-options'
import { groupMetricsBySource, isMetricForService } from '@/lib/metricGroups'
import { useTelemetryStore } from '@/stores/telemetryStore'
import { useLiveRefresh } from '@/hooks/useLiveRefresh'
import {
  getMetricMetadata,
  getSeriesLabel,
  formatMetricValue,
  getServiceDisplayName,
} from '@/lib/metricMetadata'
import { toast } from 'sonner'
import {
  isAbsoluteTimeSelection,
  getTimeSelectionLabel,
  type TimeSelection,
} from '@/types/dashboard'
import { formatIntervalSeconds } from '@/lib/utils'
import { useTimeSelection } from '@/hooks/useTimeSelection'

const TIME_SELECTION_STORAGE_KEY = 'ai-observer-metrics-timeselection'

export function MetricsPage() {
  const [searchParams, setSearchParams] = useSearchParams()
  const [metricNames, setMetricNames] = useState<string[]>([])
  const [selectedMetric, setSelectedMetric] = useState<string>('')
  const [series, setSeries] = useState<TimeSeries[]>([])
  const [services, setServices] = useState<string[]>([])
  const [selectedService, setSelectedService] = useState<string>('')
  const [loading, setLoading] = useState(false)
  const [stacked, setStacked] = useState(true)

  // Time selection with localStorage persistence
  const { timeSelection, setTimeSelection, fromTime, toTime, isAbsoluteRange, intervalSeconds, tickInterval } = useTimeSelection({
    storageKey: TIME_SELECTION_STORAGE_KEY,
    searchParams,
    defaultTimeframe: '15m',
  })

  const handleTimeSelectionChange = (selection: TimeSelection) => {
    setTimeSelection(selection) // Also persists to localStorage

    // Update URL params
    const newParams = new URLSearchParams(searchParams)
    if (isAbsoluteTimeSelection(selection)) {
      newParams.set('from', selection.range.from.toISOString())
      newParams.set('to', selection.range.to.toISOString())
      newParams.delete('timeframe')
    } else {
      newParams.delete('from')
      newParams.delete('to')
      if (selection.timeframe.value !== '15m') {
        newParams.set('timeframe', selection.timeframe.value)
      } else {
        newParams.delete('timeframe')
      }
    }
    setSearchParams(newParams)
  }

  // Real-time metrics from WebSocket
  const metricsUpdateCount = useTelemetryStore((state) => state.metricsUpdateCount)
  const metricsReceivedCount = useTelemetryStore((state) => state.metricsReceivedCount)
  // Refresh at the X-axis tick interval and on new metrics (not for absolute
  // ranges, which show fixed historical data)
  const refreshKey = useLiveRefresh({
    enabled: !isAbsoluteRange,
    intervalMs: intervalSeconds * 1000 * (tickInterval + 1),
    updateCount: metricsUpdateCount,
  })

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
        if (isAbortError(err)) {
          return // Ignore abort errors
        }
        console.error('Failed to fetch metric names:', err)
        toast.error('Failed to fetch metric names')
      }
    }
    fetchMetricNames()

    return () => abortController.abort()
  }, [selectedService])

  useEffect(() => {
    if (!selectedMetric) {
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
        if (isAbortError(err)) {
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
  }, [selectedMetric, selectedService, refreshKey, timeSelection, fromTime, toTime, intervalSeconds, isAbsoluteRange, series.length])

  const handleServiceChange = (value: string) => {
    setSelectedService(value)
    if (!isMetricForService(selectedMetric, value)) {
      setSelectedMetric('')
      setSeries([])
    }
  }

  const handleMetricChange = (value: string) => {
    setSelectedMetric(value)
    if (!value) {
      setSeries([])
    }
  }

  // Get metadata for selected metric
  const metadata = useMemo(
    () => (selectedMetric ? getMetricMetadata(selectedMetric) : null),
    [selectedMetric]
  )

  // Group metric names by source - filter by service if selected
  const groupedMetrics = useMemo(
    () => groupMetricsBySource(metricNames, selectedService),
    [metricNames, selectedService]
  )

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

  const durationSeconds = (toTime.getTime() - fromTime.getTime()) / 1000
  const { data: sortedData, series: chartSeries, colorMap } = useMemo(
    () =>
      buildChartData(series, getSeriesKey, getDisplayLabel, (timestamp) =>
        formatTickLabel(timestamp, durationSeconds, intervalSeconds)
      ),
    [series, getSeriesKey, getDisplayLabel, durationSeconds, intervalSeconds]
  )

  // Custom tooltip formatter
  const tooltipFormatter = useCallback((value: number | undefined, name: string | undefined): [string, string] => {
    if (value === undefined) return ['—', name || 'value']
    const displayLabel = chartSeries.find((s) => s.key === name)?.label ?? (name || 'value')
    const formattedValue = metadata ? formatMetricValue(value, metadata.unit) : value.toString()
    return [formattedValue, displayLabel]
  }, [metadata, chartSeries])

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
                onChange={(e) => handleServiceChange(e.target.value)}
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
                onChange={(e) => handleMetricChange(e.target.value)}
              >
                <option value="">Select a metric</option>
                <MetricOptions groups={groupedMetrics} />
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
              {metricsReceivedCount > 0 && (
                <Badge variant="secondary" className="text-xs">
                  {metricsReceivedCount} real-time updates
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
