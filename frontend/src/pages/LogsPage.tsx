import { useEffect, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Select } from '@/components/ui/select'
import { DateRangePicker } from '@/components/ui/date-range-picker'
import { DataPagination } from '@/components/ui/data-pagination'
import { api } from '@/lib/api'
import { isAbortError } from '@/lib/errors'
import { formatTimestamp, getSeverityColor, cn } from '@/lib/utils'
import { getServiceDisplayName } from '@/lib/metricMetadata'
import type { LogRecord } from '@/types/logs'
import { ChevronDown, ChevronUp, RefreshCw } from 'lucide-react'
import { useTelemetryStore } from '@/stores/telemetryStore'
import { useDebounce } from '@/hooks/useDebounce'
import { usePagination } from '@/hooks/usePagination'
import { useTimeSelection } from '@/hooks/useTimeSelection'
import { isAbsoluteTimeSelection, type TimeSelection } from '@/types/dashboard'
import { toast } from 'sonner'

const SEVERITY_OPTIONS = ['', 'TRACE', 'DEBUG', 'INFO', 'WARN', 'ERROR', 'FATAL']

// localStorage keys
const TIME_SELECTION_STORAGE_KEY = 'ai-observer-logs-timeselection'
const PAGE_SIZE_STORAGE_KEY = 'ai-observer-logs-pageSize'

