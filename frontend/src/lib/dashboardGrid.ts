// Layout of dashboard widgets on the grid: rows of GRID_COLUMNS columns,
// positions starting at 1. Pure functions used by the dashboard store, the
// grid and the add-widget panel.
import type { DashboardWidget, EmptyCell } from '@/types/dashboard'

export const GRID_COLUMNS = 4

export interface GridPosition {
  gridRow: number
  gridColumn: number
}

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

// The empty runs of each row, up to one row below the last widget: start
// column and length
function emptyRuns(widgets: DashboardWidget[], maxColumns: number): { gridRow: number; gridColumn: number; length: number }[] {
  const occupied = getOccupiedCells(widgets)
  const maxRow = Math.max(...widgets.map((w) => w.gridRow + w.rowSpan - 1), 1)
  const runs: { gridRow: number; gridColumn: number; length: number }[] = []
  for (let row = 1; row <= maxRow + 1; row++) {
    let start: number | null = null
    for (let col = 1; col <= maxColumns + 1; col++) {
      const free = col <= maxColumns && !occupied.has(`${row}-${col}`)
      if (free && start === null) {
        start = col
      } else if (!free && start !== null) {
        runs.push({ gridRow: row, gridColumn: start, length: col - start })
        start = null
      }
    }
  }
  return runs
}

// Visible empty cell placeholders: one per contiguous empty space
export function getEmptyCells(widgets: DashboardWidget[], maxColumns: number = GRID_COLUMNS): EmptyCell[] {
  if (widgets.length === 0) {
    // A single full-width placeholder for an empty dashboard
    return [{ gridRow: 1, gridColumn: 1, colSpan: maxColumns }]
  }
  return emptyRuns(widgets, maxColumns).map((run) => ({
    gridRow: run.gridRow,
    gridColumn: run.gridColumn,
    colSpan: run.length,
  }))
}

// The number of free columns from position to the right, up to the next
// widget or the end of the row
export function availableColumns(widgets: DashboardWidget[], position: GridPosition): number {
  const occupiedInRow = new Set<number>()
  for (const w of widgets) {
    if (position.gridRow >= w.gridRow && position.gridRow <= w.gridRow + w.rowSpan - 1) {
      for (let c = w.gridColumn; c < w.gridColumn + w.colSpan; c++) {
        occupiedInRow.add(c)
      }
    }
  }
  let available = 0
  for (let col = position.gridColumn; col <= GRID_COLUMNS && !occupiedInRow.has(col); col++) {
    available++
  }
  return available
}

// Where a new widget goes without a chosen position: the first free column
// of the last row, or the start of a new row
export function nextFreePosition(widgets: DashboardWidget[]): GridPosition {
  if (widgets.length === 0) {
    return { gridColumn: 1, gridRow: 1 }
  }
  const lastRow = Math.max(...widgets.map((w) => w.gridRow))
  const occupied = new Set<number>()
  for (const w of widgets.filter((w) => w.gridRow === lastRow)) {
    for (let i = 0; i < w.colSpan; i++) {
      occupied.add(w.gridColumn + i)
    }
  }
  for (let col = 1; col <= GRID_COLUMNS; col++) {
    if (!occupied.has(col)) {
      return { gridColumn: col, gridRow: lastRow }
    }
  }
  return { gridColumn: 1, gridRow: lastRow + 1 }
}

// Why a widget of colSpan columns does not fit at position, or null if it does
export function widgetFitError(widgets: DashboardWidget[], position: GridPosition, colSpan: number): string | null {
  const available = availableColumns(widgets, position)
  if (colSpan <= available) {
    return null
  }
  return `This widget requires ${colSpan} column${colSpan > 1 ? 's' : ''}, but only ${available} column${available !== 1 ? 's are' : ' is'} available at this position.`
}

// The widgets after dropping activeId onto overId. In the same row the two
// trade places in drag direction (the displaced widget wraps to the next row
// when it no longer fits); across rows they swap positions.
export function reorderWidgets(widgets: DashboardWidget[], activeId: string, overId: string): DashboardWidget[] {
  const active = widgets.find((w) => w.id === activeId)
  const over = widgets.find((w) => w.id === overId)
  if (!active || !over) return widgets

  let activePos: GridPosition
  let overPos: GridPosition
  if (active.gridRow !== over.gridRow) {
    activePos = { gridColumn: over.gridColumn, gridRow: over.gridRow }
    overPos = { gridColumn: active.gridColumn, gridRow: active.gridRow }
  } else if (active.gridColumn > over.gridColumn) {
    // Moving left: active takes over's place, over moves right of it
    activePos = { gridColumn: over.gridColumn, gridRow: over.gridRow }
    overPos = { gridColumn: activePos.gridColumn + active.colSpan, gridRow: activePos.gridRow }
    if (overPos.gridColumn + over.colSpan - 1 > GRID_COLUMNS) {
      overPos = { gridColumn: 1, gridRow: activePos.gridRow + 1 }
    }
  } else {
    // Moving right: over takes active's place, active moves right of it
    overPos = { gridColumn: active.gridColumn, gridRow: active.gridRow }
    activePos = { gridColumn: overPos.gridColumn + over.colSpan, gridRow: over.gridRow }
  }

  return widgets.map((w) => {
    if (w.id === activeId) return { ...w, ...activePos }
    if (w.id === overId) return { ...w, ...overPos }
    return w
  })
}
