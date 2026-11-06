// Grouping of metric names by the tool that sends them, for the metric
// pickers of the metrics page and the add-widget panel
import { getMetricMetadata, METRIC_CATALOG } from '@/lib/metricMetadata'
import type { SourceTool } from '@/lib/metricTypes'

export type MetricGroup = Exclude<SourceTool, 'unknown'> | 'other'
export type MetricGroups = Record<MetricGroup, string[]>

// Order of the groups in a metric picker
export const METRIC_GROUP_ORDER: MetricGroup[] = [
  'claude_code',
  'gemini_cli',
  'codex_cli_rs',
  'github_copilot',
  'opencode',
  'other',
]

// The tools that send the OTel semantic convention gen_ai.* metrics
const GENAI_SOURCES: ReadonlySet<SourceTool> = new Set(['gemini_cli', 'github_copilot', 'opencode'])

export function isSharedGenAIMetric(metricName: string): boolean {
  return metricName.startsWith('gen_ai.')
}

// The tool of an OTLP service name, or null for services of unknown tools
export function sourceFromService(service: string): SourceTool | null {
  const normalized = service.toLowerCase().replace(/-/g, '_')
  if (normalized.includes('claude')) return 'claude_code'
  if (normalized.includes('gemini')) return 'gemini_cli'
  if (normalized.includes('codex')) return 'codex_cli_rs'
  if (normalized.includes('copilot')) return 'github_copilot'
  if (normalized.includes('opencode')) return 'opencode'
  return null
}

// Whether metricName can come from service ("" matches every metric)
export function isMetricForService(metricName: string, service: string): boolean {
  if (!metricName || !service) return true
  const source = sourceFromService(service)
  if (!source) return true
  if (isSharedGenAIMetric(metricName) && GENAI_SOURCES.has(source)) return true
  const metricSource = getMetricMetadata(metricName).source
  return metricSource === source || metricSource === 'unknown'
}

// The catalog metrics of service's tool (all with service "") and the stored
// metricNames, grouped by tool and sorted. Shared gen_ai.* metrics are listed
// under the selected tool.
export function groupMetricsBySource(metricNames: string[], service: string): MetricGroups {
  const groups: MetricGroups = {
    claude_code: [],
    gemini_cli: [],
    codex_cli_rs: [],
    github_copilot: [],
    opencode: [],
    other: [],
  }
  const filterSource = service ? sourceFromService(service) : null

  const names = new Set<string>(metricNames)
  for (const metric of METRIC_CATALOG) {
    if (
      !filterSource ||
      metric.source === filterSource ||
      (isSharedGenAIMetric(metric.name) && GENAI_SOURCES.has(filterSource))
    ) {
      names.add(metric.name)
    }
  }

  for (const name of names) {
    const source = filterSource && isSharedGenAIMetric(name) ? filterSource : getMetricMetadata(name).source
    groups[source === 'unknown' ? 'other' : source].push(name)
  }
  for (const group of METRIC_GROUP_ORDER) {
    groups[group].sort()
  }
  return groups
}
