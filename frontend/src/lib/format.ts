// Shared formatters for numbers, durations, tokens, costs and sizes. One
// locale policy: numbers use en-US grouping ("1,234,567"), whatever the
// browser's locale, as the rest of the dashboard (metric values, costs) does.
// Timestamps keep the browser's locale (formatTimestamp in lib/utils.ts).

const LOCALE = 'en-US'

const countFormat = new Intl.NumberFormat(LOCALE)

/** An integer count with thousands separators: 1234567 → "1,234,567" */
export function formatCount(n: number): string {
  return Number.isFinite(n) ? countFormat.format(n) : '—'
}

/** A span duration in nanoseconds: "512.00µs", "12.34ms", "1.50s" */
export function formatDurationNanos(nanos: number): string {
  const ms = nanos / 1_000_000
  if (ms < 1) {
    return `${(nanos / 1000).toFixed(2)}µs`
  }
  if (ms < 1000) {
    return `${ms.toFixed(2)}ms`
  }
  return `${(ms / 1000).toFixed(2)}s`
}

/** A duration in milliseconds (tool calls, API requests): "850ms", "2.5s", "1.5m", "2h 5m" */
export function formatDurationMs(ms: number): string {
  if (ms < 1000) {
    return `${Math.round(ms)}ms`
  }
  if (ms < 60_000) {
    return `${(ms / 1000).toFixed(1)}s`
  }
  if (ms < 3_600_000) {
    return `${(ms / 60_000).toFixed(1)}m`
  }
  const hours = Math.floor(ms / 3_600_000)
  return `${hours}h ${Math.round((ms % 3_600_000) / 60_000)}m`
}

function plural(n: number, unit: string): string {
  return `${n} ${unit}${n === 1 ? '' : 's'}`
}

/**
 * The time between two timestamps (a session's length). short: "45s",
 * "12m", "2h 5m"; long: "45 seconds", "12 minutes", "2h 5m".
 */
export function formatTimeSpan(start: string | Date, end: string | Date, style: 'short' | 'long' = 'short'): string {
  const ms = Math.max(0, new Date(end).getTime() - new Date(start).getTime())
  if (Number.isNaN(ms)) {
    return '—'
  }
  if (ms < 60_000) {
    const seconds = Math.round(ms / 1000)
    return style === 'long' ? plural(seconds, 'second') : `${seconds}s`
  }
  if (ms < 3_600_000) {
    const minutes = Math.round(ms / 60_000)
    return style === 'long' ? plural(minutes, 'minute') : `${minutes}m`
  }
  const hours = Math.floor(ms / 3_600_000)
  return `${hours}h ${Math.round((ms % 3_600_000) / 60_000)}m`
}

/** A token count: "1,234 tokens" (unit: false → "1,234") */
export function formatTokens(n: number, unit = true): string {
  return unit ? `${formatCount(n)} ${n === 1 ? 'token' : 'tokens'}` : formatCount(n)
}

export interface TokenUsage {
  input?: number
  output?: number
  cacheRead?: number
  cacheWrite?: number
}

/** A message's tokens by type: "1,200 in / 340 out / 5,000 cache / 800 cached"; '' without any */
export function formatTokenUsage({ input, output, cacheRead, cacheWrite }: TokenUsage): string {
  const parts: string[] = []
  if (input) parts.push(`${formatCount(input)} in`)
  if (output) parts.push(`${formatCount(output)} out`)
  if (cacheRead) parts.push(`${formatCount(cacheRead)} cache`)
  if (cacheWrite) parts.push(`${formatCount(cacheWrite)} cached`)
  return parts.join(' / ')
}

/**
 * A cost in USD: two decimals from $1 ("$12.35", "$1,234.50"), four below,
 * where single requests cost fractions of a cent ("$0.0123")
 */
export function formatCost(usd: number): string {
  const small = usd !== 0 && Math.abs(usd) < 1
  return new Intl.NumberFormat(LOCALE, {
    style: 'currency',
    currency: 'USD',
    minimumFractionDigits: small ? 4 : 2,
    maximumFractionDigits: small ? 4 : 2,
  }).format(usd)
}

/** A size in bytes: "512 B", "1.5 KB", "2.0 MB", "1.2 GB" */
export function formatBytes(bytes: number): string {
  if (bytes >= 1_073_741_824) return `${(bytes / 1_073_741_824).toFixed(1)} GB`
  if (bytes >= 1_048_576) return `${(bytes / 1_048_576).toFixed(1)} MB`
  if (bytes >= 1_024) return `${(bytes / 1_024).toFixed(1)} KB`
  return `${bytes} B`
}
