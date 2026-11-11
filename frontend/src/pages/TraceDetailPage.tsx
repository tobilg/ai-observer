import { useEffect, useState, useMemo } from 'react'
import { useParams, useNavigate } from 'react-router-dom'
import { Card, CardContent } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { WaterfallView } from '@/components/traces/WaterfallView'
import { api } from '@/lib/api'
import { isAbortError } from '@/lib/errors'
import { formatDuration, formatTimestamp, getStatusColor } from '@/lib/utils'
import { getServiceDisplayName } from '@/lib/metricMetadata'
import type { Span, TraceKind, TraceOverview } from '@/types/traces'
import { ArrowLeft } from 'lucide-react'
import { toast } from 'sonner'

export function TraceDetailPage() {
  const { kind, id } = useParams<{ kind: TraceKind; id: string }>()
  const navigate = useNavigate()
  const [spans, setSpans] = useState<Span[]>([])
  // The overview of all spans of the trace and the cursor of the next page:
  // a trace is loaded a page at a time (Codex turns have thousands of spans)
  const [overview, setOverview] = useState<TraceOverview | null>(null)
  const [nextCursor, setNextCursor] = useState<string | undefined>()
  const [loadingMore, setLoadingMore] = useState(false)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    if (!id || !kind) return

    const abortController = new AbortController()

    const fetchSpans = async () => {
      setLoading(true)
      setError(null)
      setSpans([])
      setOverview(null)
      setNextCursor(undefined)
      try {
        const data = await api.getTraceSpans(id, kind, { signal: abortController.signal })
        if (data.spans && data.spans.length > 0) {
          setSpans(data.spans)
          setOverview(data.trace ?? null)
          setNextCursor(data.hasMore ? data.nextCursor : undefined)
        } else {
          setError('No spans found for this trace')
        }
      } catch (err) {
        if (isAbortError(err)) {
          return
        }
        console.error('Failed to fetch trace spans:', err)
        setError('Failed to load trace')
        toast.error('Failed to load trace')
      } finally {
        if (!abortController.signal.aborted) {
          setLoading(false)
        }
      }
    }

    fetchSpans()

    return () => abortController.abort()
  }, [id, kind])

  const loadMore = async () => {
    if (!id || !kind || !nextCursor || loadingMore) return
    setLoadingMore(true)
    try {
      const data = await api.getTraceSpans(id, kind, undefined, nextCursor)
      setSpans((prev) => [...prev, ...(data.spans ?? [])])
      setNextCursor(data.hasMore ? data.nextCursor : undefined)
    } catch (err) {
      console.error('Failed to fetch more trace spans:', err)
      toast.error('Failed to load more spans')
    } finally {
      setLoadingMore(false)
    }
  }

  // The server's overview of the whole trace, or one computed from the spans
  const traceOverview: TraceOverview | null = useMemo(() => {
    if (spans.length === 0) return null
    if (overview) return overview

    // Prefer the requested row root; grouped Codex rows may include same-turn sibling roots.
    const spanIds = new Set(spans.map(s => s.spanId))
    const rootSpan = spans.find(s => s.spanId === id) ?? spans.find(s => !s.parentSpanId || !spanIds.has(s.parentSpanId))
    const isCodexTurn = kind === 'codex_operation' && spans.some(s => s.spanAttributes?.turn_id)

    // Calculate total duration from earliest start to latest end
    let minStart = Number.MAX_SAFE_INTEGER
    let maxEnd = Number.MIN_SAFE_INTEGER
    let hasError = false

    for (const span of spans) {
      const start = new Date(span.timestamp).getTime()
      const end = start + span.duration / 1_000_000
      if (start < minStart) minStart = start
      if (end > maxEnd) maxEnd = end
      if (span.statusCode === 'ERROR') hasError = true
    }

    const totalDuration = (maxEnd - minStart) * 1_000_000 // Convert back to nanoseconds

    return {
      id: id!,
      kind: kind!,
      traceId: rootSpan?.traceId || spans[0]?.traceId || id!,
      rootSpanId: rootSpan?.spanId || spans[0]?.spanId || id!,
      rootSpan: rootSpan?.spanName || 'Unknown',
      serviceName: rootSpan?.serviceName || spans[0]?.serviceName || 'Unknown',
      groupLevel: isCodexTurn ? 'codex_turn' : kind === 'codex_operation' ? 'codex_operation' : undefined,
      startTime: rootSpan?.timestamp || spans[0]?.timestamp || new Date().toISOString(),
      duration: totalDuration,
      spanCount: spans.length,
      status: hasError ? 'ERROR' : 'OK'
    }
  }, [spans, overview, id, kind])

  const handleBack = () => {
    navigate('/traces')
  }

  const traceLabel = traceOverview?.groupLevel === 'codex_turn'
    ? 'Codex Turn'
    : kind === 'codex_operation'
      ? 'Codex Operation'
      : 'Trace'

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center gap-4">
        <button
          onClick={handleBack}
          className="p-2 -ml-2 rounded-md hover:bg-accent transition-colors"
          aria-label="Back to traces"
        >
          <ArrowLeft className="h-5 w-5" />
        </button>
        <h1 className="text-2xl font-bold tracking-tight font-mono">
          {traceLabel} {id}
        </h1>
      </div>

      {/* Content */}
      {loading ? (
        <Card>
          <CardContent className="py-8">
            <div className="text-center text-muted-foreground">
              Loading trace...
            </div>
          </CardContent>
        </Card>
      ) : error ? (
        <Card>
          <CardContent className="py-8">
            <div className="text-center text-muted-foreground">
              {error}
            </div>
          </CardContent>
        </Card>
      ) : traceOverview ? (
        <>
          {/* Trace Summary */}
          <Card>
            <CardContent className="py-4">
              <div className="flex flex-wrap items-center gap-x-6 gap-y-2 text-sm">
                <div className="flex items-center gap-2">
                  <span className="text-muted-foreground">Status:</span>
                  <Badge className={getStatusColor(traceOverview.status)}>
                    {traceOverview.status}
                  </Badge>
                </div>
                <div className="flex items-center gap-2">
                  <span className="text-muted-foreground">Service:</span>
                  <span className="font-medium">{getServiceDisplayName(traceOverview.serviceName)}</span>
                </div>
                {traceOverview.kind === 'codex_operation' && (
                  <div className="flex items-center gap-2">
                    <span className="text-muted-foreground">Trace ID:</span>
                    <span className="font-medium font-mono">{traceOverview.traceId}</span>
                  </div>
                )}
                <div className="flex items-center gap-2">
                  <span className="text-muted-foreground">Root Span:</span>
                  <span className="font-medium font-mono">{traceOverview.rootSpanId}</span>
                </div>
                <div className="flex items-center gap-2">
                  <span className="text-muted-foreground">Duration:</span>
                  <span className="font-medium font-mono">{formatDuration(traceOverview.duration)}</span>
                </div>
                <div className="flex items-center gap-2">
                  <span className="text-muted-foreground">Spans:</span>
                  <span className="font-medium">{traceOverview.spanCount.toLocaleString('en-US')}</span>
                </div>
                <div className="flex items-center gap-2">
                  <span className="text-muted-foreground">Started:</span>
                  <span className="font-medium">{formatTimestamp(traceOverview.startTime)}</span>
                </div>
              </div>
            </CardContent>
          </Card>

          {/* Waterfall View */}
          <Card>
            <CardContent className="py-4">
              <WaterfallView spans={spans} trace={traceOverview} />
              {nextCursor && (
                <div className="mt-4 flex flex-wrap items-center justify-center gap-3 border-t pt-4 text-sm text-muted-foreground">
                  <span>
                    Showing the first {spans.length.toLocaleString('en-US')} of {traceOverview.spanCount.toLocaleString('en-US')} spans
                  </span>
                  <Button variant="outline" size="sm" onClick={loadMore} disabled={loadingMore}>
                    {loadingMore ? 'Loading...' : 'Load more spans'}
                  </Button>
                </div>
              )}
            </CardContent>
          </Card>
        </>
      ) : null}
    </div>
  )
}
