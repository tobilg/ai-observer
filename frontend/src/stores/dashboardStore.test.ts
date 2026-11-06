import { describe, it, expect, vi, beforeEach } from 'vitest'
import { api } from '@/lib/api'
import { useDashboardStore } from './dashboardStore'
import type { Dashboard, DashboardWidget } from '@/types/dashboard'

describe('dashboardStore.importDashboard', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
  })

  const exportData = {
    schemaVersion: 1,
    name: 'Imported',
    widgets: [
      { widgetType: 'stats_traces', gridColumn: 1, gridRow: 1, colSpan: 1, rowSpan: 1 },
      { widgetType: 'stats_logs', gridColumn: 2, gridRow: 1, colSpan: 1, rowSpan: 1 },
    ],
  }

  it('removes the dashboard again when a widget cannot be created', async () => {
    vi.spyOn(api, 'createDashboard').mockResolvedValue({ id: 'd1', name: 'Imported' } as Dashboard)
    vi.spyOn(api, 'createWidget')
      .mockResolvedValueOnce({} as never)
      .mockRejectedValueOnce(new Error('HTTP error! status: 400'))
    const deleteDashboard = vi.spyOn(api, 'deleteDashboard').mockResolvedValue()
    const loadDashboards = vi.spyOn(api, 'getDashboards')

    await expect(useDashboardStore.getState().importDashboard(exportData)).rejects.toThrow('status: 400')

    expect(deleteDashboard).toHaveBeenCalledWith('d1')
    expect(loadDashboards).not.toHaveBeenCalled()
  })

  it('creates all widgets and refreshes the list', async () => {
    vi.spyOn(api, 'createDashboard').mockResolvedValue({ id: 'd2', name: 'Imported' } as Dashboard)
    const createWidget = vi.spyOn(api, 'createWidget').mockResolvedValue({} as never)
    const deleteDashboard = vi.spyOn(api, 'deleteDashboard').mockResolvedValue()
    vi.spyOn(api, 'getDashboards').mockResolvedValue({ dashboards: [] })

    const dashboard = await useDashboardStore.getState().importDashboard(exportData)

    expect(dashboard.id).toBe('d2')
    expect(createWidget).toHaveBeenCalledTimes(2)
    expect(deleteDashboard).not.toHaveBeenCalled()
  })
})

describe('dashboardStore.importDashboard naming', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
  })

  it('renames an import whose name is taken', async () => {
    useDashboardStore.setState({ dashboards: [{ id: 'd0', name: 'Imported' } as Dashboard] })
    const createDashboard = vi.spyOn(api, 'createDashboard').mockResolvedValue({ id: 'd3', name: 'Imported (Copy)' } as Dashboard)
    vi.spyOn(api, 'getDashboards').mockResolvedValue({ dashboards: [] })

    await useDashboardStore.getState().importDashboard({ schemaVersion: 1, name: 'Imported', widgets: [] })

    expect(createDashboard).toHaveBeenCalledWith(expect.objectContaining({ name: 'Imported (Copy)', isDefault: false }))
  })
})

describe('dashboardStore widgets', () => {
  const widget = (id: string, gridColumn: number, gridRow: number, colSpan = 1): DashboardWidget => ({
    id, dashboardId: 'd1', widgetType: 'stats_traces', title: id,
    gridColumn, gridRow, colSpan, rowSpan: 1, config: {}, createdAt: '', updatedAt: '',
  })
  const positions = () =>
    Object.fromEntries(useDashboardStore.getState().widgets.map((w) => [w.id, [w.gridColumn, w.gridRow]]))

  beforeEach(() => {
    vi.restoreAllMocks()
    vi.spyOn(console, 'error').mockImplementation(() => {})
    useDashboardStore.setState({
      dashboard: { id: 'd1', name: 'Main' } as Dashboard,
      widgets: [widget('a', 1, 1), widget('b', 2, 1), widget('c', 1, 2)],
    })
  })

  it('adds a created widget', async () => {
    vi.spyOn(api, 'createWidget').mockResolvedValue(widget('new', 3, 1))
    await useDashboardStore.getState().addWidget({ widgetType: 'stats_logs', title: 'new', gridColumn: 3, gridRow: 1, colSpan: 1, rowSpan: 1 })
    expect(useDashboardStore.getState().widgets.map((w) => w.id)).toEqual(['a', 'b', 'c', 'new'])
  })

  it('removes a widget, and restores it when the request fails', async () => {
    const deleteWidget = vi.spyOn(api, 'deleteWidget').mockResolvedValue()
    await useDashboardStore.getState().removeWidget('b')
    expect(deleteWidget).toHaveBeenCalledWith('d1', 'b')
    expect(useDashboardStore.getState().widgets.map((w) => w.id)).toEqual(['a', 'c'])

    deleteWidget.mockRejectedValue(new Error('offline'))
    await expect(useDashboardStore.getState().removeWidget('a')).rejects.toThrow('offline')
    expect(useDashboardStore.getState().widgets.map((w) => w.id)).toEqual(['a', 'c'])
  })

  it('swaps widgets dragged onto another row, and reflows within a row', () => {
    useDashboardStore.getState().reorderWidgets('a', 'c')
    expect(positions()).toMatchObject({ a: [1, 2], c: [1, 1] })

    // Moving b left onto c (same row): b takes c's place, c moves right of it
    useDashboardStore.setState({ widgets: [widget('b', 2, 1), widget('c', 1, 1)] })
    useDashboardStore.getState().reorderWidgets('b', 'c')
    expect(positions()).toMatchObject({ b: [1, 1], c: [2, 1] })
  })

  it('moves a wide widget that no longer fits the row to the next one', () => {
    useDashboardStore.setState({ widgets: [widget('wide', 1, 1, 3), widget('x', 4, 1, 2)] })
    useDashboardStore.getState().reorderWidgets('x', 'wide')
    expect(positions()).toMatchObject({ x: [1, 1], wide: [1, 2] })
  })

  it('moves a widget to an empty cell', () => {
    useDashboardStore.getState().moveWidgetToPosition('c', 3, 4)
    expect(positions()).toMatchObject({ c: [4, 3] })
  })

  it('inserts a row by shifting the widgets below it, and rolls back on failure', async () => {
    const update = vi.spyOn(api, 'updateWidgetPositions').mockResolvedValue()
    await useDashboardStore.getState().insertRowAt(2)
    expect(positions()).toEqual({ a: [1, 1], b: [2, 1], c: [1, 3] })
    expect(update).toHaveBeenCalledWith('d1', [{ id: 'c', gridRow: 3, gridColumn: 1 }])

    update.mockRejectedValue(new Error('offline'))
    await expect(useDashboardStore.getState().insertRowAt(1)).rejects.toThrow('offline')
    expect(positions()).toEqual({ a: [1, 1], b: [2, 1], c: [1, 3] })
  })

  it('does nothing when inserting a row below all widgets', async () => {
    const update = vi.spyOn(api, 'updateWidgetPositions').mockResolvedValue()
    await useDashboardStore.getState().insertRowAt(10)
    expect(update).not.toHaveBeenCalled()
  })
})
