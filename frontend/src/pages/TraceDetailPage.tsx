import { useCallback, useMemo, type ReactNode } from 'react'
import { Link, useParams } from 'react-router-dom'
import { ArrowLeft, MessagesSquare, RefreshCw, ScrollText } from 'lucide-react'
import { Card, CardContent } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { CopyButton } from '@/components/common/copy-button'
import { EmptyState } from '@/components/common/empty-state'
import { ListSkeleton } from '@/components/common/list-skeleton'
import { PageHeader } from '@/components/common/page-header'
import { QueryError } from '@/components/common/query-error'
import { WaterfallView } from '@/components/traces/WaterfallView'
import { useServiceNames } from '@/hooks/useServiceNames'
import { useBackLink } from '@/hooks/useBackLink'
import { useListLinkState } from '@/hooks/useListLinkState'
import { sessionPath, traceLogsPath } from '@/lib/listLinks'
import { errorText } from '@/lib/errors'
import { formatCount, formatDurationNanos } from '@/lib/format'
import { useTraceSpans } from '@/lib/queries'
import { formatTimestamp, getStatusColor } from '@/lib/utils'
import type { Span, TraceKind, TraceOverview } from '@/types/traces'

const TRACE_KINDS: readonly TraceKind[] = ['otel_trace', 'codex_operation']

// An overview computed from the loaded spans, for responses without one
function overviewFromSpans(spans: Span[], id: string, kind: TraceKind): TraceOverview {
  // Prefer the requested row root; grouped Codex rows may include same-turn sibling roots
  const spanIds = new Set(spans.map((s) => s.spanId))
  const rootSpan = spans.find((s) => s.spanId === id) ?? spans.find((s) => !s.parentSpanId || !spanIds.has(s.parentSpanId))
  const isCodexTurn = kind === 'codex_operation' && spans.some((s) => s.spanAttributes?.turn_id)

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
  const sessions = new Set(spans.map((s) => s.sessionId).filter(Boolean))

  return {
    id,
    kind,
    traceId: rootSpan?.traceId || spans[0]?.traceId || id,
    rootSpanId: rootSpan?.spanId || spans[0]?.spanId || id,
    rootSpan: rootSpan?.spanName || 'Unknown',
    serviceName: rootSpan?.serviceName || spans[0]?.serviceName || 'Unknown',
    groupLevel: isCodexTurn ? 'codex_turn' : kind === 'codex_operation' ? 'codex_operation' : undefined,
    startTime: rootSpan?.timestamp || spans[0]?.timestamp || new Date().toISOString(),
    duration: (maxEnd - minStart) * 1_000_000,
    spanCount: spans.length,
    status: hasError ? 'ERROR' : 'OK',
    sessionId: sessions.size === 1 ? [...sessions][0] : undefined,
  }
}

function SummaryItem({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="flex min-w-0 items-center gap-2">
      <span className="text-muted-foreground">{label}:</span>
      {children}
    </div>
  )
}

