import { useEffect } from 'react'
import { keepPreviousData, useInfiniteQuery, useQuery } from '@tanstack/react-query'
import { api, type MetricQuery } from '@/lib/api'
import { resolveRangeParams, type RangeSource, type TimeSelectionKey } from '@/lib/timeUtils'
import { useProviderStore } from '@/stores/providerStore'
import type { TraceKind } from '@/types/traces'
import type { HistogramStat } from '@/types/metrics'

// Query keys and hooks of every read endpoint. Keys are arrays starting with
// the resource, so queryClient.invalidateQueries({ queryKey: queryKeys.logs.all })
// refreshes every logs query. Keys hold the time selection's identity
// (RangeSource.key: timeframe or absolute bounds), never the moving bounds
// of a relative window: queryFns resolve those when they run.

// Filters of the list endpoints; undefined and '' mean "not filtered"
export interface TraceFilters {
  service?: string
  search?: string
  background?: boolean
}

export interface LogFilters {
  service?: string
  severity?: string
  traceId?: string
  search?: string
}

export interface SessionFilters {
  service?: string
  // Words and "quoted phrases" one record of the session must all contain
  search?: string
}

export interface MetricPointFilters {
  service?: string
  name?: string
  type?: string
}

// A page of a cursor-paginated list (usePagination provides both)
export interface PageParams {
  limit: number
  cursor?: string
}

export interface MetricSeriesParams {
  name: string
  service?: string
  intervalSeconds?: number
  aggregate?: boolean
  // Attribute keys the series are grouped by (default: type)
  groupBy?: string[]
  // Exact attribute values the points must have
  filters?: Record<string, string>
  // Statistic of a histogram metric
  stat?: HistogramStat
  // One series of all points
  merge?: boolean
  // IANA zone the buckets follow (browserTimeZone in lib/api)
  timeZone?: string
}

export interface BatchSeriesParams {
  queries: MetricQuery[]
  intervalSeconds?: number
  timeZone?: string
}

export interface MetricAttributeKeysParams {
  name: string
  service?: string
}

export interface BreakdownValuesParams {
  name: string
  attribute: string
  service?: string
}

// Empty strings are not filters: leave them out of keys and requests, so ''
// and undefined share a cache entry
function clean<T extends object>(params: T): T {
  const result = {} as Record<string, unknown>
  for (const [key, value] of Object.entries(params)) {
    if (value !== undefined && value !== '' && value !== false) {
      result[key] = value
    }
  }
  return result as T
}

// The key part of an optional range: null covers all stored data
function rangeKey(range: RangeSource | undefined): TimeSelectionKey | null {
  return range?.key ?? null
}

function rangeParams(range: RangeSource | undefined): { from?: string; to?: string } {
  return range ? resolveRangeParams(range) : {}
}

