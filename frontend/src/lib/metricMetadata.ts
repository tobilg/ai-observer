// Metric Metadata System for AI Observer
// Provides human-readable names, descriptions, and formatting for metrics from
// Claude Code, Gemini CLI, Codex CLI, GitHub Copilot, and OpenCode

import { Bot, BotMessageSquare, Terminal, Sparkles, type LucideIcon } from 'lucide-react'
import { useProviderStore } from '@/stores/providerStore'
import { formatBytes } from '@/lib/format'
import { UNIT_FORMATS, type MetricType, type SourceTool, type UnitFormat, type AttributeBreakdown, type MetricMetadata } from '@/lib/metricTypes'
import type { HistogramStat as APIHistogramStat } from '@/types/metrics'
import { CLAUDE_CODE_METRICS } from '@/lib/metricCatalog/claudeCode'
import { CODEX_EVENT_METRICS, CODEX_METRICS } from '@/lib/metricCatalog/codex'
import { GEMINI_METRICS } from '@/lib/metricCatalog/gemini'
import { GITHUB_COPILOT_METRICS } from '@/lib/metricCatalog/githubCopilot'
import { OPENCODE_METRICS } from '@/lib/metricCatalog/opencode'

export * from '@/lib/metricTypes'

// ============================================================================
// Metric Catalog
// ============================================================================

// Per-provider definitions live in metricCatalog/. Within each source,
// metrics are listed in the order they appear here.
export const METRIC_CATALOG: MetricMetadata[] = [
  ...CLAUDE_CODE_METRICS,
  ...CODEX_METRICS,
  ...GEMINI_METRICS,
  ...OPENCODE_METRICS,
  ...GITHUB_COPILOT_METRICS,
  ...CODEX_EVENT_METRICS,
]

// ============================================================================
// Lookup Map for O(1) Access
// ============================================================================

export const METRIC_METADATA_MAP: Map<string, MetricMetadata> = new Map(
  METRIC_CATALOG.map((m) => [m.name, m])
)

// ============================================================================
// Utility Functions
// ============================================================================

/**
 * Get metadata for a metric, with graceful fallback for unknown metrics
 */
export function getMetricMetadata(metricName: string): MetricMetadata {
  const metadata = METRIC_METADATA_MAP.get(metricName)
  if (metadata) return metadata

  // Infer source from metric prefix
  let source: SourceTool = 'unknown'
  if (metricName.startsWith('claude_code.')) source = 'claude_code'
  else if (metricName.startsWith('gemini_cli.')) source = 'gemini_cli'
  // exec_server_* come from Codex's exec server (codex-exec-server)
  else if (metricName.startsWith('codex.') || metricName.startsWith('codex_cli_rs.') || metricName.startsWith('exec_server_'))
    source = 'codex_cli_rs'
  else if (metricName.startsWith('github_copilot.') || metricName.startsWith('copilot_chat.')) source = 'github_copilot'
  else if (metricName.startsWith('opencode.')) source = 'opencode'
  else if (metricName.startsWith('gen_ai.')) source = 'gemini_cli'

  let unit = UNIT_FORMATS.count
  let metricType: MetricType = 'counter'
  let isMonotonic = true
  if (source === 'opencode') {
    if (metricName.includes('cost')) {
      unit = UNIT_FORMATS.usd
    } else if (metricName.includes('token')) {
      unit = UNIT_FORMATS.tokens
    } else if (metricName.includes('duration')) {
      unit = UNIT_FORMATS.milliseconds
      metricType = 'histogram'
      isMonotonic = false
    }
  }

  // Return fallback metadata
  return {
    name: metricName,
    displayName: formatMetricName(metricName),
    description: '',
    source,
    metricType,
    unit,
    isMonotonic,
  }
}

/**
 * Convert raw metric name to human-readable format
 * e.g., "claude_code.token.usage" -> "Token Usage"
 */
export function formatMetricName(name: string): string {
  // Remove common prefixes
  const prefixes = [
    'claude_code.',
    'gemini_cli.',
    'codex_cli_rs.',
    'codex.',
    'github_copilot.',
    'copilot_chat.',
    'opencode.',
    'gen_ai.',
  ]
  let cleaned = name
  for (const prefix of prefixes) {
    if (cleaned.startsWith(prefix)) {
      cleaned = cleaned.slice(prefix.length)
      break
    }
  }

  // Convert snake_case and dots to Title Case
  return cleaned
    .replace(/[._]/g, ' ')
    .replace(/\b\w/g, (c) => c.toUpperCase())
    .trim()
}

