import { Card } from '@/components/ui/card'
import { GitBranch, BarChart3, FileText, AlertTriangle } from 'lucide-react'
import type { StatsResponse } from '@/lib/api'
import { WIDGET_TYPES } from '@/types/dashboard'

interface StatsWidgetProps {
  widgetType: string
  title: string
  stats: StatsResponse | null
}

export function StatsWidget({ widgetType, title, stats }: StatsWidgetProps) {
  const getIcon = () => {
    switch (widgetType) {
      case WIDGET_TYPES.STATS_TRACES:
        return <GitBranch className="h-4 w-4 text-muted-foreground" />
      case WIDGET_TYPES.STATS_METRICS:
        return <BarChart3 className="h-4 w-4 text-muted-foreground" />
      case WIDGET_TYPES.STATS_LOGS:
        return <FileText className="h-4 w-4 text-muted-foreground" />
      case WIDGET_TYPES.STATS_ERROR_RATE:
        return <AlertTriangle className="h-4 w-4 text-muted-foreground" />
      default:
        return null
    }
  }

  const getValue = () => {
    if (!stats) return '—'
    switch (widgetType) {
      case WIDGET_TYPES.STATS_TRACES:
        return stats.traceCount ?? 0
      case WIDGET_TYPES.STATS_METRICS:
        return stats.metricCount ?? 0
      case WIDGET_TYPES.STATS_LOGS:
        return stats.logCount ?? 0
      case WIDGET_TYPES.STATS_ERROR_RATE:
        return `${(stats.errorRate ?? 0).toFixed(2)}%`
      default:
        return '—'
    }
  }

  const getSubtext = () => {
    if (!stats) return ''
    switch (widgetType) {
      case WIDGET_TYPES.STATS_TRACES:
        return `${stats.spanCount ?? 0} spans`
      case WIDGET_TYPES.STATS_METRICS:
        return 'data points'
      case WIDGET_TYPES.STATS_LOGS:
        return 'log records'
      case WIDGET_TYPES.STATS_ERROR_RATE:
        return 'of spans'
      default:
        return ''
    }
  }

  return (
    <Card className="@container border-0 shadow-none h-full">
      <div className="h-full flex flex-col p-2 @[140px]:p-3">
        {/* Header */}
        <div className="flex flex-row items-start justify-between">
          <span className="text-sm @[140px]:text-base font-medium truncate">
            {title}
          </span>
          {getIcon()}
        </div>
        {/* Value - centered in remaining space */}
        <div className="flex-1 flex flex-col justify-center">
          <div className="text-2xl @[140px]:text-4xl font-bold">{getValue()}</div>
          <p className="text-xs text-muted-foreground">{getSubtext()}</p>
        </div>
      </div>
    </Card>
  )
}