export const queryKeys = {
  stats: {
    all: ['stats'] as const,
    summary: (range?: RangeSource) => ['stats', rangeKey(range)] as const,
  },
  providers: ['providers'] as const,
  status: ['status'] as const,
  services: ['services'] as const,
  traces: {
    all: ['traces'] as const,
    lists: ['traces', 'list'] as const,
    list: (filters: TraceFilters, range: RangeSource, page: PageParams) =>
      ['traces', 'list', clean(filters), range.key, page] as const,
    recent: (limit: number, range?: RangeSource) => ['traces', 'recent', limit, rangeKey(range)] as const,
    spans: (id: string, kind: TraceKind) => ['traces', 'spans', kind, id] as const,
  },
  metrics: {
    all: ['metrics'] as const,
    names: (service?: string) => ['metrics', 'names', service || null] as const,
    series: (params: MetricSeriesParams, range: RangeSource) => ['metrics', 'series', clean(params), range.key] as const,
    batchSeries: (params: BatchSeriesParams, range: RangeSource) =>
      ['metrics', 'batch-series', params.queries, params.intervalSeconds ?? null, params.timeZone ?? null, range.key] as const,
    attributeKeys: (params: MetricAttributeKeysParams) => ['metrics', 'attribute-keys', clean(params)] as const,
    breakdownValues: (params: BreakdownValuesParams) => ['metrics', 'breakdown-values', clean(params)] as const,
    points: (filters: MetricPointFilters, range: RangeSource, page: PageParams) =>
      ['metrics', 'points', clean(filters), range.key, page] as const,
  },
  logs: {
    all: ['logs'] as const,
    lists: ['logs', 'list'] as const,
    list: (filters: LogFilters, range: RangeSource, page: PageParams) => ['logs', 'list', clean(filters), range.key, page] as const,
    levels: (range?: RangeSource) => ['logs', 'levels', rangeKey(range)] as const,
  },
  sessions: {
    all: ['sessions'] as const,
    lists: ['sessions', 'list'] as const,
    list: (filters: SessionFilters, range: RangeSource, page: PageParams) =>
      ['sessions', 'list', clean(filters), range.key, page] as const,
    transcript: (sessionId: string) => ['sessions', 'transcript', sessionId] as const,
    // Not under transcript(id), so invalidating a transcript's pages leaves it
    transcriptSearch: (sessionId: string, search: string) => ['sessions', 'transcript-search', sessionId, search] as const,
  },
  // For the dashboard store's mutations: invalidate dashboards.all after a change
  dashboards: {
    all: ['dashboards'] as const,
    list: ['dashboards', 'list'] as const,
    default: ['dashboards', 'default'] as const,
    detail: (id: string) => ['dashboards', 'detail', id] as const,
  },
  // useNewItems: count of a list's items since an anchor (Unix ms), asked
  // again when more records were announced in real time
  newItems: (list: string, filters: object, since: number, announced: number) =>
    ['new-items', list, clean(filters), since, announced] as const,
}

// Lists keep showing the previous page while the next one (or the list for
// changed filters) loads; isPlaceholderData tells pages to dim it
const listOptions = { placeholderData: keepPreviousData }

// Lists of what the backend knows change rarely: one fetch serves every page
const SERVICES_STALE_MS = 5 * 60_000

interface QueryOptions {
  enabled?: boolean
}

// ============================================================================
// Stats, providers, services
// ============================================================================

/** Aggregate statistics; without a range all stored telemetry */
export function useStats(range?: RangeSource, options: QueryOptions = {}) {
  return useQuery({
    queryKey: queryKeys.stats.summary(range),
    queryFn: ({ signal }) => api.getStats(rangeParams(range), { signal }),
    placeholderData: keepPreviousData,
    ...options,
  })
}

/**
 * The supported tools (GET /api/providers). Loaded once per app; the
 * result also feeds providerStore, which getServiceDisplayName and
 * serviceFilterOptions read (React components use useServiceNames to
 * re-render when it arrives). Mounted by the layout.
 */
export function useProviders() {
  const query = useQuery({
    queryKey: queryKeys.providers,
    queryFn: ({ signal }) => api.getProviders({ signal }),
    staleTime: Infinity,
  })
  const setProviders = useProviderStore((s) => s.setProviders)
  useEffect(() => {
    if (query.data) {
      setProviders(query.data.providers ?? [])
    }
  }, [query.data, setProviders])
  return query
}

/** Services that have sent telemetry; shared by every page's service filter */
export function useServices(options: QueryOptions = {}) {
  return useQuery({
    queryKey: queryKeys.services,
    queryFn: ({ signal }) => api.getServices({ signal }),
    select: (data) => data.services ?? [],
    staleTime: SERVICES_STALE_MS,
    ...options,
  })
}

// ============================================================================
// Traces
// ============================================================================

export function useTraces(filters: TraceFilters, range: RangeSource, page: PageParams, options: QueryOptions = {}) {
  return useQuery({
    queryKey: queryKeys.traces.list(filters, range, page),
    queryFn: ({ signal }) =>
      api.getTraces({ ...clean(filters), ...resolveRangeParams(range), limit: page.limit, cursor: page.cursor }, { signal }),
    ...listOptions,
    ...options,
  })
}

export function useRecentTraces(limit: number, range?: RangeSource, options: QueryOptions = {}) {
  return useQuery({
    queryKey: queryKeys.traces.recent(limit, range),
    queryFn: ({ signal }) => api.getRecentTraces(limit, rangeParams(range), { signal }),
    ...listOptions,
    ...options,
  })
}

/**
 * The spans of a trace view, a page (1,000 spans) at a time: data.pages[i]
 * is a SpansResponse; fetchNextPage() loads the next while hasNextPage.
 */
