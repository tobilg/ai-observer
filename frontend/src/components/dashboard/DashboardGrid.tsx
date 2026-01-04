import { useCallback, useMemo } from 'react'
import {
  DndContext,
  MouseSensor,
  TouchSensor,
  useSensor,
  useSensors,
} from '@dnd-kit/core'
import type { DragEndEvent } from '@dnd-kit/core'
import { SortableContext, rectSortingStrategy } from '@dnd-kit/sortable'
import { useIsMobile } from '@/hooks/use-mobile'
import { useDashboardStore, getEmptyCells } from '@/stores/dashboardStore'
import { WidgetWrapper } from './widgets/WidgetWrapper'
import { WidgetRenderer } from './widgets/WidgetRenderer'
import { EmptyCellPlaceholder } from './widgets/EmptyCellPlaceholder'
import { RowSeparator } from './widgets/RowSeparator'
import type { StatsResponse } from '@/lib/api'
import type { TraceOverview } from '@/types/traces'
import type { DashboardWidget } from '@/types/dashboard'

// Check if placing a widget at a position would overlap with any existing widget
function wouldOverlap(
  widget: { colSpan: number; rowSpan: number },
  targetRow: number,
  targetCol: number,
  allWidgets: DashboardWidget[],
  excludeWidgetIds: string[]
): boolean {
  const widgetEndCol = targetCol + widget.colSpan - 1
  const widgetEndRow = targetRow + widget.rowSpan - 1

  // Check grid boundaries
  if (widgetEndCol > 4) return true

  // Check overlap with other widgets
  for (const w of allWidgets) {
    if (excludeWidgetIds.includes(w.id)) continue

    const wEndCol = w.gridColumn + w.colSpan - 1
    const wEndRow = w.gridRow + w.rowSpan - 1

    // Check if rectangles overlap
    const horizontalOverlap = targetCol <= wEndCol && widgetEndCol >= w.gridColumn
    const verticalOverlap = targetRow <= wEndRow && widgetEndRow >= w.gridRow

    if (horizontalOverlap && verticalOverlap) {
      return true
    }
  }

  return false
}

interface DashboardGridProps {
  stats: StatsResponse | null
  recentTraces: TraceOverview[]
}