/**
 * Format a metric value based on its unit type
 */
export function formatMetricValue(value: number | null, unit: UnitFormat): string {
  if (value === null || value === undefined) return '—'

  const scaledValue = unit.scale ? value / unit.scale : value

  switch (unit.formatter) {
    case 'duration':
      return formatDuration(scaledValue, unit.displayUnit)
    case 'bytes':
      return formatBytes(scaledValue)
    case 'currency':
      return formatCurrency(scaledValue)
    case 'percentage':
      return `${scaledValue.toFixed(1)}%`
    case 'tokens':
      return formatNumber(scaledValue)
    case 'ratio':
      return scaledValue.toFixed(3)
    case 'number':
    default:
      return formatNumber(scaledValue)
  }
}

/**
 * Format a number with K/M abbreviations
 */
export function formatNumber(n: number): string {
  if (Math.abs(n) >= 1_000_000_000) return `${(n / 1_000_000_000).toFixed(2)}B`
  if (Math.abs(n) >= 1_000_000) return `${(n / 1_000_000).toFixed(2)}M`
  if (Math.abs(n) >= 1_000) return `${(n / 1_000).toFixed(2)}K`
  return Number.isInteger(n) ? n.toString() : n.toFixed(2)
}

/**
 * Format a duration value
 */
export function formatDuration(value: number, unit: string): string {
  // If value is in milliseconds
  if (unit === 'ms') {
    if (value >= 60000) return `${(value / 60000).toFixed(1)}m`
    if (value >= 1000) return `${(value / 1000).toFixed(2)}s`
    return `${Math.round(value)}ms`
  }

  // If value is in seconds
  if (value >= 3600) return `${(value / 3600).toFixed(1)}h`
  if (value >= 60) return `${(value / 60).toFixed(1)}m`
  if (value >= 1) return `${value.toFixed(1)}s`
  return `${Math.round(value * 1000)}ms`
}

// Format bytes to human-readable format
export { formatBytes }

/**
 * Format currency (USD)
 */
export function formatCurrency(amount: number): string {
  return new Intl.NumberFormat('en-US', {
    style: 'currency',
    currency: 'USD',
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  }).format(amount)
}

/**
 * Get the display label for a series based on its labels and metadata
 */
export function getSeriesLabel(
  labels: Record<string, string> | undefined,
  metadata: MetricMetadata,
  breakdownAttribute?: string
): string {
  if (!labels) {
    return metadata.displayName || 'value'
  }

  // If a specific breakdown attribute is requested, use it
  if (breakdownAttribute && labels[breakdownAttribute]) {
    const breakdown = metadata.breakdowns?.find((b) => b.attributeKey === breakdownAttribute)
    if (breakdown) {
      const value = labels[breakdownAttribute]
      return breakdown.knownValues?.[value] || value
    }
    return labels[breakdownAttribute]
  }

  // Otherwise, use the first breakdown with a value
  if (metadata.breakdowns) {
    for (const breakdown of metadata.breakdowns) {
      const value = labels[breakdown.attributeKey]
      if (value) {
        return breakdown.knownValues?.[value] || value
      }
    }
  }

  // Fall back to service name or type
  if (labels.type) {
    const serviceName = labels.service ? getServiceDisplayName(labels.service) : ''
    return serviceName ? `${serviceName}: ${labels.type}` : labels.type
  }

  return labels.service ? getServiceDisplayName(labels.service) : 'value'
}

/**
 * Get available breakdown attributes for a metric
 */
export function getAvailableBreakdowns(metricName: string): AttributeBreakdown[] {
  const metadata = getMetricMetadata(metricName)
  return metadata.breakdowns || []
}

/**
 * Get metrics grouped by source tool
 */
export function getMetricsBySource(): Record<SourceTool, MetricMetadata[]> {
  const grouped: Record<SourceTool, MetricMetadata[]> = {
    claude_code: [],
    gemini_cli: [],
    codex_cli_rs: [],
    github_copilot: [],
    opencode: [],
    unknown: [],
  }

  for (const metric of METRIC_CATALOG) {
    grouped[metric.source].push(metric)
  }

  return grouped
}

