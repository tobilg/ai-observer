import { useMemo, useCallback } from 'react'
import { BarChart3, Layers, LineChart } from 'lucide-react'
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from '@/components/ui/card'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'
import { Skeleton } from '@/components/ui/skeleton'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { DateRangePicker } from '@/components/common/date-range-picker'
import { EmptyState } from '@/components/common/empty-state'
import { Refreshing } from '@/components/common/list-skeleton'
import { MetricOptions } from '@/components/common/metric-options'
import { PageHeader } from '@/components/common/page-header'
import { QueryError } from '@/components/common/query-error'
import { MetricBarChart, buildChartData, formatTickLabel } from '@/components/charts'
import { LiveIndicator } from '@/components/metrics/LiveIndicator'
import { MetricPointsTable } from '@/components/metrics/MetricPointsTable'
import { useLiveInvalidate } from '@/hooks/useLiveInvalidate'
import { useServiceNames } from '@/hooks/useServiceNames'
import { useUrlState } from '@/hooks/useUrlState'
import { browserTimeZone } from '@/lib/timeZone'
import { groupMetricsBySource, isMetricForService } from '@/lib/metricGroups'
import {
  HISTOGRAM_STATS,
  breakdownOptions,
  defaultHistogramStat,
  formatMetricValue,
  getMetricMetadata,
  getSeriesLabel,
  isAdditiveStat,
  isHistogramMetric,
  statParam,
  statUnit,
} from '@/lib/metricMetadata'
import { queryKeys, useMetricAttributeKeys, useMetricNames, useMetricSeries, useServices } from '@/lib/queries'
import { formatIntervalSeconds } from '@/lib/utils'
import { useTelemetryStore } from '@/stores/telemetryStore'
import { getTimeSelectionLabel } from '@/types/dashboard'

const TIME_SELECTION_STORAGE_KEY = 'ai-observer-metrics-timeselection'

// Kept in the URL, so a reload or a shared link shows the same chart.
// groupBy: the attribute series are broken down by ('' = type); stat: the
// statistic of a histogram metric ('' = the metric's default).
const FILTER_DEFAULTS = { service: '', metric: '', groupBy: '', stat: '', stacked: true, view: 'chart' }

