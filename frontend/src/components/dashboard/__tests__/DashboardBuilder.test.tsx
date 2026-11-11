import { describe, it, expect, vi, beforeEach } from 'vitest'
import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { Link, Route, Routes } from 'react-router-dom'
import { Dashboard } from '@/pages/Dashboard'
import { useDashboardStore } from '@/stores/dashboardStore'
import { api, type StatsResponse } from '@/lib/api'
import { httpError } from '@/lib/errors'
import { renderWithProviders } from '@/test/queryWrapper'
import type { DashboardWidget, DashboardWithWidgets } from '@/types/dashboard'

vi.mock('@/lib/api', () => ({
  api: {
    getDefaultDashboard: vi.fn(),
    getDashboard: vi.fn(),
    getDashboards: vi.fn(),
    createDashboard: vi.fn(),
    createWidget: vi.fn(),
    updateWidgetPositions: vi.fn(),
    updateDashboard: vi.fn(),
    getStats: vi.fn(),
    getRecentTraces: vi.fn(),
    getBatchMetricSeries: vi.fn(),
    getServices: vi.fn(),
    getMetricNames: vi.fn(),
  },
}))

vi.mock('sonner', () => ({ toast: { error: vi.fn(), success: vi.fn() } }))

const widget = (id: string, widgetType: string, gridColumn: number, gridRow: number, colSpan = 1): DashboardWidget => ({
  id, dashboardId: 'd1', widgetType, title: id, gridColumn, gridRow, colSpan, rowSpan: 1, config: {}, createdAt: '', updatedAt: '',
})

const dashboard = (id: string, name: string, widgets: DashboardWidget[]): DashboardWithWidgets => ({
  id, name, description: `${name} description`, isDefault: id === 'd1', createdAt: '', updatedAt: '', widgets,
})

const stats: StatsResponse = {
  traceCount: 1234, spanCount: 5678, logCount: 9, metricCount: 10, serviceCount: 1, services: ['claude-code'], errorRate: 0,
}

function renderDashboard(route = '/') {
  return renderWithProviders(
    <>
      <Routes>
        <Route path="/" element={<Dashboard />} />
        <Route path="/dashboard/:id" element={<Dashboard />} />
      </Routes>
      <Link to="/dashboard/d2">Other dashboard</Link>
    </>,
    { route }
  )
}

describe('DashboardBuilder', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    localStorage.clear()
    useDashboardStore.getState().reset()
    vi.mocked(api.getDefaultDashboard).mockResolvedValue(
      dashboard('d1', 'Default Dashboard', [widget('Total Traces', 'stats_traces', 1, 1), widget('Recent Traces', 'recent_activity', 1, 2, 4)])
    )
    vi.mocked(api.getDashboard).mockImplementation(async (id) => {
      if (id === 'd2') return dashboard('d2', 'Second', [widget('Logs', 'stats_logs', 1, 1)])
      throw httpError(404, 'dashboard not found')
    })
    vi.mocked(api.getStats).mockResolvedValue(stats)
    vi.mocked(api.getRecentTraces).mockResolvedValue({ traces: [], total: 0, hasMore: false })
  })

  it('shows the default dashboard the server provides, without creating one', async () => {
    renderDashboard()

    expect(screen.getByRole('status', { name: 'Loading dashboard' })).toBeInTheDocument()
    expect(await screen.findByRole('heading', { level: 1, name: 'Default Dashboard' })).toBeInTheDocument()
    expect(document.title).toBe('Default Dashboard · AI Observer')
    expect(await screen.findByText('1,234')).toBeInTheDocument()
    expect(api.createDashboard).not.toHaveBeenCalled()
    expect(api.createWidget).not.toHaveBeenCalled()
  })

  it('never creates a default dashboard when the server has none', async () => {
    vi.mocked(api.getDefaultDashboard).mockResolvedValue(null)
    renderDashboard()

    expect(await screen.findByText('No default dashboard')).toBeInTheDocument()
    expect(api.createDashboard).not.toHaveBeenCalled()
    expect(api.createWidget).not.toHaveBeenCalled()
  })

  it('shows a not-found state with a link home for an unknown dashboard', async () => {
    renderDashboard('/dashboard/missing')

    expect(await screen.findByRole('heading', { name: 'Dashboard not found' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Go to the default dashboard' })).toHaveAttribute('href', '/')
  })

  it('shows why loading failed, and retries', async () => {
    const user = userEvent.setup()
    vi.mocked(api.getDefaultDashboard).mockRejectedValueOnce(httpError(401))
    renderDashboard()

    expect(await screen.findByText(/Authentication required/)).toBeInTheDocument()
    expect(screen.queryByText(/^Error:/)).not.toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Retry' }))
    expect(await screen.findByRole('heading', { level: 1, name: 'Default Dashboard' })).toBeInTheDocument()
  })

  it('takes the time range from the URL and queries it at fetch time', async () => {
    renderDashboard('/?timeframe=24h')

    expect(await screen.findByRole('button', { name: 'Timeframe: Last 24 hours' })).toBeInTheDocument()
    await waitFor(() => expect(api.getStats).toHaveBeenCalled())
    const [range] = vi.mocked(api.getStats).mock.calls[0]
    expect(new Date(range!.to!).getTime() - new Date(range!.from!).getTime()).toBe(86_400_000)
    expect(Math.abs(Date.now() - new Date(range!.to!).getTime())).toBeLessThan(5_000)
  })

  it('leaves edit mode when another dashboard is opened', async () => {
    const user = userEvent.setup()
    renderDashboard()
    await screen.findByRole('heading', { level: 1, name: 'Default Dashboard' })

    await user.click(screen.getByRole('button', { name: /Edit/ }))
    expect(useDashboardStore.getState().isEditMode).toBe(true)

    await user.click(screen.getByRole('link', { name: 'Other dashboard' }))
    expect(await screen.findByRole('heading', { level: 1, name: 'Second' })).toBeInTheDocument()
    expect(useDashboardStore.getState().isEditMode).toBe(false)
  })

  it('closes empty rows when editing ends', async () => {
    const user = userEvent.setup()
    vi.mocked(api.getDefaultDashboard).mockResolvedValue(
      dashboard('d1', 'Default Dashboard', [widget('a', 'stats_traces', 1, 1), widget('b', 'stats_logs', 1, 3)])
    )
    vi.mocked(api.updateWidgetPositions).mockResolvedValue()
    renderDashboard()
    await screen.findByRole('heading', { level: 1, name: 'Default Dashboard' })

    await user.click(screen.getByRole('button', { name: /Edit/ }))
    await user.click(screen.getByRole('button', { name: /Done/ }))

    expect(api.updateWidgetPositions).toHaveBeenCalledWith('d1', [{ id: 'b', gridRow: 2, gridColumn: 1 }])
    expect(useDashboardStore.getState().isEditMode).toBe(false)
  })

  it('stays in edit mode when closing the empty rows fails', async () => {
    const user = userEvent.setup()
    vi.mocked(api.getDefaultDashboard).mockResolvedValue(
      dashboard('d1', 'Default Dashboard', [widget('a', 'stats_traces', 1, 1), widget('b', 'stats_logs', 1, 3)])
    )
    vi.mocked(api.updateWidgetPositions).mockRejectedValue(httpError(500))
    renderDashboard()
    await screen.findByRole('heading', { level: 1, name: 'Default Dashboard' })

    await user.click(screen.getByRole('button', { name: /Edit/ }))
    await user.click(screen.getByRole('button', { name: /Done/ }))

    await waitFor(() => expect(useDashboardStore.getState().isEditMode).toBe(true))
  })
})