/**
 * Get display name for a source tool
 */
export function getSourceDisplayName(source: SourceTool): string {
  switch (source) {
    case 'claude_code':
      return 'Claude Code'
    case 'gemini_cli':
      return 'Gemini CLI'
    case 'codex_cli_rs':
      return 'OpenAI Codex'
    case 'github_copilot':
      return 'GitHub Copilot'
    case 'opencode':
      return 'OpenCode'
    default:
      return 'Other'
  }
}

// Display names of the supported tools' service names, used until the
// backend's provider list (providerStore) has loaded or when it cannot be
// loaded. Keep in sync with the provider table in backend/internal/tools.
const FALLBACK_SERVICE_NAMES: Record<string, string> = {
  'claude-code': 'Claude Code',
  'gemini-cli': 'Gemini CLI',
  gemini_cli: 'Gemini CLI',
  codex_cli_rs: 'OpenAI Codex CLI',
  'codex-app-server': 'OpenAI Codex',
  'codex-exec-server': 'OpenAI Codex exec server',
  'copilot-chat': 'GitHub Copilot (VS Code)',
  'github-copilot': 'GitHub Copilot CLI',
  opencode: 'OpenCode',
}

// The service names of tools that send several (backend tools.Provider
// OtherServices), by tool name, used until the provider list has loaded. A
// service filter with the tool name covers all of them.
const FALLBACK_TOOL_SERVICES: Record<string, string[]> = {
  codex: ['codex_cli_rs', 'codex-app-server', 'codex-exec-server'],
}

// Display names of those tool filters
const TOOL_FILTER_NAMES: Record<string, string> = {
  codex: 'OpenAI Codex (all services)',
}

// What service names depend on: the backend's provider list once loaded
// (providerStore), otherwise the fallbacks above
export interface ProviderNames {
  displayNames: Record<string, string>
  toolServices: Record<string, string[]>
  loaded: boolean
}

// The service names of the tools that send several, by tool name
function toolServicesOf(names: ProviderNames): Record<string, string[]> {
  return names.loaded ? names.toolServices : FALLBACK_TOOL_SERVICES
}

/**
 * Display name of a service name (from API/backend), or of a tool name used
 * as service filter (codex: all of Codex's services), given the provider
 * names (useServiceNames passes the store's state)
 */
export function serviceDisplayName(serviceName: string, names: ProviderNames): string {
  const name = names.displayNames[serviceName] ?? FALLBACK_SERVICE_NAMES[serviceName]
  if (name) return name
  if (toolServicesOf(names)[serviceName]) return TOOL_FILTER_NAMES[serviceName] ?? `${serviceName} (all services)`
  return serviceName
}

/**
 * serviceDisplayName with the provider names loaded so far. Components use
 * useServiceNames instead, which re-renders them when the names arrive.
 */
export function getServiceDisplayName(serviceName: string): string {
  return serviceDisplayName(serviceName, useProviderStore.getState())
}

export interface ServiceFilterOption {
  value: string
  label: string
}

/**
 * Options of a service filter: the services, each with its display name, and
 * before the first service of a tool that sends several of them, the tool
 * name (which the API accepts as filter for all of them), when at least two
 * of them are present
 */
export function serviceFilterOptionsFor(services: string[], names: ProviderNames): ServiceFilterOption[] {
  const toolServices = toolServicesOf(names)
  const toolOf = new Map<string, string>()
  for (const [tool, serviceNames] of Object.entries(toolServices)) {
    if (serviceNames.filter((name) => services.includes(name)).length >= 2) {
      for (const name of serviceNames) toolOf.set(name, tool)
    }
  }
  const options: ServiceFilterOption[] = []
  const added = new Set<string>()
  for (const service of services) {
    const tool = toolOf.get(service)
    if (tool && !added.has(tool)) {
      added.add(tool)
      options.push({ value: tool, label: serviceDisplayName(tool, names) })
    }
    options.push({ value: service, label: serviceDisplayName(service, names) })
  }
  return options
}

/** serviceFilterOptionsFor with the provider names loaded so far (see getServiceDisplayName) */
export function serviceFilterOptions(services: string[]): ServiceFilterOption[] {
  return serviceFilterOptionsFor(services, useProviderStore.getState())
}

