import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { act, render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createMemoryRouter, RouterProvider } from 'react-router-dom'
import { QueryClientProvider } from '@tanstack/react-query'
import { SessionTranscriptPage } from '../SessionTranscriptPage'
import { api } from '@/lib/api'
import { httpError } from '@/lib/errors'
import { useTelemetryStore } from '@/stores/telemetryStore'
import { createTestQueryClient, renderWithProviders } from '@/test/queryWrapper'
import type { TranscriptMessage, TranscriptResponse } from '@/types/sessions'

vi.mock('@/lib/api', () => ({
  api: { getSessionTranscript: vi.fn() },
}))

const transcript: TranscriptResponse = {
  sessionId: 's1',
  serviceName: 'claude-code',
  startTime: '2026-01-02T10:00:00Z',
  lastTime: '2026-01-02T10:05:00Z',
  messages: [
    { timestamp: '2026-01-02T10:00:00Z', role: 'user', content: 'Please fix the bug', index: 0 },
    { timestamp: '2026-01-02T10:01:00Z', role: 'assistant', content: 'Looking into it', index: 1, inputTokens: 120, outputTokens: 30 },
    { timestamp: '2026-01-02T10:02:00Z', role: 'tool_use', content: '', index: 2, toolName: 'ReadFile', toolInput: '{"path":"main.go"}' },
    { timestamp: '2026-01-02T10:03:00Z', role: 'tool_result', content: 'package main', index: 3, toolName: 'ReadFile' },
    // Metadata-only response (OTLP without content), hidden by default
    { timestamp: '2026-01-02T10:04:00Z', role: 'assistant', content: '', index: 4, inputTokens: 50 },
  ],
  total: 5,
  hasMore: false,
}

type GetTranscript = typeof api.getSessionTranscript

function renderPage(url = '/sessions/s1', state?: unknown) {
  const router = createMemoryRouter(
    [{ path: '/sessions/:sessionId', element: <SessionTranscriptPage /> }],
    { initialEntries: [state ? { pathname: url, state } : url] },
  )
  const queryClient = createTestQueryClient()
  render(
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  )
  return { router, queryClient }
}

