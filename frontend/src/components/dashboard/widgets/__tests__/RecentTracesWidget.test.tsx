import { describe, it, expect, vi } from 'vitest'
import { screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { Route, Routes, useLocation } from 'react-router-dom'
import { RecentTracesWidget } from '../RecentTracesWidget'
import { renderWithProviders } from '@/test/queryWrapper'
import type { TraceOverview, TracesResponse } from '@/types/traces'
import type { WidgetDataState } from '../types'

const trace = (overrides: Partial<TraceOverview>): TraceOverview => ({
  id: 't1', kind: 'otel_trace', traceId: 't1', rootSpanId: 's1', rootSpan: 'turn/start', serviceName: 'codex-app-server',
  startTime: new Date().toISOString(), duration: 1_500_000_000, spanCount: 3, status: 'OK',
  ...overrides,
})

function state(data: TracesResponse | undefined, overrides: Partial<WidgetDataState<TracesResponse>> = {}): WidgetDataState<TracesResponse> {
  return { data, isPending: !data, isError: false, error: null, isFetching: false, refetch: vi.fn(), ...overrides }
}

// Shows where a trace link led, and the state it passed
function TracePage() {
  const location = useLocation()
  return <div data-testid="trace-page">{`${location.pathname} from ${(location.state as { from?: string })?.from}`}</div>
}

function renderWidget(traces: WidgetDataState<TracesResponse>) {
  return renderWithProviders(
    <Routes>
      <Route path="/" element={<RecentTracesWidget title="Recent Traces" traces={traces} />} />
      <Route path="/traces/:kind/:id" element={<TracePage />} />
    </Routes>,
    { route: '/?timeframe=1h' }
  )
}

describe('RecentTracesWidget', () => {
  it('links each trace to its detail page with an encoded ID, returning to the dashboard', async () => {
    const user = userEvent.setup()
    renderWidget(state({
      traces: [trace({}), trace({ id: 'op/1', kind: 'codex_operation', traceId: 'big', rootSpan: 'op', serviceName: 'codex_cli_rs' })],
      total: 2,
      hasMore: false,
    }))

    const links = screen.getAllByRole('link')
    expect(links[0]).toHaveAttribute('href', '/traces/otel_trace/t1')
    expect(links[1]).toHaveAttribute('href', '/traces/codex_operation/op%2F1')
    // Service display names
    expect(screen.getAllByText(/OpenAI Codex/)).toHaveLength(2)

    await user.click(links[1])
    expect(screen.getByTestId('trace-page')).toHaveTextContent('/traces/codex_operation/op%2F1 from /?timeframe=1h')
  })

  it('says when the range has no traces, and shows errors with a retry', async () => {
    const user = userEvent.setup()
    const { unmount } = renderWidget(state({ traces: [], total: 0, hasMore: false }))
    expect(screen.getByText('No traces in the selected range yet.')).toBeInTheDocument()
    unmount()

    const refetch = vi.fn()
    renderWidget(state(undefined, { isPending: false, isError: true, error: new TypeError('Failed to fetch'), refetch }))
    expect(screen.getByRole('alert')).toHaveTextContent('Cannot reach the AI Observer server.')
    await user.click(screen.getByRole('button', { name: 'Retry' }))
    expect(refetch).toHaveBeenCalled()
  })
})
