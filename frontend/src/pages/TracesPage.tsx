import { useId, useMemo } from 'react'
import { Activity } from 'lucide-react'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { DataPagination } from '@/components/common/data-pagination'
import { EmptyState } from '@/components/common/empty-state'
import { ListSkeleton, Refreshing } from '@/components/common/list-skeleton'
import { FilterBar, NewItemsButton, SearchFilter, ServiceFilter, TimeRangeFilter } from '@/components/common/list-filters'
import { PageHeader } from '@/components/common/page-header'
import { QueryError } from '@/components/common/query-error'
import { TraceRow } from '@/components/traces/TraceRow'
import { useDebounce } from '@/hooks/useDebounce'
import { useExpandedRow } from '@/hooks/useExpandedRow'
import { useListLinkState } from '@/hooks/useListLinkState'
import { useLiveInvalidate } from '@/hooks/useLiveInvalidate'
import { useNewItems } from '@/hooks/useNewItems'
import { usePagination } from '@/hooks/usePagination'
import { useServiceNames } from '@/hooks/useServiceNames'
import { useUrlState } from '@/hooks/useUrlState'
import { countTraces, queryKeys, useServices, useTraces } from '@/lib/queries'
import { traceRowKey, uniqueRowKeys } from '@/lib/rowKeys'
import { useTelemetryStore } from '@/stores/telemetryStore'

// localStorage keys
const TIME_SELECTION_STORAGE_KEY = 'ai-observer-traces-timeselection'
const PAGE_SIZE_STORAGE_KEY = 'ai-observer-traces-pageSize'

// URL parameters and their defaults. background: Codex's app and exec
// servers record internal requests (file, settings and account reads) as
// hundreds of traces an hour; the API leaves out those outside a
// conversation unless asked for.
const FILTER_DEFAULTS = { service: '', search: '', background: false }

const SEARCH_DEBOUNCE_MS = 200

// The first page of a relative range is fetched again this often, so the
// window keeps moving while nobody looks at new traces; new traces are only
// shown on request ("N new traces"), so rows don't move under the reader
const WINDOW_REFRESH_MS = 60_000

