import { Card } from '@/components/ui/card'
import { GitBranch, BarChart3, FileText, AlertTriangle } from 'lucide-react'
import type { StatsResponse } from '@/lib/api'
import { errorText } from '@/lib/errors'
import { formatCount } from '@/lib/format'
import { WIDGET_TYPES } from '@/types/dashboard'
import type { WidgetDataState } from './types'
import { WidgetError, WidgetLoading } from './WidgetStatus'

interface StatsWidgetProps {
  widgetType: string
  title: string
  stats: WidgetDataState<StatsResponse>
}

function StatsIcon({ widgetType }: { widgetType: string }) {
  const className = 'h-4 w-4 shrink-0 text-muted-foreground'
  switch (widgetType) {
    case WIDGET_TYPES.STATS_TRACES:
      return <GitBranch className={className} aria-hidden="true" />
    case WIDGET_TYPES.STATS_METRICS:
      return <BarChart3 className={className} aria-hidden="true" />
    case WIDGET_TYPES.STATS_LOGS:
      return <FileText className={className} aria-hidden="true" />
    case WIDGET_TYPES.STATS_ERROR_RATE:
      return <AlertTriangle className={className} aria-hidden="true" />
    default:
      return null
  }
}

// The counts cover the dashboard's time range, which the subtext says
function statsValue(widgetType: string, stats: StatsResponse): { value: string; subtext: string } {
  switch (widgetType) {
    case WIDGET_TYPES.STATS_TRACES:
      return {
        value: formatCount(stats.traceCount ?? 0),
        subtext: `${formatCount(stats.spanCount ?? 0)} spans in selected range`,
      }
    case WIDGET_TYPES.STATS_METRICS:
      return { value: formatCount(stats.metricCount ?? 0), subtext: 'data points in selected range' }
    case WIDGET_TYPES.STATS_LOGS:
      return { value: formatCount(stats.logCount ?? 0), subtext: 'log records in selected range' }
    case WIDGET_TYPES.STATS_ERROR_RATE:
      return { value: `${(stats.errorRate ?? 0).toFixed(2)}%`, subtext: 'of spans in selected range' }
    default:
      return { value: '—', subtext: '' }
  }
}

export function StatsWidget({ widgetType, title, stats }: StatsWidgetProps) {
  const shown = stats.data ? statsValue(widgetType, stats.data) : null

  return (
    <Card className="@container border-0 shadow-none h-full">
      <div className="h-full flex flex-col p-2 @[140px]:p-3">
        <div className="flex flex-row items-start justify-between gap-2">
          <span className="text-sm @[140px]:text-base font-medium truncate">{title}</span>
          <StatsIcon widgetType={widgetType} />
        </div>
        <div className="flex-1 flex flex-col justify-center">
          {shown ? (
            <>
              <div className="text-2xl @[140px]:text-4xl font-bold tabular-nums">{shown.value}</div>
              <p className="text-xs text-muted-foreground">{shown.subtext}</p>
            </>
          ) : stats.isError ? (
            <WidgetError
              message={errorText(stats.error, "Couldn't load the counts")}
              onRetry={() => stats.refetch()}
              retrying={stats.isFetching}
            />
          ) : (
            <WidgetLoading className="h-12" />
          )}
        </div>
      </div>
    </Card>
  )
}
