import { describe, it, expect } from 'vitest'
import { traceLogsPath } from './listLinks'

describe('traceLogsPath', () => {
  it('filters the Logs page by the trace ID over the time around the trace', () => {
    const path = traceLogsPath('abc/1', '2024-01-15T10:00:00Z', 2_000_000_000)
    const url = new URL(path, 'http://localhost')
    expect(url.pathname).toBe('/logs')
    expect(url.searchParams.get('traceId')).toBe('abc/1')
    expect(url.searchParams.get('from')).toBe('2024-01-15T09:00:00.000Z')
    expect(url.searchParams.get('to')).toBe('2024-01-15T11:00:02.000Z')
  })

  it('leaves the range to the page when the start time is unknown', () => {
    expect(traceLogsPath('abc', '', 0)).toBe('/logs?traceId=abc')
  })
})
