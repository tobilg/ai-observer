import { useMemo } from 'react'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { DataPagination } from '@/components/common/data-pagination'
import { EmptyState } from '@/components/common/empty-state'
import { ListSkeleton, Refreshing } from '@/components/common/list-skeleton'
import { QueryError } from '@/components/common/query-error'
import { usePagination } from '@/hooks/usePagination'
import { useServiceNames } from '@/hooks/useServiceNames'
import { useMetricPoints } from '@/lib/queries'
import type { RangeSource } from '@/lib/timeUtils'
import { formatTimestamp } from '@/lib/utils'
import { formatPointValue, temporalityLabel } from './pointFormat'

const PAGE_SIZE_STORAGE_KEY = 'ai-observer-metric-points-pageSize'

interface MetricPointsTableProps {
  name: string
  service: string
  range: RangeSource
}

/**
 * The stored data points of a metric (GET /api/metrics), newest first, a
 * page at a time: what the chart is computed from. Cumulative counters show
 * their running totals here; the chart shows their increases.
 */
export function MetricPointsTable({ name, service, range }: MetricPointsTableProps) {
  const { displayName } = useServiceNames()
  const filters = useMemo(() => ({ name, service }), [name, service])
  // Cursors belong to one list: another metric, service or range starts over
  const pagination = usePagination({
    defaultPageSize: 25,
    storageKey: PAGE_SIZE_STORAGE_KEY,
    resetOn: JSON.stringify([filters, range.key]),
  })
  const page = useMemo(() => ({ limit: pagination.pageSize, cursor: pagination.cursor }), [pagination.pageSize, pagination.cursor])
  const points = useMetricPoints(filters, range, page)

  if (points.isPending) {
    return <ListSkeleton rows={6} rowClassName="h-10" label="Loading data points" />
  }
  if (points.isError) {
    return (
      <QueryError
        error={points.error}
        title="Couldn't load the data points"
        onRetry={() => points.refetch()}
        retrying={points.isFetching}
      />
    )
  }

  const rows = points.data.metrics ?? []
  if (rows.length === 0) {
    return <EmptyState title="No data points in this time range" description="Choose a longer time range or another service." />
  }

  return (
    <div className="space-y-2">
      <Refreshing active={points.isPlaceholderData}>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Time</TableHead>
              <TableHead>Service</TableHead>
              <TableHead>Type</TableHead>
              <TableHead className="text-right">Value</TableHead>
              <TableHead>Attributes</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {rows.map((point, i) => {
              const attributes = Object.entries(point.attributes ?? {})
              const temporality = temporalityLabel(point)
              return (
                <TableRow key={`${point.timestamp}-${i}`}>
                  <TableCell className="whitespace-nowrap font-mono text-xs">{formatTimestamp(point.timestamp)}</TableCell>
                  <TableCell className="whitespace-nowrap" title={point.serviceName}>
                    {displayName(point.serviceName)}
                  </TableCell>
                  <TableCell className="whitespace-nowrap">
                    {point.metricType}
                    {temporality && <span className="text-muted-foreground"> ({temporality})</span>}
                  </TableCell>
                  <TableCell className="whitespace-nowrap text-right font-mono tabular-nums">{formatPointValue(point)}</TableCell>
                  <TableCell className="min-w-48 whitespace-normal">
                    {attributes.length === 0 ? (
                      <span className="text-muted-foreground">—</span>
                    ) : (
                      <div className="flex flex-wrap gap-1">
                        {attributes.map(([key, value]) => (
                          <code
                            key={key}
                            className="max-w-72 truncate rounded bg-muted px-1.5 py-0.5 text-xs"
                            title={`${key}=${value}`}
                          >
                            {key}={value}
                          </code>
                        ))}
                      </div>
                    )}
                  </TableCell>
                </TableRow>
              )
            })}
          </TableBody>
        </Table>
      </Refreshing>
      <DataPagination
        page={pagination.page}
        pageSize={pagination.pageSize}
        itemCount={rows.length}
        total={points.data.total ?? 0}
        // A placeholder's nextCursor belongs to the page shown before
        hasNext={!points.isPlaceholderData && Boolean(points.data.nextCursor)}
        onFirst={pagination.resetToFirstPage}
        onPrevious={pagination.previousPage}
        onNext={() => pagination.nextPage(points.isPlaceholderData ? undefined : points.data.nextCursor)}
        onPageSizeChange={pagination.setPageSize}
      />
    </div>
  )
}
