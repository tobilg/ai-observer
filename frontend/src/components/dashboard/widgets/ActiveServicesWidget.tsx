import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { useServiceNames } from '@/hooks/useServiceNames'
import type { StatsResponse } from '@/lib/api'
import { errorText } from '@/lib/errors'
import type { WidgetDataState } from './types'
import { WidgetError, WidgetLoading } from './WidgetStatus'

interface ActiveServicesWidgetProps {
  title: string
  stats: WidgetDataState<StatsResponse>
}

export function ActiveServicesWidget({ title, stats }: ActiveServicesWidgetProps) {
  const { displayName } = useServiceNames()
  const services = stats.data?.services ?? []

  return (
    <Card className="border-0 shadow-none">
      <CardHeader className="p-4 pb-2">
        <CardTitle className="flex items-center gap-2">{title}</CardTitle>
        <CardDescription>Services that have sent telemetry</CardDescription>
      </CardHeader>
      <CardContent className="px-4 pb-4 pt-0">
        {stats.data ? (
          services.length > 0 ? (
            <div className="flex flex-wrap gap-2">
              {services.map((service) => (
                <Badge key={service} variant="secondary">
                  {displayName(service)}
                </Badge>
              ))}
            </div>
          ) : (
            <p className="text-muted-foreground text-sm">
              No services detected yet. Configure your AI coding tool to send telemetry to this endpoint.
            </p>
          )
        ) : stats.isError ? (
          <WidgetError
            message={errorText(stats.error, "Couldn't load the services")}
            onRetry={() => stats.refetch()}
            retrying={stats.isFetching}
          />
        ) : (
          <WidgetLoading className="h-6 w-1/2" />
        )}
      </CardContent>
    </Card>
  )
}
