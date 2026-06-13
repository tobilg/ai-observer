import { useEffect, useState, useRef, useCallback } from 'react'
import { useNavigate } from 'react-router-dom'
import { api } from '@/lib/api'
import type { StatsResponse } from '@/lib/api'
import type { TraceOverview } from '@/types/traces'
import { useDashboardStore } from '@/stores/dashboardStore'
import { useTelemetryStore } from '@/stores/telemetryStore'
import { DashboardGrid } from './DashboardGrid'
import { DashboardToolbar } from './DashboardToolbar'
import { AddWidgetPanel } from './AddWidgetPanel'
import { MetricDataProvider } from '@/contexts/MetricDataContext'
import { EditableText } from '@/components/ui/editable-text'

interface DashboardBuilderProps {
  dashboardId?: string
}

export function DashboardBuilder({ dashboardId }: DashboardBuilderProps) {
  const navigate = useNavigate()
  const { loading, error, dashboard, loadDefaultDashboard, loadDashboard, isEditMode, updateDashboardDetails } = useDashboardStore()
  const [stats, setStats] = useState<StatsResponse | null>(null)
  const [recentTraces, setRecentTraces] = useState<TraceOverview[]>([])

  // Track WebSocket data for triggering refreshes (use counters, not array length)
  const spansUpdateCount = useTelemetryStore((state) => state.spansUpdateCount)
  const logsUpdateCount = useTelemetryStore((state) => state.logsUpdateCount)
  const prevSpansCountRef = useRef(0)
  const prevLogsCountRef = useRef(0)

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

  // Fetch data function
  const fetchData = useCallback(async () => {
    try {
      const [statsData, tracesData] = await Promise.all([
        api.getStats(),
        api.getRecentTraces(10),
      ])
      setStats(statsData)
      setRecentTraces(tracesData.traces ?? [])
    } catch (err) {
      console.error('Failed to fetch data:', err)
    }
  }, [])

  // Initial fetch and polling
  useEffect(() => {
    const initialFetch = setTimeout(fetchData, 0)
    const interval = setInterval(fetchData, 10000)
    return () => {
      clearTimeout(initialFetch)
      clearInterval(interval)
    }
  }, [fetchData])

  // Refresh when new spans arrive via WebSocket (debounced)
  useEffect(() => {
    if (spansUpdateCount > prevSpansCountRef.current) {
      const timer = setTimeout(fetchData, 500)
      prevSpansCountRef.current = spansUpdateCount
      return () => clearTimeout(timer)
    }
  }, [spansUpdateCount, fetchData])

  // Refresh when new logs arrive via WebSocket (debounced)
  useEffect(() => {
    if (logsUpdateCount > prevLogsCountRef.current) {
      const timer = setTimeout(fetchData, 500)
      prevLogsCountRef.current = logsUpdateCount
      return () => clearTimeout(timer)
    }
  }, [logsUpdateCount, fetchData])

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
