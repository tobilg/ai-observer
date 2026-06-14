import { useEffect, useState } from 'react'
import { useSearchParams, useNavigate } from 'react-router-dom'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { Select } from '@/components/ui/select'
import { DateRangePicker } from '@/components/ui/date-range-picker'
import { DataPagination } from '@/components/ui/data-pagination'
import { api } from '@/lib/api'
import { isAbortError } from '@/lib/errors'
import { formatTimestamp, cn } from '@/lib/utils'
import { getServiceDisplayName, getServiceIcon } from '@/lib/metricMetadata'
import type { Session } from '@/types/sessions'
import { MessageSquare, ChevronRight } from 'lucide-react'
import { usePagination } from '@/hooks/usePagination'
import { useTimeSelection } from '@/hooks/useTimeSelection'
import { isAbsoluteTimeSelection, type TimeSelection } from '@/types/dashboard'
import { toast } from 'sonner'

// localStorage keys
const TIME_SELECTION_STORAGE_KEY = 'ai-observer-sessions-timeselection'
const PAGE_SIZE_STORAGE_KEY = 'ai-observer-sessions-pageSize'

export function SessionsPage() {
  const [searchParams, setSearchParams] = useSearchParams()
  const navigate = useNavigate()
  const [sessions, setSessions] = useState<Session[]>([])
  const [total, setTotal] = useState(0)
  const [services, setServices] = useState<string[]>([])
  const [loading, setLoading] = useState(true)
  const [service, setService] = useState(searchParams.get('service') || '')

  // Time selection with localStorage persistence
  const { timeSelection, setTimeSelection, fromTime, toTime } = useTimeSelection({
    storageKey: TIME_SELECTION_STORAGE_KEY,
    searchParams,
  })

  // Pagination state with localStorage persistence
  const { page, pageSize, offset, setPage, setPageSize, resetToFirstPage } = usePagination({
    defaultPageSize: 20,
    storageKey: PAGE_SIZE_STORAGE_KEY,
  })

  useEffect(() => {
    const fetchServices = async () => {
      try {
        const data = await api.getServices()
        setServices(data.services ?? [])
      } catch (err) {
        console.error('Failed to fetch services:', err)
        toast.error('Failed to fetch services')
      }
    }
    fetchServices()
  }, [])

  useEffect(() => {
    const abortController = new AbortController()

    const fetchSessions = async () => {
      setLoading(true)
      try {
        const data = await api.getSessions({
          service: service || undefined,
          from: fromTime?.toISOString(),
          to: toTime.toISOString(),
          limit: pageSize,
          offset,
        }, { signal: abortController.signal })
        setSessions(data.sessions ?? [])
        setTotal(data.total ?? 0)
      } catch (err) {
        if (isAbortError(err)) {
          return
        }
        console.error('Failed to fetch sessions:', err)
        toast.error('Failed to fetch sessions')
      } finally {
        if (!abortController.signal.aborted) {
          setLoading(false)
        }
      }
    }
    fetchSessions()

    return () => abortController.abort()
  }, [service, fromTime, toTime, pageSize, offset])

  const handleTimeSelectionChange = (selection: TimeSelection) => {
    resetToFirstPage()
    setTimeSelection(selection) // Also persists to localStorage

    // Update URL params
    const params: Record<string, string> = {}
    if (service) params.service = service
    if (isAbsoluteTimeSelection(selection)) {
      params.from = selection.range.from.toISOString()
      params.to = selection.range.to.toISOString()
    } else {
      params.timeframe = selection.timeframe.value
    }
    setSearchParams(params)
  }

  const handleSessionClick = (sessionId: string) => {
    navigate(`/sessions/${encodeURIComponent(sessionId)}`)
  }

  const formatDuration = (startTime: string, lastTime: string) => {
    const start = new Date(startTime)
    const end = new Date(lastTime)
    const durationMs = end.getTime() - start.getTime()

    if (durationMs < 60000) {
      return `${Math.round(durationMs / 1000)}s`
    } else if (durationMs < 3600000) {
      return `${Math.round(durationMs / 60000)}m`
    } else {
      const hours = Math.floor(durationMs / 3600000)
      const mins = Math.round((durationMs % 3600000) / 60000)
      return `${hours}h ${mins}m`
    }
  }

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-3xl font-bold tracking-tight">Sessions</h1>
        <p className="text-muted-foreground">
          View session transcripts from your AI coding tools
        </p>
      </div>

      {/* Filters */}
      <Card>
        <CardContent className="pt-6">
          <div className="flex gap-4">
            <div className="flex items-center gap-2">
              <label className="text-sm text-muted-foreground whitespace-nowrap">Timeframe</label>
              <DateRangePicker
                value={timeSelection}
                onChange={handleTimeSelectionChange}
              />
            </div>
            <div className="w-48">
              <Select value={service} onChange={(e) => {
                resetToFirstPage()
                setService(e.target.value)
              }}>
                <option value="">All Services</option>
                {services.map((s) => (
                  <option key={s} value={s}>
                    {getServiceDisplayName(s)}
                  </option>
                ))}
              </Select>
            </div>
          </div>
        </CardContent>
      </Card>

      {/* Sessions List */}
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <MessageSquare className="h-5 w-5" />
            Sessions
          </CardTitle>
        </CardHeader>
        <CardContent>
          {loading ? (
            <div className="py-8 text-center text-muted-foreground">
              Loading...
            </div>
          ) : sessions.length === 0 ? (
            <div className="py-8 text-center text-muted-foreground">
              No sessions found. Import session data using <code className="bg-muted px-1 rounded">ai-observer import</code> or configure your AI coding tools to send telemetry.
            </div>
          ) : (
            <>
              <div className="space-y-2">
                {sessions.map((session) => {
                  const ServiceIcon = getServiceIcon(session.serviceName)
                  return (
                    <div
                      key={session.sessionId}
                      className={cn(
                        'flex items-center gap-4 p-4 rounded-lg border cursor-pointer',
                        'hover:bg-accent/50 transition-colors'
                      )}
                      onClick={() => handleSessionClick(session.sessionId)}
                      role="button"
                      tabIndex={0}
                      onKeyDown={(e) => {
                        if (e.key === 'Enter' || e.key === ' ') {
                          e.preventDefault()
                          handleSessionClick(session.sessionId)
                        }
                      }}
                    >
                      {/* Service Icon */}
                      <div className="shrink-0">
                        <ServiceIcon className="h-8 w-8 text-muted-foreground" />
                      </div>

                      {/* Session Info */}
                      <div className="flex-1 min-w-0">
                        <div className="flex items-center gap-2 mb-1">
                          <span className="font-medium truncate">
                            {session.sessionId}
                          </span>
                          <Badge variant="outline" className="shrink-0">
                            {getServiceDisplayName(session.serviceName)}
                          </Badge>
                        </div>
                        <div className="flex items-center gap-4 text-sm text-muted-foreground">
                          <span>{formatTimestamp(session.startTime)}</span>
                          <span className="text-xs">
                            Duration: {formatDuration(session.startTime, session.lastTime)}
                          </span>
                          {session.model && (
                            <span className="text-xs truncate max-w-[200px]">
                              Model: {session.model}
                            </span>
                          )}
                        </div>
                      </div>

                      {/* Message Count */}
                      <div className="shrink-0 text-right">
                        <div className="font-medium">{session.messageCount}</div>
                        <div className="text-xs text-muted-foreground">messages</div>
                      </div>

                      {/* Arrow */}
                      <ChevronRight className="h-5 w-5 text-muted-foreground shrink-0" />
                    </div>
                  )
                })}
              </div>
              <DataPagination
                page={page}
                pageSize={pageSize}
                total={total}
                onPageChange={setPage}
                onPageSizeChange={setPageSize}
              />
            </>
          )}
        </CardContent>
      </Card>
    </div>
  )
}
