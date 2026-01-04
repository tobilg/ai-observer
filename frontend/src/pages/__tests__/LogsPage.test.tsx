import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { BrowserRouter } from 'react-router-dom'
import { LogsPage } from '../LogsPage'
import { api } from '@/lib/api'

// Mock the API module
vi.mock('@/lib/api', () => ({
  api: {
    getServices: vi.fn(),
    getLogs: vi.fn(),
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
      recentLogs: [],
      clearRecentLogs: vi.fn(),
    }
    return selector(state)
  }),
}))

const mockLogs = [
  {
    timestamp: '2024-01-15T10:00:00Z',
    serviceName: 'claude-code',
    severityText: 'INFO',
    body: 'Starting conversation with user',
    traceId: 'trace-1',
    spanId: 'span-1',
    logAttributes: { session_id: 'session-123' },
  },
  {
    timestamp: '2024-01-15T09:55:00Z',
    serviceName: 'claude-code',
    severityText: 'ERROR',
    body: 'Failed to read file: permission denied',
    traceId: 'trace-2',
    spanId: 'span-2',
    logAttributes: { file_path: '/etc/passwd' },
  },
  {
    timestamp: '2024-01-15T09:50:00Z',
    serviceName: 'gemini-cli',
    severityText: 'WARN',
    body: 'Rate limit approaching',
    traceId: '',
    spanId: '',
    logAttributes: {},
  },
]

function renderLogsPage() {
  return render(
    <BrowserRouter>
      <LogsPage />
    </BrowserRouter>
  )
}

