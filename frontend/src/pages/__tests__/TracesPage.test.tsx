import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { BrowserRouter } from 'react-router-dom'
import { TracesPage } from '../TracesPage'
import { api } from '@/lib/api'

// Mock the API module
vi.mock('@/lib/api', () => ({
  api: {
    getServices: vi.fn(),
    getTraces: vi.fn(),
    getTraceSpans: vi.fn(),
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
      recentSpans: [],
      clearRecentSpans: vi.fn(),
    }
    return selector(state)
  }),
}))

const mockTraces = [
  {
    traceId: 'trace-1',
    rootSpan: 'llm/chat',
    serviceName: 'claude-code',
    duration: 1500000000, // 1.5s in nanoseconds
    spanCount: 3,
    status: 'OK',
    startTime: '2024-01-15T10:00:00Z',
  },
  {
    traceId: 'trace-2',
    rootSpan: 'tool/read_file',
    serviceName: 'claude-code',
    duration: 250000000, // 250ms in nanoseconds
    spanCount: 1,
    status: 'ERROR',
    startTime: '2024-01-15T09:55:00Z',
  },
]

const mockSpans = [
  {
    spanId: 'span-1',
    traceId: 'trace-1',
    parentSpanId: '',
    spanName: 'llm/chat',
    spanKind: 'CLIENT',
    timestamp: '2024-01-15T10:00:00Z',
    duration: 1500000000,
    statusCode: 'OK',
    statusMessage: '',
    serviceName: 'claude-code',
    spanAttributes: { model: 'claude-3' },
    events: [],
  },
]

function renderTracesPage() {
  return render(
    <BrowserRouter>
      <TracesPage />
    </BrowserRouter>
  )
}

describe('TracesPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(api.getServices).mockResolvedValue({ services: ['claude-code', 'gemini-cli'] })
    vi.mocked(api.getTraces).mockResolvedValue({ traces: mockTraces, total: 2 })
    vi.mocked(api.getTraceSpans).mockResolvedValue({ spans: mockSpans })
  })

  it('renders the page title and description', async () => {
    renderTracesPage()

    expect(screen.getByRole('heading', { level: 1, name: /traces/i })).toBeInTheDocument()
    expect(screen.getByText(/view and analyze distributed traces/i)).toBeInTheDocument()
  })

  it('shows loading state initially', async () => {
    renderTracesPage()

    expect(screen.getByText(/loading/i)).toBeInTheDocument()
  })

  it('displays traces after loading', async () => {
    renderTracesPage()

    await waitFor(() => {
      expect(screen.getByText('llm/chat')).toBeInTheDocument()
    })

    expect(screen.getByText('tool/read_file')).toBeInTheDocument()
  })

  it('displays trace status badges', async () => {
    renderTracesPage()

    await waitFor(() => {
      expect(screen.getByText('OK')).toBeInTheDocument()
    })

    expect(screen.getByText('ERROR')).toBeInTheDocument()
  })

  it('shows span count for each trace', async () => {
    renderTracesPage()

    await waitFor(() => {
      expect(screen.getByText('3 spans')).toBeInTheDocument()
    })

    expect(screen.getByText('1 spans')).toBeInTheDocument()
  })

  it('displays empty state when no traces found', async () => {
    vi.mocked(api.getTraces).mockResolvedValue({ traces: [], total: 0 })

    renderTracesPage()

    await waitFor(() => {
      expect(screen.getByText(/no traces found/i)).toBeInTheDocument()
    })
  })

  it('populates service filter dropdown', async () => {
    renderTracesPage()

    await waitFor(() => {
      expect(api.getServices).toHaveBeenCalled()
    })

    // Find the service select dropdown
    const selects = screen.getAllByRole('combobox')
    const serviceSelect = selects.find(s => {
      const option = within(s).queryByText(/all services/i)
      return option !== null
    })

    expect(serviceSelect).toBeDefined()
  })

  it('calls API with service filter when service is selected', async () => {
    const user = userEvent.setup()
    renderTracesPage()

    await waitFor(() => {
      expect(screen.queryByText(/loading/i)).not.toBeInTheDocument()
    })

    // Find and interact with service select (first select - DateRangePicker is a button)
    const selects = screen.getAllByRole('combobox')
    const serviceSelect = selects[0]

    await user.selectOptions(serviceSelect, 'claude-code')

    await waitFor(() => {
      expect(api.getTraces).toHaveBeenCalledWith(
        expect.objectContaining({ service: 'claude-code' }),
        expect.any(Object)
      )
    })
  })

  it('expands trace to show spans when clicked', async () => {
    const user = userEvent.setup()
    renderTracesPage()

    await waitFor(() => {
      expect(screen.getByText('llm/chat')).toBeInTheDocument()
    })

    // Click on the first trace to expand it
    const traceRow = screen.getByText('llm/chat').closest('div[class*="cursor-pointer"]')
    expect(traceRow).toBeDefined()

    await user.click(traceRow!)

    // Wait for spans to load
    await waitFor(() => {
      expect(api.getTraceSpans).toHaveBeenCalledWith('trace-1', expect.any(Object))
    })
  })

  it('handles search input', async () => {
    const user = userEvent.setup()
    renderTracesPage()

    await waitFor(() => {
      expect(screen.queryByText(/loading/i)).not.toBeInTheDocument()
    })

    const searchInput = screen.getByPlaceholderText(/search spans, errors, attributes/i)
    await user.type(searchInput, 'error')

    // Wait for debounced search to trigger API call
    await waitFor(() => {
      expect(api.getTraces).toHaveBeenCalledWith(
        expect.objectContaining({ search: 'error' }),
        expect.any(Object)
      )
    }, { timeout: 500 })
  })

  it('handles API error gracefully', async () => {
    vi.mocked(api.getTraces).mockRejectedValue(new Error('Network error'))

    renderTracesPage()

    await waitFor(() => {
      expect(screen.queryByText(/loading/i)).not.toBeInTheDocument()
    })
  })

  it('supports pagination', async () => {
    vi.mocked(api.getTraces).mockResolvedValue({ traces: mockTraces, total: 25 })

    renderTracesPage()

    await waitFor(() => {
      expect(screen.getByText('llm/chat')).toBeInTheDocument()
    })

    // Check pagination is rendered
    expect(screen.getByRole('navigation')).toBeInTheDocument()
  })
})
