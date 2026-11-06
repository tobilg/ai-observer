import type { TimeSeries } from '@/types/metrics'
import { CHART_COLORS, type ChartSeries } from './chartUtils'

const DAY_SECONDS = 24 * 60 * 60

// The X-axis label of a bucket: the time for ranges under a day (with
// seconds for sub-minute buckets, so labels stay distinct), the date and time
// up to a week or for sub-daily buckets, the date otherwise
export function formatTickLabel(timestamp: number, durationSeconds: number, intervalSeconds: number): string {
  const date = new Date(timestamp)
  const time = () =>
    date.toLocaleTimeString([], {
      hour: '2-digit',
      minute: '2-digit',
      ...(intervalSeconds < 60 && { second: '2-digit' }),
    })
  const day = () => date.toLocaleDateString([], { month: 'short', day: 'numeric', year: 'numeric' })

  if (durationSeconds < DAY_SECONDS) {
    return time()
  }
  if (durationSeconds <= 7 * DAY_SECONDS || intervalSeconds < DAY_SECONDS) {
    return `${day()} ${time()}`
  }
  return day()
}

export interface ChartData {
  // One row per bucket: timestamp, time label and a value per series key
  data: Record<string, number | string>[]
  // The series by key, sorted so bars stack in the same order on every refresh
  series: ChartSeries[]
  colorMap: Map<string, string>
}

// Turns time series into bar chart rows. Series with the same key are added
// up; a series without a point in a bucket counts 0 there.
export function buildChartData(
  timeSeries: TimeSeries[],
  keyOf: (labels?: Record<string, string>) => string,
  labelOf: (labels?: Record<string, string>) => string,
  formatTime: (timestamp: number) => string
): ChartData {
  const labels = new Map<string, string>()
  const values = new Map<string, Map<number, number>>()
  const timestamps = new Set<number>()
  for (const s of timeSeries) {
    const key = keyOf(s.labels)
    if (!labels.has(key)) {
      labels.set(key, labelOf(s.labels))
      values.set(key, new Map())
    }
    const byTime = values.get(key)!
    for (const [timestamp, value] of s.datapoints) {
      timestamps.add(timestamp)
      byTime.set(timestamp, (byTime.get(timestamp) ?? 0) + value)
    }
  }

  const keys = Array.from(labels.keys()).sort((a, b) => a.localeCompare(b))
  const data = Array.from(timestamps)
    .sort((a, b) => a - b)
    .map((timestamp) => {
      const row: Record<string, number | string> = { timestamp, time: formatTime(timestamp) }
      for (const key of keys) {
        row[key] = values.get(key)!.get(timestamp) ?? 0
      }
      return row
    })
  // Colors follow the code-point order of the keys, as they always have
  const colorMap = new Map([...keys].sort().map((key, i) => [key, CHART_COLORS[i % CHART_COLORS.length]]))
  return { data, series: keys.map((key) => ({ key, label: labels.get(key)! })), colorMap }
}
