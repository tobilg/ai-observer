import { describe, it, expect } from 'vitest'
import {
  errorOf,
  goDurationSeconds,
  isBucketed,
  isCounter,
  metricChart,
  metricLabel,
  metricTotals,
  sessionCounts,
  sessionPlace,
  sessionsCost,
  structuredOf,
  traceSpans,
  usageBars,
  usageDaily,
  viewOf,
} from './data'
import type { MetricsResult, SessionRow, TraceResult, UsageSummary } from './types'

const usage = (groupBy: UsageSummary['group_by'], rows: UsageSummary['rows']): UsageSummary => ({
  from: '2026-11-01T00:00:00Z',
  to: '2026-11-04T00:00:00Z',
  group_by: groupBy,
  rows,
  total_cost_usd: rows.reduce((sum, r) => sum + r.cost_usd, 0),
  note: '',
})

describe('viewOf', () => {
  it('picks the view by the tool the host names', () => {
    expect(viewOf('usage_summary', {})).toBe('usage')
    expect(viewOf('query_metrics', undefined)).toBe('metrics')
    expect(viewOf('get_trace', {})).toBe('trace')
    expect(viewOf('list_sessions', {})).toBe('sessions')
  })

  it('falls back to the shape of the result', () => {
    expect(viewOf(undefined, { group_by: 'tool', rows: [] })).toBe('usage')
    expect(viewOf(undefined, { name: 'm', series: [] })).toBe('metrics')
    expect(viewOf(undefined, { kind: 'otel_trace', spans: [] })).toBe('trace')
    expect(viewOf(undefined, { total: 0, sessions: [] })).toBe('sessions')
    // search_conversations has sessions but no total, and no view
    expect(viewOf('search_conversations', { terms: [], sessions: [] })).toBeUndefined()
    expect(viewOf(undefined, 'text')).toBeUndefined()
  })
})

describe('tool results', () => {
  it('reads the structured content, or the text as JSON', () => {
    expect(structuredOf({ structuredContent: { a: 1 }, content: [{ type: 'text', text: '{"a":2}' }] })).toEqual({ a: 1 })
    expect(structuredOf({ content: [{ type: 'text', text: '{"a":2}' }] })).toEqual({ a: 2 })
    expect(structuredOf({ content: [{ type: 'text', text: 'not json' }] })).toBeUndefined()
  })

  it('returns the error text of failed calls only', () => {
    expect(errorOf({ isError: true, content: [{ type: 'text', text: 'no trace "x"' }] })).toBe('no trace "x"')
    expect(errorOf({ isError: true })).toBe('The tool call failed')
    expect(errorOf({ content: [{ type: 'text', text: '{}' }] })).toBeUndefined()
  })
})

describe('goDurationSeconds', () => {
  it('reads Go durations', () => {
    expect(goDurationSeconds('1h0m0s')).toBe(3600)
    expect(goDurationSeconds('28m48s')).toBe(1728)
    expect(goDurationSeconds('1m30.5s')).toBe(90.5)
    expect(goDurationSeconds('500ms')).toBe(0.5)
  })

  it('rejects anything else', () => {
    expect(goDurationSeconds(undefined)).toBeUndefined()
    expect(goDurationSeconds('')).toBeUndefined()
    expect(goDurationSeconds('1 hour')).toBeUndefined()
    expect(goDurationSeconds('0s')).toBeUndefined()
  })
})

