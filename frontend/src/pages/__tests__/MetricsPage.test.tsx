import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { BrowserRouter } from 'react-router-dom'
import { MetricsPage } from '../MetricsPage'
import { api } from '@/lib/api'

// Mock the API module
vi.mock('@/lib/api', () => ({
  api: {
    getServices: vi.fn(),
    getMetricNames: vi.fn(),
    getMetricSeries: vi.fn(),
  },
}))

// Mock toast
vi.mock('sonner', () => ({
  toast: {
    error: vi.fn(),
    success: vi.fn(),
  },
}))

// Mock telemetry store
vi.mock('@/stores/telemetryStore', () => ({
  useTelemetryStore: vi.fn((selector) => {
    const state = {
      recentMetrics: [],
    }
    return selector(state)
  }),
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

const mockMetricNames = [
  'claude_code.tokens.total',
  'claude_code.cost.total',
  'gemini_cli.tokens.input',
]

const mockTimeSeries = [
  {
    labels: { service: 'claude-code' },
    datapoints: [
      [1705312800000, 1500],
      [1705312860000, 2300],
      [1705312920000, 1800],
    ],
  },
]

function renderMetricsPage() {
  return render(
    <BrowserRouter>
      <MetricsPage />
    </BrowserRouter>
  )
}

describe('MetricsPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(api.getServices).mockResolvedValue({ services: ['claude-code', 'gemini-cli'] })
    vi.mocked(api.getMetricNames).mockResolvedValue({ names: mockMetricNames })
    vi.mocked(api.getMetricSeries).mockResolvedValue({ series: mockTimeSeries })
  })

  it('renders the page title and description', async () => {
    renderMetricsPage()

    expect(screen.getByRole('heading', { level: 1, name: /metrics/i })).toBeInTheDocument()
    expect(screen.getByText(/view and analyze metric time series/i)).toBeInTheDocument()
  })

  it('populates service filter dropdown', async () => {
    renderMetricsPage()

    await waitFor(() => {
      expect(api.getServices).toHaveBeenCalled()
    })

    const selects = screen.getAllByRole('combobox')
    expect(selects.length).toBeGreaterThanOrEqual(2)
  })

  it('fetches metric names on mount', async () => {
    renderMetricsPage()

    await waitFor(() => {
      expect(api.getMetricNames).toHaveBeenCalled()
    })
  })

  it('displays empty state when no metric is selected', async () => {
    renderMetricsPage()

    await waitFor(() => {
      // The chart area shows this message when no metric is selected
      const selectMetricMessage = screen.getAllByText(/select a metric/i)
      expect(selectMetricMessage.length).toBeGreaterThan(0)
    })
  })

  it('calls API with service filter when service is selected', async () => {
    const user = userEvent.setup()
    renderMetricsPage()

    await waitFor(() => {
      expect(api.getServices).toHaveBeenCalled()
    })

    // Find the service select (first select with "All Services" option)
    const selects = screen.getAllByRole('combobox')
    const serviceSelect = selects[0]

    await user.selectOptions(serviceSelect, 'claude-code')

    await waitFor(() => {
      expect(api.getMetricNames).toHaveBeenCalledWith('claude-code', expect.any(Object))
    })
  })

  it('fetches metric series when a metric is selected', async () => {
    const user = userEvent.setup()
    renderMetricsPage()

    await waitFor(() => {
      expect(api.getMetricNames).toHaveBeenCalled()
    })

    // Find the metric select (second select)
    const selects = screen.getAllByRole('combobox')
    const metricSelect = selects[1]

    await user.selectOptions(metricSelect, 'claude_code.tokens.total')

    await waitFor(() => {
      expect(api.getMetricSeries).toHaveBeenCalledWith(
        expect.objectContaining({ name: 'claude_code.tokens.total' }),
        expect.any(Object)
      )
    })
  })

  it('renders chart component when data is available', async () => {
    const user = userEvent.setup()
    renderMetricsPage()

    await waitFor(() => {
      expect(api.getMetricNames).toHaveBeenCalled()
    })

    // Select a metric
    const selects = screen.getAllByRole('combobox')
    const metricSelect = selects[1]

    await user.selectOptions(metricSelect, 'claude_code.tokens.total')

    await waitFor(() => {
      expect(screen.getByTestId('responsive-container')).toBeInTheDocument()
    })
  })

  it('displays no data message when metric has no data', async () => {
    vi.mocked(api.getMetricSeries).mockResolvedValue({ series: [] })

    const user = userEvent.setup()
    renderMetricsPage()

    await waitFor(() => {
      expect(api.getMetricNames).toHaveBeenCalled()
    })

    // Select a metric
    const selects = screen.getAllByRole('combobox')
    const metricSelect = selects[1]

    await user.selectOptions(metricSelect, 'claude_code.tokens.total')

    await waitFor(() => {
      expect(screen.getByText(/no data available for this metric/i)).toBeInTheDocument()
    })
  })

  it('renders timeframe picker button', async () => {
    renderMetricsPage()

    await waitFor(() => {
      expect(api.getMetricNames).toHaveBeenCalled()
    })

    // DateRangePicker renders as a button with calendar icon, not a select
    // The button shows the current timeframe label
    const timeframeButton = screen.getByRole('button', { name: /last/i })
    expect(timeframeButton).toBeInTheDocument()
  })

  it('handles API error gracefully', async () => {
    vi.mocked(api.getMetricNames).mockRejectedValue(new Error('Network error'))

    renderMetricsPage()

    await waitFor(() => {
      expect(api.getMetricNames).toHaveBeenCalled()
    })

    // Page should still render without crashing
    expect(screen.getByRole('heading', { level: 1, name: /metrics/i })).toBeInTheDocument()
  })

  it('displays stacked/grouped toggle buttons', async () => {
    renderMetricsPage()

    // Look for stacked and grouped buttons by their titles
    expect(screen.getByTitle(/stacked bars/i)).toBeInTheDocument()
    expect(screen.getByTitle(/grouped bars/i)).toBeInTheDocument()
  })

  it('toggles between stacked and grouped chart modes', async () => {
    const user = userEvent.setup()
    renderMetricsPage()

    // Find the grouped button and click it
    const groupedButton = screen.getByTitle(/grouped bars/i)
    await user.click(groupedButton)

    // The grouped button should now be active (secondary variant)
    expect(groupedButton).toHaveClass('bg-secondary')
  })

  it('displays metric card with title section', async () => {
    renderMetricsPage()

    // The card should show "Select a metric" somewhere in the UI (in card title or select option)
    const selectMetricElements = screen.getAllByText(/select a metric/i)
    expect(selectMetricElements.length).toBeGreaterThan(0)
  })

  it('shows info message when no metrics are available', async () => {
    vi.mocked(api.getMetricNames).mockResolvedValue({ names: [] })

    renderMetricsPage()

    await waitFor(() => {
      expect(screen.getByText(/no metrics data available yet/i)).toBeInTheDocument()
    })
  })
})