export function DashboardGrid({ stats, recentTraces }: DashboardGridProps) {
  const isMobile = useIsMobile()
  const maxColumns = isMobile ? 2 : 4

  const {
    widgets,
    isEditMode,
    timeSelection,
    fromTime,
    toTime,
    removeWidget,
    reorderWidgets,
    updateWidgetPositions,
    moveWidgetToPosition,
    setAddPanelOpen,
    setTargetPosition,
    insertRowAt,
  } = useDashboardStore()

  const sensors = useSensors(
    useSensor(MouseSensor, {
      activationConstraint: {
        distance: 8,
      },
    }),
    useSensor(TouchSensor, {
      activationConstraint: {
        delay: 200,
        tolerance: 5,
      },
    })
  )

  const handleDragEnd = useCallback(async (event: DragEndEvent) => {
    const { active, over } = event

    if (!over) return

    const overData = over.data.current

    // Case 1: Dropped onto an empty cell placeholder
    if (overData?.type === 'empty') {
      const widget = widgets.find((w) => w.id === active.id)
      if (!widget) return

      // Check if widget would fit within grid bounds and not overlap with other widgets
      // Note: We exclude the widget being moved since its current position will become free
      if (wouldOverlap(widget, overData.gridRow, overData.gridColumn, widgets, [widget.id])) {
        console.log('Widget would overlap with existing widgets or exceed grid bounds')
        return
      }

      // Move widget to empty position (optimistic update)
      const oldRow = widget.gridRow
      const oldColumn = widget.gridColumn
      moveWidgetToPosition(widget.id, overData.gridRow, overData.gridColumn)

      // Persist to backend
      try {
        await updateWidgetPositions([
          { id: widget.id, gridColumn: overData.gridColumn, gridRow: overData.gridRow },
        ])
      } catch (error) {
        // Rollback on error
        moveWidgetToPosition(widget.id, oldRow, oldColumn)
        console.error('Failed to update position:', error)
      }
      return
    }

    // Case 2: Dropped onto another widget (swap positions)
    if (active.id === over.id) return

    const activeWidget = widgets.find((w) => w.id === active.id)
    const overWidget = widgets.find((w) => w.id === over.id)

    if (!activeWidget || !overWidget) return

    // Calculate the positions after swap using the same logic as reorderWidgets
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
          // Doesn't fit, would move to next row
          newOverColumn = 1
          newOverRow = newActiveRow + 1
        }
      } else {
        // Moving right: over takes active's position, active moves right of over's new position
        newOverColumn = activeWidget.gridColumn
        newOverRow = activeWidget.gridRow
        newActiveColumn = newOverColumn + overWidget.colSpan
        newActiveRow = overWidget.gridRow

        // Check if active widget fits in the row (max 4 columns)
        if (newActiveColumn + activeWidget.colSpan - 1 > 4) {
          // Doesn't fit, reject the move
          console.log('Active widget does not fit when moving right')
          return
        }
      }
    } else {
      // Different rows - simple swap
      newActiveColumn = overWidget.gridColumn
      newActiveRow = overWidget.gridRow
      newOverColumn = activeWidget.gridColumn
      newOverRow = activeWidget.gridRow
    }

    // Validate: Check if active widget would overlap at its new position
    // Exclude both widgets being swapped from the overlap check
    if (wouldOverlap(activeWidget, newActiveRow, newActiveColumn, widgets, [activeWidget.id, overWidget.id])) {
      console.log('Active widget would overlap at target position')
      return
    }

    // Validate: Check if over widget would overlap at its new position
    if (wouldOverlap(overWidget, newOverRow, newOverColumn, widgets, [activeWidget.id, overWidget.id])) {
      console.log('Over widget would overlap at new position')
      return
    }

    // Store old positions for rollback
    const oldActivePos = { gridRow: activeWidget.gridRow, gridColumn: activeWidget.gridColumn }
    const oldOverPos = { gridRow: overWidget.gridRow, gridColumn: overWidget.gridColumn }

    // Swap positions locally first (optimistic update)
    reorderWidgets(active.id as string, over.id as string)

    // Get the updated widget positions from the store after reorder
    // We need to read from the store since reorderWidgets updated it
    const updatedWidgets = useDashboardStore.getState().widgets
    const updatedActive = updatedWidgets.find((w) => w.id === active.id)
    const updatedOver = updatedWidgets.find((w) => w.id === over.id)

    if (updatedActive && updatedOver) {
      try {
        await updateWidgetPositions([
          { id: updatedActive.id, gridColumn: updatedActive.gridColumn, gridRow: updatedActive.gridRow },
          { id: updatedOver.id, gridColumn: updatedOver.gridColumn, gridRow: updatedOver.gridRow },
        ])
      } catch (error) {
        // Rollback on error
        moveWidgetToPosition(activeWidget.id, oldActivePos.gridRow, oldActivePos.gridColumn)
        moveWidgetToPosition(overWidget.id, oldOverPos.gridRow, oldOverPos.gridColumn)
        console.error('Failed to update positions:', error)
      }
    }
  }, [widgets, reorderWidgets, updateWidgetPositions, moveWidgetToPosition])

  const handleRemove = useCallback(async (widgetId: string) => {
    try {
      await removeWidget(widgetId)
    } catch (error) {
      console.error('Failed to remove widget:', error)
    }
  }, [removeWidget])

  // Sort widgets by row and column for consistent rendering
  const sortedWidgets = [...widgets].sort((a, b) => {
    if (a.gridRow !== b.gridRow) return a.gridRow - b.gridRow
    return a.gridColumn - b.gridColumn
  })

  // Calculate empty cells for placeholders (only in edit mode, disabled on mobile)
  const emptyCells = useMemo(
    () => (isEditMode && !isMobile ? getEmptyCells(widgets, maxColumns) : []),
    [widgets, isEditMode, isMobile, maxColumns]
  )

  // In edit mode, we use interleaved grid rows:
  // - Odd rows (1, 3, 5...) are separator rows (small height)
  // - Even rows (2, 4, 6...) are widget rows (normal height)
  // Widget logical row N maps to grid row (2*N)
  // Separator before logical row N maps to grid row (2*N - 1)

  // Calculate the maximum logical row for determining grid template
  const maxLogicalRow = useMemo(() => {
    if (widgets.length === 0) return 1
    return Math.max(...widgets.map((w) => w.gridRow + w.rowSpan - 1))
  }, [widgets])

  // Calculate separator data (only in edit mode, disabled on mobile)
  const separators = useMemo(() => {
    if (!isEditMode || isMobile || widgets.length === 0) return []

    // Get all unique logical rows that have widgets (accounting for rowSpan)
    const occupiedRows = new Set<number>()
    for (const w of widgets) {
      for (let r = w.gridRow; r < w.gridRow + w.rowSpan; r++) {
        occupiedRows.add(r)
      }
    }
    const rows = Array.from(occupiedRows).sort((a, b) => a - b)

    // Return separator data: logical row it inserts before, and actual grid row
    return rows.map((logicalRow) => ({
      beforeLogicalRow: logicalRow,
      gridRow: 2 * logicalRow - 1, // Separator grid row (odd numbers)
    }))
  }, [widgets, isEditMode])

  // Transform logical row to actual grid row (for edit mode)
  const getActualGridRow = useCallback(
    (logicalRow: number) => (isEditMode ? 2 * logicalRow : logicalRow),
    [isEditMode]
  )

  // Handle insert row
  const handleInsertRow = useCallback(
    async (beforeRow: number) => {
      try {
        await insertRowAt(beforeRow)
      } catch (error) {
        console.error('Failed to insert row:', error)
      }
    },
    [insertRowAt]
  )

  // Handle click on empty cell placeholder
  const handleEmptyCellClick = useCallback(
    (gridRow: number, gridColumn: number) => {
      setTargetPosition({ gridRow, gridColumn })
      setAddPanelOpen(true)
    },
    [setTargetPosition, setAddPanelOpen]
  )

  // Build the grid-template-rows for edit mode (disabled on mobile)
  // In edit mode: alternating 32px separator rows and auto widget rows
  // In view mode: just auto rows
  const gridTemplateRows = useMemo(() => {
    if (!isEditMode || isMobile) return undefined // Use auto-rows-[minmax(90px,auto)]
    // Generate: 32px auto 32px auto ... for each logical row
    const rows: string[] = []
    for (let i = 1; i <= maxLogicalRow; i++) {
      rows.push('32px') // Separator row
      rows.push('minmax(90px, auto)') // Widget row
    }
    return rows.join(' ')
  }, [isEditMode, isMobile, maxLogicalRow])

  return (
    <DndContext
      sensors={sensors}
      onDragEnd={handleDragEnd}
    >
      <SortableContext
        items={sortedWidgets.map((w) => w.id)}
        strategy={rectSortingStrategy}
        disabled={!isEditMode}
      >
        <div
          className="grid grid-cols-2 md:grid-cols-4 gap-4"
          style={{
            gridTemplateRows: gridTemplateRows || 'repeat(auto-fill, minmax(90px, auto))',
            gridAutoRows: (isEditMode && !isMobile) ? undefined : 'minmax(90px, auto)',
          }}
        >
          {/* Row separators for inserting new rows (edit mode only, hidden on mobile) */}
          {isEditMode && !isMobile &&
            separators.map((sep) => (
              <RowSeparator
                key={`separator-before-${sep.beforeLogicalRow}`}
                gridRow={sep.gridRow}
                onClick={() => handleInsertRow(sep.beforeLogicalRow)}
              />
            ))}
          {sortedWidgets.map((widget) => (
            <WidgetWrapper
              key={widget.id}
              widget={widget}
              isEditMode={isEditMode && !isMobile}
              onRemove={handleRemove}
              gridRowOverride={(isEditMode && !isMobile) ? getActualGridRow(widget.gridRow) : undefined}
              gridRowSpanOverride={(isEditMode && !isMobile) ? widget.rowSpan * 2 - 1 : undefined}
              maxColumns={maxColumns}
            >
              <WidgetRenderer
                widget={widget}
                stats={stats}
                recentTraces={recentTraces}
                timeSelection={timeSelection}
                fromTime={fromTime}
                toTime={toTime}
              />
            </WidgetWrapper>
          ))}
          {/* Render visible placeholders for contiguous empty spaces */}
          {isEditMode && !isMobile &&
            emptyCells.map((cell) => (
              <EmptyCellPlaceholder
                key={`empty-${cell.gridRow}-${cell.gridColumn}-${cell.colSpan || 1}`}
                gridRow={getActualGridRow(cell.gridRow)}
                gridColumn={cell.gridColumn}
                colSpan={cell.colSpan}
                logicalRow={cell.gridRow}
                onClick={() => handleEmptyCellClick(cell.gridRow, cell.gridColumn)}
                maxColumns={maxColumns}
              />
            ))}
        </div>
      </SortableContext>
    </DndContext>
  )
}
