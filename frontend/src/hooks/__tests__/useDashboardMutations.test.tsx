import { describe, it, expect, vi, beforeEach } from 'vitest'
import { act, waitFor } from '@testing-library/react'
import type { QueryKey } from '@tanstack/react-query'
import { api } from '@/lib/api'
import { queryKeys } from '@/lib/queries'
import { renderHookWithProviders, createTestQueryClient } from '@/test/queryWrapper'
import {
  importDashboard,
  useAddWidget,
  useDeleteDashboard,
  useImportDashboard,
  useRemoveWidget,
  useUpdateDashboard,
  useUpdateWidget,
  useUpdateWidgetPositions,
} from '../useDashboardMutations'
import type { Dashboard, DashboardWidget, DashboardWithWidgets, DashboardsResponse } from '@/types/dashboard'

vi.mock('@/lib/api', () => ({
  api: {
    createDashboard: vi.fn(),
    createWidget: vi.fn(),
    deleteDashboard: vi.fn(),
    updateDashboard: vi.fn(),
    deleteWidget: vi.fn(),
    updateWidget: vi.fn(),
    updateWidgetPositions: vi.fn(),
    getDashboard: vi.fn(),
    getDefaultDashboard: vi.fn(),
    getDashboards: vi.fn(),
  },
}))

const widget = (id: string, gridColumn: number, gridRow: number, colSpan = 1): DashboardWidget => ({
  id, dashboardId: 'd1', widgetType: 'stats_traces', title: id,
  gridColumn, gridRow, colSpan, rowSpan: 1, config: {}, createdAt: '', updatedAt: '',
})

const dashboard = (widgets: DashboardWidget[]): DashboardWithWidgets => ({
  id: 'd1', name: 'Main', isDefault: true, createdAt: '', updatedAt: '', widgets,
})

// A client holding dashboard d1 both by ID and as the default
function seededClient(widgets: DashboardWidget[] = [widget('a', 1, 1), widget('b', 2, 1)]) {
  const queryClient = createTestQueryClient()
  queryClient.setQueryData(queryKeys.dashboards.detail('d1'), dashboard(widgets))
  queryClient.setQueryData(queryKeys.dashboards.default, dashboard(widgets))
  queryClient.setQueryData<DashboardsResponse>(queryKeys.dashboards.list, {
    dashboards: [{ id: 'd1', name: 'Main', isDefault: true, createdAt: '', updatedAt: '' }],
  })
  return queryClient
}

function cachedWidgets(queryClient: ReturnType<typeof createTestQueryClient>, key: QueryKey = queryKeys.dashboards.detail('d1')) {
  return queryClient.getQueryData<DashboardWithWidgets>(key)?.widgets ?? []
}

// A promise the test settles
function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (error: unknown) => void
  const promise = new Promise<T>((res, rej) => {
    resolve = res
    reject = rej
  })
  return { promise, resolve, reject }
}

describe('importDashboard', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  const exportData = {
    schemaVersion: 1,
    name: 'Imported',
    widgets: [
      { widgetType: 'stats_traces', gridColumn: 1, gridRow: 1, colSpan: 1, rowSpan: 1 },
      { widgetType: 'stats_logs', gridColumn: 2, gridRow: 1, colSpan: 1, rowSpan: 1 },
    ],
  }

  it('removes the dashboard again when a widget cannot be created', async () => {
    vi.mocked(api.createDashboard).mockResolvedValue({ id: 'd1', name: 'Imported' } as Dashboard)
    vi.mocked(api.createWidget)
      .mockResolvedValueOnce({} as never)
      .mockRejectedValueOnce(new Error('HTTP error! status: 400'))
    vi.mocked(api.deleteDashboard).mockResolvedValue()

    await expect(importDashboard(exportData, [])).rejects.toThrow('status: 400')
    expect(api.deleteDashboard).toHaveBeenCalledWith('d1')
  })

  it('creates every widget', async () => {
    vi.mocked(api.createDashboard).mockResolvedValue({ id: 'd2', name: 'Imported' } as Dashboard)
    vi.mocked(api.createWidget).mockResolvedValue({} as never)

    const created = await importDashboard(exportData, [])
    expect(created.id).toBe('d2')
    expect(api.createWidget).toHaveBeenCalledTimes(2)
    expect(api.deleteDashboard).not.toHaveBeenCalled()
  })

  it('renames an import whose name is taken, and refreshes the list', async () => {
    vi.mocked(api.createDashboard).mockResolvedValue({ id: 'd3', name: 'Main (Copy)' } as Dashboard)
    const queryClient = seededClient()
    const invalidate = vi.spyOn(queryClient, 'invalidateQueries')
    const { result } = renderHookWithProviders(() => useImportDashboard(), { queryClient })

    await act(() => result.current.mutateAsync({ schemaVersion: 1, name: 'Main', widgets: [] }))

    expect(api.createDashboard).toHaveBeenCalledWith(expect.objectContaining({ name: 'Main (Copy)', isDefault: false }))
    expect(invalidate).toHaveBeenCalledWith({ queryKey: queryKeys.dashboards.list })
  })
})