export function LogsPage() {
  const [searchParams, setSearchParams] = useSearchParams()
  const [logs, setLogs] = useState<LogRecord[]>([])
  const [total, setTotal] = useState(0)
  const [services, setServices] = useState<string[]>([])
  const [loading, setLoading] = useState(true)
  const [service, setService] = useState(searchParams.get('service') || '')
  const [severity, setSeverity] = useState(searchParams.get('severity') || '')
  const [search, setSearch] = useState(searchParams.get('search') || '')
  const debouncedSearch = useDebounce(search, 200)
  const [expandedLog, setExpandedLog] = useState<number | null>(null)

  // Time selection with localStorage persistence
  const { timeSelection, setTimeSelection, fromTime, toTime } = useTimeSelection({
    storageKey: TIME_SELECTION_STORAGE_KEY,
    searchParams,
  })

  // Pagination state with localStorage persistence
  const { page, pageSize, offset, setPage, setPageSize, resetToFirstPage } = usePagination({
    defaultPageSize: 10,
    storageKey: PAGE_SIZE_STORAGE_KEY,
  })

  // Anchor time for tracking new logs (used by refresh button)
  const [anchorTime, setAnchorTime] = useState<Date>(() => new Date())

  // Real-time logs from WebSocket (kept separate, not merged)
  const recentLogs = useTelemetryStore((state) => state.recentLogs)
  const clearRecentLogs = useTelemetryStore((state) => state.clearRecentLogs)

  // Count new logs since anchor time
  const newLogsCount = recentLogs.filter(
    (log) => new Date(log.timestamp) > anchorTime
  ).length

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

    const fetchLogs = async () => {
      setLoading(true)
      try {
        const data = await api.getLogs({
          service: service || undefined,
          severity: severity || undefined,
          search: debouncedSearch || undefined,
          from: fromTime?.toISOString(),
          to: toTime.toISOString(),
          limit: pageSize,
          offset,
        }, { signal: abortController.signal })
        setLogs(data.logs ?? [])
        setTotal(data.total ?? 0)
      } catch (err) {
        if (isAbortError(err)) {
          return // Ignore abort errors
        }
        console.error('Failed to fetch logs:', err)
        toast.error('Failed to fetch logs')
      } finally {
        if (!abortController.signal.aborted) {
          setLoading(false)
        }
      }
    }
    fetchLogs()

    return () => abortController.abort()
  }, [service, severity, debouncedSearch, fromTime, toTime, pageSize, offset])

  const handleTimeSelectionChange = (selection: TimeSelection) => {
    resetToFirstPage()
    setTimeSelection(selection) // Also persists to localStorage

    // Update URL params
    const params: Record<string, string> = {}
    if (service) params.service = service
    if (severity) params.severity = severity
    if (search) params.search = search
    if (isAbsoluteTimeSelection(selection)) {
      params.from = selection.range.from.toISOString()
      params.to = selection.range.to.toISOString()
    } else {
      params.timeframe = selection.timeframe.value
    }
    setSearchParams(params)
  }

  const handleRefresh = () => {
    setAnchorTime(new Date())
    resetToFirstPage()
    clearRecentLogs()
  }

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-3xl font-bold tracking-tight">Logs</h1>
        <p className="text-muted-foreground">
          View and search log records from your AI coding tools
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
            <div className="w-32">
              <Select value={severity} onChange={(e) => {
                resetToFirstPage()
                setSeverity(e.target.value)
              }}>
                <option value="">All Levels</option>
                {SEVERITY_OPTIONS.filter(Boolean).map((s) => (
                  <option key={s} value={s}>
                    {s}
                  </option>
                ))}
              </Select>
            </div>
            <div className="flex-1">
              <Input
                placeholder="Search log messages..."
                value={search}
                onChange={(e) => {
                  resetToFirstPage()
                  setSearch(e.target.value)
                }}
              />
            </div>
          </div>
        </CardContent>
      </Card>

      {/* Logs List */}
      <Card>
        <CardHeader>
          <div className="flex items-center justify-between">
            <CardTitle>Logs</CardTitle>
            {newLogsCount > 0 && (
              <Button
                variant="outline"
                size="sm"
                onClick={handleRefresh}
                className="gap-2"
              >
                <RefreshCw className="h-3 w-3" />
                {newLogsCount} new {newLogsCount === 1 ? 'log' : 'logs'}
              </Button>
            )}
          </div>
        </CardHeader>
        <CardContent>
          {loading ? (
            <div className="py-8 text-center text-muted-foreground">
              Loading...
            </div>
          ) : logs.length === 0 ? (
            <div className="py-8 text-center text-muted-foreground">
              No logs found. Try adjusting your filters or configure your AI coding tool to send log telemetry.
            </div>
          ) : (
            <>
              <div className="space-y-1 font-mono text-sm">
                {logs.map((log, i) => {
                  // Create a unique key from timestamp, service, and trace context
                  const logKey = `${log.timestamp}-${log.serviceName}-${log.traceId || ''}-${log.spanId || ''}-${i}`
                  return (
                  <div key={logKey} className="border-b last:border-0">
                    <div
                      className={cn(
                        'flex items-start gap-3 py-2 cursor-pointer hover:bg-accent/50 rounded px-2 -mx-2',
                        expandedLog === i && 'bg-accent/50'
                      )}
                      onClick={() => setExpandedLog(expandedLog === i ? null : i)}
                      role="button"
                      tabIndex={0}
                      aria-expanded={expandedLog === i}
                      aria-controls={`log-details-${i}`}
                      onKeyDown={(e) => {
                        if (e.key === 'Enter' || e.key === ' ') {
                          e.preventDefault()
                          setExpandedLog(expandedLog === i ? null : i)
                        }
                      }}
                    >
                      <div className="w-40 shrink-0 text-muted-foreground text-xs">
                        {formatTimestamp(log.timestamp)}
                      </div>
                      <Badge
                        className={cn('shrink-0', getSeverityColor(log.severityText || ''))}
                      >
                        {log.severityText || 'UNKNOWN'}
                      </Badge>
                      <div className="w-28 shrink-0 text-muted-foreground truncate">
                        {getServiceDisplayName(log.serviceName)}
                      </div>
                      <div className="flex-1 truncate">{log.body}</div>
                      <Button
                        variant="ghost"
                        size="icon"
                        className="h-5 w-5 shrink-0"
                        aria-hidden="true"
                        tabIndex={-1}
                      >
                        {expandedLog === i ? (
                          <ChevronUp className="h-3 w-3" />
                        ) : (
                          <ChevronDown className="h-3 w-3" />
                        )}
                      </Button>
                    </div>

                    {expandedLog === i && (
                      <div id={`log-details-${i}`} className="pl-44 pb-4 space-y-2">
                        <div className="bg-muted rounded p-3">
                          <p className="whitespace-pre-wrap break-all">{log.body}</p>
                        </div>

                        {log.traceId && (
                          <div className="text-xs">
                            <span className="text-muted-foreground">Trace ID: </span>
                            <span className="font-mono">{log.traceId}</span>
                          </div>
                        )}

                        {log.spanId && (
                          <div className="text-xs">
                            <span className="text-muted-foreground">Span ID: </span>
                            <span className="font-mono">{log.spanId}</span>
                          </div>
                        )}

                        {log.logAttributes && Object.keys(log.logAttributes).length > 0 && (
                          <div className="text-xs">
                            <p className="text-muted-foreground mb-1">Attributes:</p>
                            <div className="bg-muted rounded p-2 space-y-1">
                              {Object.entries(log.logAttributes).map(([key, value]) => (
                                <div key={key}>
                                  <span className="text-muted-foreground">{key}: </span>
                                  <span>{value}</span>
                                </div>
                              ))}
                            </div>
                          </div>
                        )}
                      </div>
                    )}
                  </div>
                )})}
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
