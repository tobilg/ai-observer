import type { TracesResponse, SpansResponse, TraceKind } from '@/types/traces'
import type { MetricsResponse, TimeSeriesResponse, MetricNamesResponse, TimeSeries } from '@/types/metrics'
import type { LogsResponse, LogLevelsResponse } from '@/types/logs'
import type { SessionsResponse, TranscriptResponse } from '@/types/sessions'
import { isAbortError } from '@/lib/errors'
import type {
  Dashboard,
  DashboardWithWidgets,
  DashboardsResponse,
  DashboardWidget,
  CreateDashboardRequest,
  UpdateDashboardRequest,
  CreateWidgetRequest,
  UpdateWidgetRequest,
  WidgetPosition,
} from '@/types/dashboard'

const API_BASE = '/api'

// Retry configuration
const RETRY_CONFIG = {
  maxRetries: 5,
  baseDelayMs: 100,
  maxDelayMs: 1600,
  jitterFactor: 0.1, // 10% jitter
}

// Request timeout configuration
const REQUEST_TIMEOUT_MS = 30000 // 30 seconds

// Request deduplication cache for coalescing concurrent identical requests.
// The shared request has its own signal: it is only aborted when every caller
// that passed a signal has aborted, never because the first caller did.
interface PendingRequest<T> {
  promise: Promise<T>
  timestamp: number
  controller: AbortController
  waiting: number // callers that have not aborted
}

const pendingRequests = new Map<string, PendingRequest<unknown>>()
const CACHE_TTL_MS = 100 // 100ms TTL to coalesce same-frame requests

// Calculate exponential backoff delay with jitter
function calculateBackoffDelay(attempt: number): number {
  const exponentialDelay = Math.min(
    RETRY_CONFIG.baseDelayMs * Math.pow(2, attempt),
    RETRY_CONFIG.maxDelayMs
  )
  // Add jitter to prevent thundering herd
  const jitter = exponentialDelay * RETRY_CONFIG.jitterFactor * (Math.random() * 2 - 1)
  return Math.max(0, exponentialDelay + jitter)
}

// Sleep helper
function sleep(ms: number): Promise<void> {
  return new Promise(resolve => setTimeout(resolve, ms))
}

// Check if error is retryable (network errors, 5xx errors)
function isRetryableError(error: unknown, status?: number): boolean {
  // Network errors are retryable. fetch rejects with a TypeError whose
  // message differs by browser ("Failed to fetch", "Load failed", ...).
  if (error instanceof TypeError) {
    return true
  }
  // A query that hit the server's timeout would time out again
  if (status === 503 || status === 504) {
    return false
  }
  // 5xx server errors are retryable
  if (status && status >= 500) {
    return true
  }
  // 429 Too Many Requests is retryable
  if (status === 429) {
    return true
  }
  return false
}

function getCacheKey(endpoint: string, params: Record<string, unknown>): string {
  return `${endpoint}:${JSON.stringify(params)}`
}

function abortError(): Error {
  const error = new Error('The operation was aborted')
  error.name = 'AbortError'
  return error
}

function dedupedFetch<T>(key: string, fetchFn: (signal: AbortSignal) => Promise<T>, signal?: AbortSignal): Promise<T> {
  const now = Date.now()
  let entry = pendingRequests.get(key) as PendingRequest<T> | undefined

  if (!entry || now - entry.timestamp >= CACHE_TTL_MS || entry.controller.signal.aborted) {
    const controller = new AbortController()
    const promise = fetchFn(controller.signal).finally(() => {
      // Clean up after TTL expires
      setTimeout(() => {
        const current = pendingRequests.get(key)
        if (current?.promise === promise) {
          pendingRequests.delete(key)
        }
      }, CACHE_TTL_MS)
    })
    entry = { promise, timestamp: now, controller, waiting: 0 }
    pendingRequests.set(key, entry as PendingRequest<unknown>)
  }

  // A caller without a signal never aborts, so the request is never aborted
  entry.waiting++
  if (!signal) {
    return entry.promise
  }

  const shared = entry
  return new Promise<T>((resolve, reject) => {
    const leave = () => {
      shared.waiting--
      if (shared.waiting === 0) {
        shared.controller.abort()
      }
      reject(abortError())
    }
    if (signal.aborted) {
      leave()
      return
    }
    signal.addEventListener('abort', leave, { once: true })
    shared.promise.then(
      (value) => {
        signal.removeEventListener('abort', leave)
        resolve(value)
      },
      (error) => {
        signal.removeEventListener('abort', leave)
        reject(error)
      },
    )
  })
}

// Batch metric series types
export interface MetricQuery {
  id: string
  name: string
  service?: string
  aggregate?: boolean
}