describe('usage', () => {
  it('ranks models with their tool, leaving out empty rows', () => {
    const summary = usage('model', [
      { tool: 'codex', model: 'gpt-5', cost_usd: 1, total_tokens: 900 },
      { tool: 'claude-code', model: 'claude-opus-5-5', cost_usd: 3, total_tokens: 100 },
      { tool: 'gemini', model: 'gemini-3', cost_usd: 0, total_tokens: 0 },
    ])
    expect(usageBars(summary, 'cost')).toEqual([
      { key: 'claude-code/claude-opus-5-5', label: 'claude-opus-5-5', detail: 'Claude Code', value: 3 },
      { key: 'codex/gpt-5', label: 'gpt-5', detail: 'OpenAI Codex', value: 1 },
    ])
    expect(usageBars(summary, 'tokens').map((b) => b.value)).toEqual([900, 100])
  })

  it('labels bars by tool when grouped by tool', () => {
    const bars = usageBars(usage('tool', [{ tool: 'copilot', cost_usd: 2, total_tokens: 5 }]), 'cost')
    expect(bars).toEqual([{ key: 'copilot/', label: 'GitHub Copilot', detail: undefined, value: 2 }])
  })

  it('stacks days by tool', () => {
    const chart = usageDaily(
      usage('day', [
        { tool: 'claude-code', day: '2026-11-01', cost_usd: 1, total_tokens: 10 },
        { tool: 'codex', day: '2026-11-01', cost_usd: 2, total_tokens: 20 },
        { tool: 'claude-code', day: '2026-11-02', cost_usd: 3, total_tokens: 30 },
      ]),
      'tokens'
    )
    expect(chart.series).toEqual([
      { key: 'claude-code', label: 'Claude Code' },
      { key: 'codex', label: 'OpenAI Codex' },
    ])
    expect(chart.data.map((row) => [row.timestamp, row['claude-code'], row.codex])).toEqual([
      [Date.parse('2026-11-01T00:00:00Z'), 10, 20],
      [Date.parse('2026-11-02T00:00:00Z'), 30, 0],
    ])
  })
})

describe('metrics', () => {
  const bucketed: MetricsResult = {
    name: 'claude_code.token.usage',
    from: '2026-11-01T00:00:00Z',
    to: '2026-11-01T02:00:00Z',
    interval: '1h0m0s',
    series: [
      { labels: { service: 'claude-code', type: 'output' }, points: [{ time: '2026-11-01T01:00:00Z', value: 5 }] },
      {
        labels: { type: 'input', service: 'claude-code' },
        points: [
          { time: '2026-11-01T00:00:00Z', value: 1 },
          { time: '2026-11-01T01:00:00Z', value: 2 },
        ],
      },
    ],
  }

  it('labels series by service, then the other labels', () => {
    expect(metricLabel({ type: 'input', service: 'claude-code', model: 'm' })).toBe('Claude Code · m · input')
    expect(metricLabel(undefined)).toBe('value')
  })

  it('turns buckets into chart rows', () => {
    expect(isBucketed(bucketed)).toBe(true)
    const chart = metricChart(bucketed)
    expect(chart.series.map((s) => s.label).sort()).toEqual(['Claude Code · input', 'Claude Code · output'])
    expect(chart.data).toHaveLength(2)
    const input = chart.series.find((s) => s.label.endsWith('input'))!.key
    expect(chart.data.map((row) => row[input])).toEqual([1, 2])
  })

  it('uses the reported OTLP type ahead of name-based guesses', () => {
    expect(isCounter({ ...bucketed, name: 'custom.depth', metric_type: 'gauge' })).toBe(false)
    expect(isCounter({ ...bucketed, name: 'custom.depth', metric_type: 'sum' })).toBe(true)
    expect(isCounter(bucketed)).toBe(true)
  })

  it('keeps missing gauge and percentile readings separate from observed zeros', () => {
    const t0 = bucketed.from
    const t1 = '2026-11-01T01:00:00Z'
    const t2 = bucketed.to
    const series = [
      { labels: { model: 'A' }, points: [{ time: t0, value: 50 }, { time: t2, value: 0 }] },
      { labels: { model: 'B' }, points: [{ time: t1, value: 100 }] },
    ]
    for (const type of ['gauge', 'histogram'] as const) {
      const chart = metricChart({ ...bucketed, metric_type: type, stat: type === 'histogram' ? 'p95' : undefined, series })
      const [a, b] = chart.series
      expect(chart.data.map((p) => [p[a.key], p[b.key]])).toEqual([[50, null], [null, 100], [0, null]])
    }
    const lone = metricChart({ ...bucketed, metric_type: 'gauge', series: [
      { points: [{ time: t0, value: 50 }, { time: t2, value: 50, gap_start: t1 }] },
    ] })
    expect(lone.data.map((p) => p[lone.series[0].key])).toEqual([50, null, 50])

    const additive = metricChart({ ...bucketed, metric_type: 'histogram', stat: 'count', series })
    expect(additive.data.map((p) => p[additive.series[0].key])).toEqual([50, 0, 0])
  })

  it('keeps empty bucketed responses in time-series mode and omits unmeasured aggregate series', () => {
    const empty = { ...bucketed, series: [{ points: [] }] }
    expect(isBucketed(empty)).toBe(true)
    expect(metricChart(empty).data).toEqual([])
    const aggregate = { ...empty, interval: undefined }
    expect(isBucketed(aggregate)).toBe(false)
    expect(metricTotals(aggregate)).toEqual([])
  })

  it('labels day buckets with their date in the zone of the result', () => {
    // Midnight in Kolkata is 18:30 UTC the day before
    const daily: MetricsResult = {
      name: 'claude_code.cost.usage',
      from: '2026-11-01T00:00:00Z',
      to: '2026-11-04T00:00:00Z',
      interval: '24h0m0s',
      timezone: 'Asia/Kolkata',
      series: [{ labels: { service: 'claude-code' }, points: [{ time: '2026-11-02T00:00:00+05:30', value: 3 }] }],
    }
    const expected = new Date('2026-11-02T00:00:00+05:30').toLocaleDateString([], { month: 'short', day: 'numeric', timeZone: 'Asia/Kolkata' })
    expect(metricChart(daily).data[0].time).toBe(expected)
    expect(expected).toBe(new Date('2026-11-02T12:00:00Z').toLocaleDateString([], { month: 'short', day: 'numeric', timeZone: 'UTC' }))
  })

  it('ranks totals', () => {
    const totals: MetricsResult = {
      name: 'claude_code.cost.usage',
      from: bucketed.from,
      to: bucketed.to,
      series: [
        { labels: { model: 'a' }, points: [{ value: 1 }] },
        { labels: { model: 'b' }, points: [{ value: 4 }] },
      ],
    }
    expect(isBucketed(totals)).toBe(false)
    expect(metricTotals(totals).map((t) => [t.label, t.value])).toEqual([
      ['b', 4],
      ['a', 1],
    ])
  })
})

