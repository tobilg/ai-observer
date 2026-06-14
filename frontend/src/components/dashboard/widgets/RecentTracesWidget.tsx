import { useNavigate } from 'react-router-dom'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { ScrollArea } from '@/components/ui/scroll-area'
import { formatDuration, formatRelativeTime, getStatusColor } from '@/lib/utils'
import { getServiceDisplayName } from '@/lib/metricMetadata'
import type { TraceOverview } from '@/types/traces'

interface RecentTracesWidgetProps {
  title: string
  traces: TraceOverview[]
}

function getTraceKind(trace: TraceOverview) {
  return trace.kind ?? 'otel_trace'
}

function getTraceRequestId(trace: TraceOverview) {
  if (getTraceKind(trace) === 'codex_operation') {
    return trace.id || trace.rootSpanId || trace.traceId
  }
  return trace.traceId || trace.id || trace.rootSpanId
}

export function RecentTracesWidget({ title, traces }: RecentTracesWidgetProps) {
  const navigate = useNavigate()

  const handleTraceClick = (trace: TraceOverview) => {
    navigate(`/traces/${getTraceKind(trace)}/${getTraceRequestId(trace)}`)
  }

  return (
    <Card className="border-0 shadow-none">
      <CardHeader className="p-4 pb-2">
        <CardTitle className="flex items-center gap-2">
          {title}
        </CardTitle>
        <CardDescription>Recent traces from connected services</CardDescription>
      </CardHeader>
      <CardContent className="px-4 pb-4 pt-0">
        <ScrollArea className="h-[200px]">
          <div className="space-y-2">
            {traces.length ? (
              traces.slice(0, 10).map((trace, i) => (
                <div
                  key={`${trace.kind}-${trace.id}-${i}`}
                  className="flex items-center justify-between py-2 px-2 -mx-2 border-b last:border-0 cursor-pointer rounded-md hover:bg-accent transition-colors"
                  onClick={() => handleTraceClick(trace)}
                  role="button"
                  tabIndex={0}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter' || e.key === ' ') {
                      e.preventDefault()
                      handleTraceClick(trace)
                    }
                  }}
                >
                  <div className="flex items-center gap-3">
                    <Badge className={getStatusColor(trace.status)}>
                      {trace.status}
                    </Badge>
                    <div>
                      <p className="font-medium text-sm">{trace.rootSpan}</p>
                      <p className="text-xs text-muted-foreground">
                        {getServiceDisplayName(trace.serviceName)} &middot; {trace.spanCount} span{trace.spanCount !== 1 ? 's' : ''}
                      </p>
                    </div>
                  </div>
                  <div className="text-right">
                    <p className="text-sm font-mono">{formatDuration(trace.duration)}</p>
                    <p className="text-xs text-muted-foreground">
                      {formatRelativeTime(trace.startTime)}
                    </p>
                  </div>
                </div>
              ))
            ) : (
              <p className="text-muted-foreground text-sm py-4 text-center">
                Waiting for incoming telemetry data...
              </p>
            )}
          </div>
        </ScrollArea>
      </CardContent>
    </Card>
  )
}
