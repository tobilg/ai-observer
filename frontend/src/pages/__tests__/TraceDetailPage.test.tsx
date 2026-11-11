import { describe, it, expect, vi, beforeEach } from 'vitest'
import { act, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createMemoryRouter, RouterProvider } from 'react-router-dom'
import { QueryClientProvider } from '@tanstack/react-query'
import { render } from '@testing-library/react'
import { TraceDetailPage } from '../TraceDetailPage'
import { api } from '@/lib/api'
import { httpError } from '@/lib/errors'
import { createTestQueryClient, renderWithProviders } from '@/test/queryWrapper'
import type { Span, SpansResponse } from '@/types/traces'

vi.mock('@/lib/api', () => ({
  api: { getTraceSpans: vi.fn() },
}))

const ms = 1_000_000

function renderPage(url: string) {
  return renderWithProviders(<TraceDetailPage />, { route: url, path: '/traces/:kind/:id' })
}

const span = (id: string, second: number, parent?: string, fields: Partial<Span> = {}): Span => ({
  timestamp: `2026-01-02T10:00:0${second}.000Z`, traceId: 't1', spanId: id, parentSpanId: parent, spanName: `op ${id}`,
  serviceName: 'codex-app-server', duration: 10 * ms, ...fields,
})

// A response of the spans endpoint: all spans of the trace unless fields say otherwise
const page = (spans: Span[], fields: Partial<SpansResponse> = {}): SpansResponse => ({
  spans, total: spans.length, hasMore: false, ...fields,
})

