import { describe, it, expect } from 'vitest'
import { buildChartData, formatTickLabel } from './chartData'

const t1 = Date.parse('2026-01-02T09:00:00Z')
const t2 = Date.parse('2026-01-02T09:00:15Z')

describe('buildChartData', () => {
  const key = (labels?: Record<string, string>) => labels?.type ?? 'value'
  const label = (labels?: Record<string, string>) => `Type ${labels?.type}`

  it('adds up series with the same key, fills gaps with 0 and sorts the series', () => {
    const { data, series, colorMap } = buildChartData(
      [
        { name: 'm', labels: { type: 'output' }, datapoints: [[t2, 2]] },
        { name: 'm', labels: { type: 'input', model: 'a' }, datapoints: [[t1, 1], [t2, 3]] },
        { name: 'm', labels: { type: 'input', model: 'b' }, datapoints: [[t1, 4]] },
      ],
      key,
      label,
      (ts) => `at ${ts}`
    )

    expect(series).toEqual([
      { key: 'input', label: 'Type input' },
      { key: 'output', label: 'Type output' },
    ])
    expect(data).toEqual([
      { timestamp: t1, time: `at ${t1}`, input: 5, output: 0 },
      { timestamp: t2, time: `at ${t2}`, input: 3, output: 2 },
    ])
    expect(colorMap.get('input')).not.toEqual(colorMap.get('output'))
  })

  it('returns nothing for no series', () => {
    expect(buildChartData([], key, label, String)).toEqual({ data: [], series: [], colorMap: new Map() })
  })
})

describe('formatTickLabel', () => {
  it('shows seconds only for sub-minute buckets and dates for longer ranges', () => {
    const hour = 3600
    const withSeconds = formatTickLabel(t2, hour, 15)
    const withoutSeconds = formatTickLabel(t2, hour, 60)
    expect(withSeconds.length).toBeGreaterThan(withoutSeconds.length)

    const week = formatTickLabel(t1, 7 * 24 * hour, hour)
    expect(week).toContain('2026')
    const month = formatTickLabel(t1, 30 * 24 * hour, 24 * hour)
    expect(month).toContain('2026')
    expect(month.length).toBeLessThan(week.length)
  })
})
