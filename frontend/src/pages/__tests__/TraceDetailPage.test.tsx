import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { TraceDetailPage } from '../TraceDetailPage'
import { api } from '@/lib/api'
import type { Span } from '@/types/traces'

vi.mock('@/lib/api', () => ({
  api: { getTraceSpans: vi.fn() },
}))

vi.mock('sonner', () => ({
  toast: { error: vi.fn(), success: vi.fn() },
}))

const ms = 1_000_000

function renderPage(url: string) {
  return render(
    <MemoryRouter initialEntries={[url]}>
      <Routes>
        <Route path="/traces/:kind/:id" element={<TraceDetailPage />} />
      </Routes>
    </MemoryRouter>
  )
}

describe('TraceDetailPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('loads an OTel trace by kind and id and shows its spans', async () => {
    const spans: Span[] = [
      { timestamp: '2026-01-02T10:00:00.000Z', traceId: 't1', spanId: 's1', spanName: 'chat', serviceName: 'copilot-chat', duration: 10 * ms, statusCode: 'OK' },
      { timestamp: '2026-01-02T10:00:00.002Z', traceId: 't1', spanId: 's2', parentSpanId: 's1', spanName: 'tool call', serviceName: 'copilot-chat', duration: 5 * ms },
    ]
    vi.mocked(api.getTraceSpans).mockResolvedValue({ spans })
    renderPage('/traces/otel_trace/t1')

    expect(screen.getByText('Loading trace...')).toBeInTheDocument()
    expect(await screen.findByRole('button', { name: /^tool call, 5\.00ms/ })).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: 'Trace t1' })).toBeInTheDocument()
    expect(api.getTraceSpans).toHaveBeenCalledWith('t1', 'otel_trace', expect.objectContaining({ signal: expect.any(AbortSignal) }))
  })

  it('labels Codex operations and turns', async () => {
    vi.mocked(api.getTraceSpans).mockResolvedValue({
      spans: [{ timestamp: '2026-01-02T10:00:00Z', traceId: 't', spanId: 'op', spanName: 'submission', serviceName: 'codex_cli_rs', duration: ms }],
    })
    renderPage('/traces/codex_operation/op')
    expect(await screen.findByRole('heading', { name: 'Codex Operation op' })).toBeInTheDocument()

    vi.mocked(api.getTraceSpans).mockResolvedValue({
      spans: [{ timestamp: '2026-01-02T10:00:00Z', traceId: 't', spanId: 'turn', spanName: 'turn', serviceName: 'codex_cli_rs', duration: ms, spanAttributes: { turn_id: '1' } }],
    })
    renderPage('/traces/codex_operation/turn')
    expect(await screen.findByRole('heading', { name: 'Codex Turn turn' })).toBeInTheDocument()
  })

  it('loads a large trace a page at a time, with the overview of the whole trace', async () => {
    const span = (id: string, second: number, parent?: string): Span => ({
      timestamp: `2026-01-02T10:00:0${second}.000Z`, traceId: 't1', spanId: id, parentSpanId: parent, spanName: `op ${id}`,
      serviceName: 'codex-app-server', duration: 10 * ms,
    })
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
    expect(api.getTraceSpans).toHaveBeenLastCalledWith('t1', 'otel_trace', undefined, 'next')
    expect(screen.queryByRole('button', { name: 'Load more spans' })).not.toBeInTheDocument()
  })

  it('reports a trace without spans', async () => {
    vi.mocked(api.getTraceSpans).mockResolvedValue({ spans: [] })
    renderPage('/traces/otel_trace/empty')
    expect(await screen.findByText('No spans found for this trace')).toBeInTheDocument()
  })

  it('reports a trace that cannot be loaded, such as an unknown one (404)', async () => {
    vi.mocked(api.getTraceSpans).mockRejectedValue(new Error('HTTP error! status: 404'))
    vi.spyOn(console, 'error').mockImplementation(() => {})
    renderPage('/traces/otel_trace/missing')
    expect(await screen.findByText('Failed to load trace')).toBeInTheDocument()
  })
})
