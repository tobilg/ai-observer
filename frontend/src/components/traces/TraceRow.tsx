import { memo } from 'react'
import { Link } from 'react-router-dom'
import { ChevronRight, ExternalLink, MessageSquare } from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { CopyButton } from '@/components/common/copy-button'
import { ListSkeleton } from '@/components/common/list-skeleton'
import { QueryError } from '@/components/common/query-error'
import { WaterfallView } from '@/components/traces/WaterfallView'
import { formatCount, formatDurationNanos } from '@/lib/format'
import { sessionPath, type ListLinkState } from '@/lib/listLinks'
import { useTraceSpans } from '@/lib/queries'
import { getTraceKind, getTraceRequestId, traceDetailPath } from '@/lib/traces'
import { cn, formatTimestamp, getStatusColor } from '@/lib/utils'
import type { TraceOverview } from '@/types/traces'

interface TraceRowProps {
  trace: TraceOverview
  // DOM id of the spans panel (aria-controls)
  panelId: string
  expanded: boolean
  onToggle: () => void
  serviceName: string
  linkState: ListLinkState
}

function plural(n: number, unit: string): string {
  return `${formatCount(n)} ${unit}${n === 1 ? '' : 's'}`
}

// The first page of an expanded trace's spans as a waterfall. From the
// query cache, so expanding again shows them at once and refreshes them.
function TraceSpansPreview({ trace, linkState }: { trace: TraceOverview; linkState: ListLinkState }) {
  const spans = useTraceSpans(getTraceRequestId(trace), getTraceKind(trace))

  if (spans.isPending) {
    return <ListSkeleton rows={3} rowClassName="h-8" label="Loading spans" />
  }
  if (spans.isError) {
    return (
      <QueryError
        error={spans.error}
        title="Couldn't load the spans"
        onRetry={() => void spans.refetch()}
        retrying={spans.isFetching}
        className="my-0"
      />
    )
  }

  const firstPage = spans.data.pages[0]
  const list = firstPage?.spans ?? []
  if (list.length === 0) {
    return <p className="py-4 text-center text-sm text-muted-foreground">No spans found</p>
  }
  return (
    <>
      <WaterfallView spans={list} trace={trace} />
      {firstPage?.hasMore && (
        <p className="mt-3 border-t pt-3 text-center text-sm text-muted-foreground">
          Showing the first {formatCount(list.length)} of {formatCount(firstPage.total ?? trace.spanCount)} spans.{' '}
          <Link className="underline underline-offset-4 hover:text-foreground" to={traceDetailPath(trace)} state={linkState}>
            Open the trace
          </Link>{' '}
          to load more.
        </p>
      )}
    </>
  )
}

/**
 * A trace in the traces list: the summary is a button that expands the
 * span waterfall in place; "Open" links to the trace detail page (and
 * "Session" to the transcript when the trace's spans name one session).
 */
export const TraceRow = memo(function TraceRow({ trace, panelId, expanded, onToggle, serviceName, linkState }: TraceRowProps) {
  return (
    <li className="rounded-lg border">
      <div className="flex flex-col sm:flex-row sm:items-stretch">
        <Button
          type="button"
          variant="ghost"
          // No size: the row sizes itself (the variants set a fixed height)
          size={null}
          onClick={onToggle}
          aria-expanded={expanded}
          aria-controls={panelId}
          className={cn(
            'flex min-w-0 flex-1 shrink flex-col items-stretch justify-start gap-2 rounded-lg p-3 text-left font-normal whitespace-normal',
            expanded && 'bg-accent',
          )}
        >
          <span className="flex min-w-0 items-center gap-2">
            <ChevronRight
              aria-hidden="true"
              className={cn('h-4 w-4 shrink-0 text-muted-foreground transition-transform', expanded && 'rotate-90')}
            />
            <Badge className={getStatusColor(trace.status)}>{trace.status}</Badge>
            <span className="truncate text-sm font-medium" title={trace.rootSpan}>
              {trace.rootSpan}
            </span>
          </span>
          <span className="flex flex-wrap items-center justify-between gap-x-4 gap-y-1 text-xs text-muted-foreground">
            <span>{serviceName}</span>
            <span>{formatDurationNanos(trace.duration)}</span>
          </span>
          <span className="flex flex-wrap items-center justify-between gap-x-4 gap-y-1 text-xs text-muted-foreground">
            <span>{plural(trace.spanCount, 'span')}</span>
            <span>{formatTimestamp(trace.startTime)}</span>
          </span>
        </Button>
        <div className="flex shrink-0 items-center gap-1 border-t px-2 py-1 sm:flex-col sm:items-end sm:justify-center sm:border-t-0 sm:border-l">
          <Button asChild variant="ghost" size="sm">
            <Link to={traceDetailPath(trace)} state={linkState} aria-label={`Open trace ${trace.rootSpan}`}>
              <ExternalLink className="h-3.5 w-3.5" />
              Open
            </Link>
          </Button>
          {trace.sessionId && (
            <Button asChild variant="ghost" size="sm">
              <Link to={sessionPath(trace.sessionId)} state={linkState} aria-label="Open session">
                <MessageSquare className="h-3.5 w-3.5" />
                Session
              </Link>
            </Button>
          )}
          <CopyButton value={trace.traceId} label="Copy trace ID" />
        </div>
      </div>

      {expanded && (
        <div id={panelId} className="border-t p-3">
          <TraceSpansPreview trace={trace} linkState={linkState} />
        </div>
      )}
    </li>
  )
})
