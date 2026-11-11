import { getTraceKind, getTraceRequestId } from '@/lib/traces'
import type { LogRecord } from '@/types/logs'
import type { TraceOverview } from '@/types/traces'

// Stable identities of list rows, for React keys and for remembering which
// row is expanded across refetches: derived from the record, never from its
// position in the page.

// A short hash of a string (FNV-1a), so a long log body does not end up in a key
function hash(text: string): string {
  let h = 0x811c9dc5
  for (let i = 0; i < text.length; i++) {
    h ^= text.charCodeAt(i)
    h = Math.imul(h, 0x01000193)
  }
  return (h >>> 0).toString(36)
}

/** A log record's identity: logs have no ID, so time, origin and content */
export function logRowKey(log: LogRecord): string {
  return [
    log.timestamp,
    log.serviceName,
    log.severityText ?? '',
    log.traceId ?? '',
    log.spanId ?? '',
    hash(log.body ?? ''),
  ].join('|')
}

/** A trace row's identity: the view it opens */
export function traceRowKey(trace: TraceOverview): string {
  return `${getTraceKind(trace)}:${getTraceRequestId(trace)}`
}

/**
 * The keys of items, with a suffix for repeats ("key#2"), so identical
 * records in one page still get distinct keys
 */
export function uniqueRowKeys<T>(items: T[], keyOf: (item: T) => string): string[] {
  const seen = new Map<string, number>()
  return items.map((item) => {
    const key = keyOf(item)
    const count = (seen.get(key) ?? 0) + 1
    seen.set(key, count)
    return count === 1 ? key : `${key}#${count}`
  })
}
