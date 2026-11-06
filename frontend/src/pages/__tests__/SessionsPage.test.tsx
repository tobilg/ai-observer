import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'
import { SessionsPage } from '../SessionsPage'
import { api } from '@/lib/api'
import { toast } from 'sonner'
import type { Session } from '@/types/sessions'

vi.mock('@/lib/api', () => ({
  api: {
    getServices: vi.fn(),
    getSessions: vi.fn(),
  },
}))

vi.mock('sonner', () => ({
  toast: { error: vi.fn(), success: vi.fn() },
}))

const sessions: Session[] = [
  {
    sessionId: 'session-a',
    serviceName: 'claude-code',
    startTime: '2026-01-01T08:00:00Z',
    lastTime: '2026-01-02T10:30:00Z',
    messageCount: 42,
    model: 'claude-sonnet-4',
  },
  // The same session ID from another service is listed separately
  {
    sessionId: 'session-a',
    serviceName: 'gemini_cli',
    startTime: '2026-01-02T10:00:00Z',
    lastTime: '2026-01-02T10:00:20Z',
    messageCount: 3,
  },
]

function TranscriptRoute() {
  const location = useLocation()
  return <div data-testid="transcript">{location.pathname + location.search}</div>
}

function renderPage() {
  return render(
    <MemoryRouter initialEntries={['/sessions']}>
      <Routes>
        <Route path="/sessions" element={<SessionsPage />} />
        <Route path="/sessions/:sessionId" element={<TranscriptRoute />} />
      </Routes>
    </MemoryRouter>
  )
}

describe('SessionsPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    localStorage.clear()
    vi.mocked(api.getServices).mockResolvedValue({ services: ['claude-code', 'gemini_cli'] })
    vi.mocked(api.getSessions).mockResolvedValue({ sessions, total: 2, hasMore: false })
  })

  afterEach(() => {
    localStorage.clear()
  })

  it('lists sessions with their duration, model and message count', async () => {
    renderPage()
    expect(screen.getByText('Loading...')).toBeInTheDocument()

    const rows = await screen.findAllByRole('button', { name: /session-a/ })
    expect(rows).toHaveLength(2)
    expect(within(rows[0]).getByText('Duration: 26h 30m')).toBeInTheDocument()
    expect(within(rows[0]).getByText('Model: claude-sonnet-4')).toBeInTheDocument()
    expect(within(rows[0]).getByText('42')).toBeInTheDocument()
    expect(within(rows[1]).getByText('Duration: 20s')).toBeInTheDocument()
    expect(within(rows[1]).queryByText(/Model:/)).not.toBeInTheDocument()
  })

  it('opens the whole transcript, without a time range', async () => {
    const user = userEvent.setup()
    renderPage()
    const rows = await screen.findAllByRole('button', { name: /session-a/ })

    await user.click(rows[0])
    expect(screen.getByTestId('transcript')).toHaveTextContent(/^\/sessions\/session-a$/)
  })

  it('opens a session with the keyboard', async () => {
    const user = userEvent.setup()
    renderPage()
    const rows = await screen.findAllByRole('button', { name: /session-a/ })

    rows[1].focus()
    await user.keyboard('{Enter}')
    expect(screen.getByTestId('transcript')).toHaveTextContent('/sessions/session-a')
  })

  it('filters by service and starts again on the first page', async () => {
    const user = userEvent.setup()
    localStorage.setItem('ai-observer-sessions-pageSize', '1')
    vi.mocked(api.getSessions).mockImplementation(async (params) =>
      params?.cursor
        ? { sessions: sessions.slice(1), total: 2, hasMore: false }
        : { sessions: sessions.slice(0, 1), total: 2, hasMore: true, nextCursor: 'cursor-2' }
    )
    renderPage()
    await screen.findByText('Model: claude-sonnet-4')

    await user.click(screen.getByLabelText('Go to next page'))
    await waitFor(() =>
      expect(api.getSessions).toHaveBeenLastCalledWith(expect.objectContaining({ cursor: 'cursor-2' }), expect.anything())
    )

    await user.selectOptions(screen.getByDisplayValue('All Services'), 'gemini_cli')
    await waitFor(() =>
      expect(api.getSessions).toHaveBeenLastCalledWith(
        expect.objectContaining({ service: 'gemini_cli', cursor: undefined }),
        expect.anything()
      )
    )
  })

  it('pages with cursors and goes back', async () => {
    const user = userEvent.setup()
    localStorage.setItem('ai-observer-sessions-pageSize', '1')
    vi.mocked(api.getSessions).mockImplementation(async (params) =>
      params?.cursor
        ? { sessions: sessions.slice(1), total: 2, hasMore: false }
        : { sessions: sessions.slice(0, 1), total: 2, hasMore: true, nextCursor: 'cursor-2' }
    )
    renderPage()
    await screen.findByText('Model: claude-sonnet-4')
    expect(screen.getByText('Page 1 of 2')).toBeInTheDocument()

    await user.click(screen.getByLabelText('Go to next page'))
    expect(await screen.findByText('Duration: 20s')).toBeInTheDocument()
    expect(screen.getByText('Page 2 of 2')).toBeInTheDocument()
    expect(screen.getByLabelText('Go to next page')).toHaveAttribute('aria-disabled', 'true')

    await user.click(screen.getByLabelText('Go to previous page'))
    expect(await screen.findByText('Model: claude-sonnet-4')).toBeInTheDocument()
    expect(api.getSessions).toHaveBeenLastCalledWith(expect.objectContaining({ cursor: undefined }), expect.anything())
  })

  it('shows an empty state', async () => {
    vi.mocked(api.getSessions).mockResolvedValue({ sessions: [], total: 0, hasMore: false })
    renderPage()
    expect(await screen.findByText(/No sessions found/)).toBeInTheDocument()
  })

  it('reports a failed request', async () => {
    vi.mocked(api.getSessions).mockRejectedValue(new Error('offline'))
    vi.spyOn(console, 'error').mockImplementation(() => {})
    renderPage()
    await waitFor(() => expect(toast.error).toHaveBeenCalledWith('Failed to fetch sessions'))
  })
})
