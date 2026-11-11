import { describe, expect, it } from 'vitest'
import { logRowKey, traceRowKey, uniqueRowKeys } from './rowKeys'
import type { LogRecord } from '@/types/logs'
import type { TraceOverview } from '@/types/traces'

const log: LogRecord = { timestamp: '2026-01-01T00:00:00Z', serviceName: 'claude-code', body: 'hello' }

describe('rowKeys', () => {
  it('derives a log key from the record, not its position', () => {
    expect(logRowKey(log)).toBe(logRowKey({ ...log }))
    expect(logRowKey(log)).not.toBe(logRowKey({ ...log, body: 'other' }))
    expect(logRowKey({ ...log, body: 'x'.repeat(100_000) }).length).toBeLessThan(100)
  })

  it('keys traces by the view they open', () => {
    const trace = { id: 'op', kind: 'codex_operation', traceId: 't', rootSpanId: 'op' } as TraceOverview
    expect(traceRowKey(trace)).toBe('codex_operation:op')
  })

  it('numbers repeated keys', () => {
    expect(uniqueRowKeys(['a', 'b', 'a', 'a'], (x) => x)).toEqual(['a', 'b', 'a#2', 'a#3'])
  })
})
