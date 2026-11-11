// Structured results of the MCP tools the view draws, as
// backend/internal/mcpserver returns them (snake_case JSON)

export type UsageGroup = 'tool' | 'model' | 'day' | 'repository'

export interface UsageRow {
  tool: string
  model?: string
  repository?: string
  day?: string
  cost_usd: number
  tokens?: Record<string, number>
  total_tokens: number
}

export interface UsageSummary {
  from: string
  to: string
  group_by: UsageGroup
  rows: UsageRow[]
  total_cost_usd: number
  // Models with tokens but no cost ("tool: model"), which have no price
  unpriced_models?: string[]
  note: string
}

export interface MetricPoint {
  // Missing for totals (aggregate)
  time?: string
  value: number
}

export interface MetricSeries {
  labels?: Record<string, string>
  points: MetricPoint[]
}

export interface MetricsResult {
  name: string
  from: string
  to: string
  // A Go duration such as 1h0m0s; missing for totals
  interval?: string
  series: MetricSeries[]
}

export interface TraceSpan {
  span_id: string
  parent_span_id?: string
  depth: number
  name: string
  tool: string
  start_time: string
  duration_ms: number
  status?: string
  status_message?: string
  attributes?: Record<string, string>
}

export interface TraceResult {
  id: string
  kind: string
  total_spans: number
  spans: TraceSpan[]
  truncated?: boolean
}

export interface SessionRow {
  session_id: string
  tool: string
  model?: string
  start_time: string
  last_time: string
  messages: number
}

export interface SessionsResult {
  from: string
  to: string
  total: number
  sessions: SessionRow[]
  next_cursor?: string
}

// Calls a tool of AI Observer's MCP server through the host and returns its
// structured result; throws the tool's error text
export type CallTool = (name: string, args: Record<string, unknown>) => Promise<unknown>
