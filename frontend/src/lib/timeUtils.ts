/**
 * Time utilities for date range calculations
 */
import { TIMEFRAME_OPTIONS, isAbsoluteTimeSelection, type TimeSelection, type TimeframeOption } from '@/types/dashboard'

/**
 * Calculate the optimal interval in seconds for a given date range.
 * Targets ~24-50 buckets for optimal chart visualization.
 * Matches the intervals used in TIMEFRAME_OPTIONS presets.
 */
export function calculateInterval(from: Date, to: Date): number {
  const rangeHours = (to.getTime() - from.getTime()) / (1000 * 60 * 60)
  const rangeDays = rangeHours / 24

  // Short ranges (less than 1 hour): 1 minute intervals
  if (rangeHours <= 1) return 60 // ~60 buckets max

  // 1-3 hours: 5 minute intervals
  if (rangeHours <= 3) return 300 // ~36 buckets max

  // 3-6 hours: 10 minute intervals
  if (rangeHours <= 6) return 600 // ~36 buckets max

  // 6-24 hours (including single day selection): 1 hour intervals
  // Matches "Last 24 hours" preset which uses 3600s
  if (rangeDays <= 1) return 3600 // 24 buckets for a day

  // 1-7 days: 6 hour intervals
  if (rangeDays <= 7) return 21600 // ~28 buckets

  // 7-30 days: 1 day intervals
  if (rangeDays <= 30) return 86400 // ~30 buckets

  // 30-90 days: 3 day intervals
  if (rangeDays <= 90) return 259200 // ~30 buckets

  // 90-180 days: 1 week intervals
  if (rangeDays <= 180) return 604800 // ~26 buckets

  // 180-365 days: 2 week intervals
  if (rangeDays <= 365) return 1209600 // ~26 buckets

  // Over 1 year: 1 month intervals
  return 2592000 // ~12-24 buckets
}

/**
 * Calculate the tick interval for chart X-axis based on bucket count.
 * Returns a value indicating how many buckets between labeled ticks.
 */
export function calculateTickInterval(bucketCount: number): number {
  if (bucketCount <= 10) return 1
  if (bucketCount <= 20) return 2
  if (bucketCount <= 40) return 3
  if (bucketCount <= 60) return 4
  return 5
}

/**
 * Check if a date range exceeds a given number of days.
 */
export function isRangeExceeding(days: number, from: Date, to: Date): boolean {
  const rangeDays = (to.getTime() - from.getTime()) / (1000 * 60 * 60 * 24)
  return rangeDays > days
}

// ============================================================================
// Time selections
// ============================================================================

export function findTimeframe(value: string | null | undefined): TimeframeOption | undefined {
  return value ? TIMEFRAME_OPTIONS.find((t) => t.value === value) : undefined
}

/** The relative selection of a timeframe value, or of fallback ('7d') if it is unknown */
export function relativeTimeSelection(value: string | null | undefined, fallback = '7d'): TimeSelection {
  const timeframe = findTimeframe(value) ?? findTimeframe(fallback) ?? TIMEFRAME_OPTIONS[10]
  return { type: 'relative', timeframe }
}

/** An absolute selection with chart bucketing derived from its length */
export function absoluteTimeSelection(from: Date, to: Date): TimeSelection {
  const intervalSeconds = calculateInterval(from, to)
  const bucketCount = Math.ceil((to.getTime() - from.getTime()) / 1000 / intervalSeconds)
  return {
    type: 'absolute',
    range: { from, to, intervalSeconds, tickInterval: calculateTickInterval(bucketCount) },
  }
}

/**
 * Identity of a selection for query keys: the timeframe of a relative one,
 * the bounds of an absolute one. Never the moving bounds of a relative
 * window: they are computed when a query runs (resolveTimeRange).
 */
export type TimeSelectionKey = { timeframe: string } | { from: string; to: string }

export function timeSelectionKey(selection: TimeSelection): TimeSelectionKey {
  if (isAbsoluteTimeSelection(selection)) {
    return { from: selection.range.from.toISOString(), to: selection.range.to.toISOString() }
  }
  return { timeframe: selection.timeframe.value }
}

