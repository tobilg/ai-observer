import { useId, useMemo } from 'react'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { DataPagination } from '@/components/common/data-pagination'
import { EmptyState } from '@/components/common/empty-state'
import { ListSkeleton, Refreshing } from '@/components/common/list-skeleton'
import { FilterBar, NewItemsButton, SearchFilter, ServiceFilter, TimeRangeFilter } from '@/components/common/list-filters'
import { PageHeader } from '@/components/common/page-header'
import { QueryError } from '@/components/common/query-error'
import { LogRow } from '@/components/logs/LogRow'
import { useDebounce } from '@/hooks/useDebounce'
import { useExpandedRow } from '@/hooks/useExpandedRow'
import { useListLinkState } from '@/hooks/useListLinkState'
import { useLiveInvalidate } from '@/hooks/useLiveInvalidate'
import { useNewItems } from '@/hooks/useNewItems'
import { usePagination } from '@/hooks/usePagination'
import { useServiceNames } from '@/hooks/useServiceNames'
import { useUrlState } from '@/hooks/useUrlState'
import { formatCount } from '@/lib/format'
import { countLogs, queryKeys, useLogLevels, useLogs, useServices } from '@/lib/queries'
import { logRowKey, uniqueRowKeys } from '@/lib/rowKeys'
import { useTelemetryStore } from '@/stores/telemetryStore'
import { ScrollText, X } from 'lucide-react'

// localStorage keys
const TIME_SELECTION_STORAGE_KEY = 'ai-observer-logs-timeselection'
const PAGE_SIZE_STORAGE_KEY = 'ai-observer-logs-pageSize'

// URL parameters and their defaults
const FILTER_DEFAULTS = { service: '', severity: '', search: '', traceId: '' }

const SEARCH_DEBOUNCE_MS = 200

// The first page of a relative range is fetched again this often, so the
// window keeps moving (older logs drop out of the total) while nobody looks
// at new ones; new logs are only shown on request ("N new logs"), so rows
// don't move under the reader
const WINDOW_REFRESH_MS = 60_000

// Severity levels in their order; the options are the levels of the logs in
// the selected range (GET /api/logs/levels), these while those load
const SEVERITY_ORDER = ['TRACE', 'DEBUG', 'INFO', 'WARN', 'WARNING', 'ERROR', 'FATAL']
const FALLBACK_LEVELS = ['TRACE', 'DEBUG', 'INFO', 'WARN', 'ERROR', 'FATAL']

function severityRank(level: string): number {
  const index = SEVERITY_ORDER.indexOf(level.toUpperCase())
  return index === -1 ? SEVERITY_ORDER.length : index
}

