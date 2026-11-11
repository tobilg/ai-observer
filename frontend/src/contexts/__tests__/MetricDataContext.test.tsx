import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { screen, waitFor, act } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MetricDataProvider } from '../MetricDataContext'
import { metricWidgetQueries, useMetricData, useRefreshMetricData } from '../metricData'
import { useTelemetryStore } from '@/stores/telemetryStore'
import { renderWithProviders } from '@/test/queryWrapper'
import { absoluteTimeSelection } from '@/lib/timeUtils'
import { TIMEFRAME_OPTIONS, WIDGET_TYPES, type DashboardWidget, type TimeSelection } from '@/types/dashboard'
import { api } from '@/lib/api'
import { httpError } from '@/lib/errors'

vi.mock('@/lib/api', () => ({
  api: {
    getBatchMetricSeries: vi.fn(),
  },
}))

const widget = (id: string, widgetType: string, metricName?: string, extra: Partial<DashboardWidget> = {}): DashboardWidget => ({
  id,
  dashboardId: 'd1',
  widgetType,
  title: id,
  gridColumn: 1,
  gridRow: 1,
  colSpan: 1,
  rowSpan: 1,
  config: metricName ? { metricName } : {},
  createdAt: '',
  updatedAt: '',
  ...extra,
})

const fifteenMinutes: TimeSelection = { type: 'relative', timeframe: TIMEFRAME_OPTIONS[2] }
const WIDGETS = [
  widget('value', WIDGET_TYPES.METRIC_VALUE, 'claude_code.cost.usage'),
  widget('chart', WIDGET_TYPES.METRIC_CHART, 'claude_code.token.usage'),
  widget('stats', WIDGET_TYPES.STATS_TRACES),
]

function Probe({ id }: { id: string }) {
  const data = useMetricData(id)
  const refreshAll = useRefreshMetricData()
  const state = data.loading ? 'loading' : data.error ? `error: ${data.error}` : `${data.series.length} series`
  return (
    <div>
      <div data-testid={id}>{state}</div>
      <div data-testid={`${id}-refreshing`}>{String(data.refreshing)}</div>
      <div data-testid={`${id}-stat`}>{data.stat ?? ''}</div>
      <button onClick={refreshAll}>retry {id}</button>
    </div>
  )
}

function renderProvider(widgets = WIDGETS, timeSelection = fifteenMinutes) {
  return renderWithProviders(
    <MetricDataProvider widgets={widgets} timeSelection={timeSelection}>
      <Probe id="value" />
      <Probe id="chart" />
      <Probe id="stats" />
    </MetricDataProvider>
  )
}

function setVisibility(state: 'visible' | 'hidden') {
  Object.defineProperty(document, 'visibilityState', { configurable: true, get: () => state })
  document.dispatchEvent(new Event('visibilitychange'))
}

const series = [{ name: 'm', labels: {}, datapoints: [[0, 1]] as [number, number][] }]