describe('SessionTranscriptPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    localStorage.clear()
    vi.mocked(api.getSessionTranscript).mockResolvedValue(transcript)
    vi.spyOn(window, 'scrollTo').mockImplementation(() => {})
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('shows a loading state, then the messages', async () => {
    renderWithProviders(<SessionTranscriptPage />, { route: '/sessions/s1', path: '/sessions/:sessionId' })
    expect(screen.getByRole('status', { name: 'Loading transcript' })).toBeInTheDocument()

    expect(await screen.findByText('Please fix the bug')).toBeInTheDocument()
    expect(screen.getByText('Looking into it')).toBeInTheDocument()
    expect(screen.getAllByText('ReadFile')).toHaveLength(2)
    expect(screen.getByText('120 in / 30 out')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Copy session ID' })).toBeInTheDocument()
    expect(screen.getAllByRole('button', { name: 'Copy message' }).length).toBeGreaterThan(0)
  })

  it('loads the whole session, without a time range', async () => {
    renderPage('/sessions/s1?from=2026-01-02T10%3A00%3A00Z')
    await screen.findByText('Please fix the bug')
    expect(api.getSessionTranscript).toHaveBeenCalledWith('s1', expect.objectContaining({ signal: expect.any(AbortSignal) }), undefined)
  })

  it('shows the title and the totals of the whole session', async () => {
    vi.mocked(api.getSessionTranscript).mockResolvedValue({
      ...transcript,
      title: 'Please fix the bug in the login form',
      total: 900,
      hasMore: true,
      nextCursor: '5',
      totals: {
        costUsd: 1.5, inputTokens: 12000, outputTokens: 3400, cacheRead: 50000, cacheWrite: 800,
        userMessages: 40, assistantMessages: 300, toolCalls: 250, toolErrors: 3,
      },
    })
    renderPage()
    expect(await screen.findByRole('heading', { name: 'Please fix the bug in the login form' })).toBeInTheDocument()
    expect(document.title).toBe('Please fix the bug in the login form · AI Observer')
    expect(screen.getByText('s1')).toBeInTheDocument()

    const totals = screen.getByLabelText('Session totals')
    expect(within(totals).getByText('$1.50')).toBeInTheDocument()
    expect(within(totals).getByText('12,000 tokens')).toBeInTheDocument()
    expect(within(totals).getByText('3,400 tokens')).toBeInTheDocument()
    expect(within(totals).getByText('3')).toHaveClass('text-error')
    expect(screen.getByText('900 messages')).toBeInTheDocument()

    // Role counts of the whole session where the totals have them
    expect(screen.getByRole('button', { name: 'User (40)' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Assistant (300)' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Tool calls (250)' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Tool results (1 loaded)' })).toBeInTheDocument()
  })

  it('filters messages by role and hides metadata-only responses by default', async () => {
    const user = userEvent.setup()
    renderPage()
    await screen.findByText('Please fix the bug')

    expect(screen.getByText('Showing 4 of 5 messages')).toBeInTheDocument()
    expect(screen.queryByText('Response content not captured via OTLP telemetry')).not.toBeInTheDocument()

    const toolCalls = screen.getByRole('button', { name: 'Tool calls (1)' })
    expect(toolCalls).toHaveAttribute('aria-pressed', 'true')
    await user.click(toolCalls)
    expect(toolCalls).toHaveAttribute('aria-pressed', 'false')
    expect(screen.getByText('Showing 3 of 5 messages')).toBeInTheDocument()

    const metadataOnly = screen.getByRole('switch', { name: 'Show responses without content (1)' })
    expect(metadataOnly).toHaveAttribute('aria-checked', 'false')
    await user.click(metadataOnly)
    expect(metadataOnly).toHaveAttribute('aria-checked', 'true')
    expect(screen.getByText('Response content not captured via OTLP telemetry')).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'User (1)' }))
    expect(screen.queryByText('Please fix the bug')).not.toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Reset' }))
    expect(screen.getByText('Please fix the bug')).toBeInTheDocument()
    expect(screen.getByText('Showing 4 of 5 messages')).toBeInTheDocument()
  })

  it('shows an empty transcript', async () => {
    vi.mocked(api.getSessionTranscript).mockResolvedValue({ ...transcript, messages: [] })
    renderPage()
    expect(await screen.findByText('No messages in this session')).toBeInTheDocument()
  })

  it('reports a failed request inline, and retries', async () => {
    const user = userEvent.setup()
    vi.mocked(api.getSessionTranscript).mockRejectedValueOnce(new Error('boom'))
    renderPage()
    expect(await screen.findByText("Couldn't load the session transcript")).toBeInTheDocument()
    expect(screen.getByText('Failed to load session transcript')).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Retry' }))
    expect(await screen.findByText('Please fix the bug')).toBeInTheDocument()
  })

  it('shows the server message of an unknown session', async () => {
    vi.mocked(api.getSessionTranscript).mockRejectedValue(httpError(404, 'session not found'))
    renderPage()
    expect(await screen.findByText('session not found')).toBeInTheDocument()
  })

  it('explains a session that is too large to load', async () => {
    vi.mocked(api.getSessionTranscript).mockRejectedValue(httpError(504))
    renderPage()
    expect(await screen.findByText('This session has too many records to load in time')).toBeInTheDocument()
  })

  it('loads further pages of a long session on request', async () => {
    const user = userEvent.setup()
    vi.mocked(api.getSessionTranscript).mockImplementation(async (_id, _options, cursor) =>
      cursor === '2'
        ? { ...transcript, messages: transcript.messages.slice(2), total: 5, hasMore: false }
        : { ...transcript, messages: transcript.messages.slice(0, 2), total: 5, hasMore: true, nextCursor: '2' }
    )
    renderPage()
    await screen.findByText('Please fix the bug')
    expect(screen.getByText('5 messages')).toBeInTheDocument()
    expect(screen.getByText('Loaded the first 2 of 5 messages')).toBeInTheDocument()
    expect(screen.getByText('Showing 2 of 2 loaded messages (5 in the session)')).toBeInTheDocument()
    expect(screen.queryByText('ReadFile')).not.toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Load more messages' }))
    expect(await screen.findByText('Showing 4 of 5 messages')).toBeInTheDocument()
    expect(screen.getAllByText('ReadFile')).toHaveLength(2)
    expect(api.getSessionTranscript).toHaveBeenLastCalledWith('s1', expect.objectContaining({ signal: expect.any(AbortSignal) }), '2')
    expect(screen.queryByRole('button', { name: 'Load more messages' })).not.toBeInTheDocument()
  })

  it('refreshes edited messages, earlier insertions and search positions across loaded pages', async () => {
    const user = userEvent.setup()
    let messages = transcript.messages.slice(0, 3)
    vi.mocked(api.getSessionTranscript).mockImplementation(async (_id, _options, cursor, params) => {
      const search = params?.search
      const start = Number(cursor ?? 0)
      const end = start + 2
      return {
        ...transcript, messages: messages.slice(start, end), total: messages.length,
        hasMore: end < messages.length, nextCursor: end < messages.length ? String(end) : undefined,
        search: search ? { terms: [search], matches: messages.flatMap((m, i) => m.content.includes(search) ? [i] : []) } : undefined,
      }
    })
    renderPage()
    await screen.findByText('Looking into it')
    await user.click(screen.getByRole('button', { name: 'Load more messages' }))
    await screen.findByText('ReadFile')
    await user.type(screen.getByRole('searchbox'), 'corrected')
    await screen.findByText('No matches')

    vi.useFakeTimers({ shouldAdvanceTime: true })
    try {
      messages = messages.map((m, i) => i === 1 ? { ...m, content: 'corrected response' } : m)
      act(() => useTelemetryStore.setState({ logsUpdateCount: useTelemetryStore.getState().logsUpdateCount + 1 }))
      await act(() => vi.advanceTimersByTimeAsync(2000))
      await screen.findByText('Match 1 of 1')
      expect(screen.queryByText('Looking into it')).not.toBeInTheDocument()
      expect(screen.getByText('3 messages')).toBeInTheDocument()

      const earlier = { ...messages[0], index: 10, content: 'Earlier request' }
      const latest = { ...messages[0], index: 11, content: 'One more thing' }
      messages = [earlier, ...messages, latest]
      act(() => useTelemetryStore.setState({ spansUpdateCount: useTelemetryStore.getState().spansUpdateCount + 1 }))
      await act(() => vi.advanceTimersByTimeAsync(2000))
      expect(await screen.findByText('One more thing')).toBeInTheDocument()
      expect(screen.getByText('Earlier request')).toBeInTheDocument()
      expect(screen.getAllByText('Please fix the bug')).toHaveLength(1)
      expect(screen.getByText('5 messages')).toBeInTheDocument()
    } finally {
      vi.useRealTimers()
    }
  })

  it('drops the transcript of a session that was left while it loaded', async () => {
    let resolveFirst: (value: TranscriptResponse) => void = () => {}
    vi.mocked(api.getSessionTranscript).mockImplementation((id) =>
      id === 's1'
        ? new Promise<TranscriptResponse>((resolve) => { resolveFirst = resolve })
        : Promise.resolve({ ...transcript, sessionId: 's2', messages: [{ timestamp: '2026-01-02T10:00:00Z', role: 'user', content: 'Second session', index: 0 }] })
    )
    const { router } = renderPage()
    await act(() => router.navigate('/sessions/s2'))
    expect(await screen.findByText('Second session')).toBeInTheDocument()

    await act(async () => resolveFirst(transcript))
    expect(screen.getByText('Second session')).toBeInTheDocument()
    expect(screen.queryByText('Please fix the bug')).not.toBeInTheDocument()
  })

  it('goes back to the list it came from, with its filters', async () => {
    renderPage('/sessions/s1', { from: '/sessions?service=codex&timeframe=24h' })
    expect(await screen.findByRole('link', { name: 'Back to sessions' })).toHaveAttribute('href', '/sessions?service=codex&timeframe=24h')
  })

  it('goes back to the session list without a previous page', async () => {
    renderPage()
    expect(await screen.findByRole('link', { name: 'Back to sessions' })).toHaveAttribute('href', '/sessions')
  })

  describe('search', () => {
    // 6 messages in pages of 2; "bug" is in messages 0, 3 (a tool result) and 5
    const all: TranscriptMessage[] = [
      { timestamp: '2026-01-02T10:00:00Z', role: 'user', content: 'Please fix the bug', index: 0 },
      { timestamp: '2026-01-02T10:01:00Z', role: 'assistant', content: 'Looking into it', index: 1 },
      { timestamp: '2026-01-02T10:02:00Z', role: 'tool_use', content: '', index: 2, toolName: 'Grep', toolInput: '{"q":"login"}' },
      { timestamp: '2026-01-02T10:03:00Z', role: 'tool_result', content: '', index: 3, toolName: 'Grep', toolOutput: 'auth.go: // TODO bug here' },
      { timestamp: '2026-01-02T10:04:00Z', role: 'assistant', content: 'Found it', index: 4 },
      { timestamp: '2026-01-02T10:05:00Z', role: 'assistant', content: 'The **bug** is fixed', index: 5 },
    ]
    const getTranscript: GetTranscript = async (_id, _options, cursor, params) => {
      if (params?.search) {
        return { ...transcript, messages: all.slice(0, 1), total: 6, hasMore: true, nextCursor: '1', search: { terms: [params.search], matches: [0, 3, 5] } }
      }
      const start = Number(cursor ?? 0)
      const end = start + 2
      return { ...transcript, messages: all.slice(start, end), total: 6, hasMore: end < 6, nextCursor: end < 6 ? String(end) : undefined }
    }

    it('finds matches in the whole session and loads the pages up to them', async () => {
      const user = userEvent.setup()
      vi.mocked(api.getSessionTranscript).mockImplementation(getTranscript)
      renderPage()
      await screen.findByText('Please fix the bug')

      await user.type(screen.getByRole('searchbox', { name: 'Search this session' }), 'bug')
      expect(await screen.findByText('Match 1 of 3')).toBeInTheDocument()
      expect(api.getSessionTranscript).toHaveBeenCalledWith('s1', expect.anything(), undefined, { search: 'bug', limit: 1 })
      // The term is marked in the loaded messages
      expect(screen.getByText('bug', { selector: 'mark' })).toBeInTheDocument()
      expect(document.querySelector('[data-active]')).toHaveAttribute('data-position', '0')

      // The second match is on the second page: it is loaded, and its tool
      // output shown
      await user.click(screen.getByRole('button', { name: 'Next match' }))
      expect(await screen.findByText('Match 2 of 3')).toBeInTheDocument()
      expect(await screen.findByText(/TODO/)).toBeInTheDocument()
      expect(document.querySelector('[data-active]')).toHaveAttribute('data-position', '3')
      expect(api.getSessionTranscript).toHaveBeenCalledWith('s1', expect.anything(), '2')

      // Enter goes to the next one, on the third page
      await user.type(screen.getByRole('searchbox', { name: 'Search this session' }), '{Enter}')
      expect(await screen.findByText('Match 3 of 3')).toBeInTheDocument()
      expect(await screen.findByText('Found it')).toBeInTheDocument()
      expect(document.querySelector('[data-active]')).toHaveAttribute('data-position', '5')
      expect(window.scrollTo).toHaveBeenCalled()

      // And wraps around; Shift+Enter goes back
      await user.click(screen.getByRole('button', { name: 'Next match' }))
      expect(screen.getByText('Match 1 of 3')).toBeInTheDocument()
      await user.type(screen.getByRole('searchbox', { name: 'Search this session' }), '{Shift>}{Enter}{/Shift}')
      expect(screen.getByText('Match 3 of 3')).toBeInTheDocument()
    })

    it('shows the current match even when the filters hide its role', async () => {
      const user = userEvent.setup()
      vi.mocked(api.getSessionTranscript).mockImplementation(getTranscript)
      localStorage.setItem('ai-observer-session-transcript-filters-v1', JSON.stringify({
        showUser: true, showAssistant: true, showToolUse: true, showToolResult: false, hideMetadataOnlyAssistant: true,
      }))
      renderPage()
      await screen.findByText('Please fix the bug')

      await user.type(screen.getByRole('searchbox', { name: 'Search this session' }), 'bug')
      await screen.findByText('Match 1 of 3')
      await user.click(screen.getByRole('button', { name: 'Next match' }))
      expect(await screen.findByText('Match 2 of 3 · shown although the filters hide it')).toBeInTheDocument()
      expect(document.querySelector('[data-active]')).toHaveAttribute('data-position', '3')
    })

    it('says when nothing matches', async () => {
      const user = userEvent.setup()
      vi.mocked(api.getSessionTranscript).mockImplementation(async (_id, _options, _cursor, params) =>
        params?.search ? { ...transcript, search: { terms: ['zzz'], matches: [] } } : transcript,
      )
      renderPage()
      await screen.findByText('Please fix the bug')
      await user.type(screen.getByRole('searchbox', { name: 'Search this session' }), 'zzz')
      expect(await screen.findByText('No matches')).toBeInTheDocument()
      expect(screen.getByRole('button', { name: 'Next match' })).toBeDisabled()
    })
  })

  it('renders only the messages near the viewport of a long session', async () => {
    // jsdom lays nothing out: give every message a height
    vi.spyOn(HTMLElement.prototype, 'offsetHeight', 'get').mockReturnValue(100)
    const messages = Array.from({ length: 2000 }, (_, i): TranscriptMessage => ({
      timestamp: '2026-01-02T10:00:00Z', role: 'user', content: `Message number ${i}`, index: i,
    }))
    vi.mocked(api.getSessionTranscript).mockResolvedValue({ ...transcript, messages, total: 2000 })
    renderPage()
    expect(await screen.findByText('Message number 0')).toBeInTheDocument()
    const rendered = document.querySelectorAll('[data-position]').length
    expect(rendered).toBeGreaterThan(0)
    expect(rendered).toBeLessThan(50)
    expect(screen.queryByText('Message number 1999')).not.toBeInTheDocument()
  })
})
