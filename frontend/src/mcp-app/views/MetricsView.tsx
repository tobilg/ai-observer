import { MetricBarChart, MetricLineChart } from '@/components/charts'
import { HISTOGRAM_STATS, formatMetricValue, getMetricMetadata, statUnit } from '@/lib/metricMetadata'
import { formatIntervalSeconds } from '@/lib/utils'
import { formatRange, goDurationSeconds, isBucketed, metricChart, metricTotals } from '../data'
import type { MetricsResult } from '../types'
import { BarList, Notice, ViewHeader } from './parts'

// query_metrics: time buckets as bars for counters (increases per bucket)
// and lines otherwise, or the totals of an aggregated query as bars
export function MetricsView({ data }: { data: MetricsResult }) {
  const metadata = getMetricMetadata(data.name)
  const unit = statUnit(metadata.unit, data.stat)
  const format = (value: number) => formatMetricValue(value, unit)
  const interval = goDurationSeconds(data.interval)
  // A histogram's statistic, such as "95th percentile"
  const stat = data.stat ? (HISTOGRAM_STATS.find((s) => s.value === data.stat)?.label ?? data.stat) : undefined
  const subtitle = [formatRange(data.from, data.to), stat, interval ? `per ${formatIntervalSeconds(interval)}` : 'totals']
    .filter(Boolean)
    .join(' · ')
  const title = (
    <>
      {metadata.displayName} <span className="font-mono text-xs font-normal text-muted-foreground">{data.name}</span>
    </>
  )

  if (!isBucketed(data)) {
    return (
      <div className="space-y-4">
        <ViewHeader title={title} subtitle={subtitle} />
        <BarList rows={metricTotals(data)} format={format} label={`${metadata.displayName} per series`} />
      </div>
    )
  }

  const chart = metricChart(data)
  const Chart = metadata.metricType === 'counter' ? MetricBarChart : MetricLineChart
  return (
    <div className="space-y-4">
      <ViewHeader title={title} subtitle={subtitle} />
      {chart.data.length === 0 ? (
        <Notice text="Nothing recorded in this range." />
      ) : (
        <div className="h-72">
          <Chart
            data={chart.data}
            series={chart.series}
            colorMap={chart.colorMap}
            formatYAxis={format}
            tooltipFormatter={(value, name) => [
              value === undefined ? '—' : format(value),
              chart.series.find((s) => s.key === name)?.label ?? name ?? '',
            ]}
            showLegend={chart.series.length > 1}
            // Fewer, untilted labels when the chart has many buckets
            compact={chart.data.length > 12}
          />
        </div>
      )}
    </div>
  )
}
