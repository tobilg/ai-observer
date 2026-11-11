import { describe, it, expect } from 'vitest'
import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { WaterfallView } from '../WaterfallView'
import type { Span, TraceOverview } from '@/types/traces'

const ms = 1_000_000 // nanoseconds

// root (0-100 ms) -> child (50-100 ms) -> grandchild (75-100 ms)
const spans: Span[] = [
  {
    timestamp: '2026-01-02T10:00:00.000Z', traceId: 't', spanId: 'root', spanName: 'root op', serviceName: 'svc', duration: 100 * ms, statusCode: 'OK',
    resourceAttributes: { 'service.version': '1.2.3' }, scopeName: 'agent', scopeVersion: '0.9', spanKind: 'SERVER', sessionId: 'sess-1',
    links: [{ traceId: 'other-trace', spanId: 'other-span', attributes: { reason: 'follows' } }],
  },
  {
    timestamp: '2026-01-02T10:00:00.050Z', traceId: 't', spanId: 'child', parentSpanId: 'root', spanName: 'child op', serviceName: 'svc',
    duration: 50 * ms, statusCode: 'ERROR', statusMessage: 'boom', spanAttributes: { 'http.method': 'GET' },
    events: [{ timestamp: '2026-01-02T10:00:00.060Z', name: 'retry', attributes: { attempt: '2' } }],
  },
  { timestamp: '2026-01-02T10:00:00.075Z', traceId: 't', spanId: 'grandchild', parentSpanId: 'child', spanName: 'grandchild op', serviceName: 'svc', duration: 25 * ms },
]

const row = (name: RegExp) => screen.getByRole('button', { name })
const indent = (name: RegExp) => (row(name).previousElementSibling as HTMLElement).style.width