export function TracesPage() {
  const timeRangeId = useId()
  const backgroundId = useId()
  const rowIdPrefix = useId()
  const linkState = useListLinkState()
  const { displayName } = useServiceNames()

  const { filters, setFilters, time } = useUrlState({
    filters: FILTER_DEFAULTS,
    time: { storageKey: TIME_SELECTION_STORAGE_KEY },
  })
  const search = useDebounce(filters.search, SEARCH_DEBOUNCE_MS)
  const queryFilters = useMemo(
    () => ({ service: filters.service, search, background: filters.background }),
    [filters.service, search, filters.background],
  )

  // Cursors belong to one filtered list: any change (also from the URL,
  // e.g. browser Back) starts at the first page
  const listKey = JSON.stringify([queryFilters, time.timeKey])
  const pagination = usePagination({ defaultPageSize: 10, storageKey: PAGE_SIZE_STORAGE_KEY, resetOn: listKey })
  const page = useMemo(() => ({ limit: pagination.pageSize, cursor: pagination.cursor }), [pagination.pageSize, pagination.cursor])

  const traces = useTraces(queryFilters, time.range, page)
  const services = useServices()

  const firstPageKey = queryKeys.traces.list(queryFilters, time.range, { limit: pagination.pageSize })
  const countNew = useMemo(() => countTraces(queryFilters), [queryFilters])
  const newTraces = useNewItems({
    name: 'traces',
    filters: queryFilters,
    count: countNew,
    anchor: traces.dataUpdatedAt,
    recentTimes: useTelemetryStore((s) => s.recentSpanTimes),
    enabled: !time.isAbsoluteRange,
    refetch: firstPageKey,
    onSeen: pagination.resetToFirstPage,
  })
  useLiveInvalidate({
    queryKey: firstPageKey,
    enabled: pagination.page === 1 && !time.isAbsoluteRange && newTraces.count === 0,
    intervalMs: WINDOW_REFRESH_MS,
    updateCount: 0,
  })

  const [expanded, toggleExpanded] = useExpandedRow(`${listKey}|${pagination.pageSize}|${pagination.cursor ?? ''}`)

  const loaded = traces.data?.traces
  const rows = useMemo(() => loaded ?? [], [loaded])
  const rowKeys = useMemo(() => uniqueRowKeys(rows, traceRowKey), [rows])
  const filtered = Boolean(filters.service || search)

  let content
  if (traces.isPending) {
    content = <ListSkeleton rows={6} rowClassName="h-20" label="Loading traces" />
  } else if (traces.isError && !traces.data) {
    content = (
      <QueryError error={traces.error} title="Couldn't load traces" onRetry={() => void traces.refetch()} retrying={traces.isFetching} />
    )
  } else if (rows.length === 0) {
    content = (
      <EmptyState
        icon={Activity}
        title={filtered ? 'No traces match these filters' : 'No traces in this time range'}
        description={
          filtered
            ? 'Try another service or search, or a longer time range.'
            : 'Choose a longer time range, or configure your AI coding tool to send trace telemetry.'
        }
      />
    )
  } else {
    content = (
      <>
        {traces.isError && (
          <QueryError error={traces.error} title="Couldn't refresh traces" onRetry={() => void traces.refetch()} retrying={traces.isFetching} />
        )}
        <Refreshing active={traces.isPlaceholderData}>
          <ul className="space-y-2" aria-label="Traces">
            {rows.map((trace, index) => {
              const key = rowKeys[index]
              return (
                <TraceRow
                  key={key}
                  trace={trace}
                  panelId={`${rowIdPrefix}-trace-${index}`}
                  expanded={expanded === key}
                  onToggle={() => toggleExpanded(key)}
                  serviceName={displayName(trace.serviceName)}
                  linkState={linkState}
                />
              )
            })}
          </ul>
        </Refreshing>
        <DataPagination
          page={pagination.page}
          pageSize={pagination.pageSize}
          itemCount={rows.length}
          total={traces.data?.total ?? 0}
          // A placeholder's nextCursor belongs to the page shown before
          hasNext={!traces.isPlaceholderData && Boolean(traces.data?.nextCursor)}
          onFirst={pagination.resetToFirstPage}
          onPrevious={pagination.previousPage}
          onNext={() => pagination.nextPage(traces.isPlaceholderData ? undefined : traces.data?.nextCursor)}
          onPageSizeChange={pagination.setPageSize}
        />
      </>
    )
  }

  return (
    <div className="space-y-6">
      <PageHeader title="Traces" description="View and analyze traces from your AI coding tools" />

      <Card>
        <CardContent className="pt-6">
          <FilterBar>
            <TimeRangeFilter id={timeRangeId} value={time.timeSelection} onChange={time.setTimeSelection} />
            <ServiceFilter value={filters.service} onChange={(service) => setFilters({ service })} services={services.data ?? []} />
            <SearchFilter
              label="Search traces"
              placeholder="Search spans, errors, attributes..."
              value={filters.search}
              onChange={(value) => setFilters({ search: value })}
            />
            <Tooltip>
              <TooltipTrigger asChild>
                <div className="flex items-center gap-2">
                  <Switch
                    id={backgroundId}
                    checked={filters.background}
                    onCheckedChange={(checked) => setFilters({ background: checked })}
                  />
                  <Label htmlFor={backgroundId} className="font-normal whitespace-nowrap text-muted-foreground">
                    Background requests
                  </Label>
                </div>
              </TooltipTrigger>
              <TooltipContent>Codex app server traces outside a conversation: file, settings and account reads</TooltipContent>
            </Tooltip>
          </FilterBar>
        </CardContent>
      </Card>

      <Card>
        <CardHeader className="flex flex-row flex-wrap items-center justify-between gap-2 space-y-0">
          <CardTitle>Traces</CardTitle>
          {newTraces.count > 0 && <NewItemsButton count={newTraces.count} noun={['trace', 'traces']} onClick={newTraces.markSeen} />}
        </CardHeader>
        <CardContent>{content}</CardContent>
      </Card>
    </div>
  )
}
