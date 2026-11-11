import { describe, it, expect } from 'vitest'
import {
  buildSpanTree,
  filterSpanTree,
  flattenTree,
  formatAttributeValue,
  formatAxisTime,
  isLongValue,
  timeAxisTicks,
  valuePreview,
} from '../waterfall'
import type { Span } from '@/types/traces'

const ms = 1_000_000

const span = (spanId: string, offsetMs: number, parentSpanId?: string, fields: Partial<Span> = {}): Span => ({
  timestamp: new Date(Date.UTC(2026, 0, 2, 10) + offsetMs).toISOString(),
  traceId: 't', spanId, parentSpanId, spanName: `op ${spanId}`, serviceName: 'svc', duration: 10 * ms, ...fields,
})

describe('waterfall', () => {
  it('builds the span tree sorted by start, with spans of unknown parents as roots', () => {
    const roots = buildSpanTree([span('b', 5, 'a'), span('a', 0), span('c', 1, 'a'), span('orphan', 2, 'missing')])
    expect(roots.map((n) => n.span.spanId)).toEqual(['a', 'orphan'])
    expect(roots[0].children.map((n) => [n.span.spanId, n.depth])).toEqual([['c', 1], ['b', 1]])
  })

  it('flattens the tree, leaving out collapsed children unless filtering', () => {
    const roots = buildSpanTree([span('a', 0), span('b', 1, 'a'), span('c', 2, 'b'), span('d', 3)])
    const ids = (nodes: ReturnType<typeof flattenTree>) => nodes.map((n) => n.span.spanId)
    expect(ids(flattenTree(roots, new Set()))).toEqual(['a', 'b', 'c', 'd'])
    expect(ids(flattenTree(roots, new Set(['b'])))).toEqual(['a', 'b', 'd'])
    expect(ids(flattenTree(roots, new Set(['a']), new Set(['a', 'b', 'c'])))).toEqual(['a', 'b', 'c'])
  })

  it('filters by text and errors, keeping the ancestors of matches', () => {
    const roots = buildSpanTree([
      span('a', 0),
      span('b', 1, 'a', { spanAttributes: { 'gen_ai.prompt': 'Fix the Login bug' } }),
      span('c', 2, 'b', { statusCode: 'ERROR' }),
      span('d', 3, undefined, { resourceAttributes: { host: 'login-box' } }),
    ])
    expect(filterSpanTree(roots, { query: ' ', errorsOnly: false })).toBeNull()

    const text = filterSpanTree(roots, { query: 'LOGIN', errorsOnly: false })!
    expect([...text.matches].sort()).toEqual(['b', 'd'])
    expect([...text.visible].sort()).toEqual(['a', 'b', 'd'])

    const errors = filterSpanTree(roots, { query: '', errorsOnly: true })!
    expect([...errors.matches]).toEqual(['c'])
    expect([...errors.visible].sort()).toEqual(['a', 'b', 'c'])

    expect([...filterSpanTree(roots, { query: 'login', errorsOnly: true })!.matches]).toEqual([])
  })

  it('makes time axis ticks of round steps', () => {
    expect(timeAxisTicks(100).map((t) => t.label)).toEqual(['0', '20ms', '40ms', '60ms', '80ms', '100ms'])
    expect(timeAxisTicks(2500).map((t) => t.label)).toEqual(['0', '0.5s', '1s', '1.5s', '2s', '2.5s'])
    expect(timeAxisTicks(0.4).map((t) => t.label)).toEqual(['0', '100µs', '200µs', '300µs', '400µs'])
    expect(timeAxisTicks(0)).toEqual([{ offsetMs: 0, percent: 0, label: '0' }])
    expect(timeAxisTicks(130).map((t) => t.offsetMs)).toEqual([0, 50, 100])
    expect(formatAxisTime(180_000, 60_000)).toBe('3m')
  })

  it('pretty-prints JSON values and shortens long ones', () => {
    expect(formatAttributeValue('{"a":1}')).toEqual({ text: '{\n  "a": 1\n}', json: true })
    expect(formatAttributeValue('{not json}')).toEqual({ text: '{not json}', json: false })
    expect(formatAttributeValue('plain')).toEqual({ text: 'plain', json: false })

    expect(isLongValue('x'.repeat(300))).toBe(false)
    expect(isLongValue('x'.repeat(301))).toBe(true)
    expect(isLongValue('1\n2\n3\n4\n5')).toBe(false)
    expect(isLongValue('1\n2\n3\n4\n5\n6')).toBe(true)
    expect(valuePreview('1\n2\n3\n4\n5\n6')).toBe('1\n2\n3\n4\n5…')
    expect(valuePreview('y'.repeat(400))).toBe('y'.repeat(300) + '…')
  })
})
