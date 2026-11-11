import { describe, it, expect, vi, beforeEach } from 'vitest'
import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { DashboardGrid } from '../DashboardGrid'
import { MetricDataProvider } from '@/contexts/MetricDataContext'
import { useDashboardStore } from '@/stores/dashboardStore'
import { api, type StatsResponse } from '@/lib/api'
import { httpError } from '@/lib/errors'
import { queryKeys, useDashboard } from '@/lib/queries'
import { createTestQueryClient, renderWithProviders } from '@/test/queryWrapper'
import { TIMEFRAME_OPTIONS, type DashboardWidget, type DashboardWithWidgets, type TimeSelection } from '@/types/dashboard'
import type { TracesResponse } from '@/types/traces'
import type { WidgetDataState } from '../widgets/types'

vi.mock('@/lib/api', () => ({
  api: {
    deleteWidget: vi.fn(),
    updateWidgetPositions: vi.fn(),
    getBatchMetricSeries: vi.fn(),
    getDashboard: vi.fn(),
    getDefaultDashboard: vi.fn(),
  },
}))

vi.mock('sonner', () => ({ toast: { error: vi.fn(), success: vi.fn() } }))

const widget = (id: string, widgetType: string, gridColumn: number, gridRow: number): DashboardWidget => ({
  id, dashboardId: 'd1', widgetType, title: id, gridColumn, gridRow,
  colSpan: 1, rowSpan: 1, config: {}, createdAt: '', updatedAt: '',
})

const statsData: StatsResponse = {
  traceCount: 7000, spanCount: 21000, logCount: 40, metricCount: 300, serviceCount: 1, services: ['claude-code'], errorRate: 2.5,
}

function queryState<T>(data: T | undefined, overrides: Partial<WidgetDataState<T>> = {}): WidgetDataState<T> {
  return { data, isPending: data === undefined, isError: false, error: null, isFetching: false, refetch: vi.fn(), ...overrides }
}

const timeSelection: TimeSelection = { type: 'relative', timeframe: TIMEFRAME_OPTIONS[4] }
const WIDGETS = [widget('Traces', 'stats_traces', 1, 1), widget('Logs', 'stats_logs', 2, 1), widget('Errors', 'stats_error_rate', 1, 2)]

function renderGrid(
  isEditMode: boolean,
  { stats = queryState(statsData), recent = queryState<TracesResponse>({ traces: [], total: 0, hasMore: false }) } = {}
) {
  const queryClient = createTestQueryClient()
  const dashboard: DashboardWithWidgets = { id: 'd1', name: 'Main', isDefault: true, createdAt: '', updatedAt: '', widgets: WIDGETS }
  queryClient.setQueryData(queryKeys.dashboards.detail('d1'), dashboard)
  useDashboardStore.setState({ isEditMode })

  // The grid's widgets come from the cached dashboard, as in DashboardBuilder
  function Grid() {
    const widgets = useDashboard('d1', { enabled: false }).data?.widgets ?? []
    return (
      <MetricDataProvider widgets={widgets} timeSelection={timeSelection}>
        <DashboardGrid dashboardId="d1" widgets={widgets} stats={stats} recentTraces={recent} timeSelection={timeSelection} />
      </MetricDataProvider>
    )
  }
  return renderWithProviders(<Grid />, { queryClient })
}

describe('DashboardGrid', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    useDashboardStore.getState().reset()
  })

  it('renders the widgets with their range-scoped counts and no edit controls', () => {
    renderGrid(false)

    expect(screen.getByText('Traces')).toBeInTheDocument()
    expect(screen.getByText('7,000')).toBeInTheDocument()
    expect(screen.getByText('21,000 spans in selected range')).toBeInTheDocument()
    expect(screen.getByText('40')).toBeInTheDocument()
    expect(screen.getByText('log records in selected range')).toBeInTheDocument()
    expect(screen.getByText('2.50%')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /^Remove/ })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /^Edit/ })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Add row' })).not.toBeInTheDocument()
    expect(screen.queryByText('+ Add widget')).not.toBeInTheDocument()
  })

  it('shows loading and error states instead of empty values', async () => {
    const user = userEvent.setup()
    const refetch = vi.fn()
    const { unmount } = renderGrid(false, { stats: queryState<StatsResponse>(undefined) })
    expect(screen.getAllByRole('status', { name: 'Loading' }).length).toBeGreaterThan(0)
    expect(screen.queryByText('—')).not.toBeInTheDocument()
    unmount()

    renderGrid(false, { stats: queryState<StatsResponse>(undefined, { isPending: false, isError: true, error: httpError(401), refetch }) })
    expect(screen.getAllByText(/Authentication required/)).toHaveLength(3)
    await user.click(screen.getAllByRole('button', { name: 'Retry' })[0])
    expect(refetch).toHaveBeenCalled()
  })

  it('shows edit, remove, move and add-row controls and empty cells in edit mode', () => {
    renderGrid(true)

    expect(screen.getAllByRole('button', { name: /^Remove/ })).toHaveLength(3)
    expect(screen.getAllByRole('button', { name: /^Edit/ })).toHaveLength(3)
    expect(screen.getAllByRole('button', { name: /^Move/ })).toHaveLength(3)
    // One "add row" control before each occupied row
    expect(screen.getAllByRole('button', { name: 'Add row' })).toHaveLength(2)
    expect(screen.getAllByText('+ Add widget').length).toBeGreaterThan(0)
  })

  it('removes a widget at once', async () => {
    const user = userEvent.setup()
    vi.mocked(api.deleteWidget).mockResolvedValue()
    renderGrid(true)

    await user.click(screen.getByRole('button', { name: 'Remove Logs' }))

    expect(api.deleteWidget).toHaveBeenCalledWith('d1', 'Logs')
    await waitFor(() => expect(screen.queryByText('40')).not.toBeInTheDocument())
  })

  it('puts a widget back when removing it fails', async () => {
    const user = userEvent.setup()
    const { toast } = await import('sonner')
    vi.mocked(api.deleteWidget).mockRejectedValue(httpError(500, 'database is locked'))
    renderGrid(true)

    await user.click(screen.getByRole('button', { name: 'Remove Logs' }))

    await waitFor(() =>
      expect(toast.error).toHaveBeenCalledWith("Couldn't remove the widget", { description: 'database is locked' })
    )
    expect(screen.getByText('40')).toBeInTheDocument()
  })

  it('inserts a row by moving the widgets below it down', async () => {
    const user = userEvent.setup()
    vi.mocked(api.updateWidgetPositions).mockResolvedValue()
    renderGrid(true)

    // The second separator inserts before row 2
    await user.click(screen.getAllByRole('button', { name: 'Add row' })[1])

    expect(api.updateWidgetPositions).toHaveBeenCalledWith('d1', [{ id: 'Errors', gridRow: 3, gridColumn: 1 }])
  })

  it('opens the widget panel for an empty cell, and the edit panel for a widget', async () => {
    const user = userEvent.setup()
    renderGrid(true)

    await user.click(screen.getAllByText('+ Add widget')[0])
    expect(useDashboardStore.getState()).toMatchObject({ isAddPanelOpen: true, editingWidgetId: null })
    expect(useDashboardStore.getState().targetPosition).not.toBeNull()

    await user.click(screen.getByRole('button', { name: 'Edit Logs' }))
    expect(useDashboardStore.getState()).toMatchObject({ isAddPanelOpen: true, editingWidgetId: 'Logs', targetPosition: null })
  })
})
