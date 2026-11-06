import { describe, it, expect } from 'vitest'
import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { WaterfallView } from '../WaterfallView'
import type { Span } from '@/types/traces'

const ms = 1_000_000 // nanoseconds

// root (0-100 ms) -> child (50-100 ms) -> grandchild (75-100 ms)
const spans: Span[] = [
  { timestamp: '2026-01-02T10:00:00.000Z', traceId: 't', spanId: 'root', spanName: 'root op', serviceName: 'svc', duration: 100 * ms, statusCode: 'OK' },
  {
    timestamp: '2026-01-02T10:00:00.050Z', traceId: 't', spanId: 'child', parentSpanId: 'root', spanName: 'child op', serviceName: 'svc',
    duration: 50 * ms, statusCode: 'ERROR', statusMessage: 'boom', spanAttributes: { 'http.method': 'GET' },
  },
  { timestamp: '2026-01-02T10:00:00.075Z', traceId: 't', spanId: 'grandchild', parentSpanId: 'child', spanName: 'grandchild op', serviceName: 'svc', duration: 25 * ms },
]

const row = (name: RegExp) => screen.getByRole('button', { name })

describe('WaterfallView', () => {
  it('indents spans under their parents', () => {
    render(<WaterfallView spans={spans} />)

    const indent = (name: RegExp) => (row(name).firstElementChild as HTMLElement).style.paddingLeft
    expect(indent(/^root op/)).toBe('0px')
    expect(indent(/^child op/)).toBe('16px')
    expect(indent(/^grandchild op/)).toBe('32px')
  })

  it('places duration bars relative to the trace', () => {
    const { container } = render(<WaterfallView spans={spans} />)

    const bars = Array.from(container.querySelectorAll<HTMLElement>('[style*="left"]'))
      .map((bar) => [bar.style.left, bar.style.width])
    expect(bars).toContainEqual(['0%', '100%'])
    expect(bars).toContainEqual(['50%', '50%'])
    expect(bars).toContainEqual(['75%', '25%'])
  })

  it('labels rows with their duration and status', () => {
    render(<WaterfallView spans={spans} />)
    expect(row(/^child op, 50\.00ms, ERROR$/)).toBeInTheDocument()
    expect(row(/^grandchild op, 25\.00ms, OK$/)).toBeInTheDocument()
  })

  it('expands span details on click', async () => {
    const user = userEvent.setup()
    render(<WaterfallView spans={spans} />)

    const child = row(/^child op/)
    expect(child).toHaveAttribute('aria-expanded', 'false')
    await user.click(child)

    expect(child).toHaveAttribute('aria-expanded', 'true')
    const details = document.getElementById('span-details-child')!
    expect(within(details).getByText('http.method')).toBeInTheDocument()
    expect(within(details).getByText('GET')).toBeInTheDocument()
  })

  it('collapses the children of a span', async () => {
    const user = userEvent.setup()
    render(<WaterfallView spans={spans} />)

    const toggle = row(/^child op/).querySelector<HTMLElement>('[aria-hidden="true"]')!
    await user.click(toggle)

    expect(screen.queryByRole('button', { name: /^grandchild op/ })).not.toBeInTheDocument()
    expect(row(/^child op/)).toBeInTheDocument()
  })
})