/**
 * The bounds to query: fixed for an absolute selection, the window ending
 * now for a relative one. Call it when the request is sent (in a queryFn),
 * so a relative window moves with every refresh.
 */
export function resolveTimeRange(selection: TimeSelection, now: Date = new Date()): { from: Date; to: Date } {
  if (isAbsoluteTimeSelection(selection)) {
    return { from: selection.range.from, to: selection.range.to }
  }
  return { from: new Date(now.getTime() - selection.timeframe.durationSeconds * 1000), to: now }
}

export function timeSelectionDurationSeconds(selection: TimeSelection): number {
  if (isAbsoluteTimeSelection(selection)) {
    return (selection.range.to.getTime() - selection.range.from.getTime()) / 1000
  }
  return selection.timeframe.durationSeconds
}

export function timeSelectionIntervalSeconds(selection: TimeSelection): number {
  return isAbsoluteTimeSelection(selection) ? selection.range.intervalSeconds : selection.timeframe.intervalSeconds
}

export function timeSelectionTickInterval(selection: TimeSelection): number {
  return isAbsoluteTimeSelection(selection) ? selection.range.tickInterval : selection.timeframe.tickInterval
}

/**
 * What query hooks take as time range (lib/queries.ts): key goes into the
 * query key, resolve() is called by the queryFn.
 */
export interface RangeSource {
  key: TimeSelectionKey
  resolve: () => { from: Date; to: Date }
}

export function timeRangeSource(selection: TimeSelection): RangeSource {
  return { key: timeSelectionKey(selection), resolve: () => resolveTimeRange(selection) }
}

/** resolve() as RFC 3339 strings, as the API takes them */
export function resolveRangeParams(range: RangeSource): { from: string; to: string } {
  const { from, to } = range.resolve()
  return { from: from.toISOString(), to: to.toISOString() }
}

// ============================================================================
// URL and storage representation
// ============================================================================

/** URL parameters of a time selection; other parameters must not use these names */
export const TIME_SELECTION_PARAMS = ['timeframe', 'from', 'to'] as const

/**
 * The selection in URL parameters: from and to (absolute, RFC 3339) win over
 * timeframe (relative). null when the parameters name no valid selection.
 */
export function timeSelectionFromParams(params: URLSearchParams): TimeSelection | null {
  const fromParam = params.get('from')
  const toParam = params.get('to')
  if (fromParam && toParam) {
    const from = new Date(fromParam)
    const to = new Date(toParam)
    if (!isNaN(from.getTime()) && !isNaN(to.getTime()) && from <= to) {
      return absoluteTimeSelection(from, to)
    }
  }
  const timeframe = findTimeframe(params.get('timeframe'))
  return timeframe ? { type: 'relative', timeframe } : null
}

/** Writes the selection's parameters to params, removing the other kind's */
export function writeTimeSelectionParams(params: URLSearchParams, selection: TimeSelection): void {
  for (const name of TIME_SELECTION_PARAMS) {
    params.delete(name)
  }
  if (isAbsoluteTimeSelection(selection)) {
    params.set('from', selection.range.from.toISOString())
    params.set('to', selection.range.to.toISOString())
  } else {
    params.set('timeframe', selection.timeframe.value)
  }
}

/**
 * The relative selection stored under a page's localStorage key, if any.
 * Absolute selections are never restored from storage (earlier versions
 * stored them; they live in the URL only).
 */
export function readStoredTimeSelection(storageKey: string): TimeSelection | null {
  try {
    const raw = localStorage.getItem(storageKey)
    if (!raw) {
      return null
    }
    const stored = JSON.parse(raw) as { type?: string; timeframeValue?: string } | null
    const timeframe = stored?.type === 'relative' ? findTimeframe(stored.timeframeValue) : undefined
    return timeframe ? { type: 'relative', timeframe } : null
  } catch {
    return null
  }
}

/** Stores a relative selection; absolute ones are not stored */
export function storeTimeSelection(storageKey: string, selection: TimeSelection): void {
  if (isAbsoluteTimeSelection(selection)) {
    return
  }
  try {
    localStorage.setItem(storageKey, JSON.stringify({ type: 'relative', timeframeValue: selection.timeframe.value }))
  } catch {
    // Ignore storage errors (private mode, quota)
  }
}
