import { useEffect, useState, useMemo } from 'react'
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
import { formatDuration, formatTimestamp, getStatusColor, cn } from '@/lib/utils'
import { getServiceDisplayName } from '@/lib/metricMetadata'
import type { TraceOverview, Span } from '@/types/traces'
import { ChevronRight, ChevronDown, RefreshCw } from 'lucide-react'
import {
  TIMEFRAME_OPTIONS,
  isAbsoluteTimeSelection,
  type TimeSelection,
} from '@/types/dashboard'
import { useDebounce } from '@/hooks/useDebounce'
import { usePagination } from '@/hooks/usePagination'
import { getLocalStorageValue } from '@/hooks/useLocalStorage'
import { toast } from 'sonner'
import { useTelemetryStore } from '@/stores/telemetryStore'
import { calculateInterval, calculateTickInterval } from '@/lib/timeUtils'

// localStorage keys
const TIME_SELECTION_STORAGE_KEY = 'ai-observer-traces-timeselection'
const PAGE_SIZE_STORAGE_KEY = 'ai-observer-traces-pageSize'

export function TracesPage() {
  const [searchParams, setSearchParams] = useSearchParams()
  const [traces, setTraces] = useState<TraceOverview[]>([])
  const [total, setTotal] = useState(0)
  const [expandedTraces, setExpandedTraces] = useState<Set<string>>(new Set())
  const [spansMap, setSpansMap] = useState<Map<string, Span[]>>(new Map())
  const [services, setServices] = useState<string[]>([])
  const [loading, setLoading] = useState(true)
  const [service, setService] = useState(searchParams.get('service') || '')
  const [search, setSearch] = useState(searchParams.get('search') || '')
  const debouncedSearch = useDebounce(search, 200)

  // Get initial time selection from URL or localStorage
  const getInitialTimeSelection = (): TimeSelection => {
    // Check for absolute date range in URL
    const fromParam = searchParams.get('from')
    const toParam = searchParams.get('to')
    if (fromParam && toParam) {
      const from = new Date(fromParam)
      const to = new Date(toParam)
      if (!isNaN(from.getTime()) && !isNaN(to.getTime())) {
        const intervalSeconds = calculateInterval(from, to)
        const rangeDays = (to.getTime() - from.getTime()) / (1000 * 60 * 60 * 24)
        const bucketCount = Math.ceil((rangeDays * 86400) / intervalSeconds)
        return {
          type: 'absolute',
          range: {
            from,
            to,
            intervalSeconds,
            tickInterval: calculateTickInterval(bucketCount),
          },
        }
      }
    }

    // Check for relative timeframe in URL or localStorage
    const storedSelection = getLocalStorageValue<{ type: string; timeframeValue: string } | null>(TIME_SELECTION_STORAGE_KEY, null)
    const timeframeParam = searchParams.get('timeframe')
      || storedSelection?.timeframeValue
      || '7d'
    const timeframe = TIMEFRAME_OPTIONS.find((t) => t.value === timeframeParam)
      || TIMEFRAME_OPTIONS.find((t) => t.value === '7d')!
    return { type: 'relative', timeframe }
  }

  const [timeSelection, setTimeSelectionState] = useState<TimeSelection>(getInitialTimeSelection)

  // Real-time data from WebSocket
  const recentSpans = useTelemetryStore((state) => state.recentSpans)
  const clearRecentSpans = useTelemetryStore((state) => state.clearRecentSpans)

  // Pagination state with localStorage persistence
  const { page, pageSize, offset, setPage, setPageSize, resetToFirstPage } = usePagination({
    defaultPageSize: 10,
    storageKey: PAGE_SIZE_STORAGE_KEY,
  })

  // Anchor time for stable pagination (reset on filter changes)
  const [anchorTime, setAnchorTime] = useState<Date>(new Date())

  // Calculate time range from selected time selection
  const fromTime = useMemo(() => {
    if (isAbsoluteTimeSelection(timeSelection)) {
      return timeSelection.range.from
    }
    return new Date(anchorTime.getTime() - timeSelection.timeframe.durationSeconds * 1000)
  }, [timeSelection, anchorTime])

  const toTime = useMemo(() => {
    if (isAbsoluteTimeSelection(timeSelection)) {
      return timeSelection.range.to
    }
    return anchorTime
  }, [timeSelection, anchorTime])

  // Count unique new traces since anchor time
  const newTracesCount = useMemo(() => {
    const newTraceIds = new Set<string>()
    for (const span of recentSpans) {
      if (new Date(span.timestamp) > anchorTime) {
        newTraceIds.add(span.traceId)
      }
    }
    return newTraceIds.size
  }, [recentSpans, anchorTime])

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

  // Reset pagination and anchor time when filters change
  useEffect(() => {
    resetToFirstPage()
    setAnchorTime(new Date())
  }, [service, debouncedSearch, timeSelection, resetToFirstPage])

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
        if (err instanceof Error && err.name === 'AbortError') {
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
    const abortController = new AbortController()

    // Fetch spans for each expanded trace that isn't already loaded
    const fetchMissingSpans = async () => {
      for (const traceId of expandedTraces) {
        if (!spansMap.has(traceId)) {
          try {
            const data = await api.getTraceSpans(traceId, { signal: abortController.signal })
            if (!abortController.signal.aborted) {
              setSpansMap(prev => new Map(prev).set(traceId, data.spans ?? []))
            }
          } catch (err) {
            if (err instanceof Error && err.name === 'AbortError') {
              return
            }
            console.error('Failed to fetch spans:', err)
            toast.error('Failed to fetch trace spans')
          }
        }
      }
    }
    fetchMissingSpans()

    return () => abortController.abort()
  }, [expandedTraces, spansMap])

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
    setService(value)
    updateSearchParams({ service: value })
  }

  const handleTimeSelectionChange = (selection: TimeSelection) => {
    setTimeSelectionState(selection)

    // Persist relative selections to localStorage
    if (!isAbsoluteTimeSelection(selection)) {
      try {
        localStorage.setItem(
          TIME_SELECTION_STORAGE_KEY,
          JSON.stringify({ type: 'relative', timeframeValue: selection.timeframe.value })
        )
      } catch {
        // Ignore storage errors
      }
    }

    updateSearchParams({ timeSelection: selection })
  }

  const handleRefresh = () => {
    setAnchorTime(new Date())
    resetToFirstPage()
    clearRecentSpans()
  }

  const toggleTrace = (traceId: string) => {
    setExpandedTraces(prev => {
      const next = new Set(prev)
      if (next.has(traceId)) {
        next.delete(traceId)
      } else {
        next.add(traceId)
      }
      return next
    })
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
        {newTracesCount > 0 && (
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
                onChange={(e) => setSearch(e.target.value)}
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
                {traces.map((trace) => {
                  const isExpanded = expandedTraces.has(trace.traceId)
                  const spans = spansMap.get(trace.traceId)

                  return (
                    <div key={trace.traceId} className="rounded-lg border">
                      {/* Trace header (clickable) */}
                      <div
                        className={cn(
                          'p-3 cursor-pointer transition-colors hover:bg-accent',
                          isExpanded && 'bg-accent'
                        )}
                        onClick={() => toggleTrace(trace.traceId)}
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
