// Pure helpers of the MCP tools' view: which view a tool result gets and the
// chart and table rows drawn from it

import { buildChartData, formatTickLabel, type ChartData } from '@/components/charts'
import { getServiceDisplayName } from '@/lib/metricMetadata'
import type { Span } from '@/types/traces'
import type { TimeSeries } from '@/types/metrics'
import type { MetricsResult, SessionRow, TraceResult, UsageRow, UsageSummary } from './types'

export type ViewKind = 'usage' | 'metrics' | 'trace' | 'sessions'

// The tools whose _meta names the view (backend/internal/mcpserver/server.go)
const VIEW_OF_TOOL: Record<string, ViewKind> = {
  usage_summary: 'usage',
  query_metrics: 'metrics',
  get_trace: 'trace',
  list_sessions: 'sessions',
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

// The view of a tool result: by the tool's name when the host tells it,
// otherwise by the result's shape
export function viewOf(toolName: string | undefined, content: unknown): ViewKind | undefined {
  if (toolName && VIEW_OF_TOOL[toolName]) {
    return VIEW_OF_TOOL[toolName]
  }
  if (!isRecord(content)) {
    return undefined
  }
  if (typeof content.group_by === 'string' && Array.isArray(content.rows)) return 'usage'
  if (typeof content.name === 'string' && Array.isArray(content.series)) return 'metrics'
  if (typeof content.kind === 'string' && Array.isArray(content.spans)) return 'trace'
  if (typeof content.total === 'number' && Array.isArray(content.sessions)) return 'sessions'
  return undefined
}

// The parts of an MCP CallToolResult the view reads
export interface ToolResult {
  content?: Array<{ type: string; text?: string }>
  structuredContent?: unknown
  isError?: boolean
}

function resultText(result: ToolResult): string {
  return (result.content ?? [])
    .filter((c) => c.type === 'text' && c.text)
    .map((c) => c.text)
    .join('\n')
}

// The structured content of a result, or its text content as JSON for hosts
// that pass only that
export function structuredOf(result: ToolResult): unknown {
  if (result.structuredContent != null) {
    return result.structuredContent
  }
  try {
    return JSON.parse(resultText(result))
  } catch {
    return undefined
  }
}

// The error text of a failed tool call
export function errorOf(result: ToolResult): string | undefined {
  return result.isError ? resultText(result) || 'The tool call failed' : undefined
}

// Display names of the tool names the MCP tools return (internal/tools;
// usage_summary reports both GitHub Copilot services as copilot, and the
// Codex app and CLI as codex)
const TOOL_NAMES: Record<string, string> = {
  'claude-code': 'Claude Code',
  codex: 'OpenAI Codex',
  gemini: 'Gemini CLI',
  opencode: 'OpenCode',
  'copilot-chat': 'GitHub Copilot (VS Code)',
  'github-copilot': 'GitHub Copilot CLI',
  copilot: 'GitHub Copilot',
}

export function toolDisplayName(tool: string): string {
  return TOOL_NAMES[tool] ?? getServiceDisplayName(tool)
}

// Seconds of a Go duration such as 1h0m0s or 28m48s
export function goDurationSeconds(value: string | undefined): number | undefined {
  if (!value) return undefined
  const units: Record<string, number> = { h: 3600, m: 60, s: 1, ms: 0.001 }
  let seconds = 0
  let matched = ''
  for (const m of value.matchAll(/(\d+(?:\.\d+)?)(ms|h|m|s)/g)) {
    seconds += Number(m[1]) * units[m[2]]
    matched += m[0]
  }
  return matched === value && seconds > 0 ? seconds : undefined
}

export type UsageMeasure = 'cost' | 'tokens'

export function usageValue(row: UsageRow, measure: UsageMeasure): number {
  return measure === 'cost' ? row.cost_usd : row.total_tokens
}

// One bar of a ranked bar list
export interface BarRow {
  key: string
  label: string
  // The tool, when bars are models or repositories
  detail?: string
  value: number
}

// The bars of a summary grouped by tool, model or repository, largest first
export function usageBars(summary: UsageSummary, measure: UsageMeasure): BarRow[] {
  return summary.rows
    .map((row) => {
      const group = summary.group_by === 'model' ? row.model : summary.group_by === 'repository' ? row.repository : undefined
      return {
        key: `${row.tool}/${group ?? ''}`,
        label: group ?? toolDisplayName(row.tool),
        detail: group !== undefined ? toolDisplayName(row.tool) : undefined,
        value: usageValue(row, measure),
      }
    })
    .filter((bar) => bar.value > 0)
    .sort((a, b) => b.value - a.value || a.label.localeCompare(b.label))
}

const DAY_SECONDS = 24 * 60 * 60
const DAY_MS = DAY_SECONDS * 1000

// Daily bars stacked by tool, for a summary grouped by day
export function usageDaily(summary: UsageSummary, measure: UsageMeasure): ChartData {
  const byTool = new Map<string, TimeSeries>()
  for (const row of summary.rows) {
    if (!row.day) continue
    let series = byTool.get(row.tool)
    if (!series) {
      series = { name: row.tool, labels: { tool: row.tool }, datapoints: [] }
      byTool.set(row.tool, series)
    }
    series.datapoints.push([Date.parse(`${row.day}T00:00:00Z`), usageValue(row, measure)])
  }
  const days = Math.max(1, (Date.parse(summary.to) - Date.parse(summary.from)) / DAY_MS)
  return buildChartData(
    [...byTool.values()],
    (labels) => labels?.tool ?? '',
    (labels) => toolDisplayName(labels?.tool ?? ''),
    // UTC days, as the server buckets them
    (timestamp) =>
      days > 7
        ? new Date(timestamp).toLocaleDateString([], { month: 'short', day: 'numeric', timeZone: 'UTC' })
        : new Date(timestamp).toLocaleDateString([], { weekday: 'short', month: 'short', day: 'numeric', timeZone: 'UTC' })
  )
}

// The label of a metric series: the service's display name, then the other
// label values by key
export function metricLabel(labels: Record<string, string> | undefined): string {
  if (!labels || Object.keys(labels).length === 0) {
    return 'value'
  }
  const parts = labels.service ? [getServiceDisplayName(labels.service)] : []
  for (const key of Object.keys(labels).sort()) {
    if (key !== 'service' && labels[key]) {
      parts.push(labels[key])
    }
  }
  return parts.join(' · ')
}

function seriesKey(labels: Record<string, string> | undefined): string {
  return JSON.stringify(Object.entries(labels ?? {}).sort(([a], [b]) => a.localeCompare(b)))
}

// The time series chart of a bucketed query_metrics result
export function metricChart(result: MetricsResult): ChartData {
  const series: TimeSeries[] = result.series.map((s) => ({
    name: result.name,
    labels: s.labels,
    datapoints: s.points.filter((p) => p.time).map((p) => [Date.parse(p.time!), p.value] as [number, number]),
  }))
  const durationSeconds = (Date.parse(result.to) - Date.parse(result.from)) / 1000
  const intervalSeconds = goDurationSeconds(result.interval) ?? durationSeconds / 50
  return buildChartData(series, seriesKey, metricLabel, (timestamp) =>
    // Buckets of whole days start at midnight in the result's zone: their
    // date there only
    intervalSeconds % DAY_SECONDS === 0
      ? formatDay(timestamp, result.timezone)
      : formatTickLabel(timestamp, durationSeconds, intervalSeconds)
  )
}

// The date of a day bucket in its zone (UTC when unknown or not supported)
function formatDay(timestamp: number, timeZone = 'UTC'): string {
  try {
    return new Date(timestamp).toLocaleDateString([], { month: 'short', day: 'numeric', timeZone })
  } catch {
    return new Date(timestamp).toLocaleDateString([], { month: 'short', day: 'numeric', timeZone: 'UTC' })
  }
}

// The totals of an aggregated query_metrics result, largest first
export function metricTotals(result: MetricsResult): BarRow[] {
  return result.series
    .map((s) => ({
      key: seriesKey(s.labels),
      label: metricLabel(s.labels),
      value: s.points.reduce((sum, p) => sum + p.value, 0),
    }))
    .sort((a, b) => b.value - a.value || a.label.localeCompare(b.label))
}

// Whether a query_metrics result has time buckets (not aggregated)
export function isBucketed(result: MetricsResult): boolean {
  return result.series.some((s) => s.points.some((p) => p.time))
}

// The spans of a get_trace result in the dashboard's span type, so the
// trace page's waterfall draws them
export function traceSpans(result: TraceResult): Span[] {
  return result.spans.map((s) => ({
    timestamp: s.start_time,
    traceId: result.id,
    spanId: s.span_id,
    parentSpanId: s.parent_span_id,
    spanName: s.name,
    serviceName: s.tool,
    spanAttributes: s.attributes,
    duration: Math.round(s.duration_ms * 1_000_000),
    statusCode: s.status,
    statusMessage: s.status_message,
  }))
}

// Sessions per tool, most first
export function sessionCounts(sessions: SessionRow[]): Array<{ tool: string; count: number }> {
  const counts = new Map<string, number>()
  for (const s of sessions) {
    counts.set(s.tool, (counts.get(s.tool) ?? 0) + 1)
  }
  return [...counts.entries()]
    .map(([tool, count]) => ({ tool, count }))
    .sort((a, b) => b.count - a.count || a.tool.localeCompare(b.tool))
}

// The summed cost of the sessions that record one; undefined when none does
export function sessionsCost(sessions: SessionRow[]): number | undefined {
  let total: number | undefined
  for (const s of sessions) {
    if (s.cost_usd != null) {
      total = (total ?? 0) + s.cost_usd
    }
  }
  return total
}

// Where a session worked: repository and branch, when known
export function sessionPlace(s: SessionRow): string | undefined {
  if (!s.repository) return undefined
  return s.branch ? `${s.repository} @ ${s.branch}` : s.repository
}

export function formatRange(from: string, to: string): string {
  const format = (value: string) =>
    new Date(value).toLocaleString([], { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })
  return `${format(from)} – ${format(to)}`
}