describe('traceSpans', () => {
  it('converts spans for the waterfall', () => {
    const trace: TraceResult = {
      id: 't1',
      kind: 'otel_trace',
      total_spans: 1,
      spans: [
        {
          span_id: 's1',
          parent_span_id: 'p',
          depth: 1,
          name: 'tool call',
          tool: 'codex',
          start_time: '2026-11-01T00:00:01.250Z',
          duration_ms: 1.5,
          status: 'ERROR',
          status_message: 'failed',
          attributes: { command: 'ls' },
        },
      ],
    }
    expect(traceSpans(trace)).toEqual([
      {
        timestamp: '2026-11-01T00:00:01.250Z',
        traceId: 't1',
        spanId: 's1',
        parentSpanId: 'p',
        spanName: 'tool call',
        serviceName: 'codex',
        spanAttributes: { command: 'ls' },
        duration: 1_500_000,
        statusCode: 'ERROR',
        statusMessage: 'failed',
      },
    ])
  })
})

describe('sessionCounts', () => {
  it('counts sessions per tool, most first', () => {
    const session = (tool: string): SessionRow => ({ session_id: tool, tool, start_time: '', last_time: '', messages: 1 })
    expect(sessionCounts([session('codex'), session('claude-code'), session('codex')])).toEqual([
      { tool: 'codex', count: 2 },
      { tool: 'claude-code', count: 1 },
    ])
  })
})

describe('sessionsCost', () => {
  const session = (cost?: number): SessionRow => ({
    session_id: 's',
    tool: 'claude-code',
    start_time: '',
    last_time: '',
    messages: 1,
    cost_usd: cost,
  })
  it('sums the sessions that record a cost', () => {
    expect(sessionsCost([session(1.5), session(), session(0.25)])).toBe(1.75)
  })
  it('is undefined when no session records one', () => {
    expect(sessionsCost([session(), session()])).toBeUndefined()
    expect(sessionsCost([session(0)])).toBe(0)
  })
})

describe('sessionPlace', () => {
  const base: SessionRow = { session_id: 's', tool: 'codex', start_time: '', last_time: '', messages: 1 }
  it('names repository and branch when known', () => {
    expect(sessionPlace({ ...base, repository: 'acme/api', branch: 'main' })).toBe('acme/api @ main')
    expect(sessionPlace({ ...base, repository: 'acme/api' })).toBe('acme/api')
    expect(sessionPlace(base)).toBeUndefined()
  })
})
