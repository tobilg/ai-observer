import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen } from '@testing-library/react'
import { MetricChartWidget } from '../MetricChartWidget'
import { MetricDataContext, type MetricData } from '@/contexts/metricData'
import { TIMEFRAME_OPTIONS, type TimeSelection, type WidgetConfig } from '@/types/dashboard'
import { getMetricMetadata, getSourceDisplayName } from '@/lib/metricMetadata'

interface ChartProps {
  data: Record<string, number | string>[]
  series: { key: string; label: string }[]
  stacked: boolean
  tooltipFormatter: (value: number | undefined, name: string | undefined) => [string, string]
}

const chartProps: ChartProps[] = []
vi.mock('@/components/charts', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/components/charts')>()),
  MetricBarChart: (props: ChartProps) => {
    chartProps.push(props)
    return <div data-testid="chart" />
  },
}))

const METRIC = 'claude_code.token.usage'
const oneHour = TIMEFRAME_OPTIONS.find((t) => t.value === '1h')!
const relative: TimeSelection = { type: 'relative', timeframe: oneHour }

function renderWidget(data: Partial<MetricData>, config: WidgetConfig = { metricName: METRIC }) {
  const value = {
    getMetricData: () => ({ series: [], loading: false, error: null, ...data }),
    refreshAll: () => {},
  }
  return render(
    <MetricDataContext.Provider value={value}>
      <MetricChartWidget
        widgetId="w1"
        title="Token usage"
        config={config}
        timeSelection={relative}
        fromTime={new Date('2026-01-02T09:00:00Z')}
        toTime={new Date('2026-01-02T10:00:00Z')}
      />
    </MetricDataContext.Provider>
  )
}

const t1 = Date.parse('2026-01-02T09:00:00Z')
const t2 = Date.parse('2026-01-02T09:01:00Z')

describe('MetricChartWidget', () => {
  beforeEach(() => {
    chartProps.length = 0
  })

  it('shows the loading and error states', () => {
    const { unmount } = renderWidget({ loading: true })
    expect(screen.getByText('Loading...')).toBeInTheDocument()
    unmount()

    renderWidget({ error: 'query timed out' })
    expect(screen.getByText('query timed out')).toBeInTheDocument()
  })

  it('says whether the widget lacks a metric or data', () => {
    const { unmount } = renderWidget({}, {})
    expect(screen.getByText('Not configured')).toBeInTheDocument()
    unmount()

    renderWidget({ series: [] })
    expect(screen.getByText('No data')).toBeInTheDocument()
    expect(chartProps).toHaveLength(0)
  })

  it('adds up series with the same key and fills missing points with zero', () => {
    renderWidget({
      series: [
        // Same service and type, different models: one bar per timestamp
        { name: METRIC, labels: { service: 'claude-code', type: 'input', model: 'a' }, datapoints: [[t1, 10], [t2, 5]] },
        { name: METRIC, labels: { service: 'claude-code', type: 'input', model: 'b' }, datapoints: [[t1, 1]] },
        { name: METRIC, labels: { service: 'claude-code', type: 'cacheRead' }, datapoints: [[t2, 7]] },
      ],
    })

    expect(screen.getByTestId('chart')).toBeInTheDocument()
    const { data, series, stacked } = chartProps.at(-1)!
    expect(series.map((s) => s.key)).toEqual(['claude-code:cacheRead', 'claude-code:input'])
    expect(data.map((p) => p.timestamp)).toEqual([t1, t2])
    expect(data[0]['claude-code:input']).toBe(11)
    expect(data[0]['claude-code:cacheRead']).toBe(0)
    expect(data[1]['claude-code:input']).toBe(5)
    expect(data[1]['claude-code:cacheRead']).toBe(7)
    expect(stacked).toBe(true)
  })

  it('groups by the configured breakdown attribute and can be unstacked', () => {
    renderWidget(
      {
        series: [
          { name: METRIC, labels: { model: 'opus', type: 'input' }, datapoints: [[t1, 2]] },
          { name: METRIC, labels: { model: 'sonnet', type: 'input' }, datapoints: [[t1, 3]] },
        ],
      },
      { metricName: METRIC, breakdownAttribute: 'model', chartStacked: false }
    )

    const { series, stacked, tooltipFormatter } = chartProps.at(-1)!
    expect(series.map((s) => s.key)).toEqual(['opus', 'sonnet'])
    expect(stacked).toBe(false)
    expect(tooltipFormatter(undefined, 'opus')).toEqual(['—', 'opus'])
  })

  it('names the service from the configuration, the series or the metric source', () => {
    const { unmount } = renderWidget({ series: [] }, { metricName: METRIC, service: 'codex_cli_rs' })
    expect(screen.getByText('OpenAI Codex CLI')).toBeInTheDocument()
    unmount()

    const fromSeries = renderWidget({
      series: [{ name: METRIC, labels: { service: 'gemini_cli', type: 'input' }, datapoints: [[t1, 1]] }],
    })
    expect(screen.getByText('Gemini CLI')).toBeInTheDocument()
    fromSeries.unmount()

    renderWidget({ series: [{ name: METRIC, labels: { type: 'input' }, datapoints: [[t1, 1]] }] })
    const source = getMetricMetadata(METRIC)!.source
    expect(screen.getByText(getSourceDisplayName(source))).toBeInTheDocument()
    expect(screen.getByText('Token usage')).toBeInTheDocument()
  })
})
