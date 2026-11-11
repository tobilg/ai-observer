import { useId, useMemo } from 'react'
import { MessageSquare, SearchX } from 'lucide-react'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { DataPagination } from '@/components/common/data-pagination'
import { EmptyState } from '@/components/common/empty-state'
import { ListSkeleton, Refreshing } from '@/components/common/list-skeleton'
import { FilterBar, SearchFilter, ServiceFilter, TimeRangeFilter } from '@/components/common/list-filters'
import { PageHeader } from '@/components/common/page-header'
import { QueryError } from '@/components/common/query-error'
import { SessionRow } from '@/components/sessions/SessionRow'
import { useDebounce } from '@/hooks/useDebounce'
import { useListLinkState } from '@/hooks/useListLinkState'
import { useLiveInvalidate } from '@/hooks/useLiveInvalidate'
import { usePagination } from '@/hooks/usePagination'
import { useServiceNames } from '@/hooks/useServiceNames'
import { useUrlState } from '@/hooks/useUrlState'
import { isHTTPError } from '@/lib/errors'
import { queryKeys, useServices, useSessions } from '@/lib/queries'
import { searchTerms } from '@/lib/searchTerms'
import { useTelemetryStore } from '@/stores/telemetryStore'

// localStorage keys
const TIME_SELECTION_STORAGE_KEY = 'ai-observer-sessions-timeselection'
const PAGE_SIZE_STORAGE_KEY = 'ai-observer-sessions-pageSize'

// URL parameters and their defaults
const FILTER_DEFAULTS = { service: '', search: '' }

// A session search reads every record in the range: wait for a pause in typing
const SEARCH_DEBOUNCE_MS = 300

// Sessions change by gaining messages, which moves them up the list: the
// first page refreshes itself on new logs and spans (debounced), and polls
// while the WebSocket is disconnected. Later pages stay as loaded.
const LIVE_POLL_MS = 30_000
const LIVE_DEBOUNCE_MS = 1_000
const LIVE_MAX_WAIT_MS = 5_000

export function SessionsPage() {
  const timeRangeId = useId()
  const searchErrorId = useId()
  const linkState = useListLinkState()
  const { displayName } = useServiceNames()

  const { filters, setFilters, time } = useUrlState({
    filters: FILTER_DEFAULTS,
    time: { storageKey: TIME_SELECTION_STORAGE_KEY },
  })
  const search = useDebounce(filters.search, SEARCH_DEBOUNCE_MS).trim()
  const queryFilters = useMemo(() => ({ service: filters.service, search }), [filters.service, search])
  const terms = useMemo(() => searchTerms(search), [search])

  // Cursors belong to one filtered list: any change (also from the URL,
  // e.g. browser Back) starts at the first page
  const listKey = JSON.stringify([queryFilters, time.timeKey])
  const pagination = usePagination({ defaultPageSize: 20, storageKey: PAGE_SIZE_STORAGE_KEY, resetOn: listKey })
  const page = useMemo(() => ({ limit: pagination.pageSize, cursor: pagination.cursor }), [pagination.pageSize, pagination.cursor])

  const sessions = useSessions(queryFilters, time.range, page)
  const services = useServices()

  const logsUpdateCount = useTelemetryStore((s) => s.logsUpdateCount)
  const spansUpdateCount = useTelemetryStore((s) => s.spansUpdateCount)
  useLiveInvalidate({
    queryKey: queryKeys.sessions.list(queryFilters, time.range, { limit: pagination.pageSize }),
    enabled: pagination.page === 1 && !time.isAbsoluteRange,
    intervalMs: LIVE_POLL_MS,
    updateCount: logsUpdateCount + spansUpdateCount,
    debounceMs: LIVE_DEBOUNCE_MS,
    maxWaitMs: LIVE_MAX_WAIT_MS,
  })

  const rows = sessions.data?.sessions ?? []
  // The API rejects searches without terms or with too many (400, with a
  // message saying why)
  const invalidSearch = sessions.isError && Boolean(search) && isHTTPError(sessions.error, 400)

  let content
  if (sessions.isPending) {
    content = <ListSkeleton rows={6} rowClassName="h-20" label="Loading sessions" />
  } else if (sessions.isError && !sessions.data) {
    content = (
      <div id={invalidSearch ? searchErrorId : undefined}>
        <QueryError
          error={sessions.error}
          title={invalidSearch ? 'Invalid search' : "Couldn't load sessions"}
          onRetry={invalidSearch ? undefined : () => void sessions.refetch()}
          retrying={sessions.isFetching}
        />
      </div>
    )
  } else if (rows.length === 0) {
    content = search ? (
      <EmptyState
        icon={SearchX}
        title={`No sessions match “${search}”`}
        description="Every word (or “quoted phrase”) must appear in one message, tool call or event of the session. Try fewer words or a longer time range."
      />
    ) : (
      <EmptyState
        icon={MessageSquare}
        title={filters.service ? 'No sessions of this service in this time range' : 'No sessions in this time range'}
        description={
          <>
            Import session data using <code className="rounded bg-muted px-1">ai-observer import</code> or configure your AI coding
            tools to send telemetry.
          </>
        }
      />
    )
  } else {
    content = (
      <>
        {sessions.isError && (
          <QueryError
            error={sessions.error}
            title="Couldn't refresh sessions"
            onRetry={() => void sessions.refetch()}
            retrying={sessions.isFetching}
          />
        )}
        <Refreshing active={sessions.isPlaceholderData}>
          <ul className="space-y-2" aria-label="Sessions">
            {rows.map((session) => (
              <SessionRow
                // A session ID can be listed once per service
                key={`${session.serviceName}:${session.sessionId}`}
                session={session}
                serviceName={displayName(session.serviceName)}
                linkState={linkState}
                terms={terms}
              />
            ))}
          </ul>
        </Refreshing>
        <DataPagination
          page={pagination.page}
          pageSize={pagination.pageSize}
          itemCount={rows.length}
          total={sessions.data?.total ?? 0}
          // A placeholder's nextCursor belongs to the page shown before
          hasNext={!sessions.isPlaceholderData && Boolean(sessions.data?.nextCursor)}
          onFirst={pagination.resetToFirstPage}
          onPrevious={pagination.previousPage}
          onNext={() => pagination.nextPage(sessions.isPlaceholderData ? undefined : sessions.data?.nextCursor)}
          onPageSizeChange={pagination.setPageSize}
          pageSizeOptions={[10, 20, 50, 100]}
        />
      </>
    )
  }

  return (
    <div className="space-y-6">
      <PageHeader title="Sessions" description="View and search sessions from your AI coding tools" />

      <Card>
        <CardContent className="pt-6">
          <FilterBar>
            <TimeRangeFilter id={timeRangeId} value={time.timeSelection} onChange={time.setTimeSelection} />
            <ServiceFilter value={filters.service} onChange={(service) => setFilters({ service })} services={services.data ?? []} />
            <SearchFilter
              label="Search sessions"
              placeholder='Search prompts, responses, tool calls... ("exact phrase")'
              value={filters.search}
              onChange={(value) => setFilters({ search: value })}
              invalid={invalidSearch}
              describedBy={invalidSearch ? searchErrorId : undefined}
            />
          </FilterBar>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <MessageSquare className="h-5 w-5" aria-hidden="true" />
            {search ? 'Matching sessions' : 'Sessions'}
          </CardTitle>
        </CardHeader>
        <CardContent>{content}</CardContent>
      </Card>
    </div>
  )
}
