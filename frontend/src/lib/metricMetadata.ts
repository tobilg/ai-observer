// Metric Metadata System for AI Observer
// Provides human-readable names, descriptions, and formatting for metrics from
// Claude Code, Gemini CLI, Codex CLI, GitHub Copilot, and OpenCode

import { Bot, BotMessageSquare, Terminal, Sparkles, type LucideIcon } from 'lucide-react'
import { useProviderStore } from '@/stores/providerStore'
import { UNIT_FORMATS, type MetricType, type SourceTool, type UnitFormat, type AttributeBreakdown, type MetricMetadata } from '@/lib/metricTypes'
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
  else if (metricName.startsWith('codex.') || metricName.startsWith('codex_cli_rs.')) source = 'codex_cli_rs'
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

/**
 * Format bytes to human-readable format
 */
export function formatBytes(bytes: number): string {
  if (bytes >= 1_073_741_824) return `${(bytes / 1_073_741_824).toFixed(1)} GB`
  if (bytes >= 1_048_576) return `${(bytes / 1_048_576).toFixed(1)} MB`
  if (bytes >= 1_024) return `${(bytes / 1_024).toFixed(1)} KB`
  return `${bytes} B`
}

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
      return 'Codex CLI'
    case 'github_copilot':
      return 'GitHub Copilot'
    case 'opencode':
      return 'OpenCode'
    default:
      return 'Other'
  }
}

// Display names of the supported tools' service names, used until the
// backend's provider list (useProviderStore) has loaded or when it cannot be
// loaded. Keep in sync with the provider table in backend/internal/tools.
const FALLBACK_SERVICE_NAMES: Record<string, string> = {
  'claude-code': 'Claude Code',
  'gemini-cli': 'Gemini CLI',
  gemini_cli: 'Gemini CLI',
  codex_cli_rs: 'OpenAI Codex CLI',
  'copilot-chat': 'GitHub Copilot (VS Code)',
  'github-copilot': 'GitHub Copilot CLI',
  opencode: 'OpenCode',
}

/**
 * Get display name for a service name (from API/backend)
 */
export function getServiceDisplayName(serviceName: string): string {
  return (
    useProviderStore.getState().displayNames[serviceName] ??
    FALLBACK_SERVICE_NAMES[serviceName] ??
    serviceName
  )
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
