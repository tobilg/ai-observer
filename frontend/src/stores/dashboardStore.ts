import { create } from 'zustand'
import type { GridPosition } from '@/lib/dashboardGrid'

// UI state of the dashboard page. The dashboards and their widgets are server
// state: they live in the TanStack Query cache (useDashboard,
// useDefaultDashboard, useDashboards) and change through the mutations of
// hooks/useDashboardMutations.ts. The time selection lives in the URL
// (DashboardBuilder).

export interface DashboardUiState {
  isEditMode: boolean

  // The widget panel: adding a widget (editingWidgetId null), optionally at
  // a chosen empty cell (targetPosition), or editing a widget
  isAddPanelOpen: boolean
  targetPosition: GridPosition | null
  editingWidgetId: string | null

  setEditMode: (enabled: boolean) => void
  // Opens the panel to add a widget, at position or the next free one
  openAddPanel: (position?: GridPosition | null) => void
  // Opens the panel to change a widget
  openEditPanel: (widgetId: string) => void
  closePanel: () => void
  // Leaves edit mode and closes the panel (on switching dashboards)
  reset: () => void
}

const closedPanel = { isAddPanelOpen: false, targetPosition: null, editingWidgetId: null }

export const useDashboardStore = create<DashboardUiState>((set) => ({
  isEditMode: false,
  ...closedPanel,

  setEditMode: (enabled) => set(enabled ? { isEditMode: true } : { isEditMode: false, ...closedPanel }),

  openAddPanel: (position = null) => set({ isAddPanelOpen: true, targetPosition: position, editingWidgetId: null }),

  openEditPanel: (widgetId) => set({ isAddPanelOpen: true, targetPosition: null, editingWidgetId: widgetId }),

  closePanel: () => set(closedPanel),

  reset: () => set({ isEditMode: false, ...closedPanel }),
}))
