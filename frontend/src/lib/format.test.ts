import { describe, expect, it } from 'vitest'
import {
  formatBytes,
  formatCost,
  formatCount,
  formatDurationMs,
  formatDurationNanos,
  formatTimeSpan,
  formatTokenUsage,
  formatTokens,
} from './format'

describe('formatCount', () => {
  it('groups thousands the same way in every locale', () => {
    expect(formatCount(1234567)).toBe('1,234,567')
    expect(formatCount(0)).toBe('0')
    expect(formatCount(Number.NaN)).toBe('—')
  })
})

describe('formatDurationNanos', () => {
  it('formats span durations', () => {
    expect(formatDurationNanos(512_000)).toBe('512.00µs')
    expect(formatDurationNanos(12_340_000)).toBe('12.34ms')
    expect(formatDurationNanos(1_500_000_000)).toBe('1.50s')
  })
})

describe('formatDurationMs', () => {
  it('formats milliseconds up to hours', () => {
    expect(formatDurationMs(850)).toBe('850ms')
    expect(formatDurationMs(2500)).toBe('2.5s')
    expect(formatDurationMs(90_000)).toBe('1.5m')
    expect(formatDurationMs(7_500_000)).toBe('2h 5m')
  })
})

describe('formatTimeSpan', () => {
  const start = '2026-01-01T10:00:00Z'
  const after = (ms: number) => new Date(new Date(start).getTime() + ms)

  it('formats short spans', () => {
    expect(formatTimeSpan(start, after(45_000))).toBe('45s')
    expect(formatTimeSpan(start, after(12 * 60_000))).toBe('12m')
    expect(formatTimeSpan(start, after(125 * 60_000))).toBe('2h 5m')
  })

  it('spells out long spans', () => {
    expect(formatTimeSpan(start, after(1000), 'long')).toBe('1 second')
    expect(formatTimeSpan(start, after(45_000), 'long')).toBe('45 seconds')
    expect(formatTimeSpan(start, after(60_000), 'long')).toBe('1 minute')
    expect(formatTimeSpan(start, after(125 * 60_000), 'long')).toBe('2h 5m')
  })

  it('never shows a negative span', () => {
    expect(formatTimeSpan(after(5000), start)).toBe('0s')
  })
})

describe('tokens', () => {
  it('formats a token count', () => {
    expect(formatTokens(1234)).toBe('1,234 tokens')
    expect(formatTokens(1)).toBe('1 token')
    expect(formatTokens(1234, false)).toBe('1,234')
  })

  it('formats token usage by type', () => {
    expect(formatTokenUsage({ input: 1200, output: 340, cacheRead: 5000, cacheWrite: 800 })).toBe(
      '1,200 in / 340 out / 5,000 cache / 800 cached',
    )
    expect(formatTokenUsage({ output: 5 })).toBe('5 out')
    expect(formatTokenUsage({})).toBe('')
  })
})

describe('formatCost', () => {
  it('shows cents from a dollar and fractions of a cent below', () => {
    expect(formatCost(12.345)).toBe('$12.35')
    expect(formatCost(1234.5)).toBe('$1,234.50')
    expect(formatCost(0.0123)).toBe('$0.0123')
    expect(formatCost(0)).toBe('$0.00')
  })
})

describe('formatBytes', () => {
  it('formats sizes', () => {
    expect(formatBytes(512)).toBe('512 B')
    expect(formatBytes(1536)).toBe('1.5 KB')
    expect(formatBytes(2 * 1_048_576)).toBe('2.0 MB')
    expect(formatBytes(1.2 * 1_073_741_824)).toBe('1.2 GB')
  })
})
