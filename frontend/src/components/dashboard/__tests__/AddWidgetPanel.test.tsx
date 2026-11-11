import { describe, it, expect, vi, beforeEach } from 'vitest'
import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { AddWidgetPanel } from '../AddWidgetPanel'
import { useDashboardStore } from '@/stores/dashboardStore'
import { api } from '@/lib/api'
import { httpError } from '@/lib/errors'
import { queryKeys, useDashboard } from '@/lib/queries'
import { createTestQueryClient, renderWithProviders } from '@/test/queryWrapper'
import { toast } from 'sonner'
import type { DashboardWidget, DashboardWithWidgets, WidgetConfig } from '@/types/dashboard'

vi.mock('@/lib/api', () => ({
  api: {
    getServices: vi.fn(),
    getMetricNames: vi.fn(),
    getBreakdownValues: vi.fn(),
    getMetricAttributeKeys: vi.fn(),
    createWidget: vi.fn(),
    updateWidget: vi.fn(),
    getDashboard: vi.fn(),
    getDefaultDashboard: vi.fn(),
  },
}))

vi.mock('sonner', () => ({
  toast: { error: vi.fn(), success: vi.fn() },
}))

function widget(gridColumn: number, gridRow: number, colSpan = 1, extra: Partial<DashboardWidget> = {}): DashboardWidget {
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
    ...extra,
  }
}

// The panel with the cached dashboard's widgets, as DashboardBuilder renders it
function Panel() {
  const widgets = useDashboard('d1', { enabled: false }).data?.widgets ?? []
  return <AddWidgetPanel dashboardId="d1" widgets={widgets} />
}

function renderPanel(widgets: DashboardWidget[] = []) {
  const queryClient = createTestQueryClient()
  const dashboard: DashboardWithWidgets = { id: 'd1', name: 'Main', isDefault: true, createdAt: '', updatedAt: '', widgets }
  queryClient.setQueryData(queryKeys.dashboards.detail('d1'), dashboard)
  return renderWithProviders(<Panel />, { queryClient })
}

function openPanel(widgets: DashboardWidget[] = [], targetPosition: { gridRow: number; gridColumn: number } | null = null) {
  useDashboardStore.getState().openAddPanel(targetPosition)
  return renderPanel(widgets)
}

function openEditor(widgets: DashboardWidget[], widgetId: string) {
  useDashboardStore.getState().openEditPanel(widgetId)
  return renderPanel(widgets)
}

function cached(queryClient: ReturnType<typeof createTestQueryClient>, id: string) {
  return queryClient.getQueryData<DashboardWithWidgets>(queryKeys.dashboards.detail('d1'))?.widgets.find((w) => w.id === id)
}

async function addBuiltin(user: ReturnType<typeof userEvent.setup>, label: string) {
  await user.click(screen.getByRole('tab', { name: 'Built-in' }))
  await user.click(screen.getByRole('button', { name: `Add ${label}` }))
}

