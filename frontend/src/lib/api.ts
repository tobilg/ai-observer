import type { TracesResponse, SpansResponse, TraceKind } from '@/types/traces'
import type {
  MetricsResponse,
  TimeSeriesResponse,
  MetricNamesResponse,
  MetricAttributeKeysResponse,
  BreakdownValuesResponse,
  MetricQuery,
  BatchMetricSeriesRequest,
  BatchMetricSeriesResponse,
  HistogramStat,
} from '@/types/metrics'
import type { LogsResponse, LogLevelsResponse } from '@/types/logs'
import type { SessionsResponse, TranscriptResponse } from '@/types/sessions'
import type {
  AdminBackupRequest,
  AdminBackupResponse,
  AdminCheckpointResponse,
  AdminDeleteRequest,
  AdminDeleteResponse,
  StatusResponse,
} from '@/types/status'
import type { StatsResponse, Provider, ProvidersResponse, ServicesResponse } from '@/types/api.gen'
import { TimeoutError, httpError, isAbortError, isHTTPError } from '@/lib/errors'
import type {
  Dashboard,
  DashboardWithWidgets,
  DashboardsResponse,
  DashboardWidget,
  CreateDashboardRequest,
  UpdateDashboardRequest,
  CreateWidgetRequest,
  UpdateWidgetRequest,
  UpdateWidgetPositionsRequest,
  WidgetPosition,
} from '@/types/dashboard'

// The API client only sends requests. Retrying failed reads and coalescing
// identical ones is TanStack Query's job (lib/queryClient.ts, lib/queries.ts),
// so there is a single retry policy.

export { HTTPError } from '@/lib/errors'

const API_BASE = '/api'

// Every request, including reading its body, gives up after this long and
// fails with a TimeoutError
export const REQUEST_TIMEOUT_MS = 30_000

// Types of requests and responses, generated from the Go types of the API
// (types/api.gen.ts); re-exported where callers import them from here
export type {
  MetricQuery,
  MetricQueryErrorCode,
  MetricQueryResult,
  BatchMetricSeriesResponse,
} from '@/types/metrics'
export type { StatsResponse, Provider, ProvidersResponse, ServicesResponse }

interface QueryParams {
  service?: string
  from?: string
  to?: string
  limit?: number
  // Deprecated by cursor, which keeps pages stable while data arrives; only
  // sent when given
  offset?: number
  // nextCursor of the previous page
  cursor?: string
}

export interface FetchOptions {
  // Aborts the request (TanStack Query passes one to every queryFn)
  signal?: AbortSignal
  // Overrides REQUEST_TIMEOUT_MS, for operations that take long
  timeoutMs?: number
}

// Admin operations (backup, deletion) can take minutes on a large database;
// a client that gives up cancels them on the server
export const ADMIN_TIMEOUT_MS = 30 * 60_000

// The message of an API error response ({"error", "message"}), if any
async function errorMessage(response: Response): Promise<string | undefined> {
  try {
    const body = await response.json()
    return typeof body?.message === 'string' ? body.message : undefined
  } catch {
    return undefined
  }
}

// Combines AbortSignals; cleanup removes the listeners from the inputs
function combineSignals(...signals: AbortSignal[]): { signal: AbortSignal; cleanup: () => void } {
  const controller = new AbortController()
  const listeners: Array<[AbortSignal, () => void]> = []
  const cleanup = () => {
    for (const [signal, listener] of listeners) {
      signal.removeEventListener('abort', listener)
    }
  }

  for (const signal of signals) {
    if (signal.aborted) {
      controller.abort(signal.reason)
      cleanup()
      return { signal: controller.signal, cleanup: () => {} }
    }
    const listener = () => controller.abort(signal.reason)
    signal.addEventListener('abort', listener, { once: true })
    listeners.push([signal, listener])
  }

  return { signal: controller.signal, cleanup }
}

/**
 * Sends a request and reads its response with read, within
 * REQUEST_TIMEOUT_MS. An error status throws an HTTPError carrying the
 * server's message; the timeout throws a TimeoutError, an abort by the
 * caller's signal the AbortError fetch raised.
 */