describe('MetricDataProvider', () => {
  beforeEach(() => {
    vi.mocked(api.getBatchMetricSeries).mockReset()
    vi.mocked(api.getBatchMetricSeries).mockResolvedValue({
      results: [
        { id: 'value', success: true, series },
        { id: 'chart', success: true, series: [...series, ...series] },
      ],
    })
    setVisibility('visible')
  })

  afterEach(() => {
    setVisibility('visible')
  })

  it('fetches the metric widgets in one batch with the range resolved at fetch time', async () => {
    renderProvider()

    await waitFor(() => expect(screen.getByTestId('value')).toHaveTextContent('1 series'))
    expect(screen.getByTestId('chart')).toHaveTextContent('2 series')
    // Widgets without a metric take no part
    expect(screen.getByTestId('stats')).toHaveTextContent('0 series')
    expect(api.getBatchMetricSeries).toHaveBeenCalledTimes(1)
    const [params] = vi.mocked(api.getBatchMetricSeries).mock.calls[0]
    expect(params.queries).toEqual([
      { id: 'chart', name: 'claude_code.token.usage', service: undefined, aggregate: false },
      { id: 'value', name: 'claude_code.cost.usage', service: undefined, aggregate: true, merge: true },
    ])
    expect(params.intervalSeconds).toBe(15)
    // Buckets follow the browser's clock
    expect(params.timeZone).toBe(Intl.DateTimeFormat().resolvedOptions().timeZone)
    expect(new Date(params.to).getTime() - new Date(params.from).getTime()).toBe(900_000)
    expect(Math.abs(Date.now() - new Date(params.to).getTime())).toBeLessThan(5_000)
  })

  it('does not refetch when widgets only move', async () => {
    const { rerender } = renderProvider()
    await waitFor(() => expect(screen.getByTestId('value')).toHaveTextContent('1 series'))

    const moved = [WIDGETS[1], { ...WIDGETS[0], gridRow: 3 }, WIDGETS[2]]
    rerender(
      <MetricDataProvider widgets={moved} timeSelection={fifteenMinutes}>
        <Probe id="value" />
      </MetricDataProvider>
    )
    await new Promise((resolve) => setTimeout(resolve, 50))
    expect(api.getBatchMetricSeries).toHaveBeenCalledTimes(1)
  })

  it('passes on the statistic the server computed', async () => {
    vi.mocked(api.getBatchMetricSeries).mockResolvedValue({
      results: [
        { id: 'value', success: true, series, stat: 'p95', metricType: 'histogram' },
        { id: 'chart', success: true, series },
      ],
    })
    renderProvider()
    await waitFor(() => expect(screen.getByTestId('value-stat')).toHaveTextContent('p95'))
    expect(screen.getByTestId('chart-stat')).toHaveTextContent('')
  })

  it('reports a failed widget query on that widget only', async () => {
    vi.mocked(api.getBatchMetricSeries).mockResolvedValue({
      results: [
        { id: 'value', success: false, error: 'unknown metric' },
        { id: 'chart', success: true, series },
      ],
    })
    renderProvider()

    await waitFor(() => expect(screen.getByTestId('value')).toHaveTextContent('error: unknown metric'))
    expect(screen.getByTestId('chart')).toHaveTextContent('1 series')
  })

  it('reports a failed batch on every widget with its message, and retries', async () => {
    const user = userEvent.setup()
    vi.mocked(api.getBatchMetricSeries).mockRejectedValueOnce(httpError(504))
    renderProvider()

    await waitFor(() => expect(screen.getByTestId('value')).toHaveTextContent('error: The request took too long'))
    expect(screen.getByTestId('chart')).toHaveTextContent('error: The request took too long')

    await user.click(screen.getByRole('button', { name: 'retry chart' }))
    await waitFor(() => expect(screen.getByTestId('value')).toHaveTextContent('1 series'))
    expect(api.getBatchMetricSeries).toHaveBeenCalledTimes(2)
  })

  it('keeps showing the series while a live refresh loads', async () => {
    renderProvider()
    await waitFor(() => expect(screen.getByTestId('value')).toHaveTextContent('1 series'))

    let release!: () => void
    vi.mocked(api.getBatchMetricSeries).mockImplementation(
      () =>
        new Promise((resolve) => {
          release = () => resolve({ results: [{ id: 'value', success: true, series }] })
        })
    )
    act(() => useTelemetryStore.getState().addMetrics({ count: 1, services: ['claude-code'], timestamps: [Date.now()] }))

    await waitFor(() => expect(screen.getByTestId('value-refreshing')).toHaveTextContent('true'), { timeout: 3000 })
    // No "loading" flash: the previous series stay
    expect(screen.getByTestId('value')).toHaveTextContent('1 series')
    await act(async () => release())
    await waitFor(() => expect(screen.getByTestId('value-refreshing')).toHaveTextContent('false'))
  })

  it('does not refresh for new metrics while the tab is hidden, and refreshes when shown', async () => {
    renderProvider()
    await waitFor(() => expect(api.getBatchMetricSeries).toHaveBeenCalledTimes(1))

    act(() => setVisibility('hidden'))
    act(() => useTelemetryStore.getState().addMetrics({ count: 1, services: ['claude-code'], timestamps: [Date.now()] }))
    await new Promise((resolve) => setTimeout(resolve, 700))
    expect(api.getBatchMetricSeries).toHaveBeenCalledTimes(1)

    act(() => setVisibility('visible'))
    await waitFor(() => expect(api.getBatchMetricSeries).toHaveBeenCalledTimes(2))
  })

  it('does not refresh an absolute range for new metrics', async () => {
    renderProvider(WIDGETS, absoluteTimeSelection(new Date('2026-01-01T00:00:00Z'), new Date('2026-01-02T00:00:00Z')))
    await waitFor(() => expect(api.getBatchMetricSeries).toHaveBeenCalledTimes(1))
    const [params] = vi.mocked(api.getBatchMetricSeries).mock.calls[0]
    expect(params.from).toBe('2026-01-01T00:00:00.000Z')

    act(() => useTelemetryStore.getState().addMetrics({ count: 1, services: ['claude-code'], timestamps: [Date.now()] }))
    await new Promise((resolve) => setTimeout(resolve, 700))
    expect(api.getBatchMetricSeries).toHaveBeenCalledTimes(1)
  })
})

