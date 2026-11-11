import { describe, it, expect } from 'vitest'
import { backLinkFor } from './backLink'

describe('backLinkFor', () => {
  it('goes back to the page that opened the detail page, with its filters', () => {
    expect(backLinkFor('/traces?service=codex', '/traces')).toEqual({ to: '/traces?service=codex', label: 'Back to traces' })
    expect(backLinkFor('/sessions?search=bug', '/sessions')).toEqual({ to: '/sessions?search=bug', label: 'Back to sessions' })
    expect(backLinkFor('/logs?traceId=t1', '/sessions')).toEqual({ to: '/logs?traceId=t1', label: 'Back to logs' })
    expect(backLinkFor('/traces/otel_trace/t1', '/sessions')).toEqual({ to: '/traces/otel_trace/t1', label: 'Back to trace' })
    expect(backLinkFor('/', '/traces')).toEqual({ to: '/', label: 'Back to dashboard' })
  })

  it('falls back to the list page without a path of this app', () => {
    expect(backLinkFor(undefined, '/traces')).toEqual({ to: '/traces', label: 'Back to traces' })
    expect(backLinkFor(42, '/sessions')).toEqual({ to: '/sessions', label: 'Back to sessions' })
    expect(backLinkFor('//evil.example/x', '/traces').to).toBe('/traces')
    expect(backLinkFor('https://evil.example', '/traces').to).toBe('/traces')
  })
})
