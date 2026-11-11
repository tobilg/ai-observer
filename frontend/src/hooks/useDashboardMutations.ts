import { useMutation, useQueryClient, type QueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api'
import { queryKeys } from '@/lib/queries'
import { applyPositions } from '@/lib/dashboardGrid'
import { deriveWidgetTitle, generateUniqueName } from '@/lib/dashboard-export'
import type {
  CreateWidgetRequest,
  Dashboard,
  DashboardWidget,
  DashboardWithWidgets,
  DashboardsResponse,
  UpdateDashboardRequest,
  UpdateWidgetRequest,
  WidgetPosition,
} from '@/types/dashboard'
import type { DashboardExport } from '@/types/dashboard-export'

// Mutations of dashboards and widgets. They update the query cache: widget
// changes optimistically (rolled back, then refetched, when the request
// fails), dashboard creation and deletion by refetching the list.
//
// A dashboard is cached as dashboards.detail(id) when opened by its ID and
// as dashboards.default when it is the default one; both are kept in step.

// ============================================================================
// Cache helpers
// ============================================================================

interface DashboardSnapshot {
  detail: DashboardWithWidgets | undefined
  default: DashboardWithWidgets | null | undefined
}

async function cancelDashboard(queryClient: QueryClient, id: string): Promise<DashboardSnapshot> {
  // A refetch finishing after the optimistic update would undo it
  await Promise.all([
    queryClient.cancelQueries({ queryKey: queryKeys.dashboards.detail(id) }),
    queryClient.cancelQueries({ queryKey: queryKeys.dashboards.default }),
  ])
  return {
    detail: queryClient.getQueryData<DashboardWithWidgets>(queryKeys.dashboards.detail(id)),
    default: queryClient.getQueryData<DashboardWithWidgets | null>(queryKeys.dashboards.default),
  }
}

/** Applies update to the cached dashboard id (by ID and as the default) */
export function updateCachedDashboard(
  queryClient: QueryClient,
  id: string,
  update: (dashboard: DashboardWithWidgets) => DashboardWithWidgets,
): void {
  queryClient.setQueryData<DashboardWithWidgets>(queryKeys.dashboards.detail(id), (d) => (d ? update(d) : d))
  queryClient.setQueryData<DashboardWithWidgets | null>(queryKeys.dashboards.default, (d) =>
    d && d.id === id ? update(d) : d,
  )
}

// Puts the snapshot back and asks the server for the current state, since
// other changes may have been made meanwhile
function rollback(queryClient: QueryClient, id: string, snapshot: DashboardSnapshot | undefined): void {
  if (snapshot) {
    queryClient.setQueryData(queryKeys.dashboards.detail(id), snapshot.detail)
    queryClient.setQueryData(queryKeys.dashboards.default, snapshot.default)
  }
  void queryClient.invalidateQueries({ queryKey: queryKeys.dashboards.detail(id) })
  void queryClient.invalidateQueries({ queryKey: queryKeys.dashboards.default })
}

function updateWidgets(
  queryClient: QueryClient,
  id: string,
  update: (widgets: DashboardWidget[]) => DashboardWidget[],
): void {
  updateCachedDashboard(queryClient, id, (d) => ({ ...d, widgets: update(d.widgets ?? []) }))
}

function requireDashboard(dashboardId: string | undefined): string {
  if (!dashboardId) {
    throw new Error('No dashboard is loaded')
  }
  return dashboardId
}

// ============================================================================
// Dashboards
// ============================================================================

function cachedDashboardNames(queryClient: QueryClient): string[] {
  return queryClient.getQueryData<DashboardsResponse>(queryKeys.dashboards.list)?.dashboards?.map((d) => d.name) ?? []
}

/**
 * Creates a dashboard from an export under a name not taken yet, with all
 * its widgets. When a widget cannot be created the dashboard is removed
 * again, so no partial copy is left behind.
 */
export async function importDashboard(exportData: DashboardExport, existingNames: string[]): Promise<Dashboard> {
  const dashboard = await api.createDashboard({
    name: generateUniqueName(exportData.name, existingNames),
    description: exportData.description,
    isDefault: false,
  })
  try {
    for (const widget of exportData.widgets) {
      await api.createWidget(dashboard.id, {
        widgetType: widget.widgetType,
        title: deriveWidgetTitle(widget),
        gridColumn: widget.gridColumn,
        gridRow: widget.gridRow,
        colSpan: widget.colSpan,
        rowSpan: widget.rowSpan,
        config: widget.config,
      })
    }
  } catch (error) {
    await api.deleteDashboard(dashboard.id).catch(() => {
      // The import failed either way; the error below says why
    })
    throw error
  }
  return dashboard
}

export function useCreateDashboard() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: ({ name, description }: { name: string; description?: string }) =>
      api.createDashboard({ name, description, isDefault: false }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: queryKeys.dashboards.list }),
  })
}

export function useImportDashboard() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (exportData: DashboardExport) => importDashboard(exportData, cachedDashboardNames(queryClient)),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: queryKeys.dashboards.list }),
  })
}

