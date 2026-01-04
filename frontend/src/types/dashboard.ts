export interface WidgetConfig {
  service?: string
  metricName?: string
  breakdownAttribute?: string // Attribute key to use for series breakdown (e.g., "type", "model")
  breakdownValue?: string // Specific breakdown value to filter by (for metric_value widgets)
  chartStacked?: boolean // Whether to stack bars (default: true)
}

export interface DashboardWidget {
  id: string
  dashboardId: string
  widgetType: string
  title: string
  gridColumn: number
  gridRow: number
  colSpan: number
  rowSpan: number
  config: WidgetConfig
  createdAt: string
  updatedAt: string
}

export interface Dashboard {
  id: string
  name: string
  description?: string
  isDefault: boolean
  createdAt: string
  updatedAt: string
}

export interface DashboardWithWidgets extends Dashboard {
  widgets: DashboardWidget[]
}

export interface CreateDashboardRequest {
  name: string
  description?: string
  isDefault?: boolean
}

export interface UpdateDashboardRequest {
  name?: string
  description?: string
}

export interface CreateWidgetRequest {
  widgetType: string
  title: string
  gridColumn: number
  gridRow: number
  colSpan: number
  rowSpan: number
  config?: WidgetConfig
}

export interface UpdateWidgetRequest {
  title?: string
  gridColumn?: number
  gridRow?: number
  colSpan?: number
  rowSpan?: number
  config?: WidgetConfig
}

export interface WidgetPosition {
  id: string
  gridColumn: number
  gridRow: number
}

export interface EmptyCell {
  gridRow: number
  gridColumn: number
  colSpan?: number
}

export interface UpdateWidgetPositionsRequest {
  positions: WidgetPosition[]
}

export interface DashboardsResponse {
  dashboards: Dashboard[]
}

// Widget type constants
export const WIDGET_TYPES = {
  STATS_TRACES: 'stats_traces',
  STATS_METRICS: 'stats_metrics',
  STATS_LOGS: 'stats_logs',
  STATS_ERROR_RATE: 'stats_error_rate',
  ACTIVE_SERVICES: 'active_services',
  RECENT_ACTIVITY: 'recent_activity',
  METRIC_VALUE: 'metric_value',
  METRIC_CHART: 'metric_chart',
} as const

export type WidgetType = (typeof WIDGET_TYPES)[keyof typeof WIDGET_TYPES]

// Widget definitions for the add widget panel
export interface WidgetDefinition {
  type: WidgetType
  label: string
  description: string
  defaultColSpan: number
  defaultRowSpan: number
  configurable: boolean
  category: 'builtin' | 'metrics'
}

export const WIDGET_DEFINITIONS: WidgetDefinition[] = [
  {
    type: WIDGET_TYPES.STATS_TRACES,
    label: 'Total Traces',
    description: 'Shows the total number of traces',
    defaultColSpan: 1,
    defaultRowSpan: 1,
    configurable: false,
    category: 'builtin',
  },
  {
    type: WIDGET_TYPES.STATS_METRICS,
    label: 'Metrics Count',
    description: 'Shows the total number of metrics',
    defaultColSpan: 1,
    defaultRowSpan: 1,
    configurable: false,
    category: 'builtin',
  },
  {
    type: WIDGET_TYPES.STATS_LOGS,
    label: 'Log Count',
    description: 'Shows the total number of logs',
    defaultColSpan: 1,
    defaultRowSpan: 1,
    configurable: false,
    category: 'builtin',
  },
  {
    type: WIDGET_TYPES.STATS_ERROR_RATE,
    label: 'Error Rate',
    description: 'Shows the error rate percentage',
    defaultColSpan: 1,
    defaultRowSpan: 1,
    configurable: false,
    category: 'builtin',
  },
  {
    type: WIDGET_TYPES.ACTIVE_SERVICES,
    label: 'Active Services',
    description: 'Shows services sending telemetry',
    defaultColSpan: 2,
    defaultRowSpan: 1,
    configurable: false,
    category: 'builtin',
  },
  {
    type: WIDGET_TYPES.RECENT_ACTIVITY,
    label: 'Recent Traces',
    description: 'Shows recent traces',
    defaultColSpan: 4,
    defaultRowSpan: 1,
    configurable: false,
    category: 'builtin',
  },
  {
    type: WIDGET_TYPES.METRIC_VALUE,
    label: 'Metric Value',
    description: 'Display a single metric value',
    defaultColSpan: 1,
    defaultRowSpan: 1,
    configurable: true,
    category: 'metrics',
  },
  {
    type: WIDGET_TYPES.METRIC_CHART,
    label: 'Metric Chart',
    description: 'Display a metric as a line chart',
    defaultColSpan: 2,
    defaultRowSpan: 1,
    configurable: true,
    category: 'metrics',
  },
]

