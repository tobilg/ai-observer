import { useCallback, useEffect } from 'react'
import { Link } from 'react-router-dom'
import { LayoutDashboard } from 'lucide-react'
import { toast } from 'sonner'
import { useShallow } from 'zustand/react/shallow'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { EditableText } from '@/components/common/editable-text'
import { EmptyState } from '@/components/common/empty-state'
import { PageHeader } from '@/components/common/page-header'
import { QueryError } from '@/components/common/query-error'
import { MetricDataProvider } from '@/contexts/MetricDataContext'
import { useDashboardStore } from '@/stores/dashboardStore'
import { useTelemetryStore } from '@/stores/telemetryStore'
import { useDocumentTitle } from '@/hooks/useDocumentTitle'
import { useLiveInvalidate } from '@/hooks/useLiveInvalidate'
import { useTimeSelection } from '@/hooks/useTimeSelection'
import { useUrlWriter } from '@/hooks/useUrlState'
import { useUpdateDashboard, useUpdateWidgetPositions } from '@/hooks/useDashboardMutations'
import { compactRowPositions } from '@/lib/dashboardGrid'
import { errorText, isHTTPError } from '@/lib/errors'
import { queryKeys, useDashboard, useDefaultDashboard, useRecentTraces, useStats } from '@/lib/queries'
import type { DashboardWithWidgets } from '@/types/dashboard'
import { DashboardGrid } from './DashboardGrid'
import { DashboardToolbar } from './DashboardToolbar'
import { AddWidgetPanel } from './AddWidgetPanel'

// localStorage key of the dashboards' relative time selection (the URL
// holds the selection of a shown dashboard)
const DASHBOARD_TIME_STORAGE_KEY = 'ai-observer-dashboard-timeselection'
const DEFAULT_DESCRIPTION = 'Monitor your AI coding tools telemetry in real-time'
const RECENT_TRACES = 10

interface DashboardBuilderProps {
  // The dashboard to show; without one the default dashboard
  dashboardId?: string
}

export function DashboardBuilder({ dashboardId }: DashboardBuilderProps) {
  // The server creates the default dashboard when there is none yet
  const byId = useDashboard(dashboardId ?? '', { enabled: Boolean(dashboardId) })
  const byDefault = useDefaultDashboard({ enabled: !dashboardId })
  const query = dashboardId ? byId : byDefault
  const dashboard = query.data ?? undefined

  const url = useUrlWriter()
  const time = useTimeSelection({ storageKey: DASHBOARD_TIME_STORAGE_KEY, defaultTimeframe: '1h', url })

  const { isEditMode, setEditMode, reset } = useDashboardStore(
    useShallow((s) => ({ isEditMode: s.isEditMode, setEditMode: s.setEditMode, reset: s.reset }))
  )
  // Another dashboard (or leaving the page) ends editing
  useEffect(() => reset, [dashboardId, reset])

  // Stats and recent traces of the selected range, for the built-in widgets
  const stats = useStats(time.range, { enabled: Boolean(dashboard) })
  const recentTraces = useRecentTraces(RECENT_TRACES, time.range, { enabled: Boolean(dashboard) })
  const updateCount = useTelemetryStore((s) => s.spansUpdateCount + s.logsUpdateCount + s.metricsUpdateCount)
  useLiveInvalidate({
    queryKey: [queryKeys.stats.summary(time.range), queryKeys.traces.recent(RECENT_TRACES, time.range)],
    // Absolute ranges show fixed historical data
    enabled: Boolean(dashboard) && !time.isAbsoluteRange,
    intervalMs: 10_000,
    updateCount,
  })

  const { mutate: movePositions } = useUpdateWidgetPositions(dashboard?.id)
  // Here rather than in the header, which unmounts when editing ends
  const { mutate: changeDashboard } = useUpdateDashboard()
  const saveDetails = useCallback(
    (id: string, changes: { name: string; description?: string }) =>
      changeDashboard(
        { id, changes },
        { onError: (error) => toast.error("Couldn't save the dashboard", { description: errorText(error) }) }
      ),
    [changeDashboard]
  )
  const widgets = dashboard?.widgets
  // Leaving edit mode closes the rows left empty
  const finishEditing = useCallback(() => {
    setEditMode(false)
    const positions = compactRowPositions(widgets ?? [])
    if (positions.length > 0) {
      movePositions(positions, {
        onError: (error) => {
          setEditMode(true)
          toast.error("Couldn't close the empty rows", { description: errorText(error) })
        },
      })
    }
  }, [widgets, movePositions, setEditMode])

  if (query.isPending) {
    return <DashboardSkeleton />
  }

  // Also when a cached dashboard turns out to be deleted
  if (query.isError && dashboardId && isHTTPError(query.error, 404)) {
    return <DashboardNotFound />
  }

  // A failed refresh keeps showing the loaded dashboard
  if (query.isError && !dashboard) {
    return (
      <div className="space-y-6">
        <PageHeader title="Dashboard" />
        <QueryError
          error={query.error}
          title="Couldn't load the dashboard"
          onRetry={() => query.refetch()}
          retrying={query.isFetching}
        />
      </div>
    )
  }

  if (!dashboard) {
    // An older server answers 404 when no default dashboard exists
    return (
      <div className="space-y-6">
        <PageHeader title="Dashboard" />
        <QueryError
          error={null}
          title="No default dashboard"
          fallback="The server returned no default dashboard. Set one as default in the sidebar, or update AI Observer, which creates it."
          onRetry={() => query.refetch()}
          retrying={query.isFetching}
        />
      </div>
    )
  }

  return (
    <MetricDataProvider widgets={dashboard.widgets ?? []} timeSelection={time.timeSelection}>
      <div className="space-y-6">
        <div className="flex flex-col gap-4 md:flex-row md:items-start md:justify-between">
          {isEditMode ? (
            <EditableDashboardHeader dashboard={dashboard} onSave={saveDetails} />
          ) : (
            <PageHeader
              className="min-w-0 flex-1"
              title={dashboard.name || 'Dashboard'}
              description={dashboard.description || DEFAULT_DESCRIPTION}
            />
          )}
          <DashboardToolbar
            dashboard={dashboard}
            timeSelection={time.timeSelection}
            onTimeSelectionChange={time.setTimeSelection}
            onDone={finishEditing}
          />
        </div>

        {(dashboard.widgets ?? []).length === 0 && !isEditMode ? (
          <EmptyState
            icon={LayoutDashboard}
            title="This dashboard has no widgets yet"
            description="Click Edit to add widgets for metrics, counts and recent traces."
            action={
              <Button variant="outline" onClick={() => setEditMode(true)} className="hidden md:inline-flex">
                Edit dashboard
              </Button>
            }
          />
        ) : (
          <DashboardGrid
            dashboardId={dashboard.id}
            widgets={dashboard.widgets ?? []}
            stats={stats}
            recentTraces={recentTraces}
            timeSelection={time.timeSelection}
          />
        )}

        <AddWidgetPanel dashboardId={dashboard.id} widgets={dashboard.widgets ?? []} />
      </div>
    </MetricDataProvider>
  )
}

