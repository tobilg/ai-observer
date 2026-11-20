import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { act, fireEvent, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { Route, Routes, useLocation } from 'react-router-dom'
import { TracesPage } from '../TracesPage'
import { api } from '@/lib/api'
import { httpError } from '@/lib/errors'
import { useTelemetryStore } from '@/stores/telemetryStore'
import { renderWithProviders } from '@/test/queryWrapper'
import type { SpansResponse, TraceOverview, TracesResponse } from '@/types/traces'

vi.mock('@/lib/api', () => ({
  api: {
    getServices: vi.fn(),
    getTraces: vi.fn(),
    getTraceSpans: vi.fn(),
  },
}))

type TraceParams = Parameters<typeof api.getTraces>[0] & object

const traceA: TraceOverview = {
  id: 'trace-1',
  kind: 'otel_trace',
  traceId: 'trace-1',
  rootSpanId: 'span-1',
  rootSpan: 'llm/chat',
  serviceName: 'claude-code',
  duration: 1_500_000_000,
  spanCount: 3,
  status: 'OK',
  startTime: '2026-01-15T10:00:00Z',
  sessionId: 'session-9',
}
const traceB: TraceOverview = {
  id: 'trace-2',
  kind: 'otel_trace',
  traceId: 'trace-2',
  rootSpanId: 'span-2',
  rootSpan: 'tool/read_file',
  serviceName: 'claude-code',
  duration: 250_000_000,
  spanCount: 1,
  status: 'ERROR',
  startTime: '2026-01-15T09:55:00Z',
}
const codexOperation: TraceOverview = {
  id: 'op root/1',
  kind: 'codex_operation',
  traceId: 'codex-trace',
  rootSpanId: 'op root/1',
  rootSpan: 'codex.turn',
  serviceName: 'codex_cli_rs',
  duration: 12_000_000_000,
  spanCount: 1_500,
  status: 'OK',
  startTime: '2026-01-15T09:00:00Z',
}

const page1: TracesResponse = { traces: [traceA, traceB], total: 25, hasMore: true, nextCursor: 'c2' }
const page2: TracesResponse = { traces: [codexOperation], total: 25, hasMore: false }

const spans: SpansResponse = {
  spans: [
    {
      spanId: 'span-1',
      traceId: 'trace-1',
      spanName: 'llm/chat',
      spanKind: 'CLIENT',
      timestamp: '2026-01-15T10:00:00Z',
      duration: 1_500_000_000,
      statusCode: 'OK',
      serviceName: 'claude-code',
      spanAttributes: { model: 'claude-sonnet-4' },
    },
  ],
  total: 3,
  hasMore: false,
}

function listCalls(): TraceParams[] {
  return vi.mocked(api.getTraces).mock.calls.map(([params]) => params as TraceParams).filter((p) => p.limit !== 1)
}
function countCalls(): TraceParams[] {
  return vi.mocked(api.getTraces).mock.calls.map(([params]) => params as TraceParams).filter((p) => p.limit === 1)
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

function renderPage(route = '/traces') {
  return renderWithProviders(
    <Routes>
      <Route
        path="/traces"
        element={
          <>
            <TracesPage />
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

function expandButton(rootSpan: string) {
  return screen.getByText(rootSpan, { selector: 'span' }).closest('button') as HTMLButtonElement
}

function rowOf(rootSpan: string) {
  return screen.getByText(rootSpan, { selector: 'span' }).closest('li') as HTMLElement
}

async function announceNewSpans(n: number) {
  vi.useFakeTimers({ shouldAdvanceTime: true })
  await act(() => vi.advanceTimersByTimeAsync(1))
  act(() => {
    useTelemetryStore.getState().addSpans({ count: n, services: ['claude-code'], timestamps: Array.from({ length: n }, () => Date.now()) })
  })
  await act(() => vi.advanceTimersByTimeAsync(250))
}

describe('TracesPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    localStorage.clear()
    newCount = 0
    useTelemetryStore.setState({ recentLogTimes: [], recentSpanTimes: [], logsUpdateCount: 0, spansUpdateCount: 0 })
    vi.mocked(api.getServices).mockResolvedValue({ services: ['claude-code', 'codex_cli_rs'] })
    vi.mocked(api.getTraces).mockImplementation(async (params = {}) => {
      if (params.limit === 1) {
        return { traces: [], total: newCount, hasMore: false }
      }
      return params.cursor === 'c2' ? page2 : page1
    })
    vi.mocked(api.getTraceSpans).mockResolvedValue(spans)
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('renders the header, a skeleton first and then the traces', async () => {
    renderPage()
    expect(screen.getByRole('heading', { level: 1, name: 'Traces' })).toBeInTheDocument()
    expect(screen.getByText('View and analyze traces from your AI coding tools')).toBeInTheDocument()
    expect(document.title).toBe('Traces · AI Observer')
    expect(screen.getByRole('status', { name: 'Loading traces' })).toBeInTheDocument()

    await screen.findByText('llm/chat', { selector: 'span' })
    expect(within(rowOf('llm/chat')).getByText('OK')).toBeInTheDocument()
    expect(within(rowOf('llm/chat')).getByText('3 spans')).toBeInTheDocument()
    expect(within(rowOf('llm/chat')).getByText('1.50s')).toBeInTheDocument()
    expect(within(rowOf('tool/read_file')).getByText('1 span')).toBeInTheDocument()
    expect(within(rowOf('tool/read_file')).getByText('ERROR')).toBeInTheDocument()
  })

  it('links every row to its detail page, with the list URL to go back to', async () => {
    const user = userEvent.setup()
    renderPage('/traces?service=claude-code')
    await screen.findByText('llm/chat', { selector: 'span' })

    expect(within(rowOf('llm/chat')).getByRole('link', { name: 'Open trace llm/chat' })).toHaveAttribute('href', '/traces/otel_trace/trace-1')
    expect(within(rowOf('tool/read_file')).getByRole('link', { name: 'Open trace tool/read_file' })).toHaveAttribute(
      'href',
      '/traces/otel_trace/trace-2',
    )
    // The session of the trace's spans
    expect(within(rowOf('llm/chat')).getByRole('link', { name: 'Open session' })).toHaveAttribute('href', '/sessions/session-9')
    expect(within(rowOf('tool/read_file')).queryByRole('link', { name: 'Open session' })).not.toBeInTheDocument()
    expect(within(rowOf('llm/chat')).getByRole('button', { name: 'Copy trace ID' })).toBeInTheDocument()

    await user.click(within(rowOf('llm/chat')).getByRole('link', { name: 'Open trace llm/chat' }))
    expect(screen.getByTestId('detail')).toHaveTextContent('/traces/otel_trace/trace-1 from=/traces?service=claude-code')
  })

  it('links Codex operations by their root span ID', async () => {
    vi.mocked(api.getTraces).mockResolvedValue({ traces: [codexOperation], total: 1, hasMore: false })
    renderPage()
    await screen.findByText('codex.turn', { selector: 'span' })
    expect(screen.getByRole('link', { name: 'Open trace codex.turn' })).toHaveAttribute(
      'href',
      '/traces/codex_operation/op%20root%2F1',
    )
  })

  it('expands the waterfall with a real button, and keeps the spans cached', async () => {
    const user = userEvent.setup()
    renderPage()
    await screen.findByText('llm/chat', { selector: 'span' })

    const button = expandButton('llm/chat')
    expect(button).toHaveAttribute('aria-expanded', 'false')
    button.focus()
    await user.keyboard('{Enter}')

    expect(button).toHaveAttribute('aria-expanded', 'true')
    const panel = document.getElementById(button.getAttribute('aria-controls')!)!
    expect(panel).toBeInTheDocument()
    await waitFor(() => expect(api.getTraceSpans).toHaveBeenCalledWith('trace-1', 'otel_trace', expect.any(Object), undefined))
    expect(api.getTraceSpans).toHaveBeenCalledTimes(1)
    await waitFor(() => expect(within(panel).queryByRole('status')).not.toBeInTheDocument())

    // Collapsing and expanding again shows the cached spans at once
    await user.click(button)
    expect(button).toHaveAttribute('aria-expanded', 'false')
    await user.click(button)
    const again = document.getElementById(button.getAttribute('aria-controls')!)!
    expect(within(again).queryByRole('status', { name: 'Loading spans' })).not.toBeInTheDocument()
  })

  it('opens only the clicked row when identifiers are missing from a stale response', async () => {
    const user = userEvent.setup()
    vi.mocked(api.getTraces).mockResolvedValue({
      traces: [
        { traceId: 'legacy-1', rootSpan: 'legacy/root-1', serviceName: 'claude-code', duration: 1, spanCount: 1, status: 'OK', startTime: '2026-01-15T10:00:00Z' },
        { traceId: 'legacy-2', rootSpan: 'legacy/root-2', serviceName: 'claude-code', duration: 1, spanCount: 1, status: 'OK', startTime: '2026-01-15T10:01:00Z' },
      ],
      total: 2,
      hasMore: false,
    } as unknown as TracesResponse)
    renderPage()
    await screen.findByText('legacy/root-1', { selector: 'span' })

    await user.click(expandButton('legacy/root-1'))

    expect(expandButton('legacy/root-2')).toHaveAttribute('aria-expanded', 'false')
    await waitFor(() => expect(api.getTraceSpans).toHaveBeenCalledWith('legacy-1', 'otel_trace', expect.any(Object), undefined))
    expect(api.getTraceSpans).toHaveBeenCalledTimes(1)
  })

  it('shows a failed span load inline with a retry', async () => {
    const user = userEvent.setup()
    vi.mocked(api.getTraceSpans).mockRejectedValueOnce(httpError(500, 'span read failed'))
    renderPage()
    await screen.findByText('llm/chat', { selector: 'span' })
    await user.click(expandButton('llm/chat'))

    const alert = await screen.findByRole('alert')
    expect(alert).toHaveTextContent('span read failed')
    await user.click(within(alert).getByRole('button', { name: 'Retry' }))
    await waitFor(() => expect(api.getTraceSpans).toHaveBeenCalledTimes(2))
  })

  it('points to the detail page when the first page holds only part of the spans', async () => {
    const user = userEvent.setup()
    vi.mocked(api.getTraceSpans).mockResolvedValue({ ...spans, total: 4_200, hasMore: true, nextCursor: 'n' })
    renderPage()
    await screen.findByText('llm/chat', { selector: 'span' })
    await user.click(expandButton('llm/chat'))

    expect(await screen.findByText(/Showing the first 1 of 4,200 spans/)).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Open the trace' })).toHaveAttribute('href', '/traces/otel_trace/trace-1')
  })

  it('shows the error with its message and a retry, not "No traces"', async () => {
    const user = userEvent.setup()
    vi.mocked(api.getTraces).mockRejectedValueOnce(httpError(504))
    renderPage()

    const alert = await screen.findByRole('alert')
    expect(alert).toHaveTextContent("Couldn't load traces")
    expect(alert).toHaveTextContent('The request took too long')
    expect(screen.queryByText(/no traces/i)).not.toBeInTheDocument()

    await user.click(within(alert).getByRole('button', { name: 'Retry' }))
    await screen.findByText('llm/chat', { selector: 'span' })
  })

  it('shows an empty state for filters without results', async () => {
    vi.mocked(api.getTraces).mockResolvedValue({ traces: [], total: 0, hasMore: false })
    renderPage('/traces?search=nothing')
    expect(await screen.findByText('No traces match these filters')).toBeInTheDocument()
  })

  it('reads the filters from the URL and writes changes back', async () => {
    const user = userEvent.setup()
    renderPage('/traces?service=claude-code&search=read&background=true&timeframe=1h')

    await waitFor(() => expect(listCalls().length).toBeGreaterThan(0))
    expect(listCalls()[0]).toMatchObject({ service: 'claude-code', search: 'read', background: true })
    expect(screen.getByRole('switch', { name: 'Background requests' })).toBeChecked()
    expect(screen.getByRole('searchbox', { name: 'Search traces' })).toHaveValue('read')

    await user.click(screen.getByRole('switch', { name: 'Background requests' }))
    await user.selectOptions(screen.getByRole('combobox', { name: 'Service' }), '')
    const location = screen.getByTestId('location').textContent!
    expect(location).not.toContain('background=')
    expect(location).not.toContain('service=')
    expect(location).toContain('search=read')
    expect(location).toContain('timeframe=1h')
    await waitFor(() => expect(listCalls().at(-1)).not.toHaveProperty('background'))
  })

  it('searches with the debounced input', async () => {
    renderPage()
    await screen.findByText('llm/chat', { selector: 'span' })
    vi.useFakeTimers()
    const input = screen.getByRole('searchbox', { name: 'Search traces' })
    for (const value of ['e', 'er', 'err', 'erro', 'error']) {
      fireEvent.change(input, { target: { value } })
    }
    expect(listCalls().filter((p) => p.search)).toHaveLength(0)
    await act(() => vi.advanceTimersByTimeAsync(200))
    expect(listCalls().filter((p) => p.search).map((p) => p.search)).toEqual(['error'])
  })

  it('loads the new traces of the filtered list when "N new traces" is clicked', async () => {
    const user = userEvent.setup()
    renderPage('/traces?service=claude-code')
    await screen.findByText('llm/chat', { selector: 'span' })
    await user.click(screen.getByRole('button', { name: 'Go to next page' }))
    await screen.findByText('codex.turn', { selector: 'span' })

    newCount = 3
    await announceNewSpans(5)
    const button = await screen.findByRole('button', { name: '3 new traces' })
    expect(countCalls()[0]).toMatchObject({ service: 'claude-code', limit: 1 })
    const before = listCalls().length

    await user.click(button)

    await screen.findByText('llm/chat', { selector: 'span' })
    expect(listCalls().length).toBeGreaterThan(before)
    expect(listCalls().at(-1)?.cursor).toBeUndefined()
    expect(screen.getByText('Page 1 of 3')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /new traces?$/ })).not.toBeInTheDocument()
  })

  it('moves a relative window once a minute on page 1', async () => {
    vi.useFakeTimers({ toFake: ['Date', 'setInterval', 'clearInterval'] })
    vi.setSystemTime(new Date('2026-03-01T12:00:00Z'))
    renderPage('/traces?timeframe=24h')
    await vi.waitFor(() => expect(listCalls()).toHaveLength(1))
    const firstTo = Date.parse(listCalls()[0].to!)

    await act(async () => {
      vi.advanceTimersByTime(60_000)
    })

    await vi.waitFor(() => expect(listCalls()).toHaveLength(2))
    expect(Date.parse(listCalls()[1].to!) - firstTo).toBeGreaterThanOrEqual(60_000)
  })

  it('does not move an absolute range', async () => {
    vi.useFakeTimers({ toFake: ['Date', 'setInterval', 'clearInterval'] })
    vi.setSystemTime(new Date('2026-03-01T12:00:00Z'))
    renderPage('/traces?from=2026-02-01T00:00:00.000Z&to=2026-02-02T00:00:00.000Z')
    await vi.waitFor(() => expect(listCalls()).toHaveLength(1))
    expect(listCalls()[0]).toMatchObject({ from: '2026-02-01T00:00:00.000Z', to: '2026-02-02T00:00:00.000Z' })

    await act(async () => {
      vi.advanceTimersByTime(120_000)
    })
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 20))
    })
    expect(listCalls()).toHaveLength(1)
  })
})
