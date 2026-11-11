import { createElement, useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { Link, useParams } from 'react-router-dom'
import { useQueryClient } from '@tanstack/react-query'
import { ArrowLeft, ChevronDown, ChevronUp, Clock, MessageSquare, RefreshCw, Search } from 'lucide-react'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Separator } from '@/components/ui/separator'
import { Skeleton } from '@/components/ui/skeleton'
import { Switch } from '@/components/ui/switch'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { CopyButton } from '@/components/common/copy-button'
import { EmptyState } from '@/components/common/empty-state'
import { ListSkeleton } from '@/components/common/list-skeleton'
import { QueryError } from '@/components/common/query-error'
import { ChatBubbleView } from '@/components/sessions/ChatBubbleView'
import { TimelineView } from '@/components/sessions/TimelineView'
import { api } from '@/lib/api'
import { errorText, isAbortError, isTimeoutError } from '@/lib/errors'
import { formatCost, formatCount, formatTimeSpan, formatTokens } from '@/lib/format'
import { queryKeys, useSessionTranscript, useTranscriptSearch } from '@/lib/queries'
import {
  appendTranscriptPage,
  countRoles,
  DEFAULT_TRANSCRIPT_FILTERS,
  filterTranscript,
  loadedMessageCount,
  passesFilters,
  transcriptMessages,
  type TranscriptData,
  type TranscriptFilters,
} from '@/lib/transcript'
import { formatTimestamp, cn } from '@/lib/utils'
import { getServiceIcon } from '@/lib/metricMetadata'
import { useBackLink } from '@/hooks/useBackLink'
import { useDebounce } from '@/hooks/useDebounce'
import { useDocumentTitle } from '@/hooks/useDocumentTitle'
import { useLiveRefresh } from '@/hooks/useLiveRefresh'
import { useLocalStorage } from '@/hooks/useLocalStorage'
import { useServiceNames } from '@/hooks/useServiceNames'
import { useTelemetryStore } from '@/stores/telemetryStore'
import type { TranscriptTotals } from '@/types/sessions'

const TRANSCRIPT_FILTERS_STORAGE_KEY = 'ai-observer-session-transcript-filters-v1'

// Typing pauses this long before the session is searched
const SEARCH_DEBOUNCE_MS = 300

// The tab title shows the start of the session's title
const DOCUMENT_TITLE_CHARS = 60

const NO_MATCHES: readonly number[] = []
const NO_TERMS: readonly string[] = []

type RoleFilterKey = keyof Pick<TranscriptFilters, 'showUser' | 'showAssistant' | 'showToolUse' | 'showToolResult'>

// The server stops queries after 5 seconds (504), and the client gives up on
// a response after 30: both mean the session has too many records
function isTooLarge(err: unknown): boolean {
  return isTimeoutError(err) || (err as { status?: number } | null)?.status === 504
}

export function SessionTranscriptPage() {
  const { sessionId = '' } = useParams<{ sessionId: string }>()
  const back = useBackLink('/sessions')
  const [viewMode, setViewMode] = useState<'chat' | 'timeline'>('chat')

  const backButton = (
    <Button variant="ghost" size="sm" className="-ml-2 gap-2" asChild>
      <Link to={back.to}>
        <ArrowLeft className="h-4 w-4" />
        {back.label}
      </Link>
    </Button>
  )

  if (!sessionId) {
    return (
      <div className="space-y-6">
        {backButton}
        <EmptyState title="Session ID is required" />
      </div>
    )
  }

  // Keyed by the session: search, the current match and expanded messages
  // belong to one session
  return (
    <SessionTranscript
      key={sessionId}
      sessionId={sessionId}
      backButton={backButton}
      viewMode={viewMode}
      onViewModeChange={setViewMode}
    />
  )
}

interface SessionTranscriptProps {
  sessionId: string
  backButton: ReactNode
  viewMode: 'chat' | 'timeline'
  onViewModeChange: (mode: 'chat' | 'timeline') => void
}

