import { create } from 'zustand'
import { api } from '@/lib/api'
import type {
  Dashboard,
  DashboardWithWidgets,
  DashboardWidget,
  CreateWidgetRequest,
  WidgetPosition,
  EmptyCell,
  TimeSelection,
} from '@/types/dashboard'
import { TIMEFRAME_OPTIONS, isAbsoluteTimeSelection } from '@/types/dashboard'
import type { DashboardExport } from '@/types/dashboard-export'
import { generateUniqueName, deriveWidgetTitle } from '@/lib/dashboard-export'

// Grid utility functions
function getOccupiedCells(widgets: DashboardWidget[]): Set<string> {
  const occupied = new Set<string>()
  for (const w of widgets) {
    for (let r = w.gridRow; r < w.gridRow + w.rowSpan; r++) {
      for (let c = w.gridColumn; c < w.gridColumn + w.colSpan; c++) {
        occupied.add(`${r}-${c}`)
      }
    }
  }
  return occupied
}

// Get visible empty cell placeholders - one per contiguous empty space
export function getEmptyCells(widgets: DashboardWidget[], maxColumns: number = 4): EmptyCell[] {
  if (widgets.length === 0) {
    // Return a single full-width placeholder for empty dashboard
    return [{ gridRow: 1, gridColumn: 1, colSpan: maxColumns }]
  }

  const occupied = getOccupiedCells(widgets)
  const maxRow = Math.max(...widgets.map((w) => w.gridRow + w.rowSpan - 1), 1)
  const emptyCells: EmptyCell[] = []

  // Check all rows up to maxRow + 1 (add one extra row for new widgets)
  for (let row = 1; row <= maxRow + 1; row++) {
    let consecutiveStart: number | null = null
    let consecutiveCount = 0

    for (let col = 1; col <= maxColumns; col++) {
      if (!occupied.has(`${row}-${col}`)) {
        if (consecutiveStart === null) consecutiveStart = col
        consecutiveCount++
      } else {
        // End of contiguous empty space - add one placeholder for the whole span
        if (consecutiveCount > 0) {
          emptyCells.push({
            gridRow: row,
            gridColumn: consecutiveStart!,
            colSpan: consecutiveCount,
          })
        }
        consecutiveStart = null
        consecutiveCount = 0
      }
    }
    // Handle row end - add placeholder for remaining empty cells
    if (consecutiveCount > 0) {
      emptyCells.push({
        gridRow: row,
        gridColumn: consecutiveStart!,
        colSpan: consecutiveCount,
      })
    }
  }
  return emptyCells
}

// Get additional drop zones for smaller widgets within larger empty spans
// (e.g., allow dropping a 1-col widget at any position within a 4-col empty span)
export function getEmptySpans(widgets: DashboardWidget[]): EmptyCell[] {
  if (widgets.length === 0) {
    // Additional drop zones for single-column widgets in empty dashboard
    return [
      { gridRow: 1, gridColumn: 1, colSpan: 1 },
      { gridRow: 1, gridColumn: 2, colSpan: 1 },
      { gridRow: 1, gridColumn: 3, colSpan: 1 },
      { gridRow: 1, gridColumn: 4, colSpan: 1 },
    ]
  }

  const occupied = getOccupiedCells(widgets)
  const maxRow = Math.max(...widgets.map((w) => w.gridRow + w.rowSpan - 1), 1)
  const spans: EmptyCell[] = []

  for (let row = 1; row <= maxRow + 1; row++) {
    let consecutiveStart: number | null = null
    let consecutiveCount = 0

    for (let col = 1; col <= 4; col++) {
      if (!occupied.has(`${row}-${col}`)) {
        if (consecutiveStart === null) consecutiveStart = col
        consecutiveCount++
      } else {
        // For spans > 1, add individual cell drop zones
        if (consecutiveCount > 1) {
          for (let c = consecutiveStart!; c < consecutiveStart! + consecutiveCount; c++) {
            spans.push({ gridRow: row, gridColumn: c, colSpan: 1 })
          }
        }
        consecutiveStart = null
        consecutiveCount = 0
      }
    }
    // Handle row end
    if (consecutiveCount > 1) {
      for (let c = consecutiveStart!; c < consecutiveStart! + consecutiveCount; c++) {
        spans.push({ gridRow: row, gridColumn: c, colSpan: 1 })
      }
    }
  }
  return spans
}

