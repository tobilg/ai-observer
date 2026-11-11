import { traceDetailPath } from '@/lib/traces'

/**
 * Navigation state of links from a list page to a detail page: the list's
 * path with its query, so the detail page's Back button (which reads
 * location.state?.from) returns to the list with its filters.
 */
export interface ListLinkState {
  from: string
}

/** The session transcript page of a session ID (aliases resolve there) */
export function sessionPath(sessionId: string): string {
  return `/sessions/${encodeURIComponent(sessionId)}`
}

/** The trace detail page of an OTel trace ID (e.g. a log's traceId) */
export function otelTracePath(traceId: string): string {
  return traceDetailPath({ id: traceId, traceId, rootSpanId: '', kind: 'otel_trace' })
}

// Logs are stored when they are emitted, which can be shortly before or
// after the spans of their trace
const TRACE_LOGS_MARGIN_MS = 60 * 60 * 1000

/**
 * The Logs page showing the logs of a trace: filtered by its trace ID, over
 * the trace's time (an hour around it) rather than the page's stored range
 */
export function traceLogsPath(traceId: string, startTime: string, durationNanos: number): string {
  const params = new URLSearchParams({ traceId })
  const start = new Date(startTime).getTime()
  if (!Number.isNaN(start)) {
    const end = Math.min(start + durationNanos / 1_000_000 + TRACE_LOGS_MARGIN_MS, Date.now())
    params.set('from', new Date(start - TRACE_LOGS_MARGIN_MS).toISOString())
    params.set('to', new Date(Math.max(end, start)).toISOString())
  }
  return `/logs?${params}`
}
