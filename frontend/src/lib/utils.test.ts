import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import {
  cn,
  formatDuration,
  formatTimestamp,
  formatRelativeTime,
  getStatusColor,
  getSeverityColor,
  getSpanKindIcon,
  formatEventTime,
  formatIntervalSeconds,
} from './utils'

describe('cn', () => {
  it('merges class names', () => {
    expect(cn('foo', 'bar')).toBe('foo bar')
  })

  it('handles conditional classes', () => {
    expect(cn('base', { active: true, disabled: false })).toBe('base active')
  })

  it('handles arrays', () => {
    expect(cn(['a', 'b'], 'c')).toBe('a b c')
  })

  it('merges tailwind classes correctly', () => {
    expect(cn('px-2 py-1', 'px-4')).toBe('py-1 px-4')
  })

  it('handles undefined and null', () => {
    expect(cn('base', undefined, null, 'end')).toBe('base end')
  })
})

describe('formatDuration', () => {
  it('formats nanoseconds to microseconds', () => {
    expect(formatDuration(500)).toBe('0.50µs')
    expect(formatDuration(999)).toBe('1.00µs')
  })

  it('formats to milliseconds', () => {
    expect(formatDuration(1_000_000)).toBe('1.00ms')
    expect(formatDuration(5_500_000)).toBe('5.50ms')
    expect(formatDuration(999_000_000)).toBe('999.00ms')
  })

  it('formats to seconds', () => {
    expect(formatDuration(1_000_000_000)).toBe('1.00s')
    expect(formatDuration(2_500_000_000)).toBe('2.50s')
  })

  it('handles zero', () => {
    expect(formatDuration(0)).toBe('0.00µs')
  })

  it('handles edge cases at boundaries', () => {
    // Just under 1ms
    expect(formatDuration(999_999)).toBe('1000.00µs')
    // Just over 1s
    expect(formatDuration(1_000_000_001)).toBe('1.00s')
  })
})

describe('formatTimestamp', () => {
  it('formats string timestamp', () => {
    const result = formatTimestamp('2024-01-15T10:30:00Z')
    expect(result).toBeTruthy()
    expect(typeof result).toBe('string')
  })

  it('formats Date object', () => {
    const date = new Date('2024-01-15T10:30:00Z')
    const result = formatTimestamp(date)
    expect(result).toBeTruthy()
    expect(typeof result).toBe('string')
  })
})

describe('formatRelativeTime', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2024-01-15T12:00:00Z'))
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('formats seconds ago', () => {
    const date = new Date('2024-01-15T11:59:30Z')
    expect(formatRelativeTime(date)).toBe('30s ago')
  })

  it('formats minutes ago', () => {
    const date = new Date('2024-01-15T11:45:00Z')
    expect(formatRelativeTime(date)).toBe('15m ago')
  })

  it('formats hours ago', () => {
    const date = new Date('2024-01-15T09:00:00Z')
    expect(formatRelativeTime(date)).toBe('3h ago')
  })

  it('formats days ago', () => {
    const date = new Date('2024-01-13T12:00:00Z')
    expect(formatRelativeTime(date)).toBe('2d ago')
  })

  it('handles string timestamps', () => {
    const result = formatRelativeTime('2024-01-15T11:59:30Z')
    expect(result).toBe('30s ago')
  })

  it('handles just now (0 seconds)', () => {
    const date = new Date('2024-01-15T12:00:00Z')
    expect(formatRelativeTime(date)).toBe('0s ago')
  })
})

describe('getStatusColor', () => {
  it('returns success color for OK', () => {
    expect(getStatusColor('OK')).toBe('bg-success text-white')
    expect(getStatusColor('ok')).toBe('bg-success text-white')
  })

  it('returns error color for ERROR', () => {
    expect(getStatusColor('ERROR')).toBe('bg-error text-white')
    expect(getStatusColor('error')).toBe('bg-error text-white')
  })

  it('returns muted color for unknown status', () => {
    expect(getStatusColor('UNSET')).toBe('bg-muted text-muted-foreground')
    expect(getStatusColor('unknown')).toBe('bg-muted text-muted-foreground')
    expect(getStatusColor('')).toBe('bg-muted text-muted-foreground')
  })
})

describe('getSeverityColor', () => {
  it('returns error color for FATAL and ERROR', () => {
    expect(getSeverityColor('FATAL')).toBe('bg-error text-white')
    expect(getSeverityColor('ERROR')).toBe('bg-error text-white')
    expect(getSeverityColor('fatal')).toBe('bg-error text-white')
    expect(getSeverityColor('error')).toBe('bg-error text-white')
  })

  it('returns warning color for WARN and WARNING', () => {
    expect(getSeverityColor('WARN')).toBe('bg-warning text-black')
    expect(getSeverityColor('WARNING')).toBe('bg-warning text-black')
    expect(getSeverityColor('warn')).toBe('bg-warning text-black')
  })

  it('returns info color for INFO', () => {
    expect(getSeverityColor('INFO')).toBe('bg-info text-white')
    expect(getSeverityColor('info')).toBe('bg-info text-white')
  })

  it('returns muted color for DEBUG and TRACE', () => {
    expect(getSeverityColor('DEBUG')).toBe('bg-muted text-muted-foreground')
    expect(getSeverityColor('TRACE')).toBe('bg-muted text-muted-foreground')
    expect(getSeverityColor('debug')).toBe('bg-muted text-muted-foreground')
  })

  it('returns secondary color for unknown severity', () => {
    expect(getSeverityColor('UNKNOWN')).toBe('bg-secondary text-secondary-foreground')
    expect(getSeverityColor('')).toBe('bg-secondary text-secondary-foreground')
  })
})

