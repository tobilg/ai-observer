import { useCallback, useMemo } from 'react'
import {
  DndContext,
  MouseSensor,
  TouchSensor,
  KeyboardSensor,
  useSensor,
  useSensors,
} from '@dnd-kit/core'
import type { DragEndEvent } from '@dnd-kit/core'
import { SortableContext, rectSortingStrategy, sortableKeyboardCoordinates } from '@dnd-kit/sortable'
import { toast } from 'sonner'
import { useIsMobile } from '@/hooks/use-mobile'
import { useDashboardStore } from '@/stores/dashboardStore'
import { useRemoveWidget, useUpdateWidgetPositions } from '@/hooks/useDashboardMutations'
import { dropOnWidgetPositions, getEmptyCells, insertRowPositions, wouldOverlap } from '@/lib/dashboardGrid'
import { errorText } from '@/lib/errors'
import { useShallow } from 'zustand/react/shallow'
import { WidgetWrapper } from './widgets/WidgetWrapper'
import { WidgetRenderer } from './widgets/WidgetRenderer'
import { EmptyCellPlaceholder } from './widgets/EmptyCellPlaceholder'
import { RowSeparator } from './widgets/RowSeparator'
import type { WidgetDataState } from './widgets/types'
import type { StatsResponse } from '@/lib/api'
import type { TracesResponse } from '@/types/traces'
import type { DashboardWidget, TimeSelection, WidgetPosition } from '@/types/dashboard'

interface DashboardGridProps {
  dashboardId: string
  widgets: DashboardWidget[]
  stats: WidgetDataState<StatsResponse>
  recentTraces: WidgetDataState<TracesResponse>
  timeSelection: TimeSelection
}

export function DashboardGrid({ dashboardId, widgets, stats, recentTraces, timeSelection }: DashboardGridProps) {
  const isMobile = useIsMobile()
  const maxColumns = isMobile ? 2 : 4

  const { isEditMode, openAddPanel, openEditPanel } = useDashboardStore(
    useShallow((s) => ({
      isEditMode: s.isEditMode,
      openAddPanel: s.openAddPanel,
      openEditPanel: s.openEditPanel,
    }))
  )
  const removeWidget = useRemoveWidget(dashboardId)
  const updatePositions = useUpdateWidgetPositions(dashboardId)

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
    }),
    useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates })
  )

  // Moves are shown at once and rolled back when the server refuses them
  const move = useCallback(
    (positions: WidgetPosition[], failure: string) => {
      if (positions.length === 0) return
      updatePositions.mutate(positions, {
        onError: (error) => toast.error(failure, { description: errorText(error) }),
      })
    },
    [updatePositions]
  )

  const handleDragEnd = useCallback((event: DragEndEvent) => {
    const { active, over } = event
    if (!over) return

    const widget = widgets.find((w) => w.id === active.id)
    if (!widget) return

    const overData = over.data.current

    // Dropped onto an empty cell: move there if the widget fits (its own
    // cells become free, so it does not count against itself)
    if (overData?.type === 'empty') {
      const target = { gridRow: overData.gridRow as number, gridColumn: overData.gridColumn as number }
      if (wouldOverlap(widget, target, widgets, [widget.id])) {
        return
      }
      move([{ id: widget.id, ...target }], "Couldn't move the widget")
      return
    }

    // Dropped onto another widget: the two trade places, if they fit
    const positions = dropOnWidgetPositions(widgets, String(active.id), String(over.id))
    if (positions) {
      move(positions, "Couldn't move the widgets")
    }
  }, [widgets, move])

  const handleRemove = useCallback((widgetId: string) => {
    removeWidget.mutate(widgetId, {
      onError: (error) => toast.error("Couldn't remove the widget", { description: errorText(error) }),
    })
  }, [removeWidget])

  // Handle insert row
  const handleInsertRow = useCallback(
    (beforeRow: number) => move(insertRowPositions(widgets, beforeRow), "Couldn't insert the row"),
    [widgets, move]
  )

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
  }, [widgets, isEditMode, isMobile])

  // Transform logical row to actual grid row (for edit mode)
  const getActualGridRow = useCallback(
    (logicalRow: number) => (isEditMode ? 2 * logicalRow : logicalRow),
    [isEditMode]
  )

  // Handle click on empty cell placeholder
  const handleEmptyCellClick = useCallback(
    (gridRow: number, gridColumn: number) => openAddPanel({ gridRow, gridColumn }),
    [openAddPanel]
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
              onEdit={openEditPanel}
              gridRowOverride={(isEditMode && !isMobile) ? getActualGridRow(widget.gridRow) : undefined}
              gridRowSpanOverride={(isEditMode && !isMobile) ? widget.rowSpan * 2 - 1 : undefined}
              maxColumns={maxColumns}
            >
              <WidgetRenderer
                widget={widget}
                stats={stats}
                recentTraces={recentTraces}
                timeSelection={timeSelection}
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