export function TraceDetailPage() {
  const params = useParams<{ kind: string; id: string }>()
  const id = params.id ?? ''
  const kind = TRACE_KINDS.find((k) => k === params.kind)
  // Links to sessions come back here
  const linkState = useListLinkState()
  const back = useBackLink('/traces')
  const { displayName } = useServiceNames()

  // A page (1,000 spans) at a time: Codex turns have over 100,000. The key
  // holds the trace, so a page of another trace never mixes in.
  const query = useTraceSpans(id, kind ?? 'otel_trace', { enabled: Boolean(id && kind) })
  const spans = useMemo(() => query.data?.pages.flatMap((page) => page.spans ?? []) ?? [], [query.data])
  const serverOverview = query.data?.pages.find((page) => page.trace)?.trace

  const trace = useMemo(
    () => serverOverview ?? (spans.length > 0 && kind ? overviewFromSpans(spans, id, kind) : undefined),
    [serverOverview, spans, id, kind],
  )

  const renderSessionLink = useCallback(
    (sessionId: string) => (
      <Link
        to={sessionPath(sessionId)}
        state={linkState}
        className="break-all font-mono text-primary underline-offset-4 hover:underline"
      >
        {sessionId}
      </Link>
    ),
    [linkState],
  )

  const traceLabel = trace?.groupLevel === 'codex_turn'
    ? 'Codex turn'
    : kind === 'codex_operation'
      ? 'Codex operation'
      : 'Trace'

  const backButton = (
    <Button variant="ghost" size="sm" className="-ml-2 gap-2" asChild>
      <Link to={back.to}>
        <ArrowLeft className="h-4 w-4" />
        {back.label}
      </Link>
    </Button>
  )

  if (!id || !kind) {
    return (
      <div className="space-y-6">
        {backButton}
        <EmptyState title="Unknown trace" description="The address names no trace kind and ID." />
      </div>
    )
  }

  const loadedAll = !query.hasNextPage
  const sessionId = trace?.sessionId

  return (
    <div className="space-y-6">
      <div className="space-y-2">
        {backButton}
        <PageHeader
          title={trace?.rootSpan ?? traceLabel}
          documentTitle={trace ? `${trace.rootSpan} · ${traceLabel}` : traceLabel}
          description={
            <span className="flex min-w-0 items-center gap-1">
              <span>{traceLabel}</span>
              <span className="break-all font-mono text-sm">{id}</span>
              <CopyButton value={id} label={`Copy ${traceLabel.toLowerCase()} ID`} />
            </span>
          }
          actions={
            trace ? (
              <div className="flex flex-wrap gap-2">
                <Button variant="outline" size="sm" className="gap-2" asChild>
                  <Link to={traceLogsPath(trace.traceId, trace.startTime, trace.duration)} state={linkState}>
                    <ScrollText className="h-4 w-4" />
                    View logs
                  </Link>
                </Button>
                {sessionId && (
                  <Button variant="outline" size="sm" className="gap-2" asChild>
                    <Link to={sessionPath(sessionId)} state={linkState}>
                      <MessagesSquare className="h-4 w-4" />
                      Open session
                    </Link>
                  </Button>
                )}
              </div>
            ) : undefined
          }
        />
      </div>

      {query.isPending ? (
        <Card>
          <CardContent className="space-y-4 py-4">
            <Skeleton className="h-5 w-2/3" />
            <ListSkeleton rows={8} rowClassName="h-6" label="Loading trace" />
          </CardContent>
        </Card>
      ) : query.isError && !query.data ? (
        <QueryError
          error={query.error}
          title="Couldn't load the trace"
          fallback="Failed to load trace"
          onRetry={() => query.refetch()}
          retrying={query.isFetching}
        />
      ) : !trace || spans.length === 0 ? (
        <EmptyState title="No spans found for this trace" description="Its spans may have been deleted by retention." />
      ) : (
        <>
          <Card>
            <CardContent className="py-4">
              <div className="flex flex-wrap items-center gap-x-6 gap-y-2 text-sm">
                <SummaryItem label="Status">
                  <Badge className={getStatusColor(trace.status)}>{trace.status}</Badge>
                </SummaryItem>
                <SummaryItem label="Service">
                  <span className="font-medium">{displayName(trace.serviceName)}</span>
                </SummaryItem>
                {/* The heading shows the ID of OTel traces */}
                {trace.kind === 'codex_operation' && (
                  <SummaryItem label="Trace ID">
                    <span className="break-all font-mono font-medium">{trace.traceId}</span>
                    <CopyButton value={trace.traceId} label="Copy trace ID" className="-my-1 size-6" />
                  </SummaryItem>
                )}
                <SummaryItem label="Root span">
                  <span className="break-all font-mono font-medium">{trace.rootSpanId}</span>
                  <CopyButton value={trace.rootSpanId} label="Copy root span ID" className="-my-1 size-6" />
                </SummaryItem>
                <SummaryItem label="Duration">
                  <span className="font-mono font-medium">{formatDurationNanos(trace.duration)}</span>
                </SummaryItem>
                <SummaryItem label="Spans">
                  <span className="font-medium">{formatCount(trace.spanCount)}</span>
                </SummaryItem>
                <SummaryItem label="Started">
                  <span className="font-medium">{formatTimestamp(trace.startTime)}</span>
                </SummaryItem>
              </div>
            </CardContent>
          </Card>

          <Card>
            <CardContent className="py-4">
              <WaterfallView spans={spans} trace={trace} filterable renderSessionLink={renderSessionLink} />
              {!loadedAll && (
                <div className="mt-4 flex flex-col items-center gap-2 border-t pt-4 text-sm text-muted-foreground">
                  <span>
                    Showing the first {formatCount(spans.length)} of {formatCount(trace.spanCount)} spans
                  </span>
                  {query.isFetchNextPageError && (
                    <span className="text-error" role="alert">
                      {errorText(query.error, 'Failed to load more spans')}
                    </span>
                  )}
                  <Button
                    variant="outline"
                    size="sm"
                    className="gap-2"
                    onClick={() => query.fetchNextPage()}
                    disabled={query.isFetchingNextPage}
                  >
                    {query.isFetchNextPageError && <RefreshCw className="h-3 w-3" />}
                    {query.isFetchingNextPage ? 'Loading...' : query.isFetchNextPageError ? 'Try again' : 'Load more spans'}
                  </Button>
                </div>
              )}
            </CardContent>
          </Card>
        </>
      )}
    </div>
  )
}