describe('getSpanKindIcon', () => {
  it('returns → for CLIENT', () => {
    expect(getSpanKindIcon('CLIENT')).toBe('→')
    expect(getSpanKindIcon('client')).toBe('→')
  })

  it('returns ← for SERVER', () => {
    expect(getSpanKindIcon('SERVER')).toBe('←')
    expect(getSpanKindIcon('server')).toBe('←')
  })

  it('returns ↑ for PRODUCER', () => {
    expect(getSpanKindIcon('PRODUCER')).toBe('↑')
    expect(getSpanKindIcon('producer')).toBe('↑')
  })

  it('returns ↓ for CONSUMER', () => {
    expect(getSpanKindIcon('CONSUMER')).toBe('↓')
    expect(getSpanKindIcon('consumer')).toBe('↓')
  })

  it('returns ○ for INTERNAL', () => {
    expect(getSpanKindIcon('INTERNAL')).toBe('○')
    expect(getSpanKindIcon('internal')).toBe('○')
  })

  it('returns ○ for undefined', () => {
    expect(getSpanKindIcon(undefined)).toBe('○')
  })

  it('returns ○ for unknown kind', () => {
    expect(getSpanKindIcon('UNKNOWN')).toBe('○')
    expect(getSpanKindIcon('')).toBe('○')
  })
})

describe('formatEventTime', () => {
  it('formats millisecond differences', () => {
    const span = new Date('2024-01-15T12:00:00.000Z')
    const event = new Date('2024-01-15T12:00:00.500Z')
    expect(formatEventTime(event, span)).toBe('500ms')
  })

  it('formats second differences', () => {
    const span = new Date('2024-01-15T12:00:00.000Z')
    const event = new Date('2024-01-15T12:00:02.500Z')
    expect(formatEventTime(event, span)).toBe('2.50s')
  })

  it('handles string timestamps', () => {
    const span = '2024-01-15T12:00:00.000Z'
    const event = '2024-01-15T12:00:00.100Z'
    expect(formatEventTime(event, span)).toBe('100ms')
  })

  it('handles Date objects', () => {
    const span = new Date('2024-01-15T12:00:00.000Z')
    const event = new Date('2024-01-15T12:00:01.000Z')
    expect(formatEventTime(event, span)).toBe('1.00s')
  })

  it('handles zero difference', () => {
    const time = new Date('2024-01-15T12:00:00.000Z')
    expect(formatEventTime(time, time)).toBe('0ms')
  })

  it('handles negative difference (event before span)', () => {
    const span = new Date('2024-01-15T12:00:01.000Z')
    const event = new Date('2024-01-15T12:00:00.000Z')
    // Function returns milliseconds for values < 1000
    expect(formatEventTime(event, span)).toBe('-1000ms')
  })
})

describe('formatIntervalSeconds', () => {
  it('formats seconds singular', () => {
    expect(formatIntervalSeconds(1)).toBe('1 second')
  })

  it('formats seconds plural', () => {
    expect(formatIntervalSeconds(30)).toBe('30 seconds')
    expect(formatIntervalSeconds(59)).toBe('59 seconds')
  })

  it('formats minutes singular', () => {
    expect(formatIntervalSeconds(60)).toBe('1 minute')
  })

  it('formats minutes plural', () => {
    expect(formatIntervalSeconds(120)).toBe('2 minutes')
    expect(formatIntervalSeconds(300)).toBe('5 minutes')
  })

  it('formats hours singular', () => {
    expect(formatIntervalSeconds(3600)).toBe('1 hour')
  })

  it('formats hours plural', () => {
    expect(formatIntervalSeconds(7200)).toBe('2 hours')
    expect(formatIntervalSeconds(10800)).toBe('3 hours')
  })

  it('formats days singular', () => {
    expect(formatIntervalSeconds(86400)).toBe('1 day')
  })

  it('formats days plural', () => {
    expect(formatIntervalSeconds(172800)).toBe('2 days')
  })

  it('handles zero', () => {
    expect(formatIntervalSeconds(0)).toBe('0 seconds')
  })

  it('handles fractional values', () => {
    // 90 seconds = 1.5 minutes
    expect(formatIntervalSeconds(90)).toBe('1.5 minutes')
  })
})
