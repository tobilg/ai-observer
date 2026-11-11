import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { act, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { Route, Routes, useLocation } from 'react-router-dom'
import { LogsPage } from '../LogsPage'
import { api } from '@/lib/api'
import { httpError } from '@/lib/errors'
import { useTelemetryStore } from '@/stores/telemetryStore'
import { renderWithProviders } from '@/test/queryWrapper'
import type { LogRecord, LogsResponse } from '@/types/logs'

vi.mock('@/lib/api', () => ({
  api: {
    getServices: vi.fn(),
    getLogs: vi.fn(),
    getLogLevels: vi.fn(),
  },
}))

type LogParams = Parameters<typeof api.getLogs>[0] & object

const logA: LogRecord = {
  timestamp: '2026-01-15T10:00:00Z',
  serviceName: 'claude-code',
  severityText: 'INFO',
  body: 'claude_code.api_request',
  traceId: 'trace-abc',
  spanId: 'span-123',
  sessionId: 'session-1',
  logAttributes: { model: 'claude-sonnet-4' },
  resourceAttributes: { 'host.name': 'laptop' },
}
const logB: LogRecord = {
  timestamp: '2026-01-15T09:59:00Z',
  serviceName: 'gemini_cli',
  severityText: 'ERROR',
  body: 'gemini_cli.api_error',
}
const logC: LogRecord = {
  timestamp: '2026-01-15T09:00:00Z',
  serviceName: 'claude-code',
  severityText: 'WARN',
  body: 'an older log on page 2',
}

const page1: LogsResponse = { logs: [logA, logB], total: 25, hasMore: true, nextCursor: 'c2' }
const page2: LogsResponse = { logs: [logC], total: 25, hasMore: false }

// Calls of the list (not of the "N new" count, which asks for 1 row)
function listCalls(): LogParams[] {
  return vi.mocked(api.getLogs).mock.calls.map(([params]) => params as LogParams).filter((p) => p.limit !== 1)
}
function countCalls(): LogParams[] {
  return vi.mocked(api.getLogs).mock.calls.map(([params]) => params as LogParams).filter((p) => p.limit === 1)
}

let newCount = 0

function LocationProbe() {
  const location = useLocation()
  return <output data-testid="location">{location.pathname + location.search}</output>
}

function StateProbe() {
  const location = useLocation()
  return <output data-testid="detail">{`${location.pathname} from=${(location.state as { from?: string } | null)?.from}`}</output>
}

function renderPage(route = '/logs') {
  return renderWithProviders(
    <Routes>
      <Route
        path="/logs"
        element={
          <>
            <LogsPage />
            <LocationProbe />
          </>
        }
      />
      <Route path="/traces/:kind/:id" element={<StateProbe />} />
      <Route path="/sessions/:sessionId" element={<StateProbe />} />
    </Routes>,
    { route },
  )
}

function rowButton(text: string) {
  return screen.getByText(text, { selector: 'span' }).closest('button') as HTMLButtonElement
}

function announceLogs(timestamps: number[]) {
  act(() => {
    useTelemetryStore.getState().addLogs({ count: timestamps.length, services: ['claude-code'], timestamps })
  })
}

// Logs stored after the shown list was fetched (and before the user looks)
async function announceNewLogs(n: number) {
  await new Promise((resolve) => setTimeout(resolve, 5))
  announceLogs(Array.from({ length: n }, () => Date.now()))
}

describe('LogsPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    localStorage.clear()
    newCount = 0
    useTelemetryStore.setState({ recentLogTimes: [], recentSpanTimes: [], logsUpdateCount: 0, spansUpdateCount: 0 })
    vi.mocked(api.getServices).mockResolvedValue({ services: ['claude-code', 'gemini_cli'] })
    vi.mocked(api.getLogLevels).mockResolvedValue({ INFO: 1200, ERROR: 3 })
    vi.mocked(api.getLogs).mockImplementation(async (params = {}) => {
      if (params.limit === 1) {
        return { logs: [], total: newCount, hasMore: false }
      }
      return params.cursor === 'c2' ? page2 : page1
    })
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('renders the header and sets the tab title', async () => {
    renderPage()
    expect(screen.getByRole('heading', { level: 1, name: 'Logs' })).toBeInTheDocument()
    expect(screen.getByText('View and search logs from your AI coding tools')).toBeInTheDocument()
    expect(document.title).toBe('Logs · AI Observer')
    await screen.findByText('claude_code.api_request', { selector: 'span' })
  })

  it('shows a skeleton on the first load only, then the rows', async () => {
    renderPage()
    expect(screen.getByRole('status', { name: 'Loading logs' })).toBeInTheDocument()
    await screen.findByText('claude_code.api_request', { selector: 'span' })
    expect(screen.queryByRole('status', { name: 'Loading logs' })).not.toBeInTheDocument()
    expect(within(screen.getByRole('list', { name: 'Logs' })).getByText('Gemini CLI')).toBeInTheDocument()
  })

  it('keeps the shown page dimmed while the next loads, without taking its cursor', async () => {
    const user = userEvent.setup()
    let resolvePage2: (value: LogsResponse) => void = () => {}
    vi.mocked(api.getLogs).mockImplementation((params = {}) =>
      params.cursor === 'c2' ? new Promise((resolve) => (resolvePage2 = resolve)) : Promise.resolve(page1),
    )
    renderPage()
    await screen.findByText('claude_code.api_request', { selector: 'span' })

    await user.click(screen.getByRole('button', { name: 'Go to next page' }))

    // Page 1's rows stay, marked busy; Next waits for page 2's own cursor
    expect(screen.getByText('claude_code.api_request', { selector: 'span' })).toBeInTheDocument()
    expect(screen.getByRole('list', { name: 'Logs' }).closest('[aria-busy]')).toHaveAttribute('aria-busy', 'true')
    expect(screen.getByRole('button', { name: 'Go to next page' })).toBeDisabled()
    expect(screen.queryByRole('status', { name: 'Loading logs' })).not.toBeInTheDocument()

    await act(async () => resolvePage2({ ...page2, hasMore: true, nextCursor: 'c3' }))
    await screen.findByText('an older log on page 2', { selector: 'span' })
    expect(screen.getByRole('button', { name: 'Go to next page' })).toBeEnabled()
    expect(screen.getByText('Page 2 of 3')).toBeInTheDocument()
  })

  it('shows the error with its message and a retry, not an empty list', async () => {
    const user = userEvent.setup()
    vi.mocked(api.getLogs).mockRejectedValueOnce(httpError(500, 'database is locked'))
    renderPage()

    const alert = await screen.findByRole('alert')
    expect(alert).toHaveTextContent("Couldn't load logs")
    expect(alert).toHaveTextContent('database is locked')
    expect(screen.queryByText(/no logs/i)).not.toBeInTheDocument()

    await user.click(within(alert).getByRole('button', { name: 'Retry' }))
    await screen.findByText('claude_code.api_request', { selector: 'span' })
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })

  it('shows the authentication hint for 401', async () => {
    vi.mocked(api.getLogs).mockRejectedValue(httpError(401))
    renderPage()
    expect(await screen.findByRole('alert')).toHaveTextContent('?token=')
  })

  it('shows an empty state that differs from errors', async () => {
    vi.mocked(api.getLogs).mockResolvedValue({ logs: [], total: 0, hasMore: false })
    renderPage()
    expect(await screen.findByText('No logs in this time range')).toBeInTheDocument()
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })

  it('reads all filters from the URL and writes changes back', async () => {
    const user = userEvent.setup()
    renderPage('/logs?service=claude-code&severity=ERROR&search=api&timeframe=24h')

    await waitFor(() => expect(listCalls().length).toBeGreaterThan(0))
    expect(listCalls()[0]).toMatchObject({ service: 'claude-code', severity: 'ERROR', search: 'api', limit: 10 })
    const first = listCalls()[0]
    expect(Date.parse(first.to!) - Date.parse(first.from!)).toBe(24 * 3600_000)
    expect(screen.getByRole('combobox', { name: 'Service' })).toHaveValue('claude-code')
    expect(screen.getByRole('combobox', { name: 'Level' })).toHaveValue('ERROR')
    expect(screen.getByRole('searchbox', { name: 'Search logs' })).toHaveValue('api')
    expect(screen.getByRole('button', { name: 'Time range: Last 24 hours' })).toBeInTheDocument()

    await user.selectOptions(screen.getByRole('combobox', { name: 'Level' }), '')
    const location = screen.getByTestId('location').textContent!
    expect(location).toContain('service=claude-code')
    expect(location).toContain('search=api')
    expect(location).toContain('timeframe=24h')
    expect(location).not.toContain('severity=')
  })

  it('offers the levels of the selected range', async () => {
    renderPage()
    const level = screen.getByRole('combobox', { name: 'Level' })
    // While the levels load: the usual ones
    expect(within(level).getByRole('option', { name: 'DEBUG' })).toBeInTheDocument()

    await within(level).findByRole('option', { name: 'INFO (1,200)' })
    expect(within(level).getByRole('option', { name: 'ERROR (3)' })).toBeInTheDocument()
    expect(within(level).queryByRole('option', { name: 'DEBUG' })).not.toBeInTheDocument()
  })

  it('loads the new logs on page 1 when "N new logs" is clicked', async () => {
    const user = userEvent.setup()
    renderPage()
    await screen.findByText('claude_code.api_request', { selector: 'span' })
    const loadedAt = Date.now()

    newCount = 2
    await announceNewLogs(2)
    const button = await screen.findByRole('button', { name: '2 new logs' })
    const before = listCalls().length

    await user.click(button)

    await waitFor(() => expect(listCalls().length).toBe(before + 1))
    const refetch = listCalls()[before]
    expect(refetch.cursor).toBeUndefined()
    expect(Date.parse(refetch.to!)).toBeGreaterThanOrEqual(loadedAt)
    expect(screen.queryByRole('button', { name: /new logs?$/ })).not.toBeInTheDocument()
  })

  it('goes back to page 1 when "N new logs" is clicked on a later page', async () => {
    const user = userEvent.setup()
    renderPage()
    await screen.findByText('claude_code.api_request', { selector: 'span' })
    await user.click(screen.getByRole('button', { name: 'Go to next page' }))
    await screen.findByText('an older log on page 2', { selector: 'span' })

    newCount = 1
    await announceNewLogs(1)
    await user.click(await screen.findByRole('button', { name: '1 new log' }))

    await screen.findByText('claude_code.api_request', { selector: 'span' })
    expect(screen.getByText('Page 1 of 3')).toBeInTheDocument()
    expect(listCalls().at(-1)?.cursor).toBeUndefined()
  })

  it('counts new logs with the filters of the list', async () => {
    renderPage('/logs?severity=ERROR&service=gemini_cli')
    await screen.findByText('claude_code.api_request', { selector: 'span' })

    newCount = 4
    await announceNewLogs(1)
    await screen.findByRole('button', { name: '4 new logs' })
    expect(countCalls()[0]).toMatchObject({ severity: 'ERROR', service: 'gemini_cli', limit: 1 })
  })

  it('shows no "new" count while records are older than the list', async () => {
    renderPage()
    await screen.findByText('claude_code.api_request', { selector: 'span' })
    announceLogs([Date.now() - 60_000])
    await new Promise((resolve) => setTimeout(resolve, 400))
    expect(countCalls()).toHaveLength(0)
    expect(screen.queryByRole('button', { name: /new log/ })).not.toBeInTheDocument()
  })

  it('moves a relative window once a minute on page 1, resolving it when fetching', async () => {
    vi.useFakeTimers({ toFake: ['Date', 'setInterval', 'clearInterval'] })
    vi.setSystemTime(new Date('2026-03-01T12:00:00Z'))
    renderPage('/logs?timeframe=1h')
    await vi.waitFor(() => expect(listCalls()).toHaveLength(1))
    // (vi.waitFor moves fake timers on by its polling interval)
    const firstTo = Date.parse(listCalls()[0].to!)
    expect(firstTo - Date.parse('2026-03-01T12:00:00Z')).toBeLessThan(1_000)

    await act(async () => {
      vi.advanceTimersByTime(60_000)
    })

    await vi.waitFor(() => expect(listCalls()).toHaveLength(2))
    const second = listCalls()[1]
    expect(Date.parse(second.to!) - firstTo).toBeGreaterThanOrEqual(60_000)
    expect(Date.parse(second.to!) - Date.parse(second.from!)).toBe(3600_000)
    expect(second.cursor).toBeUndefined()
  })

  it('keeps later pages as loaded', async () => {
    vi.useFakeTimers({ toFake: ['Date', 'setInterval', 'clearInterval'] })
    vi.setSystemTime(new Date('2026-03-01T12:00:00Z'))
    renderPage('/logs?timeframe=1h')
    await vi.waitFor(() => expect(screen.getByRole('button', { name: 'Go to next page' })).toBeEnabled())
    act(() => screen.getByRole('button', { name: 'Go to next page' }).click())
    await vi.waitFor(() => expect(listCalls()).toHaveLength(2))

    await act(async () => {
      vi.advanceTimersByTime(180_000)
    })
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 20))
    })
    expect(listCalls()).toHaveLength(2)
  })

  it('expands a row by its identity, keeps it across a refetch and closes it on another page', async () => {
    const user = userEvent.setup()
    renderPage()
    await screen.findByText('claude_code.api_request', { selector: 'span' })

    const row = rowButton('claude_code.api_request')
    expect(row).toHaveAttribute('aria-expanded', 'false')
    await user.click(row)
    expect(row).toHaveAttribute('aria-expanded', 'true')
    const details = document.getElementById(row.getAttribute('aria-controls')!)
    expect(details).toBeInTheDocument()

    // A refetch puts a new log first: the same log stays open, not the first row
    const newer: LogRecord = { ...logB, timestamp: '2026-01-15T10:05:00Z', body: 'a newer log' }
    vi.mocked(api.getLogs).mockImplementation(async (params = {}) =>
      params.limit === 1 ? { logs: [], total: 1, hasMore: false } : params.cursor === 'c2' ? page2 : { ...page1, logs: [newer, logA, logB] },
    )
    newCount = 1
    await announceNewLogs(1)
    await user.click(await screen.findByRole('button', { name: '1 new log' }))
    await screen.findByText('a newer log', { selector: 'span' })
    expect(rowButton('a newer log')).toHaveAttribute('aria-expanded', 'false')
    expect(rowButton('claude_code.api_request')).toHaveAttribute('aria-expanded', 'true')

    // Another page: nothing open, also when coming back
    await user.click(screen.getByRole('button', { name: 'Go to next page' }))
    await screen.findByText('an older log on page 2', { selector: 'span' })
    expect(rowButton('an older log on page 2')).toHaveAttribute('aria-expanded', 'false')
    await user.click(screen.getByRole('button', { name: 'Go to previous page' }))
    await screen.findByText('claude_code.api_request', { selector: 'span' })
    expect(rowButton('claude_code.api_request')).toHaveAttribute('aria-expanded', 'false')
  })

  it('closes the expanded row when a filter changes', async () => {
    const user = userEvent.setup()
    renderPage()
    await screen.findByText('claude_code.api_request', { selector: 'span' })
    await user.click(rowButton('claude_code.api_request'))

    await user.selectOptions(screen.getByRole('combobox', { name: 'Service' }), 'claude-code')

    await waitFor(() => expect(listCalls().at(-1)?.service).toBe('claude-code'))
    expect(rowButton('claude_code.api_request')).toHaveAttribute('aria-expanded', 'false')
  })

  it('toggles a row with the keyboard', async () => {
    const user = userEvent.setup()
    renderPage()
    await screen.findByText('claude_code.api_request', { selector: 'span' })
    rowButton('claude_code.api_request').focus()
    await user.keyboard('{Enter}')
    expect(rowButton('claude_code.api_request')).toHaveAttribute('aria-expanded', 'true')
    await user.keyboard(' ')
    expect(rowButton('claude_code.api_request')).toHaveAttribute('aria-expanded', 'false')
  })

  it('links the expanded log to its trace and session, with copy buttons and resource attributes', async () => {
    const user = userEvent.setup()
    renderPage('/logs?service=claude-code')
    await screen.findByText('claude_code.api_request', { selector: 'span' })
    await user.click(rowButton('claude_code.api_request'))
    const details = document.getElementById(rowButton('claude_code.api_request').getAttribute('aria-controls')!)!

    expect(within(details).getByRole('link', { name: 'trace-abc' })).toHaveAttribute('href', '/traces/otel_trace/trace-abc')
    expect(within(details).getByRole('link', { name: 'session-1' })).toHaveAttribute('href', '/sessions/session-1')
    expect(within(details).getByRole('button', { name: 'Copy trace ID' })).toBeInTheDocument()
    expect(within(details).getByRole('button', { name: 'Copy span ID' })).toBeInTheDocument()
    expect(within(details).getByRole('button', { name: 'Copy log body' })).toBeInTheDocument()
    expect(within(details).getByText('Resource attributes')).toBeInTheDocument()
    expect(within(details).getByText('laptop')).toBeInTheDocument()

    // The session link in the row; detail pages get the list's URL to go back to
    const rowLinks = screen.getAllByRole('link', { name: 'Open session' })
    expect(rowLinks).toHaveLength(1)
    await user.click(within(details).getByRole('link', { name: 'trace-abc' }))
    expect(screen.getByTestId('detail')).toHaveTextContent('/traces/otel_trace/trace-abc from=/logs?service=claude-code')
  })

  it('shows the logs of the trace named in the URL, and clears the filter', async () => {
    renderPage('/logs?traceId=trace-xyz')
    await waitFor(() => expect(vi.mocked(api.getLogs).mock.calls.at(-1)?.[0]).toMatchObject({ traceId: 'trace-xyz' }))
    expect(screen.getByText('trace-xyz')).toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'Clear trace filter' }))
    await waitFor(() => expect(vi.mocked(api.getLogs).mock.calls.at(-1)?.[0]?.traceId).toBeUndefined())
    expect(screen.getByTestId('location')).toHaveTextContent('/logs')
    expect(screen.getByTestId('location')).not.toHaveTextContent('traceId')
  })
})