function SessionTranscript({ sessionId, backButton, viewMode, onViewModeChange }: SessionTranscriptProps) {
  const queryClient = useQueryClient()
  const { displayName } = useServiceNames()
  const [filters, setFilters] = useLocalStorage<TranscriptFilters>(TRANSCRIPT_FILTERS_STORAGE_KEY, DEFAULT_TRANSCRIPT_FILTERS)

  // The transcript a page (500 messages) at a time. The key holds the
  // session, so a page of another session that arrives late is dropped.
  const query = useSessionTranscript(sessionId)
  const { fetchNextPage, hasNextPage, isFetchingNextPage, isFetchNextPageError } = query
  const messages = useMemo(() => transcriptMessages(query.data), [query.data])
  const pages = query.data?.pages
  const first = pages?.[0]
  const last = pages?.[pages.length - 1]
  // The latest page has the latest totals (new messages may have come)
  const totals = last?.totals ?? first?.totals
  const title = first?.title || last?.title
  const totalMessages = last?.total ?? messages.length
  const loaded = messages.length
  const allLoaded = !hasNextPage

  useDocumentTitle(
    title ? (title.length > DOCUMENT_TITLE_CHARS ? `${title.slice(0, DOCUMENT_TITLE_CHARS - 1)}…` : title) : 'Session',
  )

  // ---- Search over the whole session (server-side) ----
  const [searchInput, setSearchInput] = useState('')
  const search = useDebounce(searchInput.trim(), SEARCH_DEBOUNCE_MS)
  const searchQuery = useTranscriptSearch(sessionId, search)
  const searchResult = search ? searchQuery.data : undefined
  const matches = searchResult?.matches ?? NO_MATCHES
  const terms = searchResult?.terms ?? NO_TERMS
  // The current match of the search: index into matches; a new search
  // starts at its first. count changes with each navigation, which scrolls
  // to the match again.
  const [nav, setNav] = useState({ search: '', index: 0, count: 0 })
  const navigated = nav.search === search
  const matchIndex = matches.length > 0 ? Math.min(navigated ? nav.index : 0, matches.length - 1) : -1
  const activePosition = matchIndex >= 0 ? matches[matchIndex] : undefined
  const revealKey = activePosition === undefined ? undefined : `${search}#${navigated ? nav.count : 0}`

  const goToMatch = (delta: number) => {
    if (matches.length === 0) return
    setNav((prev) => {
      const current = prev.search === search ? Math.min(prev.index, matches.length - 1) : 0
      return { search, index: (current + delta + matches.length) % matches.length, count: prev.count + 1 }
    })
  }

  // A match on a page not loaded yet: load the pages up to it, in order
  const matchNotLoaded = activePosition !== undefined && activePosition >= loaded
  useEffect(() => {
    if (matchNotLoaded && hasNextPage && !isFetchingNextPage && !isFetchNextPageError) {
      void fetchNextPage()
    }
  }, [matchNotLoaded, hasNextPage, isFetchingNextPage, isFetchNextPageError, fetchNextPage])

  // ---- Live: new messages of the open session ----
  // Its events arrive as logs, or as spans for tools whose conversations are
  // traced. Notifications do not say which session they belong to and each
  // request assembles the transcript again, so the page waits longer than
  // list pages before it asks.
  const logsUpdateCount = useTelemetryStore((state) => state.logsUpdateCount)
  const spansUpdateCount = useTelemetryStore((state) => state.spansUpdateCount)
  const refreshKey = useLiveRefresh({
    enabled: true,
    intervalMs: 15_000,
    updateCount: logsUpdateCount + spansUpdateCount,
    debounceMs: 2000,
    maxWaitMs: 10_000,
  })
  const liveRef = useRef({ data: query.data, hasNextPage, isFetching: query.isFetching, search })
  useEffect(() => {
    liveRef.current = { data: query.data, hasNextPage, isFetching: query.isFetching, search }
  })

  useEffect(() => {
    const { data, hasNextPage: more, isFetching, search: currentSearch } = liveRef.current
    // With further pages to load, new messages come with the last of them;
    // a request in flight brings them too
    if (refreshKey === 0 || !data || more || isFetching) {
      return
    }
    // Ask for the messages after the loaded ones and append them as a page,
    // instead of refetching every page of a long session
    const cursor = String(loadedMessageCount(data))
    const controller = new AbortController()
    api.getSessionTranscript(sessionId, { signal: controller.signal }, cursor)
      .then((page) => {
        if (controller.signal.aborted || page.messages.length === 0) {
          return
        }
        queryClient.setQueryData<TranscriptData>(queryKeys.sessions.transcript(sessionId), (old) =>
          appendTranscriptPage(old, page, cursor),
        )
        if (currentSearch) {
          // The new messages may match
          void queryClient.invalidateQueries({ queryKey: queryKeys.sessions.transcriptSearch(sessionId, currentSearch) })
        }
      })
      .catch((err) => {
        if (!isAbortError(err)) {
          console.error('Failed to refresh transcript:', err) // the next refresh tries again
        }
      })
    return () => controller.abort()
  }, [refreshKey, sessionId, queryClient])

  // ---- Filters ----
  const loadedCounts = useMemo(() => countRoles(messages), [messages])
  // The current match is shown even when the filters hide its role
  const filtered = useMemo(() => filterTranscript(messages, filters, activePosition), [messages, filters, activePosition])
  const matchHiddenByFilters =
    activePosition !== undefined && activePosition < loaded && !passesFilters(messages[activePosition], filters)

  const toggleRole = (key: RoleFilterKey) => setFilters((prev) => ({ ...prev, [key]: !prev[key] }))
  const resetFilters = () => setFilters(DEFAULT_TRANSCRIPT_FILTERS)
  const filtersChanged = JSON.stringify(filters) !== JSON.stringify(DEFAULT_TRANSCRIPT_FILTERS)

  // Counts of the whole session where the totals have them; the others are
  // of the loaded messages until all are loaded
  const loadedSuffix = allLoaded ? '' : ' loaded'
  const roleFilters: Array<{ key: RoleFilterKey; label: string; count: number; suffix: string }> = [
    { key: 'showUser', label: 'User', count: totals?.userMessages ?? loadedCounts.user, suffix: totals ? '' : loadedSuffix },
    { key: 'showAssistant', label: 'Assistant', count: totals?.assistantMessages ?? loadedCounts.assistant, suffix: totals ? '' : loadedSuffix },
    { key: 'showToolUse', label: 'Tool calls', count: totals?.toolCalls ?? loadedCounts.tool_use, suffix: totals ? '' : loadedSuffix },
    { key: 'showToolResult', label: 'Tool results', count: loadedCounts.tool_result, suffix: loadedSuffix },
  ]

  if (query.isPending) {
    return (
      <div className="space-y-6">
        {backButton}
        <Card>
          <CardHeader className="space-y-3">
            <Skeleton className="h-7 w-2/3" />
            <Skeleton className="h-4 w-1/3" />
          </CardHeader>
        </Card>
        <ListSkeleton rows={6} rowClassName="h-20" label="Loading transcript" />
      </div>
    )
  }

  if (query.isError && !query.data) {
    const tooLarge = isTooLarge(query.error)
    return (
      <div className="space-y-6">
        {backButton}
        <QueryError
          error={tooLarge ? null : query.error}
          title="Couldn't load the session transcript"
          fallback={tooLarge ? 'This session has too many records to load in time' : 'Failed to load session transcript'}
          onRetry={() => query.refetch()}
          retrying={query.isFetching}
        />
      </div>
    )
  }

  if (!first) {
    return null
  }

  const serviceIcon = getServiceIcon(first.serviceName)

  let searchStatus: string | null = null
  if (search) {
    if (searchQuery.isError) {
      searchStatus = errorText(searchQuery.error, 'The search failed')
    } else if (!searchResult) {
      searchStatus = 'Searching…'
    } else if (matches.length === 0) {
      searchStatus = 'No matches'
    } else {
      searchStatus = `Match ${formatCount(matchIndex + 1)} of ${formatCount(matches.length)}`
      if (matchNotLoaded) searchStatus += ' · loading its page…'
      else if (matchHiddenByFilters) searchStatus += ' · shown although the filters hide it'
    }
  }

  return (
    <div className="space-y-6">
      {backButton}

      {/* Session info */}
      <Card>
        <CardHeader>
          <div className="flex items-start gap-4">
            {createElement(serviceIcon, { className: 'h-10 w-10 shrink-0 text-muted-foreground', 'aria-hidden': true })}
            <div className="min-w-0 flex-1 space-y-2">
              <h1 className="line-clamp-3 break-words text-xl font-semibold leading-snug" title={title}>
                {title || 'Session'}
              </h1>
              <div className="flex min-w-0 flex-wrap items-center gap-2 text-sm text-muted-foreground">
                <span className="break-all font-mono">{first.sessionId}</span>
                <CopyButton value={first.sessionId} label="Copy session ID" className="-my-1 size-7" />
                <Badge variant="outline">{displayName(first.serviceName)}</Badge>
              </div>
              <div className="flex flex-wrap items-center gap-x-4 gap-y-1 text-sm text-muted-foreground">
                <span className="flex items-center gap-1">
                  <Clock className="h-4 w-4" aria-hidden="true" />
                  {formatTimestamp(first.startTime)}
                </span>
                <span>Duration: {formatTimeSpan(first.startTime, last?.lastTime ?? first.lastTime, 'long')}</span>
                <span className="flex items-center gap-1">
                  <MessageSquare className="h-4 w-4" aria-hidden="true" />
                  <span>{formatCount(totalMessages)} messages</span>
                </span>
              </div>
              {totals && <SessionTotals totals={totals} />}
            </div>
          </div>
        </CardHeader>
      </Card>

      {/* Transcript */}
      <Card className="overflow-hidden">
        <CardHeader>
          <div className="flex flex-wrap items-center justify-between gap-4">
            <CardTitle>Transcript</CardTitle>
            <Tabs value={viewMode} onValueChange={(v) => onViewModeChange(v as 'chat' | 'timeline')}>
              <TabsList>
                <TabsTrigger value="chat">Chat</TabsTrigger>
                <TabsTrigger value="timeline">Timeline</TabsTrigger>
              </TabsList>
            </Tabs>
          </div>
          {messages.length > 0 && (
            <>
              <Separator className="my-3" />
              <div className="flex flex-col gap-3">
                {/* Search */}
                <div className="flex flex-wrap items-center gap-2">
                  <div className="relative min-w-48 flex-1">
                    <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" aria-hidden="true" />
                    <Input
                      type="search"
                      value={searchInput}
                      onChange={(e) => setSearchInput(e.target.value)}
                      onKeyDown={(e) => {
                        if (e.key === 'Enter') {
                          e.preventDefault()
                          goToMatch(e.shiftKey ? -1 : 1)
                        }
                      }}
                      placeholder='Search this session: words or "a phrase"'
                      aria-label="Search this session"
                      className="pl-9"
                    />
                  </div>
                  {searchStatus && (
                    <span className={cn('text-sm tabular-nums', searchQuery.isError ? 'text-error' : 'text-muted-foreground')} role="status">
                      {searchStatus}
                    </span>
                  )}
                  <Button
                    type="button"
                    variant="outline"
                    size="icon-sm"
                    aria-label="Previous match"
                    disabled={matches.length === 0}
                    onClick={() => goToMatch(-1)}
                  >
                    <ChevronUp className="h-4 w-4" />
                  </Button>
                  <Button
                    type="button"
                    variant="outline"
                    size="icon-sm"
                    aria-label="Next match"
                    disabled={matches.length === 0}
                    onClick={() => goToMatch(1)}
                  >
                    <ChevronDown className="h-4 w-4" />
                  </Button>
                </div>

                {/* Filters */}
                <div className="flex flex-wrap items-center gap-2">
                  <span className="mr-1 text-sm text-muted-foreground">Show:</span>
                  {roleFilters.map(({ key, label, count, suffix }) => (
                    <Button
                      key={key}
                      type="button"
                      size="sm"
                      variant={filters[key] ? 'secondary' : 'outline'}
                      aria-pressed={filters[key]}
                      onClick={() => toggleRole(key)}
                      disabled={count === 0}
                    >
                      {label} ({formatCount(count)}{suffix})
                    </Button>
                  ))}

                  <Separator orientation="vertical" className="mx-1 h-6" />

                  <div className="flex items-center gap-2" title="Assistant responses of OTLP events, which carry tokens and cost but no content">
                    <Switch
                      id="transcript-show-metadata-only"
                      checked={!filters.hideMetadataOnlyAssistant}
                      onCheckedChange={(checked) => setFilters((prev) => ({ ...prev, hideMetadataOnlyAssistant: !checked }))}
                      disabled={loadedCounts.metadataOnlyAssistant === 0}
                    />
                    <Label htmlFor="transcript-show-metadata-only" className="font-normal">
                      Show responses without content ({formatCount(loadedCounts.metadataOnlyAssistant)}{loadedSuffix})
                    </Label>
                  </div>

                  <Button
                    type="button"
                    size="sm"
                    variant="ghost"
                    onClick={resetFilters}
                    disabled={!filtersChanged}
                    className="ml-auto"
                  >
                    Reset
                  </Button>
                </div>

                <div className="text-xs text-muted-foreground">
                  Showing {formatCount(filtered.messages.length)} of {formatCount(loaded)}
                  {allLoaded ? ' messages' : ` loaded messages (${formatCount(totalMessages)} in the session)`}
                </div>
              </div>
            </>
          )}
        </CardHeader>
        <CardContent>
          {messages.length === 0 ? (
            <EmptyState title="No messages in this session" icon={MessageSquare} />
          ) : filtered.messages.length === 0 ? (
            <EmptyState
              title="No messages match your current filters."
              action={
                <Button type="button" variant="outline" size="sm" onClick={resetFilters}>
                  Reset filters
                </Button>
              }
            />
          ) : viewMode === 'chat' ? (
            <ChatBubbleView
              messages={filtered.messages}
              positions={filtered.positions}
              highlight={terms}
              activePosition={activePosition}
              revealKey={revealKey}
            />
          ) : (
            <TimelineView
              messages={filtered.messages}
              positions={filtered.positions}
              highlight={terms}
              activePosition={activePosition}
              revealKey={revealKey}
            />
          )}
          {!allLoaded && (
            <div className="mt-6 flex flex-col items-center gap-2 text-sm text-muted-foreground">
              <span>
                Loaded the first {formatCount(loaded)} of {formatCount(totalMessages)} messages
              </span>
              {isFetchNextPageError && (
                <span className="text-error" role="alert">
                  {isTooLarge(query.error)
                    ? 'This session has too many records to load in time'
                    : errorText(query.error, 'Failed to load more messages')}
                </span>
              )}
              <Button
                type="button"
                variant="outline"
                size="sm"
                className="gap-2"
                onClick={() => fetchNextPage()}
                disabled={isFetchingNextPage}
              >
                {isFetchNextPageError && <RefreshCw className="h-3 w-3" />}
                {isFetchingNextPage ? 'Loading...' : isFetchNextPageError ? 'Try again' : 'Load more messages'}
              </Button>
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  )
}

// Usage and activity of the whole session (not only the loaded pages)
function SessionTotals({ totals }: { totals: TranscriptTotals }) {
  const hasUsage = totals.costUsd > 0 || totals.inputTokens > 0 || totals.outputTokens > 0
  const items: Array<[string, ReactNode]> = [
    ...(hasUsage
      ? ([
          ['Cost', formatCost(totals.costUsd)],
          ['Input', formatTokens(totals.inputTokens)],
          ['Output', formatTokens(totals.outputTokens)],
          ['Cache read', formatTokens(totals.cacheRead)],
          ['Cache write', formatTokens(totals.cacheWrite)],
        ] as Array<[string, ReactNode]>)
      : []),
    ['Prompts', formatCount(totals.userMessages)],
    ['Responses', formatCount(totals.assistantMessages)],
    ['Tool calls', formatCount(totals.toolCalls)],
    [
      'Tool errors',
      <span className={cn(totals.toolErrors > 0 && 'text-error')}>{formatCount(totals.toolErrors)}</span>,
    ],
  ]
  return (
    <dl className="grid grid-cols-2 gap-x-6 gap-y-2 pt-2 text-sm sm:grid-cols-3 lg:grid-cols-5" aria-label="Session totals">
      {items.map(([label, value]) => (
        <div key={label} className="min-w-0">
          <dt className="text-xs text-muted-foreground">{label}</dt>
          <dd className="font-medium tabular-nums">{value}</dd>
        </div>
      ))}
    </dl>
  )
}