async function request<T>(
  url: string,
  init: RequestInit | undefined,
  options: FetchOptions | undefined,
  read: (response: Response) => Promise<T>,
): Promise<T> {
  const timeoutController = new AbortController()
  const timeoutMs = options?.timeoutMs ?? REQUEST_TIMEOUT_MS
  const timeoutId = setTimeout(() => timeoutController.abort(), timeoutMs)
  const combined = options?.signal
    ? combineSignals(options.signal, timeoutController.signal)
    : { signal: timeoutController.signal, cleanup: () => {} }

  try {
    const response = await fetch(url, { ...init, signal: combined.signal })
    if (!response.ok) {
      throw httpError(response.status, await errorMessage(response))
    }
    return await read(response)
  } catch (error) {
    // A timeout is an error the page shows, not an abort it ignores
    if (isAbortError(error) && timeoutController.signal.aborted && !options?.signal?.aborted) {
      throw new TimeoutError(`Request timed out after ${timeoutMs / 1000} seconds`, { cause: error })
    }
    throw error
  } finally {
    clearTimeout(timeoutId)
    combined.cleanup()
  }
}

function fetchJSON<T>(url: string, options?: FetchOptions): Promise<T> {
  return request(url, undefined, options, (response) => response.json())
}

// Sends a request that changes data. It is never retried: the server may
// have applied it before failing.
async function sendRequest(url: string, init: RequestInit): Promise<void> {
  await request(url, init, undefined, async () => undefined)
}

function sendJSON<T>(url: string, init: RequestInit, options?: FetchOptions): Promise<T> {
  return request(url, init, options, (response) => response.json())
}

function jsonBody(method: string, body: unknown): RequestInit {
  return {
    method,
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  }
}

// An optional time range (RFC 3339); an unset side is open
export interface TimeRange {
  from?: string
  to?: string
}

function buildQueryString(params: Record<string, string | number | undefined>): string {
  const searchParams = new URLSearchParams()
  for (const [key, value] of Object.entries(params)) {
    if (value !== undefined) {
      searchParams.append(key, String(value))
    }
  }
  const queryString = searchParams.toString()
  return queryString ? `?${queryString}` : ''
}