// The name and description, editable in edit mode
interface EditableDashboardHeaderProps {
  dashboard: DashboardWithWidgets
  onSave: (id: string, changes: { name: string; description?: string }) => void
}

function EditableDashboardHeader({ dashboard, onSave }: EditableDashboardHeaderProps) {
  useDocumentTitle(dashboard.name || 'Dashboard')
  const save = (changes: { name: string; description?: string }) => onSave(dashboard.id, changes)

  return (
    <div className="min-w-0 flex-1">
      <h1 className="sr-only">{dashboard.name}</h1>
      <EditableText
        value={dashboard.name}
        onSave={(name) => name && save({ name, description: dashboard.description })}
        isEditing
        placeholder="Dashboard name"
        className="text-3xl font-bold tracking-tight"
      />
      <EditableText
        value={dashboard.description || ''}
        onSave={(description) => save({ name: dashboard.name, description: description || undefined })}
        isEditing
        placeholder="Add a description..."
        className="text-muted-foreground text-sm"
      />
    </div>
  )
}

// The first load: the default layout's shape
function DashboardSkeleton() {
  return (
    <div role="status" aria-busy="true" aria-label="Loading dashboard" className="space-y-6">
      <div className="space-y-2">
        <Skeleton className="h-9 w-64 max-w-full" />
        <Skeleton className="h-4 w-96 max-w-full" />
      </div>
      <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
        {Array.from({ length: 4 }, (_, i) => (
          <Skeleton key={i} className="h-[90px]" />
        ))}
        <Skeleton className="col-span-2 h-[120px]" />
        <Skeleton className="col-span-2 md:col-span-4 h-[260px]" />
      </div>
    </div>
  )
}

function DashboardNotFound() {
  return (
    <div className="space-y-6">
      <PageHeader title="Dashboard not found" />
      <EmptyState
        icon={LayoutDashboard}
        title="There is no dashboard with this ID"
        description="It may have been deleted. The sidebar lists the existing dashboards."
        action={
          <Button asChild>
            <Link to="/">Go to the default dashboard</Link>
          </Button>
        }
      />
    </div>
  )
}
