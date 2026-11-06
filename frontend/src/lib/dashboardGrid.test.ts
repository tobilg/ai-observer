import { describe, it, expect } from 'vitest'
import { availableColumns, getEmptyCells, nextFreePosition, reorderWidgets, widgetFitError } from './dashboardGrid'
import type { DashboardWidget } from '@/types/dashboard'

function widget(id: string, gridColumn: number, gridRow: number, colSpan = 1, rowSpan = 1): DashboardWidget {
  return {
    id, dashboardId: 'd', widgetType: 'stats_traces', title: id,
    gridColumn, gridRow, colSpan, rowSpan, config: {}, createdAt: '', updatedAt: '',
  }
}

describe('dashboardGrid', () => {
  it('lists one placeholder per empty run, plus a free row below', () => {
    expect(getEmptyCells([])).toEqual([{ gridRow: 1, gridColumn: 1, colSpan: 4 }])
    expect(getEmptyCells([widget('a', 2, 1), widget('b', 1, 2, 2, 1)])).toEqual([
      { gridRow: 1, gridColumn: 1, colSpan: 1 },
      { gridRow: 1, gridColumn: 3, colSpan: 2 },
      { gridRow: 2, gridColumn: 3, colSpan: 2 },
      { gridRow: 3, gridColumn: 1, colSpan: 4 },
    ])
    // A tall widget occupies the rows it spans
    expect(getEmptyCells([widget('tall', 1, 1, 4, 2)])).toEqual([{ gridRow: 3, gridColumn: 1, colSpan: 4 }])
  })

  it('counts the free columns to the right of a position', () => {
    const widgets = [widget('a', 3, 1), widget('tall', 1, 1, 1, 2)]
    expect(availableColumns(widgets, { gridRow: 1, gridColumn: 2 })).toBe(1)
    expect(availableColumns(widgets, { gridRow: 2, gridColumn: 2 })).toBe(3)
    expect(availableColumns(widgets, { gridRow: 1, gridColumn: 1 })).toBe(0)
  })

  it('places new widgets after the last row\'s widgets', () => {
    expect(nextFreePosition([])).toEqual({ gridColumn: 1, gridRow: 1 })
    expect(nextFreePosition([widget('a', 1, 1, 2), widget('b', 1, 2, 1)])).toEqual({ gridColumn: 2, gridRow: 2 })
    expect(nextFreePosition([widget('wide', 1, 1, 4)])).toEqual({ gridColumn: 1, gridRow: 2 })
  })

  it('explains why a widget does not fit', () => {
    const widgets = [widget('a', 3, 1)]
    expect(widgetFitError(widgets, { gridRow: 1, gridColumn: 1 }, 2)).toBeNull()
    expect(widgetFitError(widgets, { gridRow: 1, gridColumn: 2 }, 2)).toBe(
      'This widget requires 2 columns, but only 1 column is available at this position.'
    )
    expect(widgetFitError(widgets, { gridRow: 1, gridColumn: 4 }, 4)).toBe(
      'This widget requires 4 columns, but only 1 column is available at this position.'
    )
  })

  it('reorders widgets in drag direction and swaps across rows', () => {
    const a = widget('a', 1, 1)
    const b = widget('b', 2, 1, 2)
    const c = widget('c', 1, 2)
    const pos = (ws: DashboardWidget[], id: string) => {
      const w = ws.find((x) => x.id === id)!
      return [w.gridColumn, w.gridRow]
    }

    // Dragging a right: b takes its place, a goes after b
    const right = reorderWidgets([a, b, c], 'a', 'b')
    expect(pos(right, 'b')).toEqual([1, 1])
    expect(pos(right, 'a')).toEqual([3, 1])

    // Dragging b left: b takes a's place, a goes after b
    const left = reorderWidgets([a, b, c], 'b', 'a')
    expect(pos(left, 'b')).toEqual([1, 1])
    expect(pos(left, 'a')).toEqual([3, 1])

    // Wide widgets move by the width of the other one
    const wideLeft = reorderWidgets([widget('wide', 1, 1, 3), widget('small', 4, 1)], 'small', 'wide')
    expect(pos(wideLeft, 'small')).toEqual([1, 1])
    expect(pos(wideLeft, 'wide')).toEqual([2, 1])

    const across = reorderWidgets([a, b, c], 'a', 'c')
    expect(pos(across, 'a')).toEqual([1, 2])
    expect(pos(across, 'c')).toEqual([1, 1])

    expect(reorderWidgets([a], 'a', 'missing')).toEqual([a])
  })
})