describe('AddWidgetPanel: adding', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    useDashboardStore.getState().reset()
    vi.mocked(api.getServices).mockResolvedValue({ services: ['claude-code', 'gemini_cli'] })
    vi.mocked(api.getMetricNames).mockResolvedValue({ names: ['claude_code.token.usage'] })
    vi.mocked(api.getBreakdownValues).mockResolvedValue({ values: ['input', 'output'] })
    vi.mocked(api.getMetricAttributeKeys).mockResolvedValue({ keys: ['model', 'repository', 'type'] })
    vi.mocked(api.createWidget).mockImplementation(async (_id, req) => ({ ...widget(req.gridColumn, req.gridRow), ...req, id: 'new' }))
  })

  it('adds a built-in widget at the next free position and closes', async () => {
    const user = userEvent.setup()
    const { queryClient } = openPanel([widget(1, 1, 2)])

    await addBuiltin(user, 'Total Traces')

    expect(api.createWidget).toHaveBeenCalledWith('d1', expect.objectContaining({
      widgetType: 'stats_traces', title: 'Total Traces', gridColumn: 3, gridRow: 1, colSpan: 1, rowSpan: 1,
    }))
    await waitFor(() => expect(useDashboardStore.getState().isAddPanelOpen).toBe(false))
    expect(cached(queryClient, 'new')).toBeDefined()
  })

  it('starts a new row when the last one is full', async () => {
    const user = userEvent.setup()
    openPanel([widget(1, 1, 4)])

    await addBuiltin(user, 'Total Traces')

    expect(api.createWidget).toHaveBeenCalledWith('d1', expect.objectContaining({ gridColumn: 1, gridRow: 2 }))
  })

  it('refuses a widget wider than the space at the chosen position', async () => {
    const user = userEvent.setup()
    openPanel([widget(3, 1)], { gridRow: 1, gridColumn: 2 })

    await addBuiltin(user, 'Active Services')

    expect(toast.error).toHaveBeenCalledWith('Widget does not fit', {
      description: 'This widget requires 2 columns, but only 1 column is available at this position.',
    })
    expect(api.createWidget).not.toHaveBeenCalled()
  })

  it('adds a metric chart with the chosen chart mode at the chosen position', async () => {
    const user = userEvent.setup()
    openPanel([], { gridRow: 2, gridColumn: 1 })

    // The first stored metric is preselected
    await waitFor(() => expect(screen.getByLabelText('Metric')).toHaveValue('claude_code.token.usage'))
    const add = screen.getByRole('button', { name: /Add Widget/ })
    expect(add).toBeDisabled()

    await user.click(screen.getByRole('button', { name: /Metric Chart/ }))
    expect(screen.getByRole('button', { name: /Metric Chart/ })).toHaveAttribute('aria-pressed', 'true')
    await user.click(screen.getByRole('button', { name: /Grouped/ }))
    expect(screen.getByRole('button', { name: /Grouped/ })).toHaveAttribute('aria-pressed', 'true')
    await user.click(add)

    expect(api.createWidget).toHaveBeenCalledWith('d1', expect.objectContaining({
      widgetType: 'metric_chart',
      title: 'Token Usage',
      gridColumn: 1,
      gridRow: 2,
      colSpan: 2,
      config: expect.objectContaining({ metricName: 'claude_code.token.usage', chartStacked: false, service: undefined }),
    }))
    await waitFor(() => expect(useDashboardStore.getState().targetPosition).toBeNull())
  })

  it('adds a metric value filtered to one breakdown value', async () => {
    const user = userEvent.setup()
    openPanel()
    await waitFor(() => expect(screen.getByLabelText('Metric')).toHaveValue('claude_code.token.usage'))

    await user.click(screen.getByRole('button', { name: /Metric Value/ }))
    await user.selectOptions(screen.getByLabelText(/Breakdown By/), 'type')
    await waitFor(() =>
      expect(api.getBreakdownValues).toHaveBeenCalledWith({ name: 'claude_code.token.usage', attribute: 'type' }, expect.anything())
    )
    // Known values show their display names
    const valueSelect = screen.getByLabelText('Value')
    await waitFor(() => expect(within(valueSelect).getByText('Output')).toBeInTheDocument())
    await user.selectOptions(valueSelect, 'output')
    await user.click(screen.getByRole('button', { name: /Add Widget/ }))

    expect(api.createWidget).toHaveBeenCalledWith('d1', expect.objectContaining({
      widgetType: 'metric_value',
      config: expect.objectContaining({ breakdownAttribute: 'type', breakdownValue: 'output', chartStacked: undefined }),
    }))
  })

  it('breaks a chart down by an attribute the points have', async () => {
    const user = userEvent.setup()
    openPanel()
    await waitFor(() => expect(screen.getByLabelText('Metric')).toHaveValue('claude_code.token.usage'))

    await user.click(screen.getByRole('button', { name: /Metric Chart/ }))
    const breakdown = screen.getByLabelText(/Breakdown By/)
    await waitFor(() => expect(within(breakdown).getByRole('option', { name: 'repository' })).toBeInTheDocument())
    // Counters have no statistic
    expect(screen.queryByLabelText('Statistic')).not.toBeInTheDocument()
    await user.selectOptions(breakdown, 'repository')
    await user.click(screen.getByRole('button', { name: /Add Widget/ }))

    expect(api.createWidget).toHaveBeenCalledWith('d1', expect.objectContaining({
      config: expect.objectContaining({ breakdownAttribute: 'repository', stat: undefined }),
    }))
  })

  it('adds a histogram chart with a chosen statistic, which is not stacked', async () => {
    const user = userEvent.setup()
    vi.mocked(api.getMetricNames).mockResolvedValue({ names: ['gemini_cli.api.request.latency'] })
    openPanel()
    await waitFor(() => expect(screen.getByLabelText('Metric')).toHaveValue('gemini_cli.api.request.latency'))

    await user.click(screen.getByRole('button', { name: /Metric Chart/ }))
    const stat = screen.getByLabelText('Statistic')
    expect(within(stat).getByRole('option', { name: 'Default (Average)' })).toBeInTheDocument()
    await user.selectOptions(stat, 'p95')
    expect(screen.getByRole('button', { name: /Stacked/ })).toBeDisabled()
    await user.click(screen.getByRole('button', { name: /Add Widget/ }))

    expect(api.createWidget).toHaveBeenCalledWith('d1', expect.objectContaining({
      config: expect.objectContaining({ metricName: 'gemini_cli.api.request.latency', stat: 'p95' }),
    }))
  })

  it('lists only the metrics of the selected service', async () => {
    const user = userEvent.setup()
    openPanel()
    await waitFor(() => expect(api.getMetricNames).toHaveBeenCalledWith(undefined, expect.anything()))
    const metricSelect = screen.getByLabelText('Metric')
    await waitFor(() => expect(within(metricSelect).getByRole('group', { name: 'Claude Code' })).toBeInTheDocument())

    vi.mocked(api.getMetricNames).mockResolvedValue({ names: [] })
    await user.selectOptions(screen.getByLabelText('Service (optional)'), 'gemini_cli')

    await waitFor(() => expect(api.getMetricNames).toHaveBeenLastCalledWith('gemini_cli', expect.anything()))
    await waitFor(() => expect(within(metricSelect).queryByRole('group', { name: 'Claude Code' })).not.toBeInTheDocument())
    expect(within(metricSelect).getByRole('group', { name: 'Gemini CLI' })).toBeInTheDocument()
  })

  it('stays open and says why when adding fails', async () => {
    const user = userEvent.setup()
    vi.mocked(api.createWidget).mockRejectedValue(httpError(400, 'colSpan must be at most 4'))
    openPanel()

    await addBuiltin(user, 'Total Traces')

    await waitFor(() =>
      expect(toast.error).toHaveBeenCalledWith('Failed to add widget', { description: 'colSpan must be at most 4' })
    )
    expect(useDashboardStore.getState().isAddPanelOpen).toBe(true)
  })
})