describe('dashboard mutations', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('renames a dashboard everywhere it is cached, and rolls back on failure', async () => {
    const queryClient = seededClient()
    const request = deferred<Dashboard>()
    vi.mocked(api.updateDashboard).mockReturnValue(request.promise)
    const { result } = renderHookWithProviders(() => useUpdateDashboard(), { queryClient })

    act(() => result.current.mutate({ id: 'd1', changes: { name: 'Renamed' } }))
    await waitFor(() =>
      expect(queryClient.getQueryData<DashboardsResponse>(queryKeys.dashboards.list)?.dashboards[0].name).toBe('Renamed'),
    )
    expect(queryClient.getQueryData<DashboardWithWidgets>(queryKeys.dashboards.default)?.name).toBe('Renamed')

    await act(async () => {
      request.reject(new Error('offline'))
      await request.promise.catch(() => {})
    })
    await waitFor(() => expect(result.current.isError).toBe(true))
    expect(queryClient.getQueryData<DashboardWithWidgets>(queryKeys.dashboards.detail('d1'))?.name).toBe('Main')
    expect(queryClient.getQueryData<DashboardsResponse>(queryKeys.dashboards.list)?.dashboards[0].name).toBe('Main')
  })

  it('drops a deleted dashboard from the list', async () => {
    const queryClient = seededClient()
    vi.mocked(api.deleteDashboard).mockResolvedValue()
    vi.mocked(api.getDashboards).mockResolvedValue({ dashboards: [] })
    const { result } = renderHookWithProviders(() => useDeleteDashboard(), { queryClient })

    await act(() => result.current.mutateAsync('d1'))

    expect(queryClient.getQueryData<DashboardsResponse>(queryKeys.dashboards.list)?.dashboards).toEqual([])
    expect(queryClient.getQueryState(queryKeys.dashboards.detail('d1'))?.isInvalidated).toBe(true)
  })
})

describe('widget mutations', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('adds a created widget to the cached dashboard', async () => {
    const queryClient = seededClient()
    vi.mocked(api.createWidget).mockResolvedValue(widget('new', 3, 1))
    const { result } = renderHookWithProviders(() => useAddWidget('d1'), { queryClient })

    await act(() =>
      result.current.mutateAsync({ widgetType: 'stats_logs', title: 'new', gridColumn: 3, gridRow: 1, colSpan: 1, rowSpan: 1 }),
    )

    expect(api.createWidget).toHaveBeenCalledWith('d1', expect.objectContaining({ title: 'new' }))
    expect(cachedWidgets(queryClient).map((w) => w.id)).toEqual(['a', 'b', 'new'])
    expect(cachedWidgets(queryClient, queryKeys.dashboards.default).map((w) => w.id)).toEqual(['a', 'b', 'new'])
  })

  it('removes a widget at once, and restores it when the request fails', async () => {
    const queryClient = seededClient()
    const request = deferred<void>()
    vi.mocked(api.deleteWidget).mockReturnValue(request.promise)
    vi.mocked(api.getDashboard).mockResolvedValue(dashboard([widget('a', 1, 1), widget('b', 2, 1)]))
    const { result } = renderHookWithProviders(() => useRemoveWidget('d1'), { queryClient })

    act(() => result.current.mutate('b'))
    await waitFor(() => expect(cachedWidgets(queryClient).map((w) => w.id)).toEqual(['a']))

    await act(async () => {
      request.reject(new Error('offline'))
      await request.promise.catch(() => {})
    })
    await waitFor(() => expect(result.current.isError).toBe(true))
    expect(cachedWidgets(queryClient).map((w) => w.id)).toEqual(['a', 'b'])
  })

  it('moves widgets at once and keeps the move when it is stored', async () => {
    const queryClient = seededClient()
    vi.mocked(api.updateWidgetPositions).mockResolvedValue()
    const { result } = renderHookWithProviders(() => useUpdateWidgetPositions('d1'), { queryClient })

    await act(() => result.current.mutateAsync([{ id: 'b', gridRow: 2, gridColumn: 1 }]))

    expect(api.updateWidgetPositions).toHaveBeenCalledWith('d1', [{ id: 'b', gridRow: 2, gridColumn: 1 }])
    const b = cachedWidgets(queryClient).find((w) => w.id === 'b')!
    expect([b.gridColumn, b.gridRow]).toEqual([1, 2])
  })

  it('updates a widget with its whole configuration, and rolls back a refused update', async () => {
    const queryClient = seededClient()
    const request = deferred<DashboardWidget>()
    vi.mocked(api.updateWidget).mockReturnValue(request.promise)
    const { result } = renderHookWithProviders(() => useUpdateWidget('d1'), { queryClient })
    const changes = { title: 'Costs', colSpan: 2, rowSpan: 1, config: { metricName: 'claude_code.cost.usage' } }

    act(() => result.current.mutate({ widgetId: 'a', changes }))
    await waitFor(() => expect(cachedWidgets(queryClient).find((w) => w.id === 'a')?.title).toBe('Costs'))
    expect(api.updateWidget).toHaveBeenCalledWith('d1', 'a', changes)
    expect(cachedWidgets(queryClient).find((w) => w.id === 'a')).toMatchObject({ colSpan: 2, config: changes.config })

    await act(async () => {
      request.reject(new Error('HTTP error! status: 400'))
      await request.promise.catch(() => {})
    })
    await waitFor(() => expect(result.current.isError).toBe(true))
    expect(cachedWidgets(queryClient).find((w) => w.id === 'a')).toMatchObject({ title: 'a', colSpan: 1, config: {} })
  })

  it('keeps the widget the server returns', async () => {
    const queryClient = seededClient()
    vi.mocked(api.updateWidget).mockResolvedValue({ ...widget('a', 1, 1), title: 'Stored', updatedAt: 'now' })
    const { result } = renderHookWithProviders(() => useUpdateWidget('d1'), { queryClient })

    await act(() => result.current.mutateAsync({ widgetId: 'a', changes: { title: 'Sent' } }))

    expect(cachedWidgets(queryClient).find((w) => w.id === 'a')).toMatchObject({ title: 'Stored', updatedAt: 'now' })
  })
})
