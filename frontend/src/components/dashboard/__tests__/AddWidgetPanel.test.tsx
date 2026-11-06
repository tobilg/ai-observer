import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { AddWidgetPanel } from '../AddWidgetPanel'
import { useDashboardStore } from '@/stores/dashboardStore'
import { api } from '@/lib/api'
import { toast } from 'sonner'
import type { DashboardWidget } from '@/types/dashboard'

vi.mock('@/lib/api', () => ({
  api: {
    getServices: vi.fn(),
    getMetricNames: vi.fn(),
    getBreakdownValues: vi.fn(),
  },
}))

vi.mock('sonner', () => ({
  toast: { error: vi.fn(), success: vi.fn() },
}))

function widget(gridColumn: number, gridRow: number, colSpan = 1): DashboardWidget {
  return {
    id: `w-${gridColumn}-${gridRow}`,
    dashboardId: 'd1',
    widgetType: 'stats_traces',
    title: 'Widget',
    gridColumn,
    gridRow,
    colSpan,
    rowSpan: 1,
    config: {},
    createdAt: '',
    updatedAt: '',
  }
}

const addWidget = vi.fn()

function openPanel(widgets: DashboardWidget[] = [], targetPosition: { gridRow: number; gridColumn: number } | null = null) {
  useDashboardStore.setState({ isAddPanelOpen: true, widgets, targetPosition, addWidget })
  return render(<AddWidgetPanel />)
}

describe('AddWidgetPanel', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    addWidget.mockResolvedValue(undefined)
    vi.mocked(api.getServices).mockResolvedValue({ services: ['claude-code', 'gemini_cli'] })
    vi.mocked(api.getMetricNames).mockResolvedValue({ names: ['claude_code.token.usage'] })
    vi.mocked(api.getBreakdownValues).mockResolvedValue({ values: ['input', 'output'] })
  })

  it('adds a built-in widget at the next free position and closes', async () => {
    const user = userEvent.setup()
    openPanel([widget(1, 1, 2)])

    await user.click(screen.getByRole('tab', { name: 'Built-in' }))
    const card = screen.getByText('Total Traces').closest('[class*="card"]') as HTMLElement
    await user.click(within(card).getByRole('button'))

    expect(addWidget).toHaveBeenCalledWith(expect.objectContaining({
      widgetType: 'stats_traces', title: 'Total Traces', gridColumn: 3, gridRow: 1, colSpan: 1, rowSpan: 1,
    }))
    expect(useDashboardStore.getState().isAddPanelOpen).toBe(false)
  })

  it('starts a new row when the last one is full', async () => {
    const user = userEvent.setup()
    openPanel([widget(1, 1, 4)])

    await user.click(screen.getByRole('tab', { name: 'Built-in' }))
    const card = screen.getByText('Total Traces').closest('[class*="card"]') as HTMLElement
    await user.click(within(card).getByRole('button'))

    expect(addWidget).toHaveBeenCalledWith(expect.objectContaining({ gridColumn: 1, gridRow: 2 }))
  })

  it('refuses a widget wider than the space at the chosen position', async () => {
    const user = userEvent.setup()
    openPanel([widget(3, 1)], { gridRow: 1, gridColumn: 2 })

    await user.click(screen.getByRole('tab', { name: 'Built-in' }))
    const card = screen.getByText('Active Services').closest('[class*="card"]') as HTMLElement
    await user.click(within(card).getByRole('button'))

    expect(toast.error).toHaveBeenCalledWith('Widget does not fit', {
      description: 'This widget requires 2 columns, but only 1 column is available at this position.',
    })
    expect(addWidget).not.toHaveBeenCalled()
  })

  it('adds a metric chart with the chosen chart mode at the chosen position', async () => {
    const user = userEvent.setup()
    openPanel([], { gridRow: 2, gridColumn: 1 })

    // The first stored metric is preselected
    await waitFor(() => expect(screen.getAllByText('Token Usage').length).toBeGreaterThan(1))
    const add = screen.getByRole('button', { name: /Add Widget/ })
    expect(add).toBeDisabled()

    await user.click(screen.getByText('Metric Chart'))
    await user.click(screen.getByRole('button', { name: /Grouped/ }))
    await user.click(add)

    expect(addWidget).toHaveBeenCalledWith(expect.objectContaining({
      widgetType: 'metric_chart',
      title: 'Token Usage',
      gridColumn: 1,
      gridRow: 2,
      colSpan: 2,
      config: expect.objectContaining({ metricName: 'claude_code.token.usage', chartStacked: false, service: undefined }),
    }))
    expect(useDashboardStore.getState().targetPosition).toBeNull()
  })

  it('adds a metric value filtered to one breakdown value', async () => {
    const user = userEvent.setup()
    openPanel()
    await waitFor(() => expect(screen.getAllByText('Token Usage').length).toBeGreaterThan(1))

    await user.click(screen.getByText('Metric Value'))
    await user.selectOptions(screen.getByDisplayValue('Total (all values)'), 'type')
    await waitFor(() =>
      expect(api.getBreakdownValues).toHaveBeenCalledWith({ name: 'claude_code.token.usage', attribute: 'type', service: undefined })
    )
    // Known values show their display names
    const valueSelect = await screen.findByDisplayValue('Select a value')
    await waitFor(() => expect(within(valueSelect).getByText('Output')).toBeInTheDocument())
    await user.selectOptions(valueSelect, 'output')
    await user.click(screen.getByRole('button', { name: /Add Widget/ }))

    expect(addWidget).toHaveBeenCalledWith(expect.objectContaining({
      widgetType: 'metric_value',
      config: expect.objectContaining({ breakdownAttribute: 'type', breakdownValue: 'output', chartStacked: undefined }),
    }))
  })

  it('lists only the metrics of the selected service', async () => {
    const user = userEvent.setup()
    openPanel()
    await waitFor(() => expect(api.getMetricNames).toHaveBeenCalledWith(undefined))
    const metricSelect = screen.getByDisplayValue('Token Usage')
    expect(within(metricSelect).getByRole('group', { name: 'Claude Code' })).toBeInTheDocument()

    vi.mocked(api.getMetricNames).mockResolvedValue({ names: [] })
    await user.selectOptions(screen.getByDisplayValue('All Services'), 'gemini_cli')

    await waitFor(() => expect(api.getMetricNames).toHaveBeenLastCalledWith('gemini_cli'))
    expect(within(metricSelect).queryByRole('group', { name: 'Claude Code' })).not.toBeInTheDocument()
    expect(within(metricSelect).getByRole('group', { name: 'Gemini CLI' })).toBeInTheDocument()
  })

  it('stays open when adding fails', async () => {
    const user = userEvent.setup()
    vi.spyOn(console, 'error').mockImplementation(() => {})
    addWidget.mockRejectedValue(new Error('offline'))
    openPanel()

    await user.click(screen.getByRole('tab', { name: 'Built-in' }))
    const card = screen.getByText('Total Traces').closest('[class*="card"]') as HTMLElement
    await user.click(within(card).getByRole('button'))

    await waitFor(() => expect(toast.error).toHaveBeenCalledWith('Failed to add widget'))
    expect(useDashboardStore.getState().isAddPanelOpen).toBe(true)
  })
})