describe('TraceDetailPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('loads an OTel trace by kind and id and shows its spans', async () => {
    const spans: Span[] = [
      { timestamp: '2026-01-02T10:00:00.000Z', traceId: 't1', spanId: 's1', spanName: 'chat', serviceName: 'copilot-chat', duration: 10 * ms, statusCode: 'OK' },
      { timestamp: '2026-01-02T10:00:00.002Z', traceId: 't1', spanId: 's2', parentSpanId: 's1', spanName: 'tool call', serviceName: 'copilot-chat', duration: 5 * ms },
    ]
    vi.mocked(api.getTraceSpans).mockResolvedValue(page(spans))
    renderPage('/traces/otel_trace/t1')

    expect(screen.getByRole('status', { name: 'Loading trace' })).toBeInTheDocument()
    expect(await screen.findByRole('button', { name: /^tool call, 5\.00ms/ })).toBeInTheDocument()
    // The root span names the page; the ID can be copied
    expect(screen.getByRole('heading', { name: 'chat' })).toBeInTheDocument()
    expect(document.title).toBe('chat · Trace · AI Observer')
    expect(screen.getByRole('button', { name: 'Copy trace ID' })).toBeInTheDocument()
    expect(screen.getByRole('searchbox', { name: 'Filter spans' })).toBeInTheDocument()
    expect(screen.getByTestId('time-axis')).toBeInTheDocument()
    expect(api.getTraceSpans).toHaveBeenCalledWith('t1', 'otel_trace', expect.objectContaining({ signal: expect.any(AbortSignal) }), undefined)
  })

  it('labels Codex operations and turns', async () => {
    vi.mocked(api.getTraceSpans).mockResolvedValue(page(
      [{ timestamp: '2026-01-02T10:00:00Z', traceId: 't', spanId: 'op', spanName: 'submission', serviceName: 'codex_cli_rs', duration: ms }],
    ))
    const first = renderPage('/traces/codex_operation/op')
    expect(await screen.findByRole('heading', { name: 'submission' })).toBeInTheDocument()
    expect(screen.getByText('Codex operation')).toBeInTheDocument()
    first.unmount()

    vi.mocked(api.getTraceSpans).mockResolvedValue(page(
      [{ timestamp: '2026-01-02T10:00:00Z', traceId: 't', spanId: 'turn', spanName: 'turn', serviceName: 'codex_cli_rs', duration: ms, spanAttributes: { turn_id: '1' } }],
    ))
    renderPage('/traces/codex_operation/turn')
    expect(await screen.findByText('Codex turn')).toBeInTheDocument()
  })

  it('loads a large trace a page at a time, with the overview of the whole trace', async () => {
    const trace = {
      id: 't1', kind: 'otel_trace' as const, traceId: 't1', rootSpanId: 'a', rootSpan: 'op a', serviceName: 'codex-app-server',
      startTime: '2026-01-02T10:00:00.000Z', duration: 5000 * ms, spanCount: 2500, status: 'ERROR' as const,
    }
    vi.mocked(api.getTraceSpans)
      .mockResolvedValueOnce({ spans: [span('a', 0), span('b', 1, 'a')], trace, total: 2500, hasMore: true, nextCursor: 'next' })
      .mockResolvedValueOnce({ spans: [span('c', 2, 'b')], trace, total: 2500, hasMore: false })
    renderPage('/traces/otel_trace/t1')

    // Count, status and duration are those of the whole trace
    expect(await screen.findByText('Showing the first 2 of 2,500 spans')).toBeInTheDocument()
    expect(screen.getByText('ERROR')).toBeInTheDocument()
    expect(screen.getAllByText('5.00s').length).toBeGreaterThan(0)

    await userEvent.setup().click(screen.getByRole('button', { name: 'Load more spans' }))
    expect(await screen.findByRole('button', { name: /^op c,/ })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /^op a,/ })).toBeInTheDocument()
    expect(api.getTraceSpans).toHaveBeenLastCalledWith('t1', 'otel_trace', expect.objectContaining({ signal: expect.any(AbortSignal) }), 'next')
    expect(screen.queryByRole('button', { name: 'Load more spans' })).not.toBeInTheDocument()
  })

  it('never mixes in a page of a trace that was left while it loaded', async () => {
    let resolveMore: (value: SpansResponse) => void = () => {}
    vi.mocked(api.getTraceSpans).mockImplementation((id, _kind, _options, cursor) => {
      if (id === 't1' && cursor === 'next') {
        return new Promise<SpansResponse>((resolve) => { resolveMore = resolve })
      }
      if (id === 't1') {
        return Promise.resolve(page([span('a', 0)], { total: 2, hasMore: true, nextCursor: 'next' }))
      }
      return Promise.resolve(page([span('x', 0, undefined, { traceId: 't2' })]))
    })
    const router = createMemoryRouter(
      [{ path: '/traces/:kind/:id', element: <TraceDetailPage /> }],
      { initialEntries: ['/traces/otel_trace/t1'] },
    )
    render(
      <QueryClientProvider client={createTestQueryClient()}>
        <RouterProvider router={router} />
      </QueryClientProvider>,
    )
    await userEvent.setup().click(await screen.findByRole('button', { name: 'Load more spans' }))
    await act(() => router.navigate('/traces/otel_trace/t2'))
    expect(await screen.findByRole('button', { name: /^op x,/ })).toBeInTheDocument()

    await act(async () => resolveMore(page([span('late', 1, 'a')], { total: 2 })))
    expect(screen.queryByRole('button', { name: /^op late,/ })).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: /^op x,/ })).toBeInTheDocument()
  })

  it('links to the session of the trace', async () => {
    vi.mocked(api.getTraceSpans).mockResolvedValue(page([span('a', 0, undefined, { sessionId: 'sess 1' })], {
      trace: {
        id: 't1', kind: 'otel_trace', traceId: 't1', rootSpanId: 'a', rootSpan: 'op a', serviceName: 'codex-app-server',
        startTime: '2026-01-02T10:00:00.000Z', duration: 10 * ms, spanCount: 1, status: 'OK', sessionId: 'sess 1',
      },
    }))
    renderPage('/traces/otel_trace/t1')
    expect(await screen.findByRole('link', { name: 'Open session' })).toHaveAttribute('href', '/sessions/sess%201')
    expect(screen.getByRole('link', { name: 'View logs' }).getAttribute('href')).toMatch(/^\/logs\?traceId=/)

    await userEvent.setup().click(screen.getByRole('button', { name: /^op a,/ }))
    expect(screen.getByRole('link', { name: 'sess 1' })).toHaveAttribute('href', '/sessions/sess%201')
  })

  it('goes back to the list it came from, with its filters', async () => {
    vi.mocked(api.getTraceSpans).mockResolvedValue(page([span('a', 0)]))
    const router = createMemoryRouter(
      [{ path: '/traces/:kind/:id', element: <TraceDetailPage /> }],
      { initialEntries: [{ pathname: '/traces/otel_trace/t1', state: { from: '/traces?service=codex&search=fs' } }] },
    )
    render(
      <QueryClientProvider client={createTestQueryClient()}>
        <RouterProvider router={router} />
      </QueryClientProvider>,
    )
    expect(await screen.findByRole('link', { name: 'Back to traces' })).toHaveAttribute('href', '/traces?service=codex&search=fs')
  })

  it('goes back to the trace list without a previous page', async () => {
    vi.mocked(api.getTraceSpans).mockResolvedValue(page([span('a', 0)]))
    renderPage('/traces/otel_trace/t1')
    expect(await screen.findByRole('link', { name: 'Back to traces' })).toHaveAttribute('href', '/traces')
  })

  it('reports a trace without spans', async () => {
    vi.mocked(api.getTraceSpans).mockResolvedValue(page([]))
    renderPage('/traces/otel_trace/empty')
    expect(await screen.findByText('No spans found for this trace')).toBeInTheDocument()
  })

  it('reports a trace that cannot be loaded, with the server message, and retries', async () => {
    vi.mocked(api.getTraceSpans).mockRejectedValueOnce(httpError(404, 'trace not found'))
    renderPage('/traces/otel_trace/missing')
    expect(await screen.findByText("Couldn't load the trace")).toBeInTheDocument()
    expect(screen.getByText('trace not found')).toBeInTheDocument()

    vi.mocked(api.getTraceSpans).mockResolvedValueOnce(page([span('a', 0)]))
    await userEvent.setup().click(screen.getByRole('button', { name: 'Retry' }))
    expect(await screen.findByRole('button', { name: /^op a,/ })).toBeInTheDocument()
  })

  it('reports a failed request without a server message', async () => {
    vi.mocked(api.getTraceSpans).mockRejectedValue(new Error('boom'))
    renderPage('/traces/otel_trace/missing')
    expect(await screen.findByText('Failed to load trace')).toBeInTheDocument()
  })
})
