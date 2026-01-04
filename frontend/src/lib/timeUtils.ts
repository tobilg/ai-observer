/**
 * Time utilities for date range calculations
 */

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
 * Format a date range for display.
 */
export function formatDateRange(from: Date, to: Date): string {
  const formatDate = (d: Date) =>
    d.toLocaleDateString(undefined, { month: 'short', day: 'numeric', year: 'numeric' })
  return `${formatDate(from)} - ${formatDate(to)}`
}

/**
 * Check if a date range exceeds a given number of days.
 */
export function isRangeExceeding(days: number, from: Date, to: Date): boolean {
  const rangeDays = (to.getTime() - from.getTime()) / (1000 * 60 * 60 * 24)
  return rangeDays > days
}

/**
 * Get the number of days in a date range.
 */
export function getRangeDays(from: Date, to: Date): number {
  return (to.getTime() - from.getTime()) / (1000 * 60 * 60 * 24)
}
