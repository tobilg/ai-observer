import { useEffect, useMemo, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Select } from '@/components/ui/select'
import { DateRangePicker } from '@/components/ui/date-range-picker'
import { DataPagination } from '@/components/ui/data-pagination'
import { WaterfallView } from '@/components/traces/WaterfallView'
import { api } from '@/lib/api'
import { isAbortError } from '@/lib/errors'
import { formatDuration, formatTimestamp, getStatusColor, cn } from '@/lib/utils'
import { getServiceDisplayName } from '@/lib/metricMetadata'
import type { TraceOverview, Span } from '@/types/traces'
import { ChevronRight, ChevronDown, RefreshCw } from 'lucide-react'
import { isAbsoluteTimeSelection, type TimeSelection } from '@/types/dashboard'
import { useDebounce } from '@/hooks/useDebounce'
import { usePagination } from '@/hooks/usePagination'
import { useTimeSelection } from '@/hooks/useTimeSelection'
import { toast } from 'sonner'
import { useTelemetryStore } from '@/stores/telemetryStore'

// localStorage keys
const TIME_SELECTION_STORAGE_KEY = 'ai-observer-traces-timeselection'
const PAGE_SIZE_STORAGE_KEY = 'ai-observer-traces-pageSize'

function getTraceKind(trace: TraceOverview) {
  return trace.kind ?? 'otel_trace'
}

function getTraceRequestId(trace: TraceOverview) {
  if (getTraceKind(trace) === 'codex_operation') {
    return trace.id || trace.rootSpanId || trace.traceId
  }
  return trace.traceId || trace.id || trace.rootSpanId
}

function getTraceRowKey(trace: TraceOverview, index: number) {
  return [
    getTraceKind(trace),
    getTraceRequestId(trace),
    trace.traceId,
    trace.rootSpanId,
    trace.startTime,
    index,
  ].filter(Boolean).join(':')
}

