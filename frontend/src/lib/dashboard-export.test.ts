import { describe, it, expect, vi, beforeEach } from 'vitest'
import {
  dashboardToExport,
  validateDashboardImport,
  generateUniqueName,
  fetchDashboardFromUrl,
  deriveWidgetTitle,
} from './dashboard-export'
import type { DashboardWithWidgets } from '@/types/dashboard'
import type { ExportedWidget } from '@/types/dashboard-export'
import { DASHBOARD_EXPORT_SCHEMA_VERSION } from '@/types/dashboard-export'
import { WIDGET_TYPES } from '@/types/dashboard'

// Mock metricMetadata
vi.mock('@/lib/metricMetadata', () => ({
  getMetricMetadata: vi.fn((metricName: string) => ({
    displayName: `Display: ${metricName}`,
    description: 'Mock description',
  })),
}))

describe('dashboard-export', () => {
  describe('dashboardToExport', () => {
    it('converts dashboard to export format with correct schema version', () => {
      const dashboard: DashboardWithWidgets = {
        id: 'dashboard-123',
        name: 'My Dashboard',
        description: 'Test description',
        isDefault: true,
        createdAt: '2024-01-01T00:00:00Z',
        updatedAt: '2024-01-02T00:00:00Z',
        widgets: [],
      }

      const result = dashboardToExport(dashboard)

      expect(result.schemaVersion).toBe(DASHBOARD_EXPORT_SCHEMA_VERSION)
      expect(result.name).toBe('My Dashboard')
      expect(result.description).toBe('Test description')
      expect(result.widgets).toEqual([])
    })

    it('strips IDs and timestamps from widgets', () => {
      const dashboard: DashboardWithWidgets = {
        id: 'dashboard-123',
        name: 'Test',
        isDefault: false,
        createdAt: '2024-01-01T00:00:00Z',
        updatedAt: '2024-01-02T00:00:00Z',
        widgets: [
          {
            id: 'widget-456',
            dashboardId: 'dashboard-123',
            widgetType: WIDGET_TYPES.STATS_TRACES,
            title: 'Total Traces',
            gridColumn: 1,
            gridRow: 1,
            colSpan: 2,
            rowSpan: 1,
            createdAt: '2024-01-01T00:00:00Z',
            updatedAt: '2024-01-02T00:00:00Z',
          },
        ],
      }

      const result = dashboardToExport(dashboard)

      expect(result.widgets).toHaveLength(1)
      expect(result.widgets[0]).toEqual({
        widgetType: WIDGET_TYPES.STATS_TRACES,
        gridColumn: 1,
        gridRow: 1,
        colSpan: 2,
        rowSpan: 1,
      })
      // Should not have id, dashboardId, title, timestamps
      expect(result.widgets[0]).not.toHaveProperty('id')
      expect(result.widgets[0]).not.toHaveProperty('dashboardId')
      expect(result.widgets[0]).not.toHaveProperty('title')
      expect(result.widgets[0]).not.toHaveProperty('createdAt')
    })

    it('includes config for widgets that have it', () => {
      const dashboard: DashboardWithWidgets = {
        id: 'dashboard-123',
        name: 'Test',
        isDefault: false,
        createdAt: '2024-01-01T00:00:00Z',
        updatedAt: '2024-01-02T00:00:00Z',
        widgets: [
          {
            id: 'widget-456',
            dashboardId: 'dashboard-123',
            widgetType: WIDGET_TYPES.METRIC_CHART,
            title: 'Token Usage',
            gridColumn: 1,
            gridRow: 1,
            colSpan: 2,
            rowSpan: 2,
            config: { metricName: 'claude_code.token.usage', aggregate: true },
            createdAt: '2024-01-01T00:00:00Z',
            updatedAt: '2024-01-02T00:00:00Z',
          },
        ],
      }

      const result = dashboardToExport(dashboard)

      expect(result.widgets[0].config).toEqual({
        metricName: 'claude_code.token.usage',
        aggregate: true,
      })
    })

    it('excludes empty config objects', () => {
      const dashboard: DashboardWithWidgets = {
        id: 'dashboard-123',
        name: 'Test',
        isDefault: false,
        createdAt: '2024-01-01T00:00:00Z',
        updatedAt: '2024-01-02T00:00:00Z',
        widgets: [
          {
            id: 'widget-456',
            dashboardId: 'dashboard-123',
            widgetType: WIDGET_TYPES.STATS_TRACES,
            title: 'Total Traces',
            gridColumn: 1,
            gridRow: 1,
            colSpan: 1,
            rowSpan: 1,
            config: {},
            createdAt: '2024-01-01T00:00:00Z',
            updatedAt: '2024-01-02T00:00:00Z',
          },
        ],
      }

      const result = dashboardToExport(dashboard)

      expect(result.widgets[0]).not.toHaveProperty('config')
    })
  })

  describe('validateDashboardImport', () => {
    const validExport = {
      schemaVersion: 1,
      name: 'Test Dashboard',
      description: 'A test dashboard',
      widgets: [
        {
          widgetType: WIDGET_TYPES.STATS_TRACES,
          gridColumn: 1,
          gridRow: 1,
          colSpan: 1,
          rowSpan: 1,
        },
      ],
    }

    it('validates a correct export', () => {
      const result = validateDashboardImport(validExport)

      expect(result.valid).toBe(true)
      expect(result.errors).toHaveLength(0)
      expect(result.data).toEqual(validExport)
    })

    it('rejects non-object data', () => {
      expect(validateDashboardImport(null).valid).toBe(false)
      expect(validateDashboardImport('string').valid).toBe(false)
      expect(validateDashboardImport(123).valid).toBe(false)
      expect(validateDashboardImport(undefined).valid).toBe(false)
    })

    it('requires schemaVersion', () => {
      const data = { ...validExport, schemaVersion: undefined }
      const result = validateDashboardImport(data)

      expect(result.valid).toBe(false)
      expect(result.errors).toContain('Missing or invalid schemaVersion')
    })

    it('rejects unsupported schema versions', () => {
      const data = { ...validExport, schemaVersion: 999 }
      const result = validateDashboardImport(data)

      expect(result.valid).toBe(false)
      expect(result.errors[0]).toContain('Unsupported schema version')
    })

    it('requires dashboard name', () => {
      const data = { ...validExport, name: '' }
      const result = validateDashboardImport(data)

      expect(result.valid).toBe(false)
      expect(result.errors).toContain('Dashboard name is required')
    })

    it('validates description is a string if present', () => {
      const data = { ...validExport, description: 123 }
      const result = validateDashboardImport(data)

      expect(result.valid).toBe(false)
      expect(result.errors).toContain('Description must be a string')
    })

    it('requires widgets to be an array', () => {
      const data = { ...validExport, widgets: 'not an array' }
      const result = validateDashboardImport(data)

      expect(result.valid).toBe(false)
      expect(result.errors).toContain('Widgets must be an array')
    })

    it('validates widget structure', () => {
      const data = {
        ...validExport,
        widgets: [{ widgetType: WIDGET_TYPES.STATS_TRACES }], // missing grid fields
      }
      const result = validateDashboardImport(data)

      expect(result.valid).toBe(false)
      expect(result.errors.some((e) => e.includes('gridColumn'))).toBe(true)
    })

    it('rejects unknown widget types', () => {
      const data = {
        ...validExport,
        widgets: [
          {
            widgetType: 'unknown_type',
            gridColumn: 1,
            gridRow: 1,
            colSpan: 1,
            rowSpan: 1,
          },
        ],
      }
      const result = validateDashboardImport(data)

      expect(result.valid).toBe(false)
      expect(result.errors.some((e) => e.includes('Unknown widget type'))).toBe(true)
    })

    it('validates grid bounds (max 4 columns)', () => {
      const data = {
        ...validExport,
        widgets: [
          {
            widgetType: WIDGET_TYPES.STATS_TRACES,
            gridColumn: 3,
            gridRow: 1,
            colSpan: 3, // 3 + 3 - 1 = 5 > 4
            rowSpan: 1,
          },
        ],
      }
      const result = validateDashboardImport(data)

      expect(result.valid).toBe(false)
      expect(result.errors.some((e) => e.includes('exceeds grid width'))).toBe(true)
    })

    it('requires config.metricName for metric widgets', () => {
      const data = {
        ...validExport,
        widgets: [
          {
            widgetType: WIDGET_TYPES.METRIC_CHART,
            gridColumn: 1,
            gridRow: 1,
            colSpan: 2,
            rowSpan: 2,
            // missing config
          },
        ],
      }
      const result = validateDashboardImport(data)

      expect(result.valid).toBe(false)
      expect(result.errors.some((e) => e.includes('metricName'))).toBe(true)
    })

    it('accepts metric widgets with valid config', () => {
      const data = {
        ...validExport,
        widgets: [
          {
            widgetType: WIDGET_TYPES.METRIC_CHART,
            gridColumn: 1,
            gridRow: 1,
            colSpan: 2,
            rowSpan: 2,
            config: { metricName: 'test.metric' },
          },
        ],
      }
      const result = validateDashboardImport(data)

      expect(result.valid).toBe(true)
    })
  })

  describe('generateUniqueName', () => {
    it('returns original name if no conflict', () => {
      const result = generateUniqueName('My Dashboard', ['Other Dashboard'])
      expect(result).toBe('My Dashboard')
    })

    it('appends (Copy) on first conflict', () => {
      const result = generateUniqueName('My Dashboard', ['My Dashboard'])
      expect(result).toBe('My Dashboard (Copy)')
    })

    it('appends (2) if (Copy) also exists', () => {
      const result = generateUniqueName('My Dashboard', ['My Dashboard', 'My Dashboard (Copy)'])
      expect(result).toBe('My Dashboard (2)')
    })

    it('increments number until unique', () => {
      const existing = [
        'My Dashboard',
        'My Dashboard (Copy)',
        'My Dashboard (2)',
        'My Dashboard (3)',
      ]
      const result = generateUniqueName('My Dashboard', existing)
      expect(result).toBe('My Dashboard (4)')
    })

    it('handles empty existing names array', () => {
      const result = generateUniqueName('My Dashboard', [])
      expect(result).toBe('My Dashboard')
    })
  })

  describe('fetchDashboardFromUrl', () => {
    beforeEach(() => {
      vi.resetAllMocks()
    })

    it('rejects invalid URL format', async () => {
      const result = await fetchDashboardFromUrl('not-a-url')
      expect(result.error).toBe('Invalid URL format')
    })

    it('rejects non-http protocols', async () => {
      const result = await fetchDashboardFromUrl('ftp://example.com/dashboard.json')
      expect(result.error).toBe('URL must use http:// or https://')
    })

    it('fetches and returns JSON data', async () => {
      const mockData = { schemaVersion: 1, name: 'Test', widgets: [] }
      global.fetch = vi.fn().mockResolvedValue({
        ok: true,
        headers: new Headers({ 'content-type': 'application/json' }),
        json: () => Promise.resolve(mockData),
      })

      const result = await fetchDashboardFromUrl('https://example.com/dashboard.json')

      expect(result.data).toEqual(mockData)
      expect(result.error).toBeUndefined()
    })

    it('handles HTTP errors', async () => {
      global.fetch = vi.fn().mockResolvedValue({
        ok: false,
        status: 404,
      })

      const result = await fetchDashboardFromUrl('https://example.com/dashboard.json')

      expect(result.error).toBe('Failed to fetch: HTTP 404')
    })

    it('handles non-JSON content type', async () => {
      global.fetch = vi.fn().mockResolvedValue({
        ok: true,
        headers: new Headers({ 'content-type': 'text/html' }),
        json: () => Promise.resolve({}),
      })

      const result = await fetchDashboardFromUrl('https://example.com/dashboard.json')

      // text/ is allowed, so this should succeed
      expect(result.error).toBeUndefined()
    })

    it('handles invalid JSON response', async () => {
      global.fetch = vi.fn().mockResolvedValue({
        ok: true,
        headers: new Headers({ 'content-type': 'application/json' }),
        json: () => Promise.reject(new SyntaxError('Invalid JSON')),
      })

      const result = await fetchDashboardFromUrl('https://example.com/dashboard.json')

      expect(result.error).toBe('Response is not valid JSON')
    })

    it('handles network errors', async () => {
      global.fetch = vi.fn().mockRejectedValue(new Error('Network error'))

      const result = await fetchDashboardFromUrl('https://example.com/dashboard.json')

      expect(result.error).toBe('Network error: Failed to fetch URL')
    })
  })

  describe('deriveWidgetTitle', () => {
    it('uses metric metadata for metric value widgets', () => {
      const widget: ExportedWidget = {
        widgetType: WIDGET_TYPES.METRIC_VALUE,
        gridColumn: 1,
        gridRow: 1,
        colSpan: 1,
        rowSpan: 1,
        config: { metricName: 'claude_code.token.usage' },
      }

      const result = deriveWidgetTitle(widget)

      expect(result).toBe('Display: claude_code.token.usage')
    })

    it('uses metric metadata for metric chart widgets', () => {
      const widget: ExportedWidget = {
        widgetType: WIDGET_TYPES.METRIC_CHART,
        gridColumn: 1,
        gridRow: 1,
        colSpan: 2,
        rowSpan: 2,
        config: { metricName: 'gemini_cli.api.request.latency' },
      }

      const result = deriveWidgetTitle(widget)

      expect(result).toBe('Display: gemini_cli.api.request.latency')
    })

    it('uses WIDGET_DEFINITIONS for built-in widgets', () => {
      const widget: ExportedWidget = {
        widgetType: WIDGET_TYPES.STATS_TRACES,
        gridColumn: 1,
        gridRow: 1,
        colSpan: 1,
        rowSpan: 1,
      }

      const result = deriveWidgetTitle(widget)

      expect(result).toBe('Total Traces')
    })

    it('uses WIDGET_DEFINITIONS for active services widget', () => {
      const widget: ExportedWidget = {
        widgetType: WIDGET_TYPES.ACTIVE_SERVICES,
        gridColumn: 1,
        gridRow: 1,
        colSpan: 2,
        rowSpan: 1,
      }

      const result = deriveWidgetTitle(widget)

      expect(result).toBe('Active Services')
    })

    it('formats unknown widget types as fallback', () => {
      const widget: ExportedWidget = {
        widgetType: 'custom_unknown_widget',
        gridColumn: 1,
        gridRow: 1,
        colSpan: 1,
        rowSpan: 1,
      }

      const result = deriveWidgetTitle(widget)

      expect(result).toBe('Custom Unknown Widget')
    })
  })
})
