import type { DashboardWithWidgets, DashboardWidget } from '@/types/dashboard'
import type { DashboardExport, ExportedWidget, ValidationResult } from '@/types/dashboard-export'
import { DASHBOARD_EXPORT_SCHEMA_VERSION } from '@/types/dashboard-export'
import { WIDGET_DEFINITIONS, WIDGET_TYPES } from '@/types/dashboard'
import { getMetricMetadata } from '@/lib/metricMetadata'

// =============================================================================
// Export Functions
// =============================================================================

/**
 * Convert a DashboardWithWidgets to a portable export format (strips IDs and timestamps)
 */
export function dashboardToExport(dashboard: DashboardWithWidgets): DashboardExport {
  return {
    schemaVersion: DASHBOARD_EXPORT_SCHEMA_VERSION,
    name: dashboard.name,
    description: dashboard.description,
    widgets: dashboard.widgets.map(widgetToExport),
  }
}

/**
 * Convert a widget to export format (strips ID, dashboardId, title, timestamps)
 */
function widgetToExport(widget: DashboardWidget): ExportedWidget {
  const exported: ExportedWidget = {
    widgetType: widget.widgetType,
    gridColumn: widget.gridColumn,
    gridRow: widget.gridRow,
    colSpan: widget.colSpan,
    rowSpan: widget.rowSpan,
  }

  // Only include config if it has values
  if (widget.config && Object.keys(widget.config).length > 0) {
    exported.config = widget.config
  }

  return exported
}

/**
 * Trigger a JSON file download for the dashboard export
 */
export function downloadDashboardExport(dashboard: DashboardWithWidgets): void {
  const exportData = dashboardToExport(dashboard)
  const json = JSON.stringify(exportData, null, 2)
  const blob = new Blob([json], { type: 'application/json' })
  const url = URL.createObjectURL(blob)

  const filename = `ai-observer-export-${dashboard.id}.json`

  const link = document.createElement('a')
  link.href = url
  link.download = filename
  document.body.appendChild(link)
  link.click()
  document.body.removeChild(link)
  URL.revokeObjectURL(url)
}

// =============================================================================
// Validation Functions
// =============================================================================

/**
 * Validate an imported dashboard JSON against the schema
 */
export function validateDashboardImport(data: unknown): ValidationResult {
  const errors: string[] = []

  // Check basic structure
  if (typeof data !== 'object' || data === null) {
    return { valid: false, errors: ['Invalid JSON structure'] }
  }

  const obj = data as Record<string, unknown>

  // Check schema version
  if (typeof obj.schemaVersion !== 'number') {
    errors.push('Missing or invalid schemaVersion')
  } else if (obj.schemaVersion > DASHBOARD_EXPORT_SCHEMA_VERSION) {
    errors.push(
      `Unsupported schema version ${obj.schemaVersion}. Maximum supported: ${DASHBOARD_EXPORT_SCHEMA_VERSION}`
    )
  }

  // Check name
  if (typeof obj.name !== 'string' || obj.name.trim() === '') {
    errors.push('Dashboard name is required')
  }

  // Check description (optional)
  if (obj.description !== undefined && typeof obj.description !== 'string') {
    errors.push('Description must be a string')
  }

  // Check widgets array
  if (!Array.isArray(obj.widgets)) {
    errors.push('Widgets must be an array')
  } else {
    obj.widgets.forEach((widget, index) => {
      const widgetErrors = validateWidget(widget, index)
      errors.push(...widgetErrors)
    })
  }

  if (errors.length > 0) {
    return { valid: false, errors }
  }

  return { valid: true, errors: [], data: obj as unknown as DashboardExport }
}

/**
 * Validate a single widget
 */
