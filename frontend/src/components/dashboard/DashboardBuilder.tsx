import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { api } from '@/lib/api'
import type { StatsResponse } from '@/lib/api'
import type { TraceOverview } from '@/types/traces'
import { useDashboardStore } from '@/stores/dashboardStore'
import { useShallow } from 'zustand/react/shallow'
import { useTelemetryStore } from '@/stores/telemetryStore'
import { useLiveRefresh } from '@/hooks/useLiveRefresh'
import { isAbortError } from '@/lib/errors'
import { DashboardGrid } from './DashboardGrid'
import { DashboardToolbar } from './DashboardToolbar'
import { AddWidgetPanel } from './AddWidgetPanel'
import { MetricDataProvider } from '@/contexts/MetricDataContext'
import { EditableText } from '@/components/common/editable-text'
import { isAbsoluteTimeSelection } from '@/types/dashboard'

interface DashboardBuilderProps {
  dashboardId?: string
}

export function DashboardBuilder({ dashboardId }: DashboardBuilderProps) {
  const navigate = useNavigate()
  const {
    loading,
    error,
    dashboard,
    loadDefaultDashboard,
    loadDashboard,
    isEditMode,
    updateDashboardDetails,
    timeSelection,
    fromTime,
    toTime,
    isAbsoluteRange,
  } = useDashboardStore(
    useShallow((s) => ({
      loading: s.loading,
      error: s.error,
      dashboard: s.dashboard,
      loadDefaultDashboard: s.loadDefaultDashboard,
      loadDashboard: s.loadDashboard,
      isEditMode: s.isEditMode,
      updateDashboardDetails: s.updateDashboardDetails,
      timeSelection: s.timeSelection,
      fromTime: s.fromTime,
      toTime: s.toTime,
      isAbsoluteRange: s.isAbsoluteRange,
    }))
  )
  const [stats, setStats] = useState<StatsResponse | null>(null)
  const [recentTraces, setRecentTraces] = useState<TraceOverview[]>([])

  // Track WebSocket data for triggering refreshes (use counters, not array length)
  const spansUpdateCount = useTelemetryStore((state) => state.spansUpdateCount)
  const logsUpdateCount = useTelemetryStore((state) => state.logsUpdateCount)
  // Stats and recent traces: refreshed on new spans and logs, polled every
  // 10 s while the WebSocket is disconnected
  const refreshKey = useLiveRefresh({
    enabled: true,
    intervalMs: 10000,
    updateCount: spansUpdateCount + logsUpdateCount,
  })

  // Load dashboard based on route
  useEffect(() => {
    if (dashboardId) {
      loadDashboard(dashboardId).catch(() => {
        // Dashboard not found, redirect to default
        navigate('/', { replace: true })
      })
    } else {
      loadDefaultDashboard()
    }
  }, [dashboardId, loadDashboard, loadDefaultDashboard, navigate])

  // Fetch stats and recent traces on mount, when the time selection changes
  // and on live refreshes. A newer fetch aborts an older one, so responses
  // never arrive out of order.
  useEffect(() => {
    const controller = new AbortController()

    const fetchData = async () => {
      let fetchFrom: Date
      let fetchTo: Date

      if (isAbsoluteRange) {
        fetchFrom = fromTime
        fetchTo = toTime
      } else {
        const now = new Date()
        const durationSeconds = isAbsoluteTimeSelection(timeSelection)
          ? (toTime.getTime() - fromTime.getTime()) / 1000
          : timeSelection.timeframe.durationSeconds
        fetchFrom = new Date(now.getTime() - durationSeconds * 1000)
        fetchTo = now
      }

      const timeParams = {
        from: fetchFrom.toISOString(),
        to: fetchTo.toISOString(),
      }

      try {
        const [statsData, tracesData] = await Promise.all([
          api.getStats(timeParams, { signal: controller.signal }),
          api.getRecentTraces(10, timeParams, { signal: controller.signal }),
        ])
        setStats(statsData)
        setRecentTraces(tracesData.traces ?? [])
      } catch (err) {
        if (isAbortError(err)) {
          return // superseded by a newer refresh
        }
        console.error('Failed to fetch data:', err)
      }
    }

    fetchData()
    return () => controller.abort()
  }, [timeSelection, fromTime, toTime, isAbsoluteRange, refreshKey])

  if (loading) {
    return (
      <div className="flex items-center justify-center h-64">
        <div className="text-muted-foreground">Loading dashboard...</div>
      </div>
    )
  }

  if (error) {
    return (
      <div className="flex items-center justify-center h-64">
        <div className="text-destructive">Error: {error}</div>
      </div>
    )
  }

  return (
    <MetricDataProvider>
      <div className="space-y-6">
        {/* Header */}
        <div className="flex items-center justify-between gap-4">
          <div className="flex-1 min-w-0">
            {isEditMode && dashboard ? (
              <>
                <EditableText
                  value={dashboard.name}
                  onSave={(name) => updateDashboardDetails(dashboard.id, name, dashboard.description)}
                  isEditing={isEditMode}
                  placeholder="Dashboard name"
                  className="text-3xl font-bold tracking-tight"
                />
                <EditableText
                  value={dashboard.description || ''}
                  onSave={(description) => updateDashboardDetails(dashboard.id, dashboard.name, description || undefined)}
                  isEditing={isEditMode}
                  placeholder="Add a description..."
                  className="text-muted-foreground text-sm"
                />
              </>
            ) : (
              <>
                <h1 className="text-3xl font-bold tracking-tight">
                  {dashboard?.name || 'Dashboard'}
                </h1>
                <p className="text-muted-foreground">
                  {dashboard?.description || 'Monitor your AI coding tools telemetry in real-time'}
                </p>
              </>
            )}
          </div>
          <DashboardToolbar />
        </div>

        {/* Grid */}
        <DashboardGrid stats={stats} recentTraces={recentTraces} />

        {/* Add Widget Panel */}
        <AddWidgetPanel />
      </div>
    </MetricDataProvider>
  )
}