/** Renames a dashboard or changes its description */
export function useUpdateDashboard() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: ({ id, changes }: { id: string; changes: UpdateDashboardRequest }) => api.updateDashboard(id, changes),
    onMutate: async ({ id, changes }) => {
      await queryClient.cancelQueries({ queryKey: queryKeys.dashboards.list })
      const list = queryClient.getQueryData<DashboardsResponse>(queryKeys.dashboards.list)
      const snapshot = await cancelDashboard(queryClient, id)
      queryClient.setQueryData<DashboardsResponse>(queryKeys.dashboards.list, (data) =>
        data ? { ...data, dashboards: data.dashboards.map((d) => (d.id === id ? { ...d, ...changes } : d)) } : data,
      )
      updateCachedDashboard(queryClient, id, (d) => ({ ...d, ...changes }))
      return { list, snapshot }
    },
    onError: (_error, { id }, context) => {
      if (context?.list) {
        queryClient.setQueryData(queryKeys.dashboards.list, context.list)
      }
      rollback(queryClient, id, context?.snapshot)
    },
    onSettled: () => queryClient.invalidateQueries({ queryKey: queryKeys.dashboards.list }),
  })
}

export function useDeleteDashboard() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => api.deleteDashboard(id),
    onSuccess: (_data, id) => {
      queryClient.setQueryData<DashboardsResponse>(queryKeys.dashboards.list, (data) =>
        data ? { ...data, dashboards: data.dashboards.filter((d) => d.id !== id) } : data,
      )
      // Its own query is left alone: the page showing it navigates away, and
      // removing it under that page would refetch it (404) first. Opened
      // again, it refetches and shows "not found".
      void queryClient.invalidateQueries({ queryKey: queryKeys.dashboards.detail(id), refetchType: 'none' })
      void queryClient.invalidateQueries({ queryKey: queryKeys.dashboards.list })
    },
  })
}

export function useSetDefaultDashboard() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => api.setDefaultDashboard(id),
    onSuccess: (_data, id) => {
      queryClient.setQueryData<DashboardsResponse>(queryKeys.dashboards.list, (data) =>
        data ? { ...data, dashboards: data.dashboards.map((d) => ({ ...d, isDefault: d.id === id })) } : data,
      )
      // "/" shows another dashboard now
      void queryClient.invalidateQueries({ queryKey: queryKeys.dashboards.all })
    },
  })
}

// ============================================================================
// Widgets of one dashboard
// ============================================================================

export function useAddWidget(dashboardId: string | undefined) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (req: CreateWidgetRequest) => api.createWidget(requireDashboard(dashboardId), req),
    onSuccess: (widget) => {
      if (dashboardId) {
        updateWidgets(queryClient, dashboardId, (widgets) => [...widgets, widget])
      }
    },
  })
}

export function useRemoveWidget(dashboardId: string | undefined) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (widgetId: string) => api.deleteWidget(requireDashboard(dashboardId), widgetId),
    onMutate: async (widgetId) => {
      const id = requireDashboard(dashboardId)
      const snapshot = await cancelDashboard(queryClient, id)
      updateWidgets(queryClient, id, (widgets) => widgets.filter((w) => w.id !== widgetId))
      return { snapshot }
    },
    onError: (_error, _widgetId, context) => {
      if (dashboardId) rollback(queryClient, dashboardId, context?.snapshot)
    },
  })
}

/**
 * Changes a widget's title, size or configuration. A sent config replaces
 * the stored one whole, so send the complete configuration.
 */
export function useUpdateWidget(dashboardId: string | undefined) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: ({ widgetId, changes }: { widgetId: string; changes: UpdateWidgetRequest }) =>
      api.updateWidget(requireDashboard(dashboardId), widgetId, changes),
    onMutate: async ({ widgetId, changes }) => {
      const id = requireDashboard(dashboardId)
      const snapshot = await cancelDashboard(queryClient, id)
      updateWidgets(queryClient, id, (widgets) =>
        widgets.map((w) => (w.id === widgetId ? { ...w, ...changes, config: changes.config ?? w.config } : w)),
      )
      return { snapshot }
    },
    onSuccess: (widget) => {
      if (dashboardId) {
        updateWidgets(queryClient, dashboardId, (widgets) => widgets.map((w) => (w.id === widget.id ? widget : w)))
      }
    },
    onError: (_error, _variables, context) => {
      if (dashboardId) rollback(queryClient, dashboardId, context?.snapshot)
    },
  })
}

/** Moves widgets (drag and drop, inserted rows, closing empty rows) */
export function useUpdateWidgetPositions(dashboardId: string | undefined) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (positions: WidgetPosition[]) => api.updateWidgetPositions(requireDashboard(dashboardId), positions),
    onMutate: async (positions) => {
      const id = requireDashboard(dashboardId)
      const snapshot = await cancelDashboard(queryClient, id)
      updateWidgets(queryClient, id, (widgets) => applyPositions(widgets, positions))
      return { snapshot }
    },
    onError: (_error, _positions, context) => {
      if (dashboardId) rollback(queryClient, dashboardId, context?.snapshot)
    },
  })
}
