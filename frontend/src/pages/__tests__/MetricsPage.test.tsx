import { describe, it, expect, vi, beforeEach } from 'vitest'
import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useLocation } from 'react-router-dom'
import { MetricsPage } from '../MetricsPage'
import { api } from '@/lib/api'
import { httpError } from '@/lib/errors'
import { renderWithProviders } from '@/test/queryWrapper'
import type { MetricDataPoint, MetricsResponse } from '@/types/metrics'

vi.mock('@/lib/api', () => ({
  api: {
    getServices: vi.fn(),
    getMetricNames: vi.fn(),
    getMetricSeries: vi.fn(),
    getMetricAttributeKeys: vi.fn(),
    getMetrics: vi.fn(),
  },
}))

vi.mock('sonner', () => ({
  toast: { error: vi.fn(), success: vi.fn() },
}))

// Mock recharts to avoid canvas issues in tests
vi.mock('recharts', () => ({
  ResponsiveContainer: ({ children }: { children: React.ReactNode }) => <div data-testid="responsive-container">{children}</div>,
  BarChart: ({ children }: { children: React.ReactNode }) => <div data-testid="bar-chart">{children}</div>,
  Bar: () => null,
  XAxis: () => null,
  YAxis: () => null,
  CartesianGrid: () => null,
  Tooltip: () => null,
  Legend: () => null,
}))

const METRIC = 'claude_code.token.usage'
const mockMetricNames = [METRIC, 'claude_code.cost.usage', 'gemini_cli.token.usage']

const mockTimeSeries = [
  {
    name: METRIC,
    labels: { service: 'claude-code', type: 'input' },
    datapoints: [
      [1705312800000, 1500],
      [1705312860000, 2300],
    ] as [number, number][],
  },
]

const point = (value: number, overrides: Partial<MetricDataPoint> = {}): MetricDataPoint => ({
  timestamp: '2026-11-11T10:00:00Z',
  serviceName: 'claude-code',
  metricName: METRIC,
  metricType: 'sum',
  value,
  aggregationTemporality: 2,
  attributes: { type: 'input', model: 'claude-opus' },
  ...overrides,
})

function LocationProbe() {
  const location = useLocation()
  return <div data-testid="location">{location.search}</div>
}

function renderMetricsPage(route = '/metrics') {
  return renderWithProviders(
    <>
      <MetricsPage />
      <LocationProbe />
    </>,
    { route }
  )
}

const params = () => new URLSearchParams(screen.getByTestId('location').textContent ?? '')