export interface MetricQueryResult {
  id: string
  success: boolean
  error?: string
  series?: TimeSeries[]
}

export interface BatchMetricSeriesResponse {
  results: MetricQueryResult[]
}

interface StatsResponse {
  traceCount: number
  spanCount: number
  logCount: number
  metricCount: number
  serviceCount: number
  services: string[]
  errorRate: number
}

// A supported AI coding tool (GET /api/providers)
export interface Provider {
  tool: string
  serviceName: string
  displayName: string
  sessionFiles: boolean
}

interface ProvidersResponse {
  providers: Provider[]
}

interface ServicesResponse {
  services: string[]
}

interface QueryParams {
  service?: string
  from?: string
  to?: string
  limit?: number
  // Deprecated by cursor, which keeps pages stable while data arrives
  offset?: number
  // nextCursor of the previous page
  cursor?: string
}

interface FetchOptions {
  signal?: AbortSignal
  retry?: boolean // Disable retry for specific requests
}

// Custom error class for HTTP errors
class HTTPError extends Error {
  status: number
  constructor(status: number, message: string) {
    super(message)
    this.name = 'HTTPError'
    this.status = status
  }
}

function httpError(status: number, message?: string): HTTPError {
  if (status === 401) {
    return new HTTPError(status, 'Authentication required: open AI Observer once with ?token=<AI_OBSERVER_AUTH_TOKEN>')
  }
  return new HTTPError(status, `HTTP error! status: ${status}${message ? `: ${message}` : ''}`)
}

// The message of an API error response ({"error", "message"}), if any
async function errorMessage(response: Response): Promise<string | undefined> {
  try {
    const body = await response.json()
    return typeof body?.message === 'string' ? body.message : undefined
  } catch {
    return undefined
  }
}

async function fetchJSON<T>(url: string, options?: FetchOptions): Promise<T> {
  const shouldRetry = options?.retry !== false
  let lastError: Error | null = null
  let lastStatus: number | undefined

  for (let attempt = 0; attempt <= (shouldRetry ? RETRY_CONFIG.maxRetries : 0); attempt++) {
    // Abort when the request, including reading its body, takes too long
    const timeoutController = new AbortController()
    const timeoutId = setTimeout(() => timeoutController.abort(), REQUEST_TIMEOUT_MS)
    const combined = options?.signal
      ? combineSignals(options.signal, timeoutController.signal)
      : { signal: timeoutController.signal, cleanup: () => {} }

    try {
      const response = await fetch(url, { signal: combined.signal })

      if (!response.ok) {
        lastStatus = response.status
        const error = httpError(response.status)

        // Only retry on retryable errors
        if (shouldRetry && attempt < RETRY_CONFIG.maxRetries && isRetryableError(error, response.status)) {
          lastError = error
          const delay = calculateBackoffDelay(attempt)
          await sleep(delay)
          continue
        }
        // The server's explanation, e.g. of an invalid parameter
        throw httpError(response.status, await errorMessage(response))
      }

      return await response.json()
    } catch (error) {
      // Don't retry if aborted by user. A timeout is an error the page
      // shows, not an abort it ignores.
      if (isAbortError(error)) {
        if (timeoutController.signal.aborted && !options?.signal?.aborted) {
          throw new Error(`Request timed out after ${REQUEST_TIMEOUT_MS / 1000} seconds`, { cause: error })
        }
        throw error
      }

      lastError = error instanceof Error ? error : new Error(String(error))

      // Check if we should retry
      if (shouldRetry && attempt < RETRY_CONFIG.maxRetries && isRetryableError(error, lastStatus)) {
        const delay = calculateBackoffDelay(attempt)
        await sleep(delay)
        continue
      }

      throw lastError
    } finally {
      clearTimeout(timeoutId)
      combined.cleanup()
    }
  }

  throw lastError || new Error('Request failed after retries')
}

// Sends a request that changes data. It is not retried: the server may have
// applied it before failing.
async function sendRequest(url: string, init: RequestInit): Promise<Response> {
  const response = await fetch(url, init)
  if (!response.ok) {
    throw httpError(response.status, await errorMessage(response))
  }
  return response
}

async function sendJSON<T>(url: string, init: RequestInit): Promise<T> {
  return (await sendRequest(url, init)).json()
}