describe('metricWidgetQueries', () => {
  it('sorts by widget and leaves out widgets without a metric', () => {
    const queries = metricWidgetQueries([
      widget('b', WIDGET_TYPES.METRIC_CHART, 'm2', { config: { metricName: 'm2', service: 'codex' } }),
      widget('a', WIDGET_TYPES.METRIC_VALUE, 'm1'),
      widget('c', WIDGET_TYPES.METRIC_VALUE),
      widget('d', WIDGET_TYPES.STATS_LOGS, 'm3'),
    ])
    expect(queries).toEqual([
      { id: 'a', name: 'm1', service: undefined, aggregate: true, merge: true },
      { id: 'b', name: 'm2', service: 'codex', aggregate: false },
    ])
  })

  it('breaks charts down on the server and filters values there', () => {
    const queries = metricWidgetQueries([
      widget('chart', WIDGET_TYPES.METRIC_CHART, 'm', { config: { metricName: 'm', breakdownAttribute: 'model' } }),
      // Stored by earlier versions: the value is filtered by the server now
      widget('value', WIDGET_TYPES.METRIC_VALUE, 'm', { config: { metricName: 'm', breakdownAttribute: 'type', breakdownValue: 'output' } }),
      widget('total', WIDGET_TYPES.METRIC_VALUE, 'm', { config: { metricName: 'm', breakdownAttribute: 'type' } }),
    ])
    expect(queries).toEqual([
      { id: 'chart', name: 'm', service: undefined, aggregate: false, groupBy: ['model'] },
      { id: 'total', name: 'm', service: undefined, aggregate: true, merge: true },
      { id: 'value', name: 'm', service: undefined, aggregate: true, merge: true, filters: { type: 'output' } },
    ])
  })

  it('asks histograms for their statistic, leaving out the server default', () => {
    const latency = 'gemini_cli.api.request.latency'
    const tokens = 'gen_ai.client.token.usage'
    const statOf = (metricName: string, stat?: string) =>
      metricWidgetQueries([widget('w', WIDGET_TYPES.METRIC_CHART, metricName, { config: { metricName, stat } })])[0].stat
    // Durations default to the average, the server's default
    expect(statOf(latency)).toBeUndefined()
    expect(statOf(latency, 'p95')).toBe('p95')
    expect(statOf(latency, 'bogus')).toBeUndefined()
    // Token counts default to their sum
    expect(statOf(tokens)).toBe('sum')
    expect(statOf(tokens, 'avg')).toBeUndefined()
    // Not a histogram: no stat
    expect(statOf('claude_code.cost.usage', 'p95')).toBeUndefined()
  })
})