describe('MetricsPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    localStorage.clear()
    vi.mocked(api.getServices).mockResolvedValue({ services: ['claude-code', 'gemini_cli'] })
    vi.mocked(api.getMetricNames).mockResolvedValue({ names: mockMetricNames })
    vi.mocked(api.getMetricSeries).mockResolvedValue({ series: mockTimeSeries })
    vi.mocked(api.getMetrics).mockResolvedValue({ metrics: [], total: 0, hasMore: false })
    vi.mocked(api.getMetricAttributeKeys).mockResolvedValue({ keys: ['model', 'repository', 'type'] })
  })

  it('renders the page title, description and tab title', async () => {
    renderMetricsPage()

    expect(screen.getByRole('heading', { level: 1, name: /metrics/i })).toBeInTheDocument()
    expect(screen.getByText(/view and analyze metric time series/i)).toBeInTheDocument()
    expect(document.title).toBe('Metrics · AI Observer')
  })

  it('labels the filters and lists services by their display names', async () => {
    renderMetricsPage()

    const service = screen.getByLabelText('Service')
    await waitFor(() => expect(within(service).getByRole('option', { name: 'Gemini CLI' })).toBeInTheDocument())
    expect(screen.getByLabelText('Metric')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /Timeframe: Last 15 minutes/ })).toBeInTheDocument()
  })

  it('asks to select a metric and fetches no series without one', async () => {
    renderMetricsPage()

    await waitFor(() => expect(api.getMetricNames).toHaveBeenCalledWith(undefined, expect.anything()))
    expect(screen.getByText('Select a metric to view data')).toBeInTheDocument()
    expect(api.getMetricSeries).not.toHaveBeenCalled()
  })

  it('keeps the selected metric and service in the URL', async () => {
    const user = userEvent.setup()
    renderMetricsPage()
    const metricSelect = screen.getByLabelText('Metric')
    await waitFor(() => expect(within(metricSelect).getAllByRole('option').length).toBeGreaterThan(1))

    await user.selectOptions(metricSelect, METRIC)
    await waitFor(() =>
      expect(api.getMetricSeries).toHaveBeenCalledWith(expect.objectContaining({ name: METRIC }), expect.anything())
    )
    expect(params().get('metric')).toBe(METRIC)

    await user.selectOptions(screen.getByLabelText('Service'), 'claude-code')
    await waitFor(() => expect(api.getMetricNames).toHaveBeenCalledWith('claude-code', expect.anything()))
    expect(params().get('service')).toBe('claude-code')
    // A Claude Code metric stays selected for Claude Code
    expect(params().get('metric')).toBe(METRIC)

    // ...but not for Gemini CLI
    await user.selectOptions(screen.getByLabelText('Service'), 'gemini_cli')
    expect(params().get('metric')).toBeNull()
  })

  it('restores the chart from the URL', async () => {
    renderMetricsPage(`/metrics?service=claude-code&metric=${METRIC}&timeframe=24h&stacked=false`)

    await waitFor(() =>
      expect(api.getMetricSeries).toHaveBeenCalledWith(
        expect.objectContaining({ name: METRIC, service: 'claude-code', intervalSeconds: 3600 }),
        expect.anything()
      )
    )
    const [request] = vi.mocked(api.getMetricSeries).mock.calls[0]
    expect(new Date(request.to!).getTime() - new Date(request.from!).getTime()).toBe(86_400_000)
    await waitFor(() => expect(screen.getByLabelText('Metric')).toHaveValue(METRIC))
    expect(screen.getByLabelText('Service')).toHaveValue('claude-code')
    expect(screen.getByRole('button', { name: /Timeframe: Last 24 hours/ })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Grouped bars' })).toHaveAttribute('aria-pressed', 'true')
    expect(await screen.findByTestId('responsive-container')).toBeInTheDocument()
  })

  it('toggles between stacked and grouped bars, kept in the URL', async () => {
    const user = userEvent.setup()
    renderMetricsPage(`/metrics?metric=${METRIC}`)

    const stacked = screen.getByRole('button', { name: 'Stacked bars' })
    const grouped = screen.getByRole('button', { name: 'Grouped bars' })
    expect(stacked).toHaveAttribute('aria-pressed', 'true')
    expect(grouped).toHaveAttribute('aria-pressed', 'false')

    await user.click(grouped)

    expect(grouped).toHaveAttribute('aria-pressed', 'true')
    expect(grouped).toHaveClass('bg-secondary')
    expect(params().get('stacked')).toBe('false')
  })

  it('breaks the series down by an attribute on the server, kept in the URL', async () => {
    const user = userEvent.setup()
    renderMetricsPage(`/metrics?metric=${METRIC}`)
    await waitFor(() =>
      expect(api.getMetricSeries).toHaveBeenCalledWith(
        expect.objectContaining({ name: METRIC, timeZone: Intl.DateTimeFormat().resolvedOptions().timeZone }),
        expect.anything()
      )
    )
    expect(vi.mocked(api.getMetricSeries).mock.calls[0][0].groupBy).toBeUndefined()

    // The catalog's breakdowns by name, then the other attributes the points have
    const breakdown = screen.getByLabelText('Breakdown')
    await waitFor(() => expect(within(breakdown).getByRole('option', { name: 'repository' })).toBeInTheDocument())
    expect(api.getMetricAttributeKeys).toHaveBeenCalledWith({ name: METRIC }, expect.anything())
    await user.selectOptions(breakdown, 'repository')

    await waitFor(() =>
      expect(api.getMetricSeries).toHaveBeenLastCalledWith(expect.objectContaining({ groupBy: ['repository'] }), expect.anything())
    )
    expect(params().get('groupBy')).toBe('repository')
    // Counters have no statistic to choose
    expect(screen.queryByLabelText('Statistic')).not.toBeInTheDocument()
  })

  it('shows a histogram as the chosen statistic, side by side', async () => {
    const user = userEvent.setup()
    const latency = 'gemini_cli.api.request.latency'
    vi.mocked(api.getMetricNames).mockResolvedValue({ names: [latency] })
    vi.mocked(api.getMetricSeries).mockImplementation(async (request) => ({
      series: [{ name: latency, labels: { service: 'gemini_cli' }, datapoints: [[1705312800000, 120], [1705312860000, null]] }],
      metricType: 'histogram',
      stat: request.stat ?? 'avg',
    }))
    renderMetricsPage(`/metrics?metric=${latency}`)

    // The server's default (the average) is not asked for
    await waitFor(() => expect(api.getMetricSeries).toHaveBeenCalled())
    expect(vi.mocked(api.getMetricSeries).mock.calls[0][0].stat).toBeUndefined()
    // The statistic the server computed, as a badge by the title
    const badge = (text: string) => screen.queryAllByText(text).find((el) => el.tagName !== 'OPTION')
    await waitFor(() => expect(badge('Average')).toBeInTheDocument())
    // Averages do not add up: no stacking
    expect(screen.queryByRole('button', { name: 'Stacked bars' })).not.toBeInTheDocument()

    await user.selectOptions(screen.getByLabelText('Statistic'), 'p95')
    await waitFor(() =>
      expect(api.getMetricSeries).toHaveBeenLastCalledWith(expect.objectContaining({ stat: 'p95' }), expect.anything())
    )
    expect(params().get('stat')).toBe('p95')
    await waitFor(() => expect(badge('95th percentile')).toBeInTheDocument())

    // A count is a sum again
    await user.selectOptions(screen.getByLabelText('Statistic'), 'count')
    expect(await screen.findByRole('button', { name: 'Stacked bars' })).toBeInTheDocument()
  })

  it('says when the metric has no data in the range', async () => {
    vi.mocked(api.getMetricSeries).mockResolvedValue({ series: [] })
    renderMetricsPage(`/metrics?metric=${METRIC}`)

    expect(await screen.findByText(/no data available for this metric/i)).toBeInTheDocument()
  })

  it('shows a failed series request inline with a retry', async () => {
    const user = userEvent.setup()
    vi.mocked(api.getMetricSeries).mockRejectedValueOnce(httpError(504))
    renderMetricsPage(`/metrics?metric=${METRIC}`)

    expect(await screen.findByText("Couldn't load the metric")).toBeInTheDocument()
    expect(screen.getByText(/The request took too long/)).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Retry' }))
    expect(await screen.findByTestId('responsive-container')).toBeInTheDocument()
  })

  it('shows failed metric names inline', async () => {
    vi.mocked(api.getMetricNames).mockRejectedValue(new TypeError('Failed to fetch'))
    renderMetricsPage()

    expect(await screen.findByText("Couldn't load the metric names")).toBeInTheDocument()
    expect(screen.getByText('Cannot reach the AI Observer server.')).toBeInTheDocument()
  })

  it('says whether the chart follows new data', async () => {
    const { unmount } = renderMetricsPage()
    expect(screen.getByText('Live')).toBeInTheDocument()
    unmount()

    renderMetricsPage('/metrics?from=2026-01-01T00:00:00.000Z&to=2026-01-02T00:00:00.000Z')
    expect(screen.getByText('Paused (fixed range)')).toBeInTheDocument()
    expect(screen.queryByText(/real-time updates/)).not.toBeInTheDocument()
  })

  it('shows info message when no metrics are available', async () => {
    vi.mocked(api.getMetricNames).mockResolvedValue({ names: [] })
    renderMetricsPage()

    expect(await screen.findByText(/no metrics data available yet/i)).toBeInTheDocument()
  })

  it('lists the data points of the metric a page at a time', async () => {
    const user = userEvent.setup()
    const firstPage: MetricsResponse = {
      metrics: [point(1500), point(2300, { timestamp: '2026-11-11T09:59:00Z', attributes: { type: 'output' } })],
      total: 3,
      hasMore: true,
      nextCursor: 'cursor-2',
    }
    const secondPage: MetricsResponse = {
      metrics: [point(7, { metricType: 'histogram', value: undefined, count: 4, sum: 12.5, aggregationTemporality: 1, attributes: {} })],
      total: 3,
      hasMore: false,
    }
    vi.mocked(api.getMetrics).mockImplementation(async (request) => (request?.cursor ? secondPage : firstPage))
    renderMetricsPage(`/metrics?metric=${METRIC}&service=claude-code`)

    expect(api.getMetrics).not.toHaveBeenCalled()
    await user.click(screen.getByRole('tab', { name: 'Data points' }))
    expect(params().get('view')).toBe('points')

    const table = await screen.findByRole('table')
    expect(within(table).getByText('1,500')).toBeInTheDocument()
    expect(within(table).getAllByText('(cumulative)')).toHaveLength(2)
    expect(within(table).getByText('model=claude-opus')).toBeInTheDocument()
    expect(api.getMetrics).toHaveBeenCalledWith(
      expect.objectContaining({ name: METRIC, service: 'claude-code', limit: 25, cursor: undefined }),
      expect.anything()
    )

    // A link or a button, depending on the pagination component's version
    await user.click(screen.getByLabelText('Go to next page'))

    await waitFor(() =>
      expect(api.getMetrics).toHaveBeenLastCalledWith(expect.objectContaining({ cursor: 'cursor-2' }), expect.anything())
    )
    expect(await screen.findByText('count 4 · sum 12.5')).toBeInTheDocument()
    expect(screen.getByText('(delta)')).toBeInTheDocument()
    expect(screen.getByText('Page 2 of 2')).toBeInTheDocument()
  })
})