function jsonBody(method: string, body: unknown): RequestInit {
  return {
    method,
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
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

  async getServices(): Promise<ServicesResponse> {
    return fetchJSON(`${API_BASE}/services`)
  },

  // Traces
  async getTraces(params: QueryParams & { search?: string } = {}, options?: FetchOptions): Promise<TracesResponse> {
    const query = buildQueryString({
      service: params.service,
      search: params.search,
      from: params.from,
      to: params.to,
      limit: params.limit ?? 10,
      offset: params.offset ?? 0,
      cursor: params.cursor,
    })
    return fetchJSON(`${API_BASE}/traces${query}`, options)
  },

  async getTrace(id: string, kind: TraceKind, options?: FetchOptions): Promise<SpansResponse> {
    return fetchJSON(`${API_BASE}/traces/${encodeURIComponent(id)}?kind=${kind}`, options)
  },

  async getTraceSpans(id: string, kind: TraceKind, options?: FetchOptions): Promise<SpansResponse> {
    return fetchJSON(`${API_BASE}/traces/${encodeURIComponent(id)}/spans?kind=${kind}`, options)
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
      offset: params.offset ?? 0,
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
  }, options?: FetchOptions): Promise<TimeSeriesResponse> {
    const query = buildQueryString({
      name: params.name,
      service: params.service,
      from: params.from,
      to: params.to,
      interval: params.intervalSeconds?.toString(),
      aggregate: params.aggregate ? 'true' : undefined,
    })

    // Use deduplication to coalesce concurrent identical requests
    const cacheKey = getCacheKey(`${API_BASE}/metrics/series`, params)
    return dedupedFetch(cacheKey, (signal) =>
      fetchJSON<TimeSeriesResponse>(`${API_BASE}/metrics/series${query}`, { ...options, signal }),
      options?.signal,
    )
  },

  async getBreakdownValues(params: {
    name: string
    attribute: string
    service?: string
  }, options?: FetchOptions): Promise<{ values: string[] }> {
    const query = buildQueryString({
      name: params.name,
      attribute: params.attribute,
      service: params.service,
    })
    return fetchJSON(`${API_BASE}/metrics/breakdown-values${query}`, options)
  },

  // Batch metrics
  async getBatchMetricSeries(params: {
    from: string
    to: string
    intervalSeconds?: number
    queries: MetricQuery[]
  }, options?: FetchOptions): Promise<BatchMetricSeriesResponse> {
    return sendJSON(`${API_BASE}/metrics/batch-series`, {
      ...jsonBody('POST', {
        from: params.from,
        to: params.to,
        interval: params.intervalSeconds,
        queries: params.queries,
      }),
      signal: options?.signal,
    })
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
      offset: params.offset ?? 0,
      cursor: params.cursor,
    })
    return fetchJSON(`${API_BASE}/logs${query}`, options)
  },

  async getLogLevels(range: TimeRange = {}, options?: FetchOptions): Promise<LogLevelsResponse> {
    return fetchJSON(`${API_BASE}/logs/levels${buildQueryString({ from: range.from, to: range.to })}`, options)
  },

  // Sessions
  async getSessions(params: QueryParams = {}, options?: FetchOptions): Promise<SessionsResponse> {
    const query = buildQueryString({
      service: params.service,
      from: params.from,
      to: params.to,
      limit: params.limit ?? 50,
      offset: params.offset ?? 0,
      cursor: params.cursor,
    })
    return fetchJSON(`${API_BASE}/sessions${query}`, options)
  },

  async getSessionTranscript(sessionId: string, options?: FetchOptions): Promise<TranscriptResponse> {
    return fetchJSON(`${API_BASE}/sessions/${encodeURIComponent(sessionId)}/transcript`, options)
  },

  // Dashboards
  async getDashboards(): Promise<DashboardsResponse> {
    return fetchJSON(`${API_BASE}/dashboards`)
  },

  async createDashboard(req: CreateDashboardRequest): Promise<Dashboard> {
    return sendJSON(`${API_BASE}/dashboards`, jsonBody('POST', req))
  },

  async getDefaultDashboard(): Promise<DashboardWithWidgets | null> {
    const response = await fetch(`${API_BASE}/dashboards/default`)
    if (response.status === 404) {
      // No default dashboard exists yet
      return null
    }
    if (!response.ok) {
      throw httpError(response.status, await errorMessage(response))
    }
    return response.json()
  },

  async getDashboard(id: string): Promise<DashboardWithWidgets> {
    return fetchJSON(`${API_BASE}/dashboards/${id}`)
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
    await sendRequest(`${API_BASE}/dashboards/${dashboardId}/widgets/positions`, jsonBody('PUT', { positions }))
  },

  async updateWidget(dashboardId: string, widgetId: string, req: UpdateWidgetRequest): Promise<DashboardWidget> {
    return sendJSON(`${API_BASE}/dashboards/${dashboardId}/widgets/${widgetId}`, jsonBody('PUT', req))
  },

  async deleteWidget(dashboardId: string, widgetId: string): Promise<void> {
    await sendRequest(`${API_BASE}/dashboards/${dashboardId}/widgets/${widgetId}`, { method: 'DELETE' })
  },
}

export type { StatsResponse, ServicesResponse }
