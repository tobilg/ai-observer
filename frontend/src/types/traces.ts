export type TraceKind = 'otel_trace' | 'codex_operation'
export type TraceGroupLevel = 'codex_turn' | 'codex_operation' | 'trace'

export interface TraceOverview {
  id: string
  kind: TraceKind
  traceId: string
  rootSpanId: string
  rootSpan: string
  serviceName: string
  groupLevel?: TraceGroupLevel
  startTime: string
  duration: number
  spanCount: number
  status: 'OK' | 'ERROR' | 'UNSET'
}

export interface Span {
  timestamp: string
  traceId: string
  spanId: string
  parentSpanId?: string
  traceState?: string
  spanName: string
  spanKind?: string
  serviceName: string
  resourceAttributes?: Record<string, string>
  scopeName?: string
  scopeVersion?: string
  spanAttributes?: Record<string, string>
  duration: number
  statusCode?: string
  statusMessage?: string
  events?: SpanEvent[]
  links?: SpanLink[]
}

export interface SpanEvent {
  timestamp: string
  name: string
  attributes?: Record<string, string>
}

export interface SpanLink {
  traceId: string
  spanId: string
  traceState?: string
  attributes?: Record<string, string>
}

export interface TracesResponse {
  traces: TraceOverview[]
  total: number
  hasMore: boolean
}

export interface SpansResponse {
  spans: Span[]
}
