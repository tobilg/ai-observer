import type { TraceKind, TraceOverview } from '@/types/traces'

type TraceRef = Pick<TraceOverview, 'id' | 'traceId' | 'rootSpanId'> & { kind?: TraceKind }

/** The view a trace row opens: an OTel trace unless it is a Codex operation */
export function getTraceKind(trace: Pick<TraceRef, 'kind'>): TraceKind {
  return trace.kind ?? 'otel_trace'
}

/**
 * The ID the trace detail API takes for a row: the trace ID of an OTel
 * trace, the operation's root span ID (the row's id) of a Codex operation
 */
export function getTraceRequestId(trace: TraceRef): string {
  if (getTraceKind(trace) === 'codex_operation') {
    return trace.id || trace.rootSpanId || trace.traceId
  }
  return trace.traceId || trace.id || trace.rootSpanId
}

/** The trace detail page's path for a row (/traces/:kind/:id) */
export function traceDetailPath(trace: TraceRef): string {
  return `/traces/${getTraceKind(trace)}/${encodeURIComponent(getTraceRequestId(trace))}`
}