describe('WaterfallView', () => {
  it('indents spans under their parents', () => {
    render(<WaterfallView spans={spans} />)
    expect(indent(/^root op/)).toBe('16px')
    expect(indent(/^child op/)).toBe('32px')
    expect(indent(/^grandchild op/)).toBe('48px')
  })

  it('places duration bars relative to the trace', () => {
    const { container } = render(<WaterfallView spans={spans} />)

    const bars = Array.from(container.querySelectorAll<HTMLElement>('[style*="width"][style*="left"]'))
      .map((bar) => [bar.style.left, bar.style.width])
    expect(bars).toContainEqual(['0%', '100%'])
    expect(bars).toContainEqual(['50%', '50%'])
    expect(bars).toContainEqual(['75%', '25%'])
  })

  it('draws a time axis with offsets from the trace start', () => {
    render(<WaterfallView spans={spans} />)
    const axis = screen.getByTestId('time-axis')
    const labels = Array.from(axis.querySelectorAll('span')).map((s) => s.textContent)
    expect(labels).toEqual(['0', '20ms', '40ms', '60ms', '80ms', '100ms'])
  })

  it('labels rows with their duration and status, and the start offset in the tooltip', () => {
    render(<WaterfallView spans={spans} />)
    expect(row(/^child op, 50\.00ms, ERROR$/)).toHaveAttribute('title', 'child op\nStarts +50.00ms · 50.00ms')
    expect(row(/^grandchild op, 25\.00ms, OK$/)).toBeInTheDocument()
  })

  it('expands span details on click, with attributes, events and their attributes', async () => {
    const user = userEvent.setup()
    render(<WaterfallView spans={spans} />)

    const child = row(/^child op/)
    expect(child).toHaveAttribute('aria-expanded', 'false')
    await user.click(child)

    expect(child).toHaveAttribute('aria-expanded', 'true')
    const details = document.getElementById('span-details-child')!
    expect(child).toHaveAttribute('aria-controls', 'span-details-child')
    expect(within(details).getByText('http.method')).toBeInTheDocument()
    expect(within(details).getByText('GET')).toBeInTheDocument()
    expect(within(details).getByText('boom')).toBeInTheDocument()
    expect(within(details).getByText('+50.00ms', { exact: false })).toBeInTheDocument()
    // The event, 10 ms after the span started, with its attributes
    expect(within(details).getByText('Events (1)')).toBeInTheDocument()
    expect(within(details).getByText('+10.00ms')).toBeInTheDocument()
    expect(within(details).getByText('attempt')).toBeInTheDocument()
    expect(within(details).getByRole('button', { name: 'Copy span ID' })).toBeInTheDocument()
    expect(within(details).getByRole('button', { name: 'Copy trace ID' })).toBeInTheDocument()
  })

  it('shows the resource, scope, kind, links and session of a span', async () => {
    const user = userEvent.setup()
    render(<WaterfallView spans={spans} renderSessionLink={(id) => <a href={`/sessions/${id}`}>session {id}</a>} />)

    await user.click(row(/^root op/))
    const details = document.getElementById('span-details-root')!
    expect(within(details).getByText('Resource')).toBeInTheDocument()
    expect(within(details).getByText('service.version')).toBeInTheDocument()
    expect(within(details).getByText('agent 0.9')).toBeInTheDocument()
    expect(within(details).getByText('SERVER')).toBeInTheDocument()
    expect(within(details).getByText('Links (1)')).toBeInTheDocument()
    expect(within(details).getByText('other-trace')).toBeInTheDocument()
    expect(within(details).getByText('follows')).toBeInTheDocument()
    expect(within(details).getByRole('link', { name: 'session sess-1' })).toHaveAttribute('href', '/sessions/sess-1')
  })

  it('shows long attribute values shortened until expanded, and JSON pretty-printed', async () => {
    const user = userEvent.setup()
    const prompt = 'Explain the bug. '.repeat(40)
    render(
      <WaterfallView
        allExpanded
        spans={[{
          ...spans[2], parentSpanId: undefined,
          spanAttributes: { 'gen_ai.prompt': prompt, 'tool.input': '{"path":"main.go","line":3}' },
        }]}
      />
    )

    const details = document.getElementById('span-details-grandchild')!
    expect(within(details).getByText('{\n  "path": "main.go",\n  "line": 3\n}', { normalizer: (t) => t })).toBeInTheDocument()
    expect(within(details).queryByText(prompt)).not.toBeInTheDocument()

    await user.click(within(details).getByRole('button', { name: /^Show all \(680 characters\)/ }))
    expect(within(details).getByText(prompt.trim(), { normalizer: (t) => t.trim() })).toBeInTheDocument()
    expect(within(details).getByRole('button', { name: 'Show less' })).toHaveAttribute('aria-expanded', 'true')
    expect(within(details).getByRole('button', { name: 'Copy gen_ai.prompt' })).toBeInTheDocument()
  })

  it('collapses the children of a span with a button', async () => {
    const user = userEvent.setup()
    render(<WaterfallView spans={spans} />)

    const toggle = screen.getByRole('button', { name: 'Hide the 1 child spans of child op' })
    expect(toggle).toHaveAttribute('aria-expanded', 'true')
    await user.click(toggle)

    expect(screen.queryByRole('button', { name: /^grandchild op/ })).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Show the 1 child spans of child op' })).toHaveAttribute('aria-expanded', 'false')
    // The details toggle of the row is separate
    expect(row(/^child op/)).toHaveAttribute('aria-expanded', 'false')
  })

  it('collapses and expands all spans', async () => {
    const user = userEvent.setup()
    render(<WaterfallView spans={spans} filterable />)

    await user.click(screen.getByRole('button', { name: 'Collapse all' }))
    expect(screen.queryByRole('button', { name: /^child op/ })).not.toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Expand all' }))
    expect(row(/^grandchild op/)).toBeInTheDocument()
  })

  it('filters spans by name or attribute, keeping their ancestors', async () => {
    const user = userEvent.setup()
    const trace: TraceOverview = {
      id: 't', kind: 'otel_trace', traceId: 't', rootSpanId: 'root', rootSpan: 'root op', serviceName: 'svc',
      startTime: '2026-01-02T10:00:00.000Z', duration: 100 * ms, spanCount: 10, status: 'ERROR',
    }
    render(<WaterfallView spans={spans} trace={trace} filterable />)

    await user.type(screen.getByRole('searchbox', { name: 'Filter spans' }), 'grandchild')
    expect(await screen.findByText('1 of 3 loaded spans match · 7 more spans are not loaded yet')).toBeInTheDocument()
    expect(row(/^grandchild op/).parentElement).toHaveAttribute('data-match', 'match')
    expect(row(/^child op/).parentElement).toHaveAttribute('data-match', 'context')
    expect(within(row(/^grandchild op/)).getByText('grandchild').tagName).toBe('MARK')

    // Attribute keys and values match too
    await user.clear(screen.getByRole('searchbox', { name: 'Filter spans' }))
    await user.type(screen.getByRole('searchbox', { name: 'Filter spans' }), 'http.method')
    expect(await screen.findByText(/^1 of 3 loaded spans match/)).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /^grandchild op/ })).not.toBeInTheDocument()
    expect(row(/^child op/).parentElement).toHaveAttribute('data-match', 'match')

    await user.clear(screen.getByRole('searchbox', { name: 'Filter spans' }))
    await user.type(screen.getByRole('searchbox', { name: 'Filter spans' }), 'nothing like it')
    expect(await screen.findByText(/^No loaded span matches/)).toBeInTheDocument()
  })

  it('shows only failed spans and their ancestors', async () => {
    const user = userEvent.setup()
    render(<WaterfallView spans={spans} filterable />)

    const errorsOnly = screen.getByRole('button', { name: 'Errors only' })
    expect(errorsOnly).toHaveAttribute('aria-pressed', 'false')
    await user.click(errorsOnly)
    expect(errorsOnly).toHaveAttribute('aria-pressed', 'true')
    expect(screen.getByText('1 of 3 loaded spans match')).toBeInTheDocument()
    expect(row(/^root op/)).toBeInTheDocument()
    expect(row(/^child op/)).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /^grandchild op/ })).not.toBeInTheDocument()
  })
})