export function useTraceSpans(id: string, kind: TraceKind, options: QueryOptions = {}) {
  return useInfiniteQuery({
    queryKey: queryKeys.traces.spans(id, kind),
    queryFn: ({ signal, pageParam }) => api.getTraceSpans(id, kind, { signal }, pageParam),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (last) => (last.hasMore && last.nextCursor ? last.nextCursor : undefined),
    ...options,
  })
}

// ============================================================================
// Metrics
// ============================================================================

export function useMetricNames(service?: string, options: QueryOptions = {}) {
  return useQuery({
    queryKey: queryKeys.metrics.names(service),
    queryFn: ({ signal }) => api.getMetricNames(service || undefined, { signal }),
    select: (data) => data.names ?? [],
    staleTime: SERVICES_STALE_MS,
    ...options,
  })
}

export function useMetricSeries(params: MetricSeriesParams, range: RangeSource, options: QueryOptions = {}) {
  return useQuery({
    queryKey: queryKeys.metrics.series(params, range),
    queryFn: ({ signal }) => api.getMetricSeries({ ...clean(params), name: params.name, ...resolveRangeParams(range) }, { signal }),
    placeholderData: keepPreviousData,
    ...options,
    enabled: Boolean(params.name) && (options.enabled ?? true),
  })
}

/** Several series in one request (dashboard widgets); data.results by query id */
export function useBatchMetricSeries(params: BatchSeriesParams, range: RangeSource, options: QueryOptions = {}) {
  return useQuery({
    queryKey: queryKeys.metrics.batchSeries(params, range),
    queryFn: ({ signal }) =>
      api.getBatchMetricSeries(
        { ...resolveRangeParams(range), intervalSeconds: params.intervalSeconds, timeZone: params.timeZone, queries: params.queries },
        { signal },
      ),
    placeholderData: keepPreviousData,
    ...options,
    enabled: params.queries.length > 0 && (options.enabled ?? true),
  })
}

export function useBreakdownValues(params: BreakdownValuesParams, options: QueryOptions = {}) {
  return useQuery({
    queryKey: queryKeys.metrics.breakdownValues(params),
    queryFn: ({ signal }) => api.getBreakdownValues({ ...clean(params), name: params.name, attribute: params.attribute }, { signal }),
    select: (data) => data.values ?? [],
    ...options,
    enabled: Boolean(params.name && params.attribute) && (options.enabled ?? true),
  })
}

/** The attribute keys of a metric's points (what series can be grouped by) */
export function useMetricAttributeKeys(params: MetricAttributeKeysParams, options: QueryOptions = {}) {
  return useQuery({
    queryKey: queryKeys.metrics.attributeKeys(params),
    queryFn: ({ signal }) => api.getMetricAttributeKeys({ ...clean(params), name: params.name }, { signal }),
    select: (data) => data.keys ?? [],
    staleTime: SERVICES_STALE_MS,
    ...options,
    enabled: Boolean(params.name) && (options.enabled ?? true),
  })
}

/** Raw metric data points (GET /api/metrics) */
export function useMetricPoints(filters: MetricPointFilters, range: RangeSource, page: PageParams, options: QueryOptions = {}) {
  return useQuery({
    queryKey: queryKeys.metrics.points(filters, range, page),
    queryFn: ({ signal }) =>
      api.getMetrics({ ...clean(filters), ...resolveRangeParams(range), limit: page.limit, cursor: page.cursor }, { signal }),
    ...listOptions,
    ...options,
  })
}

// ============================================================================
// Logs
// ============================================================================

export function useLogs(filters: LogFilters, range: RangeSource, page: PageParams, options: QueryOptions = {}) {
  return useQuery({
    queryKey: queryKeys.logs.list(filters, range, page),
    queryFn: ({ signal }) =>
      api.getLogs({ ...clean(filters), ...resolveRangeParams(range), limit: page.limit, cursor: page.cursor }, { signal }),
    ...listOptions,
    ...options,
  })
}

/** Log counts by severity; without a range all logs */
export function useLogLevels(range?: RangeSource, options: QueryOptions = {}) {
  return useQuery({
    queryKey: queryKeys.logs.levels(range),
    queryFn: ({ signal }) => api.getLogLevels(rangeParams(range), { signal }),
    placeholderData: keepPreviousData,
    ...options,
  })
}

