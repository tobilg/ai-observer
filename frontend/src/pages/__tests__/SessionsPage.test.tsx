import { beforeEach, describe, expect, it, vi } from 'vitest'
import { act, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { Route, Routes, useLocation } from 'react-router-dom'
import { SessionsPage } from '../SessionsPage'
import { api } from '@/lib/api'
import { httpError } from '@/lib/errors'
import { useTelemetryStore } from '@/stores/telemetryStore'
import { renderWithProviders } from '@/test/queryWrapper'
import type { Session } from '@/types/sessions'

vi.mock('@/lib/api', () => ({
  api: {
    getServices: vi.fn(),
    getSessions: vi.fn(),
  },
}))

type SessionParams = Parameters<typeof api.getSessions>[0] & object

const sessions: Session[] = [
  {
    sessionId: 'session-a',
    serviceName: 'claude-code',
    startTime: '2026-01-01T08:00:00Z',
    lastTime: '2026-01-02T10:30:00Z',
    messageCount: 1_042,
    model: 'claude-sonnet-4',
    title: 'Fix the flaky login test',
    repository: 'acme/web',
    branch: 'main',
    costUsd: 12.345,
    inputTokens: 1_200,
    outputTokens: 340,
  },
  // The same session ID from another service is listed separately; no
  // title, usage or repository (all optional)
  {
    sessionId: 'session-a',
    serviceName: 'gemini_cli',
    startTime: '2026-01-02T10:00:00Z',
    lastTime: '2026-01-02T10:00:20Z',
    messageCount: 3,
  },
]

function calls(): SessionParams[] {
  return vi.mocked(api.getSessions).mock.calls.map(([params]) => params as SessionParams)
}

function LocationProbe() {
  const location = useLocation()
  return <output data-testid="location">{location.pathname + location.search}</output>
}

function TranscriptRoute() {
  const location = useLocation()
  return (
    <output data-testid="transcript">{`${location.pathname} from=${(location.state as { from?: string } | null)?.from}`}</output>
  )
}

function renderPage(route = '/sessions') {
  return renderWithProviders(
    <Routes>
      <Route
        path="/sessions"
        element={
          <>
            <SessionsPage />
            <LocationProbe />
          </>
        }
      />
      <Route path="/sessions/:sessionId" element={<TranscriptRoute />} />
    </Routes>,
    { route },
  )
}

function rows() {
  return within(screen.getByRole('list', { name: 'Sessions' })).getAllByRole('listitem')
}

describe('SessionsPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    localStorage.clear()
    useTelemetryStore.setState({ recentLogTimes: [], recentSpanTimes: [], logsUpdateCount: 0, spansUpdateCount: 0 })
    vi.mocked(api.getServices).mockResolvedValue({ services: ['claude-code', 'gemini_cli'] })
    vi.mocked(api.getSessions).mockResolvedValue({ sessions, total: 2, hasMore: false })
  })

  it('renders the header and a skeleton on the first load', async () => {
    renderPage()
    expect(screen.getByRole('heading', { level: 1, name: 'Sessions' })).toBeInTheDocument()
    expect(document.title).toBe('Sessions · AI Observer')
    expect(screen.getByRole('status', { name: 'Loading sessions' })).toBeInTheDocument()
    await screen.findByRole('list', { name: 'Sessions' })
  })

  it('shows the title, ID, repository, usage, model, duration and messages of a session', async () => {
    renderPage()
    await screen.findByRole('list', { name: 'Sessions' })
    const [first, second] = rows()

    expect(within(first).getByRole('link', { name: 'Fix the flaky login test' })).toBeInTheDocument()
    expect(within(first).getByText('session-a')).toBeInTheDocument()
    expect(within(first).getByRole('button', { name: 'Copy session ID' })).toBeInTheDocument()
    expect(within(first).getByText('acme/web · main')).toBeInTheDocument()
    expect(within(first).getByText('$12.35')).toBeInTheDocument()
    expect(within(first).getByText('Tokens: 1,200 in / 340 out')).toBeInTheDocument()
    expect(within(first).getByText('Model: claude-sonnet-4')).toBeInTheDocument()
    expect(within(first).getByText('Duration: 26h 30m')).toBeInTheDocument()
    expect(within(first).getByText('1,042')).toBeInTheDocument()

    // Without a title the ID is the link; missing fields are left out
    expect(within(second).getByRole('link', { name: 'session-a' })).toBeInTheDocument()
    expect(within(second).getByText('Duration: 20s')).toBeInTheDocument()
    expect(within(second).queryByText(/Model:|Tokens:|\$/)).not.toBeInTheDocument()
    expect(within(second).getByText('messages')).toBeInTheDocument()
  })

  it('links each row to the transcript, with the list URL to go back to', async () => {
    const user = userEvent.setup()
    renderPage('/sessions?service=claude-code')
    await screen.findByRole('list', { name: 'Sessions' })

    const link = within(rows()[0]).getByRole('link', { name: 'Fix the flaky login test' })
    expect(link).toHaveAttribute('href', '/sessions/session-a')
    await user.click(link)
    expect(screen.getByTestId('transcript')).toHaveTextContent('/sessions/session-a from=/sessions?service=claude-code')
  })

  it('searches the sessions with the debounced, URL-kept search and marks the terms', async () => {
    const user = userEvent.setup()
    vi.mocked(api.getSessions).mockImplementation(async (params = {}) =>
      params.search
        ? {
            sessions: [{ ...sessions[0], snippet: '…the login test fails on CI…', matches: 4, matchedAt: '2026-01-02T10:00:00Z' }],
            total: 1,
            hasMore: false,
          }
        : { sessions, total: 2, hasMore: false },
    )
    renderPage()
    await screen.findByRole('list', { name: 'Sessions' })

    await user.type(screen.getByRole('searchbox', { name: 'Search sessions' }), 'login "fails on"')

    await screen.findByText('Matching sessions')
    await waitFor(() => expect(rows()[0].querySelector('mark')).not.toBeNull())
    expect(calls().filter((p) => p.search)).toEqual([expect.objectContaining({ search: 'login "fails on"' })])
    expect(screen.getByTestId('location')).toHaveTextContent('search=login+%22fails+on%22')
    const [row] = rows()
    const marks = Array.from(row.querySelectorAll('mark')).map((m) => m.textContent)
    expect(marks).toEqual(['login', 'fails on'])
    expect(within(row).getByText(/4 matches, latest/)).toBeInTheDocument()
  })

  it('reads the search from the URL', async () => {
    renderPage('/sessions?search=deploy&service=gemini_cli&timeframe=30d')
    await waitFor(() => expect(calls().length).toBeGreaterThan(0))
    expect(calls()[0]).toMatchObject({ search: 'deploy', service: 'gemini_cli' })
    expect(screen.getByRole('searchbox', { name: 'Search sessions' })).toHaveValue('deploy')
  })

  it("shows the server's message for an invalid search, with the input marked", async () => {
    vi.mocked(api.getSessions).mockRejectedValue(httpError(400, 'a search takes at most 10 terms'))
    renderPage('/sessions?search=a+b+c+d+e+f+g+h+i+j+k')

    const alert = await screen.findByRole('alert')
    expect(alert).toHaveTextContent('Invalid search')
    expect(alert).toHaveTextContent('a search takes at most 10 terms')
    expect(within(alert).queryByRole('button', { name: 'Retry' })).not.toBeInTheDocument()
    expect(screen.getByRole('searchbox', { name: 'Search sessions' })).toHaveAttribute('aria-invalid', 'true')
  })

  it('shows other errors with a retry, not an empty list', async () => {
    const user = userEvent.setup()
    vi.mocked(api.getSessions).mockRejectedValueOnce(new TypeError('Failed to fetch'))
    renderPage()

    const alert = await screen.findByRole('alert')
    expect(alert).toHaveTextContent("Couldn't load sessions")
    expect(alert).toHaveTextContent('Cannot reach the AI Observer server.')
    expect(screen.queryByText(/No sessions/)).not.toBeInTheDocument()

    await user.click(within(alert).getByRole('button', { name: 'Retry' }))
    await screen.findByRole('list', { name: 'Sessions' })
  })

  it('shows empty states for no sessions and no matches', async () => {
    vi.mocked(api.getSessions).mockResolvedValue({ sessions: [], total: 0, hasMore: false })
    const { unmount } = renderPage()
    expect(await screen.findByText('No sessions in this time range')).toBeInTheDocument()
    unmount()

    renderPage('/sessions?search=nothing')
    expect(await screen.findByText('No sessions match “nothing”')).toBeInTheDocument()
  })

  it('pages with cursors, starting again on the first page when a filter changes', async () => {
    const user = userEvent.setup()
    localStorage.setItem('ai-observer-sessions-pageSize', '1')
    vi.mocked(api.getSessions).mockImplementation(async (params = {}) =>
      params.cursor
        ? { sessions: sessions.slice(1), total: 2, hasMore: false }
        : { sessions: sessions.slice(0, 1), total: 2, hasMore: true, nextCursor: 'cursor-2' },
    )
    renderPage()
    await screen.findByText('Model: claude-sonnet-4')
    expect(screen.getByText('Page 1 of 2')).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Go to next page' }))
    expect(await screen.findByText('Duration: 20s')).toBeInTheDocument()
    expect(screen.getByText('Page 2 of 2')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Go to next page' })).toBeDisabled()

    await user.selectOptions(screen.getByRole('combobox', { name: 'Service' }), 'gemini_cli')
    await waitFor(() => expect(calls().at(-1)).toMatchObject({ service: 'gemini_cli' }))
    expect(calls().at(-1)?.cursor).toBeUndefined()
  })

  it('refreshes the first page on new logs and spans', async () => {
    renderPage()
    await screen.findByRole('list', { name: 'Sessions' })
    const before = calls().length

    act(() => {
      useTelemetryStore.getState().addLogs({ count: 1, services: ['claude-code'], timestamps: [Date.now()] })
      useTelemetryStore.getState().addSpans({ count: 1, services: ['claude-code'], timestamps: [Date.now()] })
    })

    // Debounced: the two notifications refresh once
    await waitFor(() => expect(calls().length).toBe(before + 1), { timeout: 3_000 })
    expect(calls().at(-1)?.cursor).toBeUndefined()
  })

  it('leaves later pages as loaded on notifications', async () => {
    const user = userEvent.setup()
    localStorage.setItem('ai-observer-sessions-pageSize', '1')
    vi.mocked(api.getSessions).mockImplementation(async (params = {}) =>
      params.cursor
        ? { sessions: sessions.slice(1), total: 2, hasMore: false }
        : { sessions: sessions.slice(0, 1), total: 2, hasMore: true, nextCursor: 'cursor-2' },
    )
    renderPage()
    await screen.findByText('Model: claude-sonnet-4')
    await user.click(screen.getByRole('button', { name: 'Go to next page' }))
    await screen.findByText('Duration: 20s')
    const before = calls().length

    act(() => {
      useTelemetryStore.getState().addLogs({ count: 1, services: ['claude-code'], timestamps: [Date.now()] })
    })
    await new Promise((resolve) => setTimeout(resolve, 1_300))
    expect(calls()).toHaveLength(before)
  })
})