/**
 * Get icon component for a service name (from API/backend)
 */
export function getServiceIcon(serviceName: string): LucideIcon {
  switch (serviceName) {
    case 'claude-code':
      return Bot
    case 'gemini-cli':
    case 'gemini_cli':
      return Sparkles
    case 'codex_cli_rs':
    case 'codex-app-server':
    case 'codex-exec-server':
      return Terminal
    case 'copilot-chat':
    case 'github-copilot':
      return BotMessageSquare
    case 'opencode':
      return Terminal
    default:
      return Bot // fallback
  }
}

// ============================================================================
// Histogram statistics and breakdowns of series queries
// ============================================================================

/**
 * The statistics of histogram series (the stat parameter of series queries),
 * each one the API knows (APIHistogramStat)
 */
export const HISTOGRAM_STATS = [
  { value: 'avg', label: 'Average' },
  { value: 'p50', label: 'Median (p50)' },
  { value: 'p90', label: '90th percentile' },
  { value: 'p95', label: '95th percentile' },
  { value: 'p99', label: '99th percentile' },
  { value: 'min', label: 'Minimum' },
  { value: 'max', label: 'Maximum' },
  { value: 'sum', label: 'Sum' },
  { value: 'count', label: 'Count' },
] as const satisfies readonly { value: APIHistogramStat; label: string }[]

export type HistogramStat = (typeof HISTOGRAM_STATS)[number]['value']

export function isHistogramStat(value: string | undefined): value is HistogramStat {
  return HISTOGRAM_STATS.some((s) => s.value === value)
}

export function isHistogramMetric(metadata: MetricMetadata | null | undefined): boolean {
  return metadata?.metricType === 'histogram'
}

/**
 * The statistic a histogram metric shows when none is chosen: the sum for
 * token counts (the tokens of the requests add up to the usage), the
 * average otherwise (durations and sizes, as the server defaults)
 */
export function defaultHistogramStat(metadata: MetricMetadata): HistogramStat {
  return metadata.unit.formatter === 'tokens' ? 'sum' : 'avg'
}

/**
 * The stat to request for a metric: the chosen one, or the default, for
 * histograms; undefined for other metrics, which take none
 */
export function seriesStat(metadata: MetricMetadata | null | undefined, stat?: string): HistogramStat | undefined {
  if (!metadata || !isHistogramMetric(metadata)) return undefined
  return isHistogramStat(stat) ? stat : defaultHistogramStat(metadata)
}

/**
 * The stat parameter of a series query: seriesStat, left out when it is the
 * server's default (avg). Not sending the default keeps metrics the catalog
 * takes for histograms working when their points are of another type.
 */
export function statParam(metadata: MetricMetadata | null | undefined, stat?: string): HistogramStat | undefined {
  const s = seriesStat(metadata, stat)
  return s === 'avg' ? undefined : s
}

/** Whether the values of a stat add up across series (stacked bars, sums) */
export function isAdditiveStat(stat: string | undefined): boolean {
  return !stat || stat === 'sum' || stat === 'count'
}

/** The unit of a stat's values: counts are plain numbers */
export function statUnit(unit: UnitFormat, stat: string | undefined): UnitFormat {
  return stat === 'count' ? UNIT_FORMATS.count : unit
}

export interface BreakdownOption {
  value: string
  label: string
  // A catalog breakdown of the metric (else an attribute key seen in its points)
  known: boolean
}

/**
 * The attributes a metric's series can be broken down by: the catalog's
 * breakdowns with their names first, then the other attribute keys the
 * stored points have (GET /api/metrics/attribute-keys)
 */
export function breakdownOptions(metadata: MetricMetadata | null | undefined, keys: string[] = []): BreakdownOption[] {
  const options: BreakdownOption[] = (metadata?.breakdowns ?? []).map((b) => ({
    value: b.attributeKey,
    label: b.displayName,
    known: true,
  }))
  const seen = new Set(options.map((o) => o.value))
  for (const key of [...keys].sort((a, b) => a.localeCompare(b))) {
    if (!seen.has(key)) {
      seen.add(key)
      options.push({ value: key, label: key, known: false })
    }
  }
  return options
}