describe('AddWidgetPanel: editing', () => {
  const chartConfig: WidgetConfig = { metricName: 'claude_code.token.usage', chartStacked: true, breakdownAttribute: 'model' }
  const chart = widget(1, 1, 2, { id: 'chart', widgetType: 'metric_chart', title: 'My tokens', config: chartConfig })
  const neighbour = widget(3, 1, 1, { id: 'neighbour' })
  const below = widget(1, 2, 1, { id: 'below' })

  beforeEach(() => {
    vi.clearAllMocks()
    useDashboardStore.getState().reset()
    vi.mocked(api.getServices).mockResolvedValue({ services: ['claude-code', 'codex_cli_rs'] })
    vi.mocked(api.getMetricNames).mockResolvedValue({ names: ['claude_code.token.usage', 'claude_code.cost.usage'] })
    vi.mocked(api.updateWidget).mockImplementation(async (_d, id, changes) => ({ ...chart, ...changes, id }) as DashboardWidget)
  })

  it('starts from the stored settings', async () => {
    openEditor([chart, neighbour], 'chart')

    expect(screen.getByRole('heading', { name: 'Edit Widget' })).toBeInTheDocument()
    expect(screen.getByLabelText('Title')).toHaveValue('My tokens')
    await waitFor(() => expect(screen.getByLabelText('Metric')).toHaveValue('claude_code.token.usage'))
    expect(screen.getByLabelText(/Breakdown By/)).toHaveValue('model')
    expect(screen.getByRole('button', { name: /Stacked/ })).toHaveAttribute('aria-pressed', 'true')
    expect(screen.getByLabelText('Width')).toHaveValue('2')
    expect(screen.getByLabelText('Height')).toHaveValue('1')
  })

  it('saves title, metric settings and size with the whole configuration', async () => {
    const user = userEvent.setup()
    const { queryClient } = openEditor([chart, neighbour], 'chart')
    await waitFor(() => expect(screen.getByLabelText('Metric')).toHaveValue('claude_code.token.usage'))

    await user.clear(screen.getByLabelText('Title'))
    await user.type(screen.getByLabelText('Title'), 'Tokens by type')
    await user.selectOptions(screen.getByLabelText('Service (optional)'), 'claude-code')
    await user.selectOptions(screen.getByLabelText(/Breakdown By/), '')
    await user.click(screen.getByRole('button', { name: /Grouped/ }))
    await user.selectOptions(screen.getByLabelText('Height'), '2')
    await user.click(screen.getByRole('button', { name: 'Save' }))

    expect(api.updateWidget).toHaveBeenCalledWith('d1', 'chart', {
      title: 'Tokens by type',
      colSpan: 2,
      rowSpan: 2,
      config: {
        service: 'claude-code',
        metricName: 'claude_code.token.usage',
        breakdownAttribute: undefined,
        breakdownValue: undefined,
        chartStacked: false,
      },
    })
    // Shown at once, the panel closes
    expect(useDashboardStore.getState().isAddPanelOpen).toBe(false)
    await waitFor(() => expect(cached(queryClient, 'chart')).toMatchObject({ title: 'Tokens by type', rowSpan: 2 }))
  })

  it('names the widget after a newly chosen metric unless the title was typed', async () => {
    const user = userEvent.setup()
    openEditor([chart], 'chart')
    await waitFor(() => expect(screen.getByLabelText('Metric')).toHaveValue('claude_code.token.usage'))

    await user.selectOptions(screen.getByLabelText('Metric'), 'claude_code.cost.usage')

    expect(screen.getByLabelText('Title')).toHaveValue('Cost')
  })

  it('refuses a size that overlaps other widgets', async () => {
    const user = userEvent.setup()
    openEditor([chart, neighbour, below], 'chart')

    await user.selectOptions(screen.getByLabelText('Width'), '3')
    expect(screen.getByRole('alert')).toHaveTextContent(
      'This widget requires 3 columns, but only 2 columns are available at this position.'
    )
    expect(screen.getByRole('button', { name: 'Save' })).toBeDisabled()

    await user.selectOptions(screen.getByLabelText('Width'), '2')
    await user.selectOptions(screen.getByLabelText('Height'), '2')
    expect(screen.getByRole('alert')).toHaveTextContent('in each of 2 rows, but only 0 columns are available')
    expect(screen.getByRole('button', { name: 'Save' })).toBeDisabled()

    await user.selectOptions(screen.getByLabelText('Height'), '1')
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Save' })).toBeEnabled()
  })

  it('requires a title', async () => {
    const user = userEvent.setup()
    openEditor([chart], 'chart')

    await user.clear(screen.getByLabelText('Title'))

    expect(screen.getByText('Enter a title')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Save' })).toBeDisabled()
  })

  it('edits a built-in widget\'s title and size and keeps its configuration', async () => {
    const user = userEvent.setup()
    const stats = widget(1, 1, 1, { id: 'stats', title: 'Total Traces', config: {} })
    openEditor([stats], 'stats')

    expect(screen.queryByLabelText('Metric')).not.toBeInTheDocument()
    await user.selectOptions(screen.getByLabelText('Width'), '2')
    await user.click(screen.getByRole('button', { name: 'Save' }))

    expect(api.updateWidget).toHaveBeenCalledWith('d1', 'stats', { title: 'Total Traces', colSpan: 2, rowSpan: 1, config: {} })
  })

  it('rolls the widget back and says why when the server refuses the change', async () => {
    const user = userEvent.setup()
    vi.mocked(api.updateWidget).mockRejectedValue(httpError(400, 'title is longer than 255 characters'))
    vi.mocked(api.getDashboard).mockResolvedValue({
      id: 'd1', name: 'Main', isDefault: true, createdAt: '', updatedAt: '', widgets: [chart],
    })
    const { queryClient } = openEditor([chart], 'chart')

    await user.clear(screen.getByLabelText('Title'))
    await user.type(screen.getByLabelText('Title'), 'Refused')
    await user.click(screen.getByRole('button', { name: 'Save' }))

    await waitFor(() =>
      expect(toast.error).toHaveBeenCalledWith("Couldn't save the widget", { description: 'title is longer than 255 characters' })
    )
    expect(cached(queryClient, 'chart')).toMatchObject({ title: 'My tokens', config: chartConfig })
  })
})