describe('LogsPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(api.getServices).mockResolvedValue({ services: ['claude-code', 'gemini-cli'] })
    vi.mocked(api.getLogs).mockResolvedValue({ logs: mockLogs, total: 3 })
  })

  it('renders the page title and description', async () => {
    renderLogsPage()

    expect(screen.getByRole('heading', { level: 1, name: /logs/i })).toBeInTheDocument()
    expect(screen.getByText(/view and search log records/i)).toBeInTheDocument()
  })

  it('shows loading state initially', async () => {
    renderLogsPage()

    expect(screen.getByText(/loading/i)).toBeInTheDocument()
  })

  it('displays logs after loading', async () => {
    renderLogsPage()

    await waitFor(() => {
      expect(screen.getByText(/starting conversation with user/i)).toBeInTheDocument()
    })

    expect(screen.getByText(/failed to read file/i)).toBeInTheDocument()
    expect(screen.getByText(/rate limit approaching/i)).toBeInTheDocument()
  })

  it('displays severity badges with correct text', async () => {
    renderLogsPage()

    await waitFor(() => {
      // INFO appears as a badge in the log entry
      const infoBadges = screen.getAllByText('INFO')
      expect(infoBadges.length).toBeGreaterThan(0)
    })

    // ERROR appears both as severity option and in log entry badge
    const errorElements = screen.getAllByText('ERROR')
    expect(errorElements.length).toBeGreaterThan(0)

    // WARN appears both as severity option and in log entry badge
    const warnElements = screen.getAllByText('WARN')
    expect(warnElements.length).toBeGreaterThan(0)
  })

  it('displays empty state when no logs found', async () => {
    vi.mocked(api.getLogs).mockResolvedValue({ logs: [], total: 0 })

    renderLogsPage()

    await waitFor(() => {
      expect(screen.getByText(/no logs found/i)).toBeInTheDocument()
    })
  })

  it('populates service filter dropdown', async () => {
    renderLogsPage()

    await waitFor(() => {
      expect(api.getServices).toHaveBeenCalled()
    })

    const selects = screen.getAllByRole('combobox')
    expect(selects.length).toBeGreaterThan(0)
  })

  it('calls API with service filter when service is selected', async () => {
    const user = userEvent.setup()
    renderLogsPage()

    await waitFor(() => {
      expect(screen.queryByText(/loading/i)).not.toBeInTheDocument()
    })

    // Find the service select (first select - DateRangePicker is a button, not a select)
    const selects = screen.getAllByRole('combobox')
    const serviceSelect = selects[0]

    await user.selectOptions(serviceSelect, 'claude-code')

    await waitFor(() => {
      expect(api.getLogs).toHaveBeenCalledWith(
        expect.objectContaining({ service: 'claude-code' }),
        expect.any(Object)
      )
    })
  })

  it('calls API with severity filter when severity is selected', async () => {
    const user = userEvent.setup()
    renderLogsPage()

    await waitFor(() => {
      expect(screen.queryByText(/loading/i)).not.toBeInTheDocument()
    })

    // Find the severity select (second select after service - DateRangePicker is a button)
    const selects = screen.getAllByRole('combobox')
    const severitySelect = selects[1]

    await user.selectOptions(severitySelect, 'ERROR')

    await waitFor(() => {
      expect(api.getLogs).toHaveBeenCalledWith(
        expect.objectContaining({ severity: 'ERROR' }),
        expect.any(Object)
      )
    })
  })

  it('handles search input with debounce', async () => {
    const user = userEvent.setup()
    renderLogsPage()

    await waitFor(() => {
      expect(screen.queryByText(/loading/i)).not.toBeInTheDocument()
    })

    const searchInput = screen.getByPlaceholderText(/search log messages/i)
    await user.type(searchInput, 'error')

    // Wait for debounced search to trigger API call
    await waitFor(() => {
      expect(api.getLogs).toHaveBeenCalledWith(
        expect.objectContaining({ search: 'error' }),
        expect.any(Object)
      )
    }, { timeout: 500 })
  })

  it('expands log entry when clicked', async () => {
    const user = userEvent.setup()
    renderLogsPage()

    await waitFor(() => {
      expect(screen.getByText(/starting conversation with user/i)).toBeInTheDocument()
    })

    // Click on a log entry to expand it
    const logEntry = screen.getByText(/starting conversation with user/i).closest('div[role="button"]')
    expect(logEntry).toBeDefined()

    await user.click(logEntry!)

    // Check that expanded details are shown
    await waitFor(() => {
      expect(screen.getByText(/trace id/i)).toBeInTheDocument()
    })
  })

  it('shows log attributes when expanded', async () => {
    const user = userEvent.setup()
    renderLogsPage()

    await waitFor(() => {
      expect(screen.getByText(/starting conversation with user/i)).toBeInTheDocument()
    })

    // Click on first log entry
    const logEntry = screen.getByText(/starting conversation with user/i).closest('div[role="button"]')
    await user.click(logEntry!)

    // Check for attributes section
    await waitFor(() => {
      expect(screen.getByText(/attributes/i)).toBeInTheDocument()
    })
  })

  it('handles API error gracefully', async () => {
    vi.mocked(api.getLogs).mockRejectedValue(new Error('Network error'))

    renderLogsPage()

    await waitFor(() => {
      expect(screen.queryByText(/loading/i)).not.toBeInTheDocument()
    })
  })

  it('supports pagination', async () => {
    vi.mocked(api.getLogs).mockResolvedValue({ logs: mockLogs, total: 50 })

    renderLogsPage()

    await waitFor(() => {
      expect(screen.getByText(/starting conversation with user/i)).toBeInTheDocument()
    })

    // Check pagination is rendered
    expect(screen.getByRole('navigation')).toBeInTheDocument()
  })

  it('collapses expanded log when clicking again', async () => {
    const user = userEvent.setup()
    renderLogsPage()

    await waitFor(() => {
      expect(screen.getByText(/starting conversation with user/i)).toBeInTheDocument()
    })

    // Click to expand
    const logEntry = screen.getByText(/starting conversation with user/i).closest('div[role="button"]')
    await user.click(logEntry!)

    await waitFor(() => {
      expect(screen.getByText(/trace id/i)).toBeInTheDocument()
    })

    // Click again to collapse
    await user.click(logEntry!)

    await waitFor(() => {
      expect(screen.queryByText(/trace id/i)).not.toBeInTheDocument()
    })
  })
})
