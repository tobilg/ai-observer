import { describe, expect, it } from 'vitest'
import { getTraceKind, getTraceRequestId, traceDetailPath } from './traces'

describe('trace helpers', () => {
  it('opens OTel traces by trace ID', () => {
    const trace = { id: 'row', kind: 'otel_trace' as const, traceId: 'abc', rootSpanId: 'root' }
    expect(getTraceKind(trace)).toBe('otel_trace')
    expect(getTraceRequestId(trace)).toBe('abc')
    expect(traceDetailPath(trace)).toBe('/traces/otel_trace/abc')
  })

  it('treats rows without a kind as OTel traces', () => {
    expect(getTraceKind({})).toBe('otel_trace')
  })

  it('opens Codex operations by their root span ID', () => {
    const trace = { id: 'op-1', kind: 'codex_operation' as const, traceId: 'abc', rootSpanId: 'span' }
    expect(getTraceRequestId(trace)).toBe('op-1')
    expect(getTraceRequestId({ ...trace, id: '' })).toBe('span')
    expect(traceDetailPath(trace)).toBe('/traces/codex_operation/op-1')
  })

  it('encodes the ID in the path', () => {
    expect(traceDetailPath({ id: '', traceId: 'a/b c', rootSpanId: '' })).toBe('/traces/otel_trace/a%2Fb%20c')
  })
})