function validateWidget(widget: unknown, index: number): string[] {
  const errors: string[] = []
  const prefix = `Widget ${index + 1}`

  if (typeof widget !== 'object' || widget === null) {
    return [`${prefix}: Invalid widget structure`]
  }

  const w = widget as Record<string, unknown>

  // Check widgetType
  if (typeof w.widgetType !== 'string') {
    errors.push(`${prefix}: widgetType is required`)
  } else {
    const validTypes = Object.values(WIDGET_TYPES)
    if (!validTypes.includes(w.widgetType as (typeof validTypes)[number])) {
      errors.push(`${prefix}: Unknown widget type "${w.widgetType}"`)
    }
  }

  // Check numeric fields
  const numericFields = ['gridColumn', 'gridRow', 'colSpan', 'rowSpan'] as const
  for (const field of numericFields) {
    if (typeof w[field] !== 'number' || w[field] < 1) {
      errors.push(`${prefix}: ${field} must be a positive number`)
    }
  }

  // Validate grid bounds
  if (typeof w.gridColumn === 'number' && typeof w.colSpan === 'number') {
    if (w.gridColumn + w.colSpan - 1 > 4) {
      errors.push(`${prefix}: Widget exceeds grid width (max 4 columns)`)
    }
  }

  // Validate config for metric widgets
  if (
    w.widgetType === WIDGET_TYPES.METRIC_VALUE ||
    w.widgetType === WIDGET_TYPES.METRIC_CHART
  ) {
    if (!w.config || typeof w.config !== 'object') {
      errors.push(`${prefix}: Metric widgets require config with metricName`)
    } else {
      const config = w.config as Record<string, unknown>
      if (typeof config.metricName !== 'string' || config.metricName.trim() === '') {
        errors.push(`${prefix}: Metric widgets require config.metricName`)
      }
    }
  }

  // Config must be object if present
  if (w.config !== undefined && (typeof w.config !== 'object' || w.config === null)) {
    errors.push(`${prefix}: config must be an object`)
  }

  return errors
}

// =============================================================================
// Import Helper Functions
// =============================================================================

/**
 * Generate a unique dashboard name if conflict exists
 */
export function generateUniqueName(baseName: string, existingNames: string[]): string {
  if (!existingNames.includes(baseName)) {
    return baseName
  }

  // Try "Name (Copy)" first
  const copyName = `${baseName} (Copy)`
  if (!existingNames.includes(copyName)) {
    return copyName
  }

  // Then try "Name (2)", "Name (3)", etc.
  let counter = 2
  while (counter <= 100) {
    const numberedName = `${baseName} (${counter})`
    if (!existingNames.includes(numberedName)) {
      return numberedName
    }
    counter++
  }

  // Fallback with timestamp
  return `${baseName} (${Date.now()})`
}

/**
 * Fetch dashboard JSON from a URL
 */
export async function fetchDashboardFromUrl(url: string): Promise<{ data?: unknown; error?: string }> {
  // Validate URL format
  try {
    const parsedUrl = new URL(url)
    if (!['http:', 'https:'].includes(parsedUrl.protocol)) {
      return { error: 'URL must use http:// or https://' }
    }
  } catch {
    return { error: 'Invalid URL format' }
  }

  try {
    const response = await fetch(url)

    if (!response.ok) {
      return { error: `Failed to fetch: HTTP ${response.status}` }
    }

    const contentType = response.headers.get('content-type') || ''
    if (!contentType.includes('application/json') && !contentType.includes('text/')) {
      return { error: 'Response is not JSON' }
    }

    const data = await response.json()
    return { data }
  } catch (err) {
    if (err instanceof SyntaxError) {
      return { error: 'Response is not valid JSON' }
    }
    return { error: 'Network error: Failed to fetch URL' }
  }
}

/**
 * Derive widget title from metadata based on widget type
 */
export function deriveWidgetTitle(widget: ExportedWidget): string {
  // For metric widgets, use metric metadata
  if (
    (widget.widgetType === WIDGET_TYPES.METRIC_VALUE ||
      widget.widgetType === WIDGET_TYPES.METRIC_CHART) &&
    widget.config?.metricName
  ) {
    return getMetricMetadata(widget.config.metricName).displayName
  }

  // For built-in widgets, look up from WIDGET_DEFINITIONS
  const definition = WIDGET_DEFINITIONS.find((d) => d.type === widget.widgetType)
  if (definition) {
    return definition.label
  }

  // Fallback: format widget type as title
  return widget.widgetType
    .replace(/_/g, ' ')
    .replace(/\b\w/g, (c) => c.toUpperCase())
}