export function TracesPage() {
  const [searchParams, setSearchParams] = useSearchParams()
  const [traces, setTraces] = useState<TraceOverview[]>([])
  const [total, setTotal] = useState(0)
  const [expandedTraceKey, setExpandedTraceKey] = useState<string | null>(null)
  const [spansMap, setSpansMap] = useState<Map<string, Span[]>>(new Map())
  const [services, setServices] = useState<string[]>([])
  const [loading, setLoading] = useState(true)
  const [service, setService] = useState(searchParams.get('service') || '')
  const [search, setSearch] = useState(searchParams.get('search') || '')
  const debouncedSearch = useDebounce(search, 200)

  // Time selection with localStorage persistence
  const { timeSelection, setTimeSelection, fromTime, toTime } = useTimeSelection({
    storageKey: TIME_SELECTION_STORAGE_KEY,
    searchParams,
  })

  // Real-time data from WebSocket
  const recentSpans = useTelemetryStore((state) => state.recentSpans)
  const clearRecentSpans = useTelemetryStore((state) => state.clearRecentSpans)

  // Pagination state with localStorage persistence
  const { page, pageSize, offset, setPage, setPageSize, resetToFirstPage } = usePagination({
    defaultPageSize: 10,
    storageKey: PAGE_SIZE_STORAGE_KEY,
  })

  // Anchor time for tracking new traces (used by refresh button)
  const [anchorTime, setAnchorTime] = useState<Date>(() => new Date())

  const [newTracesCount, setNewTracesCount] = useState(0)
  const hasNewSpans = useMemo(
    () => recentSpans.some((span) => new Date(span.timestamp) > anchorTime),
    [recentSpans, anchorTime]
  )

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

    const fetchTraces = async () => {
      setLoading(true)
      try {
        const data = await api.getTraces({
          service: service || undefined,
          search: debouncedSearch || undefined,
          from: fromTime.toISOString(),
          to: toTime.toISOString(),
          limit: pageSize,
          offset,
        }, { signal: abortController.signal })
        setTraces(data.traces ?? [])
        setTotal(data.total ?? 0)
      } catch (err) {
        if (isAbortError(err)) {
          return // Ignore abort errors
        }
        console.error('Failed to fetch traces:', err)
        toast.error('Failed to fetch traces')
      } finally {
        if (!abortController.signal.aborted) {
          setLoading(false)
        }
      }
    }
    fetchTraces()

    return () => abortController.abort()
  }, [service, debouncedSearch, fromTime, toTime, pageSize, offset])

  useEffect(() => {
    if (!hasNewSpans) {
      return
    }

    const abortController = new AbortController()
    const timeout = window.setTimeout(async () => {
      try {
        const data = await api.getTraces({
          service: service || undefined,
          search: debouncedSearch || undefined,
          from: anchorTime.toISOString(),
          to: new Date().toISOString(),
          limit: 1,
          offset: 0,
        }, { signal: abortController.signal })
        if (!abortController.signal.aborted) {
          setNewTracesCount(data.total ?? 0)
        }
      } catch (err) {
        if (isAbortError(err)) {
          return
        }
        console.error('Failed to count new traces:', err)
      }
    }, 250)

    return () => {
      window.clearTimeout(timeout)
      abortController.abort()
    }
  }, [hasNewSpans, anchorTime, service, debouncedSearch])

  useEffect(() => {
    const abortController = new AbortController()

    // Fetch spans only for the currently expanded trace.
    const fetchMissingSpans = async () => {
      if (!expandedTraceKey || spansMap.has(expandedTraceKey)) {
        return
      }

      const expandedTrace = traces.find((trace, index) => getTraceRowKey(trace, index) === expandedTraceKey)
      if (!expandedTrace) {
        return
      }

      try {
        const data = await api.getTraceSpans(getTraceRequestId(expandedTrace), getTraceKind(expandedTrace), { signal: abortController.signal })
        if (!abortController.signal.aborted) {
          setSpansMap(prev => new Map(prev).set(expandedTraceKey, data.spans ?? []))
        }
      } catch (err) {
        if (isAbortError(err)) {
          return
        }
        console.error('Failed to fetch spans:', err)
        toast.error('Failed to fetch trace spans')
      }
    }
    fetchMissingSpans()

    return () => abortController.abort()
  }, [expandedTraceKey, spansMap, traces])

  const updateSearchParams = (updates: { service?: string; search?: string; timeSelection?: TimeSelection }) => {
    const params: Record<string, string> = {}
    const newService = updates.service ?? service
    const newSearch = updates.search ?? search
    const newTimeSelection = updates.timeSelection ?? timeSelection

    if (newService) params.service = newService
    if (newSearch) params.search = newSearch

    // Handle time selection params
    if (isAbsoluteTimeSelection(newTimeSelection)) {
      params.from = newTimeSelection.range.from.toISOString()
      params.to = newTimeSelection.range.to.toISOString()
    } else {
      params.timeframe = newTimeSelection.timeframe.value
    }

    setSearchParams(params)
  }

  const handleServiceChange = (value: string) => {
    resetToFirstPage()
    setService(value)
    updateSearchParams({ service: value })
  }

  const handleTimeSelectionChange = (selection: TimeSelection) => {
    resetToFirstPage()
    setTimeSelection(selection) // Also persists to localStorage
    updateSearchParams({ timeSelection: selection })
  }

  const handleRefresh = () => {
    setAnchorTime(new Date())
    setNewTracesCount(0)
    setExpandedTraceKey(null)
    resetToFirstPage()
    clearRecentSpans()
  }

  const toggleTrace = (traceKey: string) => {
    setExpandedTraceKey(prev => prev === traceKey ? null : traceKey)
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">Traces</h1>
          <p className="text-muted-foreground">
            View and analyze distributed traces
          </p>
        </div>
        {hasNewSpans && newTracesCount > 0 && (
          <Button
            variant="outline"
            size="sm"
            onClick={handleRefresh}
            className="gap-2"
          >
            <RefreshCw className="h-3 w-3" />
            {newTracesCount} new {newTracesCount === 1 ? 'trace' : 'traces'}
          </Button>
        )}
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
              <Select
                value={service}
                onChange={(e) => handleServiceChange(e.target.value)}
              >
                <option value="">All Services</option>
                {services.map((s) => (
                  <option key={s} value={s}>
                    {getServiceDisplayName(s)}
                  </option>
                ))}
              </Select>
            </div>
            <div className="flex-1">
              <Input
                placeholder="Search spans, errors, attributes..."
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

      {/* Traces List */}
      <Card>
        <CardHeader>
          <CardTitle>Traces</CardTitle>
        </CardHeader>
        <CardContent>
          {loading ? (
            <div className="py-8 text-center text-muted-foreground">
              Loading...
            </div>
          ) : traces.length === 0 ? (
            <div className="py-8 text-center text-muted-foreground">
              No traces found
            </div>
          ) : (
            <>
              <div className="space-y-2">
                {traces.map((trace, index) => {
                  const traceKey = getTraceRowKey(trace, index)
                  const isExpanded = expandedTraceKey === traceKey
                  const spans = spansMap.get(traceKey)

                  return (
                    <div key={traceKey} className="rounded-lg border">
                      {/* Trace header (clickable) */}
                      <div
                        className={cn(
                          'p-3 cursor-pointer transition-colors hover:bg-accent',
                          isExpanded && 'bg-accent'
                        )}
                        onClick={() => toggleTrace(traceKey)}
                      >
                        <div className="flex items-center justify-between">
                          <div className="flex items-center gap-2 min-w-0 flex-1">
                            <Badge className={getStatusColor(trace.status)}>
                              {trace.status}
                            </Badge>
                            <span className="font-medium text-sm truncate" title={trace.rootSpan}>
                              {trace.rootSpan}
                            </span>
                          </div>
                          {isExpanded ? (
                            <ChevronDown className="h-4 w-4 text-muted-foreground shrink-0" />
                          ) : (
                            <ChevronRight className="h-4 w-4 text-muted-foreground shrink-0" />
                          )}
                        </div>
                        <div className="mt-2 flex items-center justify-between text-xs text-muted-foreground">
                          <span>{getServiceDisplayName(trace.serviceName)}</span>
                          <span>{formatDuration(trace.duration)}</span>
                        </div>
                        <div className="mt-1 flex items-center justify-between text-xs text-muted-foreground">
                          <span>{trace.spanCount} spans</span>
                          <span>{formatTimestamp(trace.startTime)}</span>
                        </div>
                      </div>

                      {/* Waterfall (expanded) */}
                      {isExpanded && (
                        <div className="border-t p-3">
                          {!spans ? (
                            <div className="py-4 text-center text-muted-foreground">
                              Loading spans...
                            </div>
                          ) : spans.length === 0 ? (
                            <div className="py-4 text-center text-muted-foreground">
                              No spans found
                            </div>
                          ) : (
                            <WaterfallView spans={spans} trace={trace} />
                          )}
                        </div>
                      )}
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
