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

// Request deduplication cache for coalescing concurrent identical requests
interface PendingRequest<T> {
  promise: Promise<T>
  timestamp: number
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
  // Network errors are retryable
  if (error instanceof TypeError && error.message.includes('fetch')) {
    return true
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

function dedupedFetch<T>(key: string, fetchFn: () => Promise<T>): Promise<T> {
  const now = Date.now()
  const existing = pendingRequests.get(key)

  if (existing && now - existing.timestamp < CACHE_TTL_MS) {
    return existing.promise as Promise<T>
  }

  const promise = fetchFn().finally(() => {
    // Clean up after TTL expires
    setTimeout(() => {
      const current = pendingRequests.get(key)
      if (current?.promise === promise) {
        pendingRequests.delete(key)
      }
    }, CACHE_TTL_MS)
  })

  pendingRequests.set(key, { promise, timestamp: now })
  return promise
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
  rawTraceCount: number
  codexOperationCount: number
  spanCount: number
  logCount: number
  metricCount: number
  serviceCount: number
  services: string[]
  errorRate: number
}

interface ServicesResponse {
  services: string[]
}

interface QueryParams {
  service?: string
  from?: string
  to?: string
  limit?: number
  offset?: number
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

async function fetchJSON<T>(url: string, options?: FetchOptions): Promise<T> {
  const shouldRetry = options?.retry !== false
  let lastError: Error | null = null
  let lastStatus: number | undefined

  for (let attempt = 0; attempt <= (shouldRetry ? RETRY_CONFIG.maxRetries : 0); attempt++) {
    try {
      // Create abort controller for timeout
      const timeoutController = new AbortController()
      const timeoutId = setTimeout(() => timeoutController.abort(), REQUEST_TIMEOUT_MS)

      // Combine external signal with timeout signal
      const signal = options?.signal
        ? combineSignals(options.signal, timeoutController.signal)
        : timeoutController.signal

      const response = await fetch(url, { signal })
      clearTimeout(timeoutId)

      if (!response.ok) {
        lastStatus = response.status
        const error = new HTTPError(response.status, `HTTP error! status: ${response.status}`)

        // Only retry on retryable errors
        if (shouldRetry && attempt < RETRY_CONFIG.maxRetries && isRetryableError(error, response.status)) {
          lastError = error
          const delay = calculateBackoffDelay(attempt)
          await sleep(delay)
          continue
        }
        throw error
      }

      return response.json()
    } catch (error) {
      // Don't retry if aborted by user
      if (isAbortError(error)) {
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
    }
  }

  throw lastError || new Error('Request failed after retries')
}

// Helper to combine multiple AbortSignals
function combineSignals(...signals: AbortSignal[]): AbortSignal {
  const controller = new AbortController()

  for (const signal of signals) {
    if (signal.aborted) {
      controller.abort(signal.reason)
      return controller.signal
    }
    signal.addEventListener('abort', () => controller.abort(signal.reason), { once: true })
  }

  return controller.signal
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
  async getStats(options?: FetchOptions): Promise<StatsResponse> {
    return fetchJSON(`${API_BASE}/stats`, options)
  },

  // Services
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
    })
    return fetchJSON(`${API_BASE}/traces${query}`, options)
  },

  async getTrace(id: string, kind: TraceKind, options?: FetchOptions): Promise<SpansResponse> {
    return fetchJSON(`${API_BASE}/traces/${id}?kind=${kind}`, options)
  },

  async getTraceSpans(id: string, kind: TraceKind, options?: FetchOptions): Promise<SpansResponse> {
    return fetchJSON(`${API_BASE}/traces/${id}/spans?kind=${kind}`, options)
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
    return dedupedFetch(cacheKey, () =>
      fetchJSON<TimeSeriesResponse>(`${API_BASE}/metrics/series${query}`, options)
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
    const response = await fetch(`${API_BASE}/metrics/batch-series`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        from: params.from,
        to: params.to,
        interval: params.intervalSeconds,
        queries: params.queries,
      }),
      signal: options?.signal,
    })
    if (!response.ok) {
      throw new Error(`HTTP error! status: ${response.status}`)
    }
    return response.json()
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
    })
    return fetchJSON(`${API_BASE}/logs${query}`, options)
  },

  async getLogLevels(options?: FetchOptions): Promise<LogLevelsResponse> {
    return fetchJSON(`${API_BASE}/logs/levels`, options)
  },

  // Sessions
  async getSessions(params: QueryParams = {}, options?: FetchOptions): Promise<SessionsResponse> {
    const query = buildQueryString({
      service: params.service,
      from: params.from,
      to: params.to,
      limit: params.limit ?? 50,
      offset: params.offset ?? 0,
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
    const response = await fetch(`${API_BASE}/dashboards`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(req),
    })
    if (!response.ok) {
      throw new Error(`HTTP error! status: ${response.status}`)
    }
    return response.json()
  },

  async getDefaultDashboard(): Promise<DashboardWithWidgets | null> {
    const response = await fetch(`${API_BASE}/dashboards/default`)
    if (response.status === 404) {
      // No default dashboard exists yet
      return null
    }
    if (!response.ok) {
      throw new Error(`HTTP error! status: ${response.status}`)
    }
    return response.json()
  },

  async getDashboard(id: string): Promise<DashboardWithWidgets> {
    return fetchJSON(`${API_BASE}/dashboards/${id}`)
  },

  async updateDashboard(id: string, req: UpdateDashboardRequest): Promise<Dashboard> {
    const response = await fetch(`${API_BASE}/dashboards/${id}`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(req),
    })
    if (!response.ok) {
      throw new Error(`HTTP error! status: ${response.status}`)
    }
    return response.json()
  },

  async deleteDashboard(id: string): Promise<void> {
    const response = await fetch(`${API_BASE}/dashboards/${id}`, {
      method: 'DELETE',
    })
    if (!response.ok) {
      throw new Error(`HTTP error! status: ${response.status}`)
    }
  },

  async setDefaultDashboard(id: string): Promise<void> {
    const response = await fetch(`${API_BASE}/dashboards/${id}/default`, {
      method: 'PUT',
    })
    if (!response.ok) {
      throw new Error(`HTTP error! status: ${response.status}`)
    }
  },

  async createWidget(dashboardId: string, req: CreateWidgetRequest): Promise<DashboardWidget> {
    const response = await fetch(`${API_BASE}/dashboards/${dashboardId}/widgets`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(req),
    })
    if (!response.ok) {
      throw new Error(`HTTP error! status: ${response.status}`)
    }
    return response.json()
  },

  async updateWidgetPositions(dashboardId: string, positions: WidgetPosition[]): Promise<void> {
    const response = await fetch(`${API_BASE}/dashboards/${dashboardId}/widgets/positions`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ positions }),
    })
    if (!response.ok) {
      throw new Error(`HTTP error! status: ${response.status}`)
    }
  },

  async updateWidget(dashboardId: string, widgetId: string, req: UpdateWidgetRequest): Promise<DashboardWidget> {
    const response = await fetch(`${API_BASE}/dashboards/${dashboardId}/widgets/${widgetId}`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(req),
    })
    if (!response.ok) {
      throw new Error(`HTTP error! status: ${response.status}`)
    }
    return response.json()
  },

  async deleteWidget(dashboardId: string, widgetId: string): Promise<void> {
    const response = await fetch(`${API_BASE}/dashboards/${dashboardId}/widgets/${widgetId}`, {
      method: 'DELETE',
    })
    if (!response.ok) {
      throw new Error(`HTTP error! status: ${response.status}`)
    }
  },
}

export type { StatsResponse, ServicesResponse }
