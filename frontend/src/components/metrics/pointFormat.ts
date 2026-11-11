import type { MetricDataPoint } from '@/types/metrics'

const valueFormat = new Intl.NumberFormat('en-US', { maximumFractionDigits: 6 })

// OTLP aggregation temporality: 1 delta, 2 cumulative
export function temporalityLabel(point: MetricDataPoint): string | null {
  if (point.aggregationTemporality === 2) return 'cumulative'
  if (point.aggregationTemporality === 1) return 'delta'
  return null
}

/** The value of a point as stored: the number of a gauge or sum, count and sum of a distribution */
export function formatPointValue(point: MetricDataPoint): string {
  if (point.value !== undefined && point.value !== null) {
    return valueFormat.format(point.value)
  }
  const parts: string[] = []
  if (point.count !== undefined) parts.push(`count ${valueFormat.format(point.count)}`)
  if (point.sum !== undefined) parts.push(`sum ${valueFormat.format(point.sum)}`)
  if (point.min !== undefined && point.max !== undefined) {
    parts.push(`min ${valueFormat.format(point.min)}, max ${valueFormat.format(point.max)}`)
  }
  return parts.length > 0 ? parts.join(' · ') : '—'
}
