import { describe, it, expect, beforeEach } from 'vitest'
import { useDashboardStore } from './dashboardStore'

// The store holds the dashboard page's UI state only; dashboards and widgets
// are server state (hooks/useDashboardMutations.ts, lib/queries.ts)
describe('dashboardStore', () => {
  beforeEach(() => {
    useDashboardStore.getState().reset()
  })

  it('holds no dashboard data', () => {
    const state = useDashboardStore.getState() as unknown as Record<string, unknown>
    expect(state).not.toHaveProperty('widgets')
    expect(state).not.toHaveProperty('dashboard')
    expect(state).not.toHaveProperty('createDefaultDashboard')
    expect(state).not.toHaveProperty('timeSelection')
  })

  it('opens the panel to add a widget, at a chosen cell or the next free one', () => {
    useDashboardStore.getState().openAddPanel({ gridRow: 2, gridColumn: 3 })
    expect(useDashboardStore.getState()).toMatchObject({
      isAddPanelOpen: true,
      targetPosition: { gridRow: 2, gridColumn: 3 },
      editingWidgetId: null,
    })

    useDashboardStore.getState().openAddPanel()
    expect(useDashboardStore.getState().targetPosition).toBeNull()
  })

  it('opens the panel to edit a widget', () => {
    useDashboardStore.getState().openAddPanel({ gridRow: 1, gridColumn: 1 })
    useDashboardStore.getState().openEditPanel('w1')
    expect(useDashboardStore.getState()).toMatchObject({ isAddPanelOpen: true, targetPosition: null, editingWidgetId: 'w1' })

    useDashboardStore.getState().closePanel()
    expect(useDashboardStore.getState()).toMatchObject({ isAddPanelOpen: false, editingWidgetId: null })
  })

  it('closes the panel when leaving edit mode, and reset ends editing', () => {
    useDashboardStore.getState().setEditMode(true)
    useDashboardStore.getState().openEditPanel('w1')
    useDashboardStore.getState().setEditMode(false)
    expect(useDashboardStore.getState()).toMatchObject({ isEditMode: false, isAddPanelOpen: false, editingWidgetId: null })

    useDashboardStore.getState().setEditMode(true)
    useDashboardStore.getState().reset()
    expect(useDashboardStore.getState().isEditMode).toBe(false)
  })
})
