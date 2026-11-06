import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import { DashboardGrid } from '../DashboardGrid'
import { useDashboardStore } from '@/stores/dashboardStore'
import { api } from '@/lib/api'
import type { Dashboard, DashboardWidget } from '@/types/dashboard'
import type { StatsResponse } from '@/lib/api'

const widget = (id: string, widgetType: string, gridColumn: number, gridRow: number): DashboardWidget => ({
  id, dashboardId: 'd1', widgetType, title: id, gridColumn, gridRow,
  colSpan: 1, rowSpan: 1, config: {}, createdAt: '', updatedAt: '',
})

const stats: StatsResponse = {
  traceCount: 7, spanCount: 21, logCount: 40, metricCount: 300, serviceCount: 1, services: ['claude-code'], errorRate: 2.5,
}

function renderGrid(isEditMode: boolean) {
  useDashboardStore.setState({
    dashboard: { id: 'd1', name: 'Main' } as Dashboard,
    isEditMode,
    widgets: [
      widget('Traces', 'stats_traces', 1, 1),
      widget('Logs', 'stats_logs', 2, 1),
      widget('Errors', 'stats_error_rate', 1, 2),
    ],
  })
  return render(
    <MemoryRouter>
      <DashboardGrid stats={stats} recentTraces={[]} />
    </MemoryRouter>
  )
}

describe('DashboardGrid', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
    useDashboardStore.setState({ isAddPanelOpen: false, targetPosition: null })
  })

  it('renders the widgets with their data and no edit controls', () => {
    renderGrid(false)

    expect(screen.getByText('Traces')).toBeInTheDocument()
    expect(screen.getByText('7')).toBeInTheDocument()
    expect(screen.getByText('40')).toBeInTheDocument()
    expect(screen.getByText('2.50%')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /^Remove/ })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Add row' })).not.toBeInTheDocument()
    expect(screen.queryByText('+ Add widget')).not.toBeInTheDocument()
  })

  it('shows remove, move and add-row controls and empty cells in edit mode', () => {
    renderGrid(true)

    expect(screen.getAllByRole('button', { name: /^Remove/ })).toHaveLength(3)
    expect(screen.getAllByRole('button', { name: /^Move/ })).toHaveLength(3)
    // One "add row" control before each occupied row
    expect(screen.getAllByRole('button', { name: 'Add row' })).toHaveLength(2)
    expect(screen.getAllByText('+ Add widget').length).toBeGreaterThan(0)
  })

  it('removes a widget', async () => {
    const user = userEvent.setup()
    const deleteWidget = vi.spyOn(api, 'deleteWidget').mockResolvedValue()
    renderGrid(true)

    await user.click(screen.getByRole('button', { name: 'Remove Logs' }))

    expect(deleteWidget).toHaveBeenCalledWith('d1', 'Logs')
    expect(screen.queryByText('40')).not.toBeInTheDocument()
  })

  it('inserts a row by moving the widgets below it down', async () => {
    const user = userEvent.setup()
    const update = vi.spyOn(api, 'updateWidgetPositions').mockResolvedValue()
    renderGrid(true)

    // The second separator inserts before row 2
    await user.click(screen.getAllByRole('button', { name: 'Add row' })[1])

    expect(update).toHaveBeenCalledWith('d1', [{ id: 'Errors', gridRow: 3, gridColumn: 1 }])
  })

  it('opens the widget panel for an empty cell', async () => {
    const user = userEvent.setup()
    renderGrid(true)

    await user.click(screen.getAllByText('+ Add widget')[0])

    const state = useDashboardStore.getState()
    expect(state.isAddPanelOpen).toBe(true)
    expect(state.targetPosition).not.toBeNull()
  })
})
