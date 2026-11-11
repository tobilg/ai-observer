import { Link, useLocation } from 'react-router-dom'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { ScrollArea } from '@/components/ui/scroll-area'
import { formatDuration, formatRelativeTime, getStatusColor } from '@/lib/utils'
import { errorText } from '@/lib/errors'
import { traceDetailPath } from '@/lib/traces'
import { useServiceNames } from '@/hooks/useServiceNames'
import type { TracesResponse } from '@/types/traces'
import type { WidgetDataState } from './types'
import { WidgetError } from './WidgetStatus'
import { ListSkeleton } from '@/components/common/list-skeleton'

interface RecentTracesWidgetProps {
  title: string
  traces: WidgetDataState<TracesResponse>
}

export function RecentTracesWidget({ title, traces }: RecentTracesWidgetProps) {
  const location = useLocation()
  const { displayName } = useServiceNames()
  const rows = traces.data?.traces ?? []
  // The trace page's back link returns to this dashboard
  const from = location.pathname + location.search

  return (
    <Card className="border-0 shadow-none">
      <CardHeader className="p-4 pb-2">
        <CardTitle className="flex items-center gap-2">{title}</CardTitle>
        <CardDescription>The newest traces in the selected range</CardDescription>
      </CardHeader>
      <CardContent className="px-4 pb-4 pt-0">
        <ScrollArea className="h-[200px]">
          {traces.data ? (
            rows.length > 0 ? (
              <ul className="space-y-2">
                {rows.slice(0, 10).map((trace, i) => (
                  <li key={`${trace.kind}-${trace.id}-${i}`} className="border-b last:border-0">
                    <Link
                      to={traceDetailPath(trace)}
                      state={{ from }}
                      className="flex items-center justify-between gap-3 py-2 px-2 -mx-2 rounded-md hover:bg-accent transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                    >
                      <div className="flex min-w-0 items-center gap-3">
                        <Badge className={getStatusColor(trace.status)}>{trace.status}</Badge>
                        <div className="min-w-0">
                          <p className="truncate font-medium text-sm">{trace.rootSpan}</p>
                          <p className="truncate text-xs text-muted-foreground">
                            {displayName(trace.serviceName)} &middot; {trace.spanCount} span{trace.spanCount !== 1 ? 's' : ''}
                          </p>
                        </div>
                      </div>
                      <div className="shrink-0 text-right">
                        <p className="text-sm font-mono">{formatDuration(trace.duration)}</p>
                        <p className="text-xs text-muted-foreground">{formatRelativeTime(trace.startTime)}</p>
                      </div>
                    </Link>
                  </li>
                ))}
              </ul>
            ) : (
              <p className="text-muted-foreground text-sm py-4 text-center">No traces in the selected range yet.</p>
            )
          ) : traces.isError ? (
            <WidgetError
              className="py-2"
              message={errorText(traces.error, "Couldn't load the recent traces")}
              onRetry={() => traces.refetch()}
              retrying={traces.isFetching}
            />
          ) : (
            <ListSkeleton rows={4} rowClassName="h-10" label="Loading traces" />
          )}
        </ScrollArea>
      </CardContent>
    </Card>
  )
}
