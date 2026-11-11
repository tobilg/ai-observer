import type { DashboardWidget, TimeSelection } from '@/types/dashboard'
import { WIDGET_TYPES } from '@/types/dashboard'
import type { StatsResponse } from '@/lib/api'
import type { TracesResponse } from '@/types/traces'
import type { WidgetDataState } from './types'
import { StatsWidget } from './StatsWidget'
import { ActiveServicesWidget } from './ActiveServicesWidget'
import { RecentTracesWidget } from './RecentTracesWidget'
import { MetricValueWidget } from './MetricValueWidget'
import { MetricChartWidget } from './MetricChartWidget'

interface WidgetRendererProps {
  widget: DashboardWidget
  stats: WidgetDataState<StatsResponse>
  recentTraces: WidgetDataState<TracesResponse>
  timeSelection: TimeSelection
}

export function WidgetRenderer({ widget, stats, recentTraces, timeSelection }: WidgetRendererProps) {
  switch (widget.widgetType) {
    case WIDGET_TYPES.STATS_TRACES:
    case WIDGET_TYPES.STATS_METRICS:
    case WIDGET_TYPES.STATS_LOGS:
    case WIDGET_TYPES.STATS_ERROR_RATE:
      return <StatsWidget widgetType={widget.widgetType} title={widget.title} stats={stats} />

    case WIDGET_TYPES.ACTIVE_SERVICES:
      return <ActiveServicesWidget title={widget.title} stats={stats} />

    case WIDGET_TYPES.RECENT_ACTIVITY:
      return <RecentTracesWidget title={widget.title} traces={recentTraces} />

    case WIDGET_TYPES.METRIC_VALUE:
      return <MetricValueWidget widgetId={widget.id} title={widget.title} config={widget.config ?? {}} />

    case WIDGET_TYPES.METRIC_CHART:
      return (
        <MetricChartWidget
          widgetId={widget.id}
          title={widget.title}
          config={widget.config ?? {}}
          timeSelection={timeSelection}
        />
      )

    default:
      return <div className="p-4 text-muted-foreground text-sm">Unknown widget type: {widget.widgetType}</div>
  }
}