export function LogsPage() {
  const timeRangeId = useId()
  const rowIdPrefix = useId()
  const linkState = useListLinkState()
  const { displayName } = useServiceNames()

  const { filters, setFilters, time } = useUrlState({
    filters: FILTER_DEFAULTS,
    time: { storageKey: TIME_SELECTION_STORAGE_KEY },
  })
  const search = useDebounce(filters.search, SEARCH_DEBOUNCE_MS)
  const queryFilters = useMemo(
    () => ({ service: filters.service, severity: filters.severity, search, traceId: filters.traceId || undefined }),
    [filters.service, filters.severity, search, filters.traceId],
  )

  // Cursors belong to one filtered list: any change (also from the URL,
  // e.g. browser Back) starts at the first page
  const listKey = JSON.stringify([queryFilters, time.timeKey])
  const pagination = usePagination({ defaultPageSize: 10, storageKey: PAGE_SIZE_STORAGE_KEY, resetOn: listKey })
  const page = useMemo(() => ({ limit: pagination.pageSize, cursor: pagination.cursor }), [pagination.pageSize, pagination.cursor])

  const logs = useLogs(queryFilters, time.range, page)
  const services = useServices()
  const levels = useLogLevels(time.range)

  const firstPageKey = queryKeys.logs.list(queryFilters, time.range, { limit: pagination.pageSize })
  const countNew = useMemo(() => countLogs(queryFilters), [queryFilters])
  const newLogs = useNewItems({
    name: 'logs',
    filters: queryFilters,
    count: countNew,
    anchor: logs.dataUpdatedAt,
    recentTimes: useTelemetryStore((s) => s.recentLogTimes),
    enabled: !time.isAbsoluteRange,
    refetch: firstPageKey,
    onSeen: pagination.resetToFirstPage,
  })
  useLiveInvalidate({
    queryKey: firstPageKey,
    enabled: pagination.page === 1 && !time.isAbsoluteRange && newLogs.count === 0,
    intervalMs: WINDOW_REFRESH_MS,
    updateCount: 0,
  })

  const [expanded, toggleExpanded] = useExpandedRow(`${listKey}|${pagination.pageSize}|${pagination.cursor ?? ''}`)

  const severityOptions = useMemo(() => {
    const counts = levels.data
    const names = counts ? Object.keys(counts).filter(Boolean) : [...FALLBACK_LEVELS]
    // A level from the URL stays selectable even without logs in the range
    if (filters.severity && !names.includes(filters.severity)) {
      names.push(filters.severity)
    }
    names.sort((a, b) => severityRank(a) - severityRank(b) || a.localeCompare(b))
    return names.map((name) => ({
      value: name,
      label: counts?.[name] !== undefined ? `${name} (${formatCount(counts[name])})` : name,
    }))
  }, [levels.data, filters.severity])

  const loaded = logs.data?.logs
  const rows = useMemo(() => loaded ?? [], [loaded])
  const rowKeys = useMemo(() => uniqueRowKeys(rows, logRowKey), [rows])
  const filtered = Boolean(filters.service || filters.severity || search || filters.traceId)

  let content
  if (logs.isPending) {
    content = <ListSkeleton rows={8} rowClassName="h-9" label="Loading logs" />
  } else if (logs.isError && !logs.data) {
    content = <QueryError error={logs.error} title="Couldn't load logs" onRetry={() => void logs.refetch()} retrying={logs.isFetching} />
  } else if (rows.length === 0) {
    content = (
      <EmptyState
        icon={ScrollText}
        title={filtered ? 'No logs match these filters' : 'No logs in this time range'}
        description={
          filtered
            ? 'Try another service, level or search, or a longer time range.'
            : 'Choose a longer time range, or configure your AI coding tool to send log telemetry.'
        }
      />
    )
  } else {
    content = (
      <>
        {logs.isError && (
          <QueryError error={logs.error} title="Couldn't refresh logs" onRetry={() => void logs.refetch()} retrying={logs.isFetching} />
        )}
        <Refreshing active={logs.isPlaceholderData}>
          <ul className="space-y-1 font-mono text-sm" aria-label="Logs">
            {rows.map((log, index) => {
              const key = rowKeys[index]
              return (
                <LogRow
                  key={key}
                  log={log}
                  detailsId={`${rowIdPrefix}-log-${index}`}
                  expanded={expanded === key}
                  onToggle={() => toggleExpanded(key)}
                  serviceName={displayName(log.serviceName)}
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
          total={logs.data?.total ?? 0}
          // A placeholder's nextCursor belongs to the page shown before
          hasNext={!logs.isPlaceholderData && Boolean(logs.data?.nextCursor)}
          onFirst={pagination.resetToFirstPage}
          onPrevious={pagination.previousPage}
          onNext={() => pagination.nextPage(logs.isPlaceholderData ? undefined : logs.data?.nextCursor)}
          onPageSizeChange={pagination.setPageSize}
        />
      </>
    )
  }

  return (
    <div className="space-y-6">
      <PageHeader title="Logs" description="View and search logs from your AI coding tools" />

      <Card>
        <CardContent className="pt-6">
          <FilterBar>
            <TimeRangeFilter id={timeRangeId} value={time.timeSelection} onChange={time.setTimeSelection} />
            <ServiceFilter value={filters.service} onChange={(service) => setFilters({ service })} services={services.data ?? []} />
            <div className="w-full md:w-40 *:w-full">
              <NativeSelect
                aria-label="Level"
                className="h-10"
                value={filters.severity}
                onChange={(e) => setFilters({ severity: e.target.value })}
              >
                <NativeSelectOption value="">All Levels</NativeSelectOption>
                {severityOptions.map((o) => (
                  <NativeSelectOption key={o.value} value={o.value}>
                    {o.label}
                  </NativeSelectOption>
                ))}
              </NativeSelect>
            </div>
            <SearchFilter
              label="Search logs"
              placeholder="Search log messages..."
              value={filters.search}
              onChange={(value) => setFilters({ search: value })}
            />
          </FilterBar>
          {filters.traceId && (
            // Set by the trace detail page's "View logs" link
            <div className="mt-3 flex flex-wrap items-center gap-2 text-sm">
              <span className="text-muted-foreground">Logs of trace</span>
              <Badge variant="secondary" className="max-w-full break-all font-mono">
                {filters.traceId}
              </Badge>
              <Button variant="ghost" size="sm" className="h-7 gap-1 px-2" onClick={() => setFilters({ traceId: '' })}>
                <X className="h-3 w-3" />
                Clear trace filter
              </Button>
            </div>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader className="flex flex-row flex-wrap items-center justify-between gap-2 space-y-0">
          <CardTitle>Logs</CardTitle>
          {newLogs.count > 0 && <NewItemsButton count={newLogs.count} noun={['log', 'logs']} onClick={newLogs.markSeen} />}
        </CardHeader>
        <CardContent>{content}</CardContent>
      </Card>
    </div>
  )
}
