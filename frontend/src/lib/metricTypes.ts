// Types and unit presets shared by the metric catalog (metricCatalog/) and
// the helpers in metricMetadata.ts

// ============================================================================
// Type Definitions
// ============================================================================

export type MetricType = 'counter' | 'gauge' | 'histogram' | 'summary'
export type SourceTool = 'claude_code' | 'gemini_cli' | 'codex_cli_rs' | 'github_copilot' | 'opencode' | 'unknown'
export type UnitFormatter = 'number' | 'duration' | 'bytes' | 'currency' | 'percentage' | 'tokens' | 'ratio'

export interface UnitFormat {
  unit: string // Raw unit string from OTLP (e.g., "1", "s", "By", "USD")
  displayUnit: string // Human-readable (e.g., "count", "seconds", "bytes", "$")
  formatter: UnitFormatter
  scale?: number // Optional scale factor (e.g., 1000 for ms -> s)
}

export interface AttributeBreakdown {
  attributeKey: string // e.g., "type", "model", "decision"
  displayName: string // e.g., "Type", "Model", "Decision"
  showInLegend: boolean // Whether to show in chart legend
  knownValues?: Record<string, string> // Optional mapping of known values to labels
}

export interface MetricMetadata {
  name: string // Exact metric name (e.g., "claude_code.token.usage")
  displayName: string // Human-readable name (e.g., "Token Usage")
  description: string // Detailed description
  source: SourceTool // Which tool emits this metric
  metricType: MetricType // counter, gauge, histogram, summary
  unit: UnitFormat // Unit and formatting info
  isMonotonic: boolean // For counters: true = cumulative, false = delta
  breakdowns?: AttributeBreakdown[] // Attributes to show as multiple series
}

// ============================================================================
// Unit Format Presets
// ============================================================================

export const UNIT_FORMATS: Record<string, UnitFormat> = {
  count: { unit: '1', displayUnit: '', formatter: 'number' },
  tokens: { unit: 'tokens', displayUnit: 'tokens', formatter: 'tokens' },
  seconds: { unit: 's', displayUnit: 's', formatter: 'duration' },
  milliseconds: { unit: 'ms', displayUnit: 'ms', formatter: 'duration' },
  bytes: { unit: 'By', displayUnit: 'bytes', formatter: 'bytes' },
  usd: { unit: 'USD', displayUnit: '$', formatter: 'currency' },
  percent: { unit: '%', displayUnit: '%', formatter: 'percentage' },
  ratio: { unit: 'ratio', displayUnit: '', formatter: 'ratio' },
  score: { unit: 'score', displayUnit: '', formatter: 'number' },
}