// ============================================================================
// Sessions
// ============================================================================

export function useSessions(filters: SessionFilters, range: RangeSource, page: PageParams, options: QueryOptions = {}) {
  return useQuery({
    queryKey: queryKeys.sessions.list(filters, range, page),
    queryFn: ({ signal }) =>
      api.getSessions({ ...clean(filters), ...resolveRangeParams(range), limit: page.limit, cursor: page.cursor }, { signal }),
    ...listOptions,
    ...options,
  })
}

/**
 * A session's transcript, a page at a time: data.pages[i] is a
 * TranscriptResponse (oldest messages first); fetchNextPage() loads the
 * next while hasNextPage.
 */
export function useSessionTranscript(sessionId: string, options: QueryOptions = {}) {
  return useInfiniteQuery({
    queryKey: queryKeys.sessions.transcript(sessionId),
    queryFn: ({ signal, pageParam }) => api.getSessionTranscript(sessionId, { signal }, pageParam),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (last) => (last.hasMore && last.nextCursor ? last.nextCursor : undefined),
    ...options,
    enabled: Boolean(sessionId) && (options.enabled ?? true),
  })
}

/**
 * The positions of the messages of a whole transcript that contain all
 * terms of search (data: { terms, matches }); disabled for an empty search.
 * Asks for one message only: the matches cover the whole transcript.
 */
export function useTranscriptSearch(sessionId: string, search: string, options: QueryOptions = {}) {
  return useQuery({
    queryKey: queryKeys.sessions.transcriptSearch(sessionId, search),
    queryFn: async ({ signal }) => {
      const page = await api.getSessionTranscript(sessionId, { signal }, undefined, { search, limit: 1 })
      return page.search ?? { terms: [], matches: [] }
    },
    ...options,
    enabled: Boolean(sessionId && search.trim()) && (options.enabled ?? true),
  })
}

// ============================================================================
// Server status
// ============================================================================

/** GET /api/status (the status page and the sidebar's version) */
export function useStatus(options: QueryOptions = {}) {
  return useQuery({
    queryKey: queryKeys.status,
    queryFn: ({ signal }) => api.getStatus({ signal }),
    staleTime: 30_000,
    ...options,
  })
}

// ============================================================================
// Dashboards
// ============================================================================

export function useDashboards(options: QueryOptions = {}) {
  return useQuery({
    queryKey: queryKeys.dashboards.list,
    queryFn: ({ signal }) => api.getDashboards({ signal }),
    select: (data) => data.dashboards ?? [],
    ...options,
  })
}

/** The default dashboard with its widgets; the server creates it when none exists (null only from servers before that) */
export function useDefaultDashboard(options: QueryOptions = {}) {
  return useQuery({
    queryKey: queryKeys.dashboards.default,
    queryFn: ({ signal }) => api.getDefaultDashboard({ signal }),
    ...options,
  })
}

export function useDashboard(id: string, options: QueryOptions = {}) {
  return useQuery({
    queryKey: queryKeys.dashboards.detail(id),
    queryFn: ({ signal }) => api.getDashboard(id, { signal }),
    ...options,
    enabled: Boolean(id) && (options.enabled ?? true),
  })
}

// ============================================================================
// Counts for "N new items" (useNewItems): the list endpoint with the list's
// filters and limit 1, reading its total
// ============================================================================

export type CountBetween = (range: { from: Date; to: Date }, signal: AbortSignal) => Promise<number>

export function countTraces(filters: TraceFilters): CountBetween {
  return async ({ from, to }, signal) =>
    (await api.getTraces({ ...clean(filters), from: from.toISOString(), to: to.toISOString(), limit: 1 }, { signal })).total ?? 0
}

export function countLogs(filters: LogFilters): CountBetween {
  return async ({ from, to }, signal) =>
    (await api.getLogs({ ...clean(filters), from: from.toISOString(), to: to.toISOString(), limit: 1 }, { signal })).total ?? 0
}

export function countSessions(filters: SessionFilters): CountBetween {
  return async ({ from, to }, signal) =>
    (await api.getSessions({ ...clean(filters), from: from.toISOString(), to: to.toISOString(), limit: 1 }, { signal })).total ?? 0
}