interface DashboardState {
  // Current dashboard
  dashboard: DashboardWithWidgets | null
  widgets: DashboardWidget[]
  loading: boolean
  error: string | null

  // Dashboard list
  dashboards: Dashboard[]
  dashboardsLoading: boolean
  dashboardsError: string | null

  // Edit mode
  isEditMode: boolean

  // Time range
  timeSelection: TimeSelection
  fromTime: Date
  toTime: Date
  intervalSeconds: number
  isAbsoluteRange: boolean

  // Add widget panel
  isAddPanelOpen: boolean
  targetPosition: { gridRow: number; gridColumn: number } | null

  // Actions
  loadDefaultDashboard: () => Promise<void>
  loadDashboard: (id: string) => Promise<void>
  createDefaultDashboard: () => Promise<void>
  setEditMode: (enabled: boolean) => Promise<void>
  setTimeSelection: (selection: TimeSelection) => void
  setAddPanelOpen: (open: boolean) => void
  setTargetPosition: (pos: { gridRow: number; gridColumn: number } | null) => void

  // Dashboard list actions
  loadDashboards: () => Promise<void>
  createNewDashboard: (name: string, description?: string) => Promise<Dashboard>
  importDashboard: (exportData: DashboardExport) => Promise<Dashboard>
  renameDashboard: (id: string, name: string) => Promise<void>
  updateDashboardDetails: (id: string, name: string, description?: string) => Promise<void>
  deleteDashboardById: (id: string) => Promise<void>
  setAsDefault: (id: string) => Promise<void>

  // Widget actions
  addWidget: (req: CreateWidgetRequest) => Promise<void>
  removeWidget: (widgetId: string) => Promise<void>
  updateWidgetPositions: (positions: WidgetPosition[]) => Promise<void>
  reorderWidgets: (activeId: string, overId: string) => void
  moveWidgetToPosition: (widgetId: string, gridRow: number, gridColumn: number) => void
  insertRowAt: (beforeRow: number) => Promise<void>
}

// localStorage key for dashboard time selection
const TIME_SELECTION_STORAGE_KEY = 'ai-observer-dashboard-timeselection'

// Calculate time range from selection
const getTimeRangeFromSelection = (selection: TimeSelection) => {
  if (isAbsoluteTimeSelection(selection)) {
    return {
      from: selection.range.from,
      to: selection.range.to,
      intervalSeconds: selection.range.intervalSeconds,
    }
  }
  const to = new Date()
  const from = new Date(to.getTime() - selection.timeframe.durationSeconds * 1000)
  return { from, to, intervalSeconds: selection.timeframe.intervalSeconds }
}

// Load initial time selection from localStorage or use default
const getInitialTimeSelection = (): TimeSelection => {
  try {
    const stored = localStorage.getItem(TIME_SELECTION_STORAGE_KEY)
    if (stored) {
      const parsed = JSON.parse(stored)
      if (parsed.type === 'relative') {
        const found = TIMEFRAME_OPTIONS.find((t) => t.value === parsed.timeframeValue)
        if (found) return { type: 'relative', timeframe: found }
      }
      // We don't restore absolute selections from localStorage as they're static historical views
    }
  } catch {
    // Ignore errors, use default
  }
  const defaultTimeframe = TIMEFRAME_OPTIONS.find((t) => t.value === '1h') || TIMEFRAME_OPTIONS[4]
  return { type: 'relative', timeframe: defaultTimeframe }
}

const initialTimeSelection = getInitialTimeSelection()
const initialTimeRange = getTimeRangeFromSelection(initialTimeSelection)