export function MetricsPage() {
  const { filters, setFilters, time } = useUrlState({
    filters: FILTER_DEFAULTS,
    time: { storageKey: TIME_SELECTION_STORAGE_KEY, defaultTimeframe: '15m' },
  })
  const { service, metric, groupBy, stacked } = filters
  const view = filters.view === 'points' && metric ? 'points' : 'chart'
  const { timeSelection, intervalSeconds, tickInterval, durationSeconds, isAbsoluteRange } = time

  const services = useServices()
  const metricNames = useMetricNames(service || undefined)
  const { filterOptions } = useServiceNames()

  const metadata = useMemo(() => (metric ? getMetricMetadata(metric) : null), [metric])
  const histogram = isHistogramMetric(metadata)
  const attributeKeys = useMetricAttributeKeys({ name: metric, service: service || undefined })
  const breakdowns = useMemo(() => breakdownOptions(metadata, attributeKeys.data), [metadata, attributeKeys.data])

  const seriesParams = useMemo(
    () => ({
      name: metric,
      service: service || undefined,
      intervalSeconds,
      groupBy: groupBy ? [groupBy] : undefined,
      stat: statParam(metadata, filters.stat),
      // Buckets follow the browser's clock: a day bar is a local day
      timeZone: browserTimeZone(),
    }),
    [metric, service, intervalSeconds, groupBy, metadata, filters.stat]
  )
  const series = useMetricSeries(seriesParams, time.range)
  // The statistic the server computed (avg unless another was asked for)
  const stat = series.data?.stat
  const canStack = isAdditiveStat(stat)

  // Refresh at the X-axis tick interval and on new metrics (not for absolute
  // ranges, which show fixed historical data)
  const metricsUpdateCount = useTelemetryStore((state) => state.metricsUpdateCount)
  useLiveInvalidate({
    queryKey: queryKeys.metrics.series(seriesParams, time.range),
    enabled: !isAbsoluteRange && Boolean(metric),
    intervalMs: intervalSeconds * 1000 * (tickInterval + 1),
    updateCount: metricsUpdateCount,
  })

  const handleServiceChange = (value: string) => {
    // Keep the metric when the service can send it
    setFilters(isMetricForService(metric, value) ? { service: value } : { service: value, metric: '', groupBy: '', stat: '' })
  }

  // Group metric names by source - filter by service if selected
  const groupedMetrics = useMemo(
    () => groupMetricsBySource(metricNames.data ?? [], service),
    [metricNames.data, service]
  )

  // Series key from labels: the breakdown attribute's value (the type by
  // default) per service, else the service
  const getSeriesKey = useCallback((labels?: Record<string, string>) => {
    const part = labels?.[groupBy || 'type']
    if (part) {
      return labels?.service ? `${labels.service}:${part}` : part
    }
    return labels?.service || 'value'
  }, [groupBy])

  // Helper to get human-readable series label
  const getDisplayLabel = useCallback((labels?: Record<string, string>) => {
    if (!metadata || !labels) return labels?.service || 'value'
    return getSeriesLabel(labels, metadata, groupBy || undefined)
  }, [metadata, groupBy])

  // Format Y-axis values; a histogram's count is a number
  const unit = useMemo(() => (metadata ? statUnit(metadata.unit, stat) : null), [metadata, stat])
  const formatYAxis = useCallback((value: number) => {
    if (!unit) return value.toString()
    return formatMetricValue(value, unit)
  }, [unit])

  const seriesData = series.data?.series
  const { data: sortedData, series: chartSeries, colorMap } = useMemo(
    () =>
      buildChartData(seriesData ?? [], getSeriesKey, getDisplayLabel, (timestamp) =>
        formatTickLabel(timestamp, durationSeconds, intervalSeconds)
      ),
    [seriesData, getSeriesKey, getDisplayLabel, durationSeconds, intervalSeconds]
  )

  // Custom tooltip formatter
  const tooltipFormatter = useCallback((value: number | undefined, name: string | undefined): [string, string] => {
    if (value === undefined) return ['—', name || 'value']
    const displayLabel = chartSeries.find((s) => s.key === name)?.label ?? (name || 'value')
    const formattedValue = unit ? formatMetricValue(value, unit) : value.toString()
    return [formattedValue, displayLabel]
  }, [unit, chartSeries])

  const rangeLabel = `${getTimeSelectionLabel(timeSelection)}, ${formatIntervalSeconds(intervalSeconds)} intervals`
  const noMetrics = metricNames.data !== undefined && metricNames.data.length === 0

  const chart = (() => {
    if (!metric) {
      return (
        <EmptyState
          icon={LineChart}
          className="h-80"
          title="Select a metric to view data"
          description="Choose a metric above; the chart shows its values in the selected time range."
        />
      )
    }
    if (series.isPending) {
      return (
        <div role="status" aria-busy="true" aria-label="Loading chart" className="h-80">
          <Skeleton className="h-full w-full" />
        </div>
      )
    }
    if (series.isError && !series.data) {
      return (
        <QueryError
          error={series.error}
          title="Couldn't load the metric"
          onRetry={() => series.refetch()}
          retrying={series.isFetching}
        />
      )
    }
    if (sortedData.length === 0) {
      return (
        <EmptyState
          className="h-80"
          title="No data available for this metric"
          description="Nothing was recorded in this time range. Choose a longer range or another service."
        />
      )
    }
    return (
      <Refreshing active={series.isPlaceholderData} className="h-80">
        <MetricBarChart
          data={sortedData}
          series={chartSeries}
          colorMap={colorMap}
          formatYAxis={formatYAxis}
          tooltipFormatter={tooltipFormatter}
          xAxisInterval={tickInterval}
          showLegend={true}
          // Averages and percentiles do not add up
          stacked={stacked && canStack}
        />
      </Refreshing>
    )
  })()

  return (
    <div className="space-y-6">
      <PageHeader
        title="Metrics"
        description="View and analyze metric time series data"
        actions={<LiveIndicator live={!isAbsoluteRange} />}
      />

      {/* Filters */}
      <Card>
        <CardContent className="pt-6">
          <div className="flex flex-col gap-4 md:flex-row">
            <div className="min-w-0 space-y-2 md:flex-1 *:w-full">
              <Label htmlFor="metrics-service">Service</Label>
              <NativeSelect
                id="metrics-service"
                className="h-10"
                value={service}
                onChange={(e) => handleServiceChange(e.target.value)}
              >
                <NativeSelectOption value="">All Services</NativeSelectOption>
                {filterOptions(services.data ?? []).map((o) => (
                  <NativeSelectOption key={o.value} value={o.value}>
                    {o.label}
                  </NativeSelectOption>
                ))}
              </NativeSelect>
            </div>
            <div className="min-w-0 space-y-2 md:flex-1 *:w-full">
              <Label htmlFor="metrics-metric">Metric</Label>
              <NativeSelect
                id="metrics-metric"
                className="h-10"
                value={metric}
                onChange={(e) => setFilters({ metric: e.target.value, groupBy: '', stat: '' })}
              >
                <NativeSelectOption value="">Select a metric</NativeSelectOption>
                <MetricOptions groups={groupedMetrics} />
              </NativeSelect>
            </div>
            <div className="min-w-0 space-y-2 md:flex-1">
              <Label htmlFor="metrics-timeframe">Timeframe</Label>
              <DateRangePicker
                id="metrics-timeframe"
                label="Timeframe"
                className="w-full"
                value={timeSelection}
                onChange={time.setTimeSelection}
              />
            </div>
          </div>
          {metric && (
            <div className="mt-4 flex flex-col gap-4 md:flex-row">
              <div className="min-w-0 space-y-2 md:flex-1 *:w-full">
                <Label htmlFor="metrics-breakdown">Breakdown</Label>
                <NativeSelect
                  id="metrics-breakdown"
                  className="h-10"
                  value={groupBy}
                  onChange={(e) => setFilters({ groupBy: e.target.value })}
                >
                  <NativeSelectOption value="">Type (default)</NativeSelectOption>
                  {/* A breakdown from the URL stays selectable before the keys load */}
                  {groupBy && !breakdowns.some((b) => b.value === groupBy) && (
                    <NativeSelectOption value={groupBy}>{groupBy}</NativeSelectOption>
                  )}
                  {breakdowns.map((b) => (
                    <NativeSelectOption key={b.value} value={b.value}>
                      {b.label}
                    </NativeSelectOption>
                  ))}
                </NativeSelect>
              </div>
              {metadata && histogram && (
                <div className="min-w-0 space-y-2 md:flex-1 *:w-full">
                  <Label htmlFor="metrics-stat">Statistic</Label>
                  <NativeSelect
                    id="metrics-stat"
                    className="h-10"
                    value={filters.stat}
                    onChange={(e) => setFilters({ stat: e.target.value })}
                  >
                    <NativeSelectOption value="">
                      Default ({HISTOGRAM_STATS.find((s) => s.value === defaultHistogramStat(metadata))?.label})
                    </NativeSelectOption>
                    {HISTOGRAM_STATS.map((s) => (
                      <NativeSelectOption key={s.value} value={s.value}>
                        {s.label}
                      </NativeSelectOption>
                    ))}
                  </NativeSelect>
                </div>
              )}
              {/* Keeps the selects as wide as those of the row above */}
              <div className="hidden md:block md:flex-1" />
              {!(metadata && histogram) && <div className="hidden md:block md:flex-1" />}
            </div>
          )}
          {metricNames.isError && (
            <QueryError
              className="mb-0"
              error={metricNames.error}
              title="Couldn't load the metric names"
              onRetry={() => metricNames.refetch()}
              retrying={metricNames.isFetching}
            />
          )}
        </CardContent>
      </Card>

      {/* Chart and data points */}
      <Card>
        <Tabs value={view} onValueChange={(value) => setFilters({ view: value })}>
          <CardHeader>
            <div className="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
              <div className="min-w-0">
                <div className="flex flex-wrap items-center gap-2">
                  <CardTitle>{metadata?.displayName || metric || 'Select a metric'}</CardTitle>
                  {unit?.displayUnit && (
                    <Badge variant="outline" className="text-xs">
                      {unit.displayUnit}
                    </Badge>
                  )}
                  {stat && (
                    <Badge variant="secondary" className="text-xs">
                      {HISTOGRAM_STATS.find((s) => s.value === stat)?.label ?? stat}
                    </Badge>
                  )}
                </div>
                <CardDescription>{metadata?.description || rangeLabel}</CardDescription>
                {metadata && <p className="text-xs text-muted-foreground mt-1">{rangeLabel}</p>}
              </div>
              <div className="flex flex-wrap items-center gap-2">
                <TabsList>
                  <TabsTrigger value="chart">Chart</TabsTrigger>
                  <TabsTrigger value="points" disabled={!metric}>
                    Data points
                  </TabsTrigger>
                </TabsList>
                {view === 'chart' && canStack && (
                  <div role="group" aria-label="Chart mode" className="flex border rounded-md">
                    <Button
                      variant={stacked ? 'secondary' : 'ghost'}
                      size="sm"
                      className="h-8 px-2 rounded-r-none"
                      onClick={() => setFilters({ stacked: true })}
                      aria-label="Stacked bars"
                      aria-pressed={stacked}
                      title="Stacked bars"
                    >
                      <Layers className="h-4 w-4" aria-hidden="true" />
                    </Button>
                    <Button
                      variant={!stacked ? 'secondary' : 'ghost'}
                      size="sm"
                      className="h-8 px-2 rounded-l-none"
                      onClick={() => setFilters({ stacked: false })}
                      aria-label="Grouped bars"
                      aria-pressed={!stacked}
                      title="Grouped bars"
                    >
                      <BarChart3 className="h-4 w-4" aria-hidden="true" />
                    </Button>
                  </div>
                )}
              </div>
            </div>
          </CardHeader>
          <CardContent>
            <TabsContent value="chart" className="mt-0">
              {chart}
            </TabsContent>
            <TabsContent value="points" className="mt-0">
              {metric && <MetricPointsTable name={metric} service={service} range={time.range} />}
            </TabsContent>
          </CardContent>
        </Tabs>
      </Card>

      {noMetrics && (
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
