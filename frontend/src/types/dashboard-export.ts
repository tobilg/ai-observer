import type { WidgetConfig } from './dashboard'

// Schema version for forward compatibility
export const DASHBOARD_EXPORT_SCHEMA_VERSION = 1

// Matches CreateWidgetRequest from API, but without 'title' (auto-derived on import)
export interface ExportedWidget {
  widgetType: string
  gridColumn: number
  gridRow: number
  colSpan: number
  rowSpan: number
  config?: WidgetConfig
}

// Matches CreateDashboardRequest from API, plus schemaVersion and widgets
export interface DashboardExport {
  schemaVersion: number
  name: string
  description?: string
  widgets: ExportedWidget[]
}

// Validation result type
export interface ValidationResult {
  valid: boolean
  errors: string[]
  data?: DashboardExport
}