export const useDashboardStore = create<DashboardState>((set, get) => ({
  dashboard: null,
  widgets: [],
  loading: false,
  error: null,
  dashboards: [],
  dashboardsLoading: false,
  dashboardsError: null,
  isEditMode: false,
  timeSelection: initialTimeSelection,
  fromTime: initialTimeRange.from,
  toTime: initialTimeRange.to,
  intervalSeconds: initialTimeRange.intervalSeconds,
  isAbsoluteRange: isAbsoluteTimeSelection(initialTimeSelection),
  isAddPanelOpen: false,
  targetPosition: null,

  loadDefaultDashboard: async () => {
    set({ loading: true, error: null })
    try {
      const dashboard = await api.getDefaultDashboard()
      if (dashboard) {
        set({
          dashboard,
          widgets: dashboard.widgets || [],
          loading: false,
        })
      } else {
        // No default dashboard exists, create one
        await get().createDefaultDashboard()
      }
    } catch (error) {
      set({
        error: error instanceof Error ? error.message : 'Failed to load dashboard',
        loading: false,
      })
    }
  },

  loadDashboard: async (id: string) => {
    set({ loading: true, error: null })
    try {
      const dashboard = await api.getDashboard(id)
      set({
        dashboard,
        widgets: dashboard.widgets || [],
        loading: false,
      })
    } catch (error) {
      const errorMessage = error instanceof Error ? error.message : 'Failed to load dashboard'
      set({
        error: errorMessage,
        loading: false,
      })
      // Re-throw to allow caller to handle navigation
      throw error
    }
  },

  createDefaultDashboard: async () => {
    set({ loading: true, error: null })
    try {
      // Create the default dashboard
      const dashboard = await api.createDashboard({
        name: 'Default Dashboard',
        description: 'AI Observer default dashboard',
        isDefault: true,
      })

      // Create the default widgets
      const defaultWidgets: CreateWidgetRequest[] = [
        { widgetType: 'stats_traces', title: 'Total Traces', gridColumn: 1, gridRow: 1, colSpan: 1, rowSpan: 1 },
        { widgetType: 'stats_metrics', title: 'Metrics', gridColumn: 2, gridRow: 1, colSpan: 1, rowSpan: 1 },
        { widgetType: 'stats_logs', title: 'Logs', gridColumn: 3, gridRow: 1, colSpan: 1, rowSpan: 1 },
        { widgetType: 'stats_error_rate', title: 'Error Rate', gridColumn: 4, gridRow: 1, colSpan: 1, rowSpan: 1 },
        { widgetType: 'active_services', title: 'Active Services', gridColumn: 1, gridRow: 2, colSpan: 2, rowSpan: 1 },
        { widgetType: 'recent_activity', title: 'Recent Traces', gridColumn: 1, gridRow: 3, colSpan: 4, rowSpan: 1 },
      ]

      const widgets: DashboardWidget[] = []
      for (const req of defaultWidgets) {
        const widget = await api.createWidget(dashboard.id, req)
        widgets.push(widget)
      }

      set({
        dashboard: { ...dashboard, widgets },
        widgets,
        loading: false,
      })
    } catch (error) {
      set({
        error: error instanceof Error ? error.message : 'Failed to create dashboard',
        loading: false,
      })
    }
  },

  setEditMode: async (enabled: boolean) => {
    if (enabled) {
      // Entering edit mode - just set the flag
      set({ isEditMode: true })
      return
    }

    // Exiting edit mode - compact rows to remove empty gaps
    const { dashboard, widgets } = get()

    if (!dashboard || widgets.length === 0) {
      set({ isEditMode: false })
      return
    }

    // Find all unique starting rows and create a mapping to compact row numbers
    const occupiedRows = new Set<number>()
    for (const w of widgets) {
      occupiedRows.add(w.gridRow)
    }
    const sortedRows = Array.from(occupiedRows).sort((a, b) => a - b)

    // Create mapping from old row to new compacted row
    const rowMapping = new Map<number, number>()
    sortedRows.forEach((oldRow, index) => {
      rowMapping.set(oldRow, index + 1) // Rows are 1-indexed
    })

    // Check if any rows need to change
    const widgetsToUpdate: WidgetPosition[] = []
    for (const w of widgets) {
      const newRow = rowMapping.get(w.gridRow)
      if (newRow !== undefined && newRow !== w.gridRow) {
        widgetsToUpdate.push({
          id: w.id,
          gridRow: newRow,
          gridColumn: w.gridColumn,
        })
      }
    }

    if (widgetsToUpdate.length === 0) {
      // No compaction needed
      set({ isEditMode: false })
      return
    }

    // Update local state optimistically
    const updatedWidgets = widgets.map((w) => {
      const newRow = rowMapping.get(w.gridRow)
      if (newRow !== undefined && newRow !== w.gridRow) {
        return { ...w, gridRow: newRow }
      }
      return w
    })
    set({ widgets: updatedWidgets, isEditMode: false })

    // Persist to backend
    try {
      await api.updateWidgetPositions(dashboard.id, widgetsToUpdate)
    } catch (error) {
      // Rollback on error - restore original positions and stay in edit mode
      set({ widgets, isEditMode: true })
      console.error('Failed to compact rows:', error)
      throw error
    }
  },

  setTimeSelection: (selection: TimeSelection) => {
    const { from, to, intervalSeconds } = getTimeRangeFromSelection(selection)
    const isAbsoluteRange = isAbsoluteTimeSelection(selection)

    // Persist to localStorage (only relative selections)
    try {
      if (!isAbsoluteRange) {
        localStorage.setItem(
          TIME_SELECTION_STORAGE_KEY,
          JSON.stringify({ type: 'relative', timeframeValue: selection.timeframe.value })
        )
      }
    } catch {
      // Ignore storage errors
    }

    set({
      timeSelection: selection,
      fromTime: from,
      toTime: to,
      intervalSeconds,
      isAbsoluteRange,
    })
  },

  setAddPanelOpen: (open: boolean) => {
    set({ isAddPanelOpen: open })
  },

  setTargetPosition: (pos: { gridRow: number; gridColumn: number } | null) => {
    set({ targetPosition: pos })
  },

  // Dashboard list actions
  loadDashboards: async () => {
    set({ dashboardsLoading: true, dashboardsError: null })
    try {
      const response = await api.getDashboards()
      set({ dashboards: response.dashboards || [], dashboardsLoading: false })
    } catch (error) {
      set({
        dashboardsError: error instanceof Error ? error.message : 'Failed to load dashboards',
        dashboardsLoading: false,
      })
    }
  },

  createNewDashboard: async (name: string, description?: string) => {
    const dashboard = await api.createDashboard({ name, description, isDefault: false })
    // Refresh dashboard list
    await get().loadDashboards()
    return dashboard
  },

  importDashboard: async (exportData: DashboardExport) => {
    const { dashboards } = get()
    const existingNames = dashboards.map((d) => d.name)

    // Generate unique name if conflict exists
    const uniqueName = generateUniqueName(exportData.name, existingNames)

    // Create the dashboard
    const dashboard = await api.createDashboard({
      name: uniqueName,
      description: exportData.description,
      isDefault: false,
    })

    // Create all widgets with derived titles
    for (const widget of exportData.widgets) {
      const title = deriveWidgetTitle(widget)
      await api.createWidget(dashboard.id, {
        widgetType: widget.widgetType,
        title,
        gridColumn: widget.gridColumn,
        gridRow: widget.gridRow,
        colSpan: widget.colSpan,
        rowSpan: widget.rowSpan,
        config: widget.config,
      })
    }

    // Refresh dashboard list
    await get().loadDashboards()

    return dashboard
  },

  renameDashboard: async (id: string, name: string) => {
    await api.updateDashboard(id, { name })
    // Update local state
    set({
      dashboards: get().dashboards.map((d) => (d.id === id ? { ...d, name } : d)),
      // Also update current dashboard if it's the one being renamed
      dashboard: get().dashboard?.id === id ? { ...get().dashboard!, name } : get().dashboard,
    })
  },

  updateDashboardDetails: async (id: string, name: string, description?: string) => {
    await api.updateDashboard(id, { name, description })
    // Update local state
    set({
      dashboards: get().dashboards.map((d) =>
        d.id === id ? { ...d, name, description } : d
      ),
      // Also update current dashboard if it's the one being updated
      dashboard: get().dashboard?.id === id
        ? { ...get().dashboard!, name, description }
        : get().dashboard,
    })
  },

  deleteDashboardById: async (id: string) => {
    await api.deleteDashboard(id)
    set({
      dashboards: get().dashboards.filter((d) => d.id !== id),
    })
  },

  setAsDefault: async (id: string) => {
    await api.setDefaultDashboard(id)
    set({
      dashboards: get().dashboards.map((d) => ({
        ...d,
        isDefault: d.id === id,
      })),
    })
  },

  addWidget: async (req: CreateWidgetRequest) => {
    const { dashboard, widgets } = get()
    if (!dashboard) return

    try {
      const widget = await api.createWidget(dashboard.id, req)
      set({ widgets: [...widgets, widget] })
    } catch (error) {
      console.error('Failed to add widget:', error)
      throw error
    }
  },

  removeWidget: async (widgetId: string) => {
    const { dashboard, widgets } = get()
    if (!dashboard) return

    // Optimistic update
    const previousWidgets = [...widgets]
    set({ widgets: widgets.filter((w) => w.id !== widgetId) })

    try {
      await api.deleteWidget(dashboard.id, widgetId)
    } catch (error) {
      // Rollback on error
      set({ widgets: previousWidgets })
      console.error('Failed to remove widget:', error)
      throw error
    }
  },

  updateWidgetPositions: async (positions: WidgetPosition[]) => {
    const { dashboard, widgets } = get()
    if (!dashboard) return

    try {
      await api.updateWidgetPositions(dashboard.id, positions)
      // Update local widget positions
      const updatedWidgets = widgets.map((w) => {
        const pos = positions.find((p) => p.id === w.id)
        if (pos) {
          return { ...w, gridColumn: pos.gridColumn, gridRow: pos.gridRow }
        }
        return w
      })
      set({ widgets: updatedWidgets })
    } catch (error) {
      console.error('Failed to update widget positions:', error)
      throw error
    }
  },

  reorderWidgets: (activeId: string, overId: string) => {
    const { widgets } = get()
    const activeWidget = widgets.find((w) => w.id === activeId)
    const overWidget = widgets.find((w) => w.id === overId)

    if (!activeWidget || !overWidget) return

    let newActiveColumn: number
    let newActiveRow: number
    let newOverColumn: number
    let newOverRow: number

    if (activeWidget.gridRow === overWidget.gridRow) {
      // Same row - smart reposition based on direction
      const isMovingLeft = activeWidget.gridColumn > overWidget.gridColumn

      if (isMovingLeft) {
        // Moving left: active takes over's position, over moves right of active
        newActiveColumn = overWidget.gridColumn
        newActiveRow = overWidget.gridRow
        newOverColumn = newActiveColumn + activeWidget.colSpan
        newOverRow = newActiveRow

        // Check if over widget fits in the row (max 4 columns)
        if (newOverColumn + overWidget.colSpan - 1 > 4) {
          // Doesn't fit, move to next row
          newOverColumn = 1
          newOverRow = newActiveRow + 1
        }
      } else {
        // Moving right: over takes active's position, active moves right of over's new position
        newOverColumn = activeWidget.gridColumn
        newOverRow = activeWidget.gridRow
        newActiveColumn = newOverColumn + overWidget.colSpan
        newActiveRow = overWidget.gridRow
      }
    } else {
      // Different rows - just swap positions
      newActiveColumn = overWidget.gridColumn
      newActiveRow = overWidget.gridRow
      newOverColumn = activeWidget.gridColumn
      newOverRow = activeWidget.gridRow
    }

    const updatedWidgets = widgets.map((w) => {
      if (w.id === activeId) {
        return { ...w, gridColumn: newActiveColumn, gridRow: newActiveRow }
      }
      if (w.id === overId) {
        return { ...w, gridColumn: newOverColumn, gridRow: newOverRow }
      }
      return w
    })

    set({ widgets: updatedWidgets })
  },

  moveWidgetToPosition: (widgetId: string, gridRow: number, gridColumn: number) => {
    const { widgets } = get()
    const updatedWidgets = widgets.map((w) =>
      w.id === widgetId ? { ...w, gridRow, gridColumn } : w
    )
    set({ widgets: updatedWidgets })
  },

  insertRowAt: async (beforeRow: number) => {
    const { dashboard, widgets } = get()
    if (!dashboard) return

    // Find all widgets that need to shift down (gridRow >= beforeRow)
    const widgetsToShift = widgets.filter((w) => w.gridRow >= beforeRow)

    if (widgetsToShift.length === 0) {
      // No widgets to shift, nothing to do
      return
    }

    // Store old positions for rollback
    const oldPositions = widgetsToShift.map((w) => ({
      id: w.id,
      gridRow: w.gridRow,
      gridColumn: w.gridColumn,
    }))

    // Optimistically update local state - shift affected widgets down by 1
    const updatedWidgets = widgets.map((w) => {
      if (w.gridRow >= beforeRow) {
        return { ...w, gridRow: w.gridRow + 1 }
      }
      return w
    })
    set({ widgets: updatedWidgets })

    // Prepare positions for API call
    const newPositions: WidgetPosition[] = widgetsToShift.map((w) => ({
      id: w.id,
      gridRow: w.gridRow + 1,
      gridColumn: w.gridColumn,
    }))

    try {
      await api.updateWidgetPositions(dashboard.id, newPositions)
    } catch (error) {
      // Rollback on error
      const rolledBackWidgets = widgets.map((w) => {
        const oldPos = oldPositions.find((p) => p.id === w.id)
        if (oldPos) {
          return { ...w, gridRow: oldPos.gridRow, gridColumn: oldPos.gridColumn }
        }
        return w
      })
      set({ widgets: rolledBackWidgets })
      console.error('Failed to insert row:', error)
      throw error
    }
  },
}))
