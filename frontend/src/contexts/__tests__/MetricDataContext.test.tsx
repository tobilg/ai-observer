import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, waitFor, act } from '@testing-library/react'
import { MetricDataProvider } from '../MetricDataContext'
import { useMetricData } from '../metricData'
import { useDashboardStore } from '@/stores/dashboardStore'
import { useTelemetryStore } from '@/stores/telemetryStore'
import { TIMEFRAME_OPTIONS, WIDGET_TYPES, type DashboardWidget } from '@/types/dashboard'
import { api } from '@/lib/api'

vi.mock('@/lib/api', () => ({
  api: {
    getBatchMetricSeries: vi.fn(),
  },
}))

const widget = (id: string, widgetType: string, metricName?: string): DashboardWidget => ({
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
})

function SeriesCount({ id }: { id: string }) {
  const data = useMetricData(id)
  return <div data-testid={id}>{data.error ?? data.series.length}</div>
}

function setVisibility(state: 'visible' | 'hidden') {
  Object.defineProperty(document, 'visibilityState', { configurable: true, get: () => state })
  document.dispatchEvent(new Event('visibilitychange'))
}

describe('MetricDataProvider', () => {
  beforeEach(() => {
    vi.mocked(api.getBatchMetricSeries).mockReset()
    vi.mocked(api.getBatchMetricSeries).mockResolvedValue({
      results: [{ id: 'value', success: true, series: [{ name: 'm', labels: {}, dataPoints: [[0, 1]] }] }],
    })
    const now = new Date()
    useDashboardStore.setState({
      widgets: [
        widget('value', WIDGET_TYPES.METRIC_VALUE, 'claude_code.cost.usage'),
        widget('stats', WIDGET_TYPES.STATS_TRACES),
      ],
      timeSelection: { type: 'relative', timeframe: TIMEFRAME_OPTIONS[2] },
      fromTime: new Date(now.getTime() - 900_000),
      toTime: now,
      intervalSeconds: 15,
      isAbsoluteRange: false,
    })
    setVisibility('visible')
  })

  afterEach(() => {
    setVisibility('visible')
  })

  it('fetches the metric widgets in one batch', async () => {
    render(
      <MetricDataProvider>
        <SeriesCount id="value" />
      </MetricDataProvider>
    )

    await waitFor(() => expect(screen.getByTestId('value')).toHaveTextContent('1'))
    expect(api.getBatchMetricSeries).toHaveBeenCalledTimes(1)
    const [params] = vi.mocked(api.getBatchMetricSeries).mock.calls[0]
    expect(params.queries).toEqual([
      { id: 'value', name: 'claude_code.cost.usage', service: undefined, aggregate: true },
    ])
  })

  it('reports a failed batch on every widget', async () => {
    vi.mocked(api.getBatchMetricSeries).mockRejectedValue(new Error('HTTP error! status: 504'))

    render(
      <MetricDataProvider>
        <SeriesCount id="value" />
      </MetricDataProvider>
    )

    await waitFor(() => expect(screen.getByTestId('value')).toHaveTextContent('HTTP error! status: 504'))
  })

  it('does not refresh for new metrics while the tab is hidden, and refreshes when shown', async () => {
    render(
      <MetricDataProvider>
        <SeriesCount id="value" />
      </MetricDataProvider>
    )
    await waitFor(() => expect(api.getBatchMetricSeries).toHaveBeenCalledTimes(1))

    act(() => setVisibility('hidden'))
    act(() => useTelemetryStore.getState().addMetrics({ count: 1, services: ['claude-code'], timestamps: [Date.now()] }))
    await new Promise((resolve) => setTimeout(resolve, 700))
    expect(api.getBatchMetricSeries).toHaveBeenCalledTimes(1)

    act(() => setVisibility('visible'))
    await waitFor(() => expect(api.getBatchMetricSeries).toHaveBeenCalledTimes(2))
  })
})