export const api = {
  // Stats
  // Without a range the counts cover all stored telemetry
  async getStats(range: TimeRange = {}, options?: FetchOptions): Promise<StatsResponse> {
    return fetchJSON(`${API_BASE}/stats${buildQueryString({ from: range.from, to: range.to })}`, options)
  },

  // Services
  async getProviders(options?: FetchOptions): Promise<ProvidersResponse> {
    return fetchJSON(`${API_BASE}/providers`, options)
  },

  async getServices(options?: FetchOptions): Promise<ServicesResponse> {
    return fetchJSON(`${API_BASE}/services`, options)
  },

  // Traces
  // background includes the background traces of Codex's app and exec
  // servers (internal requests outside a conversation), which are left out
  // by default
  async getTraces(params: QueryParams & { search?: string; background?: boolean } = {}, options?: FetchOptions): Promise<TracesResponse> {
    const query = buildQueryString({
      service: params.service,
      search: params.search,
      background: params.background ? 'true' : undefined,
      from: params.from,
      to: params.to,
      limit: params.limit ?? 10,
      offset: params.offset,
      cursor: params.cursor,
    })
    return fetchJSON(`${API_BASE}/traces${query}`, options)
  },

  async getTrace(id: string, kind: TraceKind, options?: FetchOptions): Promise<SpansResponse> {
    return fetchJSON(`${API_BASE}/traces/${encodeURIComponent(id)}?kind=${kind}`, options)
  },

  // A page of a trace's spans, oldest first (1,000 a page); cursor is the
  // previous page's nextCursor
  async getTraceSpans(id: string, kind: TraceKind, options?: FetchOptions, cursor?: string): Promise<SpansResponse> {
    const query = buildQueryString({ kind, cursor })
    return fetchJSON(`${API_BASE}/traces/${encodeURIComponent(id)}/spans${query}`, options)
  },

  async getRecentTraces(limit: number = 10, params: Pick<QueryParams, 'from' | 'to'> = {}, options?: FetchOptions): Promise<TracesResponse> {
    const query = buildQueryString({
      limit,
      from: params.from,
      to: params.to,
    })
    return fetchJSON(`${API_BASE}/traces/recent${query}`, options)
  },

  // Metrics
  async getMetrics(params: QueryParams & { name?: string; type?: string } = {}, options?: FetchOptions): Promise<MetricsResponse> {
    const query = buildQueryString({
      service: params.service,
      name: params.name,
      type: params.type,
      from: params.from,
      to: params.to,
      limit: params.limit ?? 10,
      offset: params.offset,
      cursor: params.cursor,
    })
    return fetchJSON(`${API_BASE}/metrics${query}`, options)
  },

  async getMetricNames(service?: string, options?: FetchOptions): Promise<MetricNamesResponse> {
    const query = buildQueryString({ service })
    return fetchJSON(`${API_BASE}/metrics/names${query}`, options)
  },

  async getMetricSeries(params: {
    name: string
    service?: string
    from?: string
    to?: string
    intervalSeconds?: number
    aggregate?: boolean
    groupBy?: string[]
    filters?: Record<string, string>
    stat?: HistogramStat
    merge?: boolean
    timeZone?: string
  }, options?: FetchOptions): Promise<TimeSeriesResponse> {
    const filters: Record<string, string> = {}
    for (const [key, value] of Object.entries(params.filters ?? {})) {
      filters[`attr.${key}`] = value
    }
    const query = buildQueryString({
      name: params.name,
      service: params.service,
      from: params.from,
      to: params.to,
      interval: params.intervalSeconds?.toString(),
      aggregate: params.aggregate ? 'true' : undefined,
      groupBy: params.groupBy?.length ? params.groupBy.join(',') : undefined,
      stat: params.stat || undefined,
      merge: params.merge ? 'true' : undefined,
      tz: params.timeZone,
      ...filters,
    })
    return fetchJSON(`${API_BASE}/metrics/series${query}`, options)
  },

  // The attribute keys of a metric's points, which series can be grouped
  // and filtered by
  async getMetricAttributeKeys(params: { name: string; service?: string }, options?: FetchOptions): Promise<MetricAttributeKeysResponse> {
    const query = buildQueryString({ name: params.name, service: params.service })
    return fetchJSON(`${API_BASE}/metrics/attribute-keys${query}`, options)
  },

  async getBreakdownValues(params: {
    name: string
    attribute: string
    service?: string
  }, options?: FetchOptions): Promise<BreakdownValuesResponse> {
    const query = buildQueryString({
      name: params.name,
      attribute: params.attribute,
      service: params.service,
    })
    return fetchJSON(`${API_BASE}/metrics/breakdown-values${query}`, options)
  },

  // Batch metrics: a read sent as POST (the queries do not fit a URL)
  async getBatchMetricSeries(params: {
    from: string
    to: string
    intervalSeconds?: number
    timeZone?: string
    queries: MetricQuery[]
  }, options?: FetchOptions): Promise<BatchMetricSeriesResponse> {
    const body: BatchMetricSeriesRequest = {
      from: params.from,
      to: params.to,
      interval: params.intervalSeconds,
      timeZone: params.timeZone,
      queries: params.queries,
    }
    return sendJSON(`${API_BASE}/metrics/batch-series`, jsonBody('POST', body), options)
  },

  // Logs
  async getLogs(params: QueryParams & {
    severity?: string
    traceId?: string
    search?: string
  } = {}, options?: FetchOptions): Promise<LogsResponse> {
    const query = buildQueryString({
      service: params.service,
      severity: params.severity,
      traceId: params.traceId,
      search: params.search,
      from: params.from,
      to: params.to,
      limit: params.limit ?? 10,
      offset: params.offset,
      cursor: params.cursor,
    })
    return fetchJSON(`${API_BASE}/logs${query}`, options)
  },

  async getLogLevels(range: TimeRange = {}, options?: FetchOptions): Promise<LogLevelsResponse> {
    return fetchJSON(`${API_BASE}/logs/levels${buildQueryString({ from: range.from, to: range.to })}`, options)
  },

  // Sessions
  // search: words and "quoted phrases" that one record of the session must
  // all contain; results are ordered by newest match and carry a snippet
  async getSessions(params: QueryParams & { search?: string } = {}, options?: FetchOptions): Promise<SessionsResponse> {
    const query = buildQueryString({
      service: params.service,
      search: params.search,
      from: params.from,
      to: params.to,
      limit: params.limit ?? 50,
      offset: params.offset,
      cursor: params.cursor,
    })
    return fetchJSON(`${API_BASE}/sessions${query}`, options)
  },

  // A page of a session's transcript; cursor is the previous page's nextCursor
  // (the number of messages before the page). With search, the response's
  // search lists the positions of all matching messages of the session.
  async getSessionTranscript(
    sessionId: string,
    options?: FetchOptions,
    cursor?: string,
    params: { search?: string; limit?: number } = {},
  ): Promise<TranscriptResponse> {
    const query = buildQueryString({ cursor, limit: params.limit, search: params.search || undefined })
    return fetchJSON(`${API_BASE}/sessions/${encodeURIComponent(sessionId)}/transcript${query}`, options)
  },

  // Dashboards
  async getDashboards(options?: FetchOptions): Promise<DashboardsResponse> {
    return fetchJSON(`${API_BASE}/dashboards`, options)
  },

  async createDashboard(req: CreateDashboardRequest): Promise<Dashboard> {
    return sendJSON(`${API_BASE}/dashboards`, jsonBody('POST', req))
  },

  // null when no default dashboard exists yet (404)
  async getDefaultDashboard(options?: FetchOptions): Promise<DashboardWithWidgets | null> {
    try {
      return await fetchJSON<DashboardWithWidgets>(`${API_BASE}/dashboards/default`, options)
    } catch (error) {
      if (isHTTPError(error, 404)) {
        return null
      }
      throw error
    }
  },

  async getDashboard(id: string, options?: FetchOptions): Promise<DashboardWithWidgets> {
    return fetchJSON(`${API_BASE}/dashboards/${encodeURIComponent(id)}`, options)
  },

  async updateDashboard(id: string, req: UpdateDashboardRequest): Promise<Dashboard> {
    return sendJSON(`${API_BASE}/dashboards/${id}`, jsonBody('PUT', req))
  },

  async deleteDashboard(id: string): Promise<void> {
    await sendRequest(`${API_BASE}/dashboards/${id}`, { method: 'DELETE' })
  },

  async setDefaultDashboard(id: string): Promise<void> {
    await sendRequest(`${API_BASE}/dashboards/${id}/default`, { method: 'PUT' })
  },

  async createWidget(dashboardId: string, req: CreateWidgetRequest): Promise<DashboardWidget> {
    return sendJSON(`${API_BASE}/dashboards/${dashboardId}/widgets`, jsonBody('POST', req))
  },

  async updateWidgetPositions(dashboardId: string, positions: WidgetPosition[]): Promise<void> {
    const body: UpdateWidgetPositionsRequest = { positions }
    await sendRequest(`${API_BASE}/dashboards/${dashboardId}/widgets/positions`, jsonBody('PUT', body))
  },

  async updateWidget(dashboardId: string, widgetId: string, req: UpdateWidgetRequest): Promise<DashboardWidget> {
    return sendJSON(`${API_BASE}/dashboards/${dashboardId}/widgets/${widgetId}`, jsonBody('PUT', req))
  },

  async deleteWidget(dashboardId: string, widgetId: string): Promise<void> {
    await sendRequest(`${API_BASE}/dashboards/${dashboardId}/widgets/${widgetId}`, { method: 'DELETE' })
  },

  // Server status: version, mode, endpoints, database, settings, pricing, imports
  async getStatus(options?: FetchOptions): Promise<StatusResponse> {
    return fetchJSON(`${API_BASE}/status`, options)
  },

  // Admin API (AI_OBSERVER_ADMIN_API=true): maintenance while the server runs
  async adminDelete(req: AdminDeleteRequest): Promise<AdminDeleteResponse> {
    return sendJSON(`${API_BASE}/admin/delete`, jsonBody('POST', req), { timeoutMs: ADMIN_TIMEOUT_MS })
  },

  async adminCheckpoint(): Promise<AdminCheckpointResponse> {
    return sendJSON(`${API_BASE}/admin/checkpoint`, { method: 'POST' }, { timeoutMs: ADMIN_TIMEOUT_MS })
  },

  // Writes a backup to the server's backup directory; name defaults to one with the time
  async adminBackup(name?: string): Promise<AdminBackupResponse> {
    const body: AdminBackupRequest = name ? { name } : {}
    return sendJSON(`${API_BASE}/admin/backup`, jsonBody('POST', body), { timeoutMs: ADMIN_TIMEOUT_MS })
  },
}

