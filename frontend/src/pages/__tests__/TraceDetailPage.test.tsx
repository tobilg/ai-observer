import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen } from '@testing-library/react'
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