// Timeframe options (same as MetricsPage)
export interface TimeframeOption {
  label: string
  value: string
  durationSeconds: number
  intervalSeconds: number
  tickInterval: number
}

export const TIMEFRAME_OPTIONS: TimeframeOption[] = [
  { label: 'Last 1 minute', value: '1m', durationSeconds: 60, intervalSeconds: 1, tickInterval: 4 },
  { label: 'Last 5 minutes', value: '5m', durationSeconds: 300, intervalSeconds: 5, tickInterval: 3 },
  { label: 'Last 15 minutes', value: '15m', durationSeconds: 900, intervalSeconds: 15, tickInterval: 3 },
  { label: 'Last 30 minutes', value: '30m', durationSeconds: 1800, intervalSeconds: 30, tickInterval: 6 },
  { label: 'Last 1 hour', value: '1h', durationSeconds: 3600, intervalSeconds: 60, tickInterval: 4 },
  { label: 'Last 3 hours', value: '3h', durationSeconds: 10800, intervalSeconds: 300, tickInterval: 3 },
  { label: 'Last 6 hours', value: '6h', durationSeconds: 21600, intervalSeconds: 600, tickInterval: 3 },
  { label: 'Last 12 hours', value: '12h', durationSeconds: 43200, intervalSeconds: 1200, tickInterval: 3 },
  { label: 'Last 24 hours', value: '24h', durationSeconds: 86400, intervalSeconds: 3600, tickInterval: 3 },
  { label: 'Last 3 days', value: '3d', durationSeconds: 259200, intervalSeconds: 10800, tickInterval: 3 },
  { label: 'Last 7 days', value: '7d', durationSeconds: 604800, intervalSeconds: 21600, tickInterval: 3 },
  { label: 'Last 14 days', value: '14d', durationSeconds: 1209600, intervalSeconds: 43200, tickInterval: 3 },
  { label: 'Last 30 days', value: '30d', durationSeconds: 2592000, intervalSeconds: 86400, tickInterval: 3 },
  { label: 'Last 45 days', value: '45d', durationSeconds: 3888000, intervalSeconds: 86400, tickInterval: 3 },
  { label: 'Last 60 days', value: '60d', durationSeconds: 5184000, intervalSeconds: 172800, tickInterval: 3 },
  { label: 'Last 90 days', value: '90d', durationSeconds: 7776000, intervalSeconds: 259200, tickInterval: 3 },
  { label: 'Last 180 days', value: '180d', durationSeconds: 15552000, intervalSeconds: 604800, tickInterval: 3 },
  { label: 'Last 1 year', value: '1y', durationSeconds: 31536000, intervalSeconds: 1209600, tickInterval: 3 },
]

// Custom date range type for absolute time selections
export interface CustomDateRange {
  from: Date
  to: Date
  intervalSeconds: number
  tickInterval: number
}

// Union type for any time selection
export type TimeSelection =
  | { type: 'relative'; timeframe: TimeframeOption }
  | { type: 'absolute'; range: CustomDateRange }

// Helper to check if a time selection is absolute
export function isAbsoluteTimeSelection(selection: TimeSelection): selection is { type: 'absolute'; range: CustomDateRange } {
  return selection.type === 'absolute'
}

// Helper to get display label for a time selection
export function getTimeSelectionLabel(selection: TimeSelection): string {
  if (selection.type === 'relative') {
    return selection.timeframe.label
  }
  const { from, to } = selection.range
  const formatDate = (d: Date) => d.toLocaleDateString(undefined, { month: 'short', day: 'numeric', year: 'numeric' })
  return `${formatDate(from)} - ${formatDate(to)}`
}
