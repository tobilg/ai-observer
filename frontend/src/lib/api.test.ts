import { describe, it, expect, vi, beforeEach } from 'vitest'
import { api } from './api'

describe('api', () => {
  beforeEach(() => {
    vi.mocked(global.fetch).mockReset()
  })

  const mockFetchResponse = (data: unknown, ok = true, status = 200) => {
    vi.mocked(global.fetch).mockResolvedValueOnce({
      ok,
      status,
      json: async () => data,
    } as Response)
  }

  // Mock error response for all retry attempts (1 initial + 5 retries = 6 total)
  const mockFetchErrorWithRetries = (status: number) => {
    for (let i = 0; i < 6; i++) {
      mockFetchResponse({}, false, status)
    }
  }

  describe('getStats', () => {
    it('fetches stats from /api/stats', async () => {
      const mockStats = {
        traceCount: 100,
        spanCount: 500,
        logCount: 200,
        metricCount: 1000,
        serviceCount: 5,
        services: ['service-a', 'service-b'],
        errorRate: 0.05,
      }
      mockFetchResponse(mockStats)

      const result = await api.getStats()

      expect(global.fetch).toHaveBeenCalledWith('/api/stats', expect.objectContaining({ signal: expect.any(AbortSignal) }))
      expect(result).toEqual(mockStats)
    })

    it('passes a time range to /api/stats', async () => {
      mockFetchResponse({})

      await api.getStats({ from: '2026-01-01T00:00:00.000Z', to: '2026-01-02T00:00:00.000Z' })

      expect(global.fetch).toHaveBeenCalledWith(
        '/api/stats?from=2026-01-01T00%3A00%3A00.000Z&to=2026-01-02T00%3A00%3A00.000Z',
        expect.anything()
      )
    })

    it('throws on error response', async () => {
      // 500 errors are retryable, so mock all 6 retry attempts
      mockFetchErrorWithRetries(500)

      await expect(api.getStats()).rejects.toThrow('HTTP error! status: 500')
    })

    it('reports a request that times out as an error, not an abort', async () => {
      vi.useFakeTimers()
      try {
        vi.mocked(global.fetch).mockImplementationOnce((_url, init) => new Promise((_resolve, reject) => {
          init?.signal?.addEventListener('abort', () => {
            const error = new Error('The operation was aborted')
            error.name = 'AbortError'
            reject(error)
          })
        }))

        const result = api.getStats({}, { signal: new AbortController().signal })
        const assertion = expect(result).rejects.toThrow('Request timed out')
        await vi.advanceTimersByTimeAsync(30000)
        await assertion
      } finally {
        vi.useRealTimers()
      }
    })

    it('does not retry a query that timed out on the server', async () => {
      mockFetchResponse({}, false, 504)

      await expect(api.getStats()).rejects.toThrow('HTTP error! status: 504')
      expect(global.fetch).toHaveBeenCalledTimes(1)
    })
  })

  describe('getServices', () => {
    it('fetches services from /api/services', async () => {
      const mockServices = { services: ['service-a', 'service-b'] }
      mockFetchResponse(mockServices)

      const result = await api.getServices()

      expect(global.fetch).toHaveBeenCalledWith('/api/services', expect.objectContaining({ signal: expect.any(AbortSignal) }))
      expect(result).toEqual(mockServices)
    })
  })

  describe('getTraces', () => {
    it('fetches traces with default params', async () => {
      const mockTraces = { traces: [], total: 0, hasMore: false }
      mockFetchResponse(mockTraces)

      const result = await api.getTraces()

      expect(global.fetch).toHaveBeenCalledWith('/api/traces?limit=10&offset=0', expect.objectContaining({ signal: expect.any(AbortSignal) }))
      expect(result).toEqual(mockTraces)
    })

    it('fetches traces with custom params', async () => {
      const mockTraces = { traces: [], total: 0, hasMore: false }
      mockFetchResponse(mockTraces)

      await api.getTraces({
        service: 'my-service',
        from: '2024-01-01T00:00:00Z',
        to: '2024-01-02T00:00:00Z',
        limit: 25,
        offset: 10,
      })

      expect(global.fetch).toHaveBeenCalledWith(
        '/api/traces?service=my-service&from=2024-01-01T00%3A00%3A00Z&to=2024-01-02T00%3A00%3A00Z&limit=25&offset=10',
        expect.objectContaining({ signal: expect.any(AbortSignal) })
      )
    })

    it('omits undefined params', async () => {
      mockFetchResponse({ traces: [], total: 0, hasMore: false })

      await api.getTraces({ service: 'my-service' })

      expect(global.fetch).toHaveBeenCalledWith(
        '/api/traces?service=my-service&limit=10&offset=0',
        expect.objectContaining({ signal: expect.any(AbortSignal) })
      )
    })
  })

  describe('getTrace', () => {
    it('fetches a single trace by ID', async () => {
      const mockSpans = { spans: [] }
      mockFetchResponse(mockSpans)

      const result = await api.getTrace('trace-123', 'otel_trace')

      expect(global.fetch).toHaveBeenCalledWith('/api/traces/trace-123?kind=otel_trace', expect.objectContaining({ signal: expect.any(AbortSignal) }))
      expect(result).toEqual(mockSpans)
    })
  })

  describe('getTraceSpans', () => {
    it('fetches spans for a trace', async () => {
      const mockSpans = { spans: [] }
      mockFetchResponse(mockSpans)

      const result = await api.getTraceSpans('trace-456', 'otel_trace')

      expect(global.fetch).toHaveBeenCalledWith('/api/traces/trace-456/spans?kind=otel_trace', expect.objectContaining({ signal: expect.any(AbortSignal) }))
      expect(result).toEqual(mockSpans)
    })
  })

  describe('getMetrics', () => {
    it('fetches metrics with default params', async () => {
      const mockMetrics = { metrics: [], total: 0, hasMore: false }
      mockFetchResponse(mockMetrics)

      await api.getMetrics()

      expect(global.fetch).toHaveBeenCalledWith('/api/metrics?limit=10&offset=0', expect.objectContaining({ signal: expect.any(AbortSignal) }))
    })

    it('fetches metrics with name and type filters', async () => {
      mockFetchResponse({ metrics: [], total: 0, hasMore: false })

      await api.getMetrics({
        service: 'my-service',
        name: 'http_requests_total',
        type: 'counter',
      })

      expect(global.fetch).toHaveBeenCalledWith(
        '/api/metrics?service=my-service&name=http_requests_total&type=counter&limit=10&offset=0',
        expect.objectContaining({ signal: expect.any(AbortSignal) })
      )
    })
  })

  describe('getMetricNames', () => {
    it('fetches metric names', async () => {
      const mockNames = { names: ['metric_a', 'metric_b'] }
      mockFetchResponse(mockNames)

      const result = await api.getMetricNames()

      expect(global.fetch).toHaveBeenCalledWith('/api/metrics/names', expect.objectContaining({ signal: expect.any(AbortSignal) }))
      expect(result).toEqual(mockNames)
    })
  })

  describe('getMetricSeries', () => {
    it('fetches metric series with required params', async () => {
      const mockSeries = { series: [] }
      mockFetchResponse(mockSeries)

      await api.getMetricSeries({ name: 'http_requests_total' })

      expect(global.fetch).toHaveBeenCalledWith(
        '/api/metrics/series?name=http_requests_total',
        expect.objectContaining({ signal: expect.any(AbortSignal) })
      )
    })

    it('fetches metric series with all params', async () => {
      mockFetchResponse({ series: [] })

      await api.getMetricSeries({
        name: 'http_requests_total',
        service: 'my-service',
        from: '2024-01-01T00:00:00Z',
        to: '2024-01-02T00:00:00Z',
        intervalSeconds: 60,
      })

      expect(global.fetch).toHaveBeenCalledWith(
        '/api/metrics/series?name=http_requests_total&service=my-service&from=2024-01-01T00%3A00%3A00Z&to=2024-01-02T00%3A00%3A00Z&interval=60',
        expect.objectContaining({ signal: expect.any(AbortSignal) })
      )
    })
  })

  describe('getBreakdownValues', () => {
    it('fetches breakdown values with required params', async () => {
      const mockValues = { values: ['added', 'removed'] }
      mockFetchResponse(mockValues)

      const result = await api.getBreakdownValues({
        name: 'lines_of_code',
        attribute: 'type',
      })

      expect(global.fetch).toHaveBeenCalledWith(
        '/api/metrics/breakdown-values?name=lines_of_code&attribute=type',
        expect.objectContaining({ signal: expect.any(AbortSignal) })
      )
      expect(result).toEqual(mockValues)
    })

    it('fetches breakdown values with optional service', async () => {
      const mockValues = { values: ['input', 'output'] }
      mockFetchResponse(mockValues)

      const result = await api.getBreakdownValues({
        name: 'token_usage',
        attribute: 'type',
        service: 'my-service',
      })

      expect(global.fetch).toHaveBeenCalledWith(
        '/api/metrics/breakdown-values?name=token_usage&attribute=type&service=my-service',
        expect.objectContaining({ signal: expect.any(AbortSignal) })
      )
      expect(result).toEqual(mockValues)
    })
  })

  describe('getLogs', () => {
    it('fetches logs with default params', async () => {
      const mockLogs = { logs: [], total: 0, hasMore: false }
      mockFetchResponse(mockLogs)

      await api.getLogs()

      expect(global.fetch).toHaveBeenCalledWith('/api/logs?limit=10&offset=0', expect.objectContaining({ signal: expect.any(AbortSignal) }))
    })

    it('fetches logs with all filters', async () => {
      mockFetchResponse({ logs: [], total: 0, hasMore: false })

      await api.getLogs({
        service: 'my-service',
        severity: 'ERROR',
        traceId: 'trace-123',
        search: 'error message',
        from: '2024-01-01T00:00:00Z',
        to: '2024-01-02T00:00:00Z',
        limit: 50,
        offset: 25,
      })

      expect(global.fetch).toHaveBeenCalledWith(
        '/api/logs?service=my-service&severity=ERROR&traceId=trace-123&search=error+message&from=2024-01-01T00%3A00%3A00Z&to=2024-01-02T00%3A00%3A00Z&limit=50&offset=25',
        expect.objectContaining({ signal: expect.any(AbortSignal) })
      )
    })
  })

  describe('getLogLevels', () => {
    it('fetches log levels', async () => {
      const mockLevels = { ERROR: 10, WARN: 20, INFO: 100 }
      mockFetchResponse(mockLevels)

      const result = await api.getLogLevels()

      expect(global.fetch).toHaveBeenCalledWith('/api/logs/levels', expect.objectContaining({ signal: expect.any(AbortSignal) }))
      expect(result).toEqual(mockLevels)
    })
  })

  describe('getRecentTraces', () => {
    it('fetches recent traces with default limit', async () => {
      const mockTraces = { traces: [], total: 0, hasMore: false }
      mockFetchResponse(mockTraces)

      const result = await api.getRecentTraces()

      expect(global.fetch).toHaveBeenCalledWith('/api/traces/recent?limit=10', expect.objectContaining({ signal: expect.any(AbortSignal) }))
      expect(result).toEqual(mockTraces)
    })

    it('fetches recent traces with custom limit', async () => {
      const mockTraces = { traces: [], total: 5, hasMore: false }
      mockFetchResponse(mockTraces)

      const result = await api.getRecentTraces(25)

      expect(global.fetch).toHaveBeenCalledWith('/api/traces/recent?limit=25', expect.objectContaining({ signal: expect.any(AbortSignal) }))
      expect(result).toEqual(mockTraces)
    })

    it('fetches recent traces with time range', async () => {
      const mockTraces = { traces: [], total: 0, hasMore: false }
      mockFetchResponse(mockTraces)

      await api.getRecentTraces(10, {
        from: '2024-01-01T00:00:00Z',
        to: '2024-01-02T00:00:00Z',
      })

      expect(global.fetch).toHaveBeenCalledWith(
        '/api/traces/recent?limit=10&from=2024-01-01T00%3A00%3A00Z&to=2024-01-02T00%3A00%3A00Z',
        expect.objectContaining({ signal: expect.any(AbortSignal) })
      )
    })
  })

  describe('getBatchMetricSeries', () => {
    it('posts batch request with queries', async () => {
      const mockResponse = {
        results: [
          { id: 'q1', success: true, series: [] },
          { id: 'q2', success: true, series: [] },
        ],
      }
      mockFetchResponse(mockResponse)

      const result = await api.getBatchMetricSeries({
        from: '2024-01-01T00:00:00Z',
        to: '2024-01-02T00:00:00Z',
        intervalSeconds: 60,
        queries: [
          { id: 'q1', name: 'metric_a' },
          { id: 'q2', name: 'metric_b', service: 'my-service' },
        ],
      })

      expect(global.fetch).toHaveBeenCalledWith('/api/metrics/batch-series', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          from: '2024-01-01T00:00:00Z',
          to: '2024-01-02T00:00:00Z',
          interval: 60,
          queries: [
            { id: 'q1', name: 'metric_a' },
            { id: 'q2', name: 'metric_b', service: 'my-service' },
          ],
        }),
        signal: undefined,
      })
      expect(result).toEqual(mockResponse)
    })

    it('throws on error response', async () => {
      mockFetchResponse({}, false, 500)

      await expect(
        api.getBatchMetricSeries({
          from: '2024-01-01T00:00:00Z',
          to: '2024-01-02T00:00:00Z',
          queries: [],
        })
      ).rejects.toThrow('HTTP error! status: 500')
    })
  })

  describe('dashboard API', () => {
    it('getDashboards returns list of dashboards', async () => {
      const mockDashboards = { dashboards: [{ id: 'd1', name: 'Dashboard 1' }] }
      mockFetchResponse(mockDashboards)

      const result = await api.getDashboards()

      expect(global.fetch).toHaveBeenCalledWith('/api/dashboards', expect.objectContaining({ signal: expect.any(AbortSignal) }))
      expect(result).toEqual(mockDashboards)
    })

    it('createDashboard posts new dashboard', async () => {
      const mockDashboard = { id: 'd1', name: 'New Dashboard' }
      mockFetchResponse(mockDashboard)

      const result = await api.createDashboard({ name: 'New Dashboard' })

      expect(global.fetch).toHaveBeenCalledWith('/api/dashboards', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name: 'New Dashboard' }),
      })
      expect(result).toEqual(mockDashboard)
    })

    it('createDashboard throws on error', async () => {
      mockFetchResponse({}, false, 400)

      await expect(api.createDashboard({ name: '' })).rejects.toThrow('HTTP error! status: 400')
    })

    it('getDefaultDashboard returns null on 404', async () => {
      mockFetchResponse({}, false, 404)

      const result = await api.getDefaultDashboard()

      expect(result).toBeNull()
    })

    it('getDefaultDashboard returns dashboard when exists', async () => {
      const mockDashboard = { id: 'd1', name: 'Default', widgets: [] }
      mockFetchResponse(mockDashboard)

      const result = await api.getDefaultDashboard()

      expect(global.fetch).toHaveBeenCalledWith('/api/dashboards/default')
      expect(result).toEqual(mockDashboard)
    })

    it('getDefaultDashboard throws on other errors', async () => {
      mockFetchResponse({}, false, 500)

      await expect(api.getDefaultDashboard()).rejects.toThrow('HTTP error! status: 500')
    })

    it('getDashboard returns dashboard by id', async () => {
      const mockDashboard = { id: 'd1', name: 'My Dashboard', widgets: [] }
      mockFetchResponse(mockDashboard)

      const result = await api.getDashboard('d1')

      expect(global.fetch).toHaveBeenCalledWith('/api/dashboards/d1', expect.objectContaining({ signal: expect.any(AbortSignal) }))
      expect(result).toEqual(mockDashboard)
    })

    it('updateDashboard updates dashboard', async () => {
      const mockDashboard = { id: 'd1', name: 'Updated Name' }
      mockFetchResponse(mockDashboard)

      const result = await api.updateDashboard('d1', { name: 'Updated Name' })

      expect(global.fetch).toHaveBeenCalledWith('/api/dashboards/d1', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name: 'Updated Name' }),
      })
      expect(result).toEqual(mockDashboard)
    })

    it('updateDashboard throws on error', async () => {
      mockFetchResponse({}, false, 404)

      await expect(api.updateDashboard('invalid', { name: 'Test' })).rejects.toThrow(
        'HTTP error! status: 404'
      )
    })

    it('deleteDashboard deletes dashboard', async () => {
      mockFetchResponse({})

      await api.deleteDashboard('d1')

      expect(global.fetch).toHaveBeenCalledWith('/api/dashboards/d1', {
        method: 'DELETE',
      })
    })

    it('deleteDashboard throws on error', async () => {
      mockFetchResponse({}, false, 404)

      await expect(api.deleteDashboard('invalid')).rejects.toThrow('HTTP error! status: 404')
    })

    it('setDefaultDashboard sets dashboard as default', async () => {
      mockFetchResponse({})

      await api.setDefaultDashboard('d1')

      expect(global.fetch).toHaveBeenCalledWith('/api/dashboards/d1/default', {
        method: 'PUT',
      })
    })

    it('setDefaultDashboard throws on error', async () => {
      mockFetchResponse({}, false, 404)

      await expect(api.setDefaultDashboard('invalid')).rejects.toThrow('HTTP error! status: 404')
    })
  })

  describe('widget API', () => {
    it('createWidget creates widget', async () => {
      const mockWidget = { id: 'w1', title: 'New Widget', widgetType: 'stats' }
      mockFetchResponse(mockWidget)

      const result = await api.createWidget('d1', {
        title: 'New Widget',
        widgetType: 'stats',
        gridColumn: 0,
        gridRow: 0,
      })

      expect(global.fetch).toHaveBeenCalledWith('/api/dashboards/d1/widgets', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          title: 'New Widget',
          widgetType: 'stats',
          gridColumn: 0,
          gridRow: 0,
        }),
      })
      expect(result).toEqual(mockWidget)
    })

    it('createWidget throws on error', async () => {
      mockFetchResponse({}, false, 400)

      await expect(
        api.createWidget('d1', { title: '', widgetType: 'stats', gridColumn: 0, gridRow: 0 })
      ).rejects.toThrow('HTTP error! status: 400')
    })

    it('updateWidget updates widget', async () => {
      const mockWidget = { id: 'w1', title: 'Updated Widget' }
      mockFetchResponse(mockWidget)

      const result = await api.updateWidget('d1', 'w1', { title: 'Updated Widget' })

      expect(global.fetch).toHaveBeenCalledWith('/api/dashboards/d1/widgets/w1', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ title: 'Updated Widget' }),
      })
      expect(result).toEqual(mockWidget)
    })

    it('updateWidget throws on error', async () => {
      mockFetchResponse({}, false, 404)

      await expect(api.updateWidget('d1', 'invalid', { title: 'Test' })).rejects.toThrow(
        'HTTP error! status: 404'
      )
    })

    it('updateWidgetPositions updates positions', async () => {
      mockFetchResponse({})

      const positions = [
        { id: 'w1', gridColumn: 0, gridRow: 0 },
        { id: 'w2', gridColumn: 1, gridRow: 0 },
      ]

      await api.updateWidgetPositions('d1', positions)

      expect(global.fetch).toHaveBeenCalledWith('/api/dashboards/d1/widgets/positions', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ positions }),
      })
    })

    it('updateWidgetPositions throws on error', async () => {
      mockFetchResponse({}, false, 500)

      await expect(api.updateWidgetPositions('d1', [])).rejects.toThrow('HTTP error! status: 500')
    })

    it('deleteWidget deletes widget', async () => {
      mockFetchResponse({})

      await api.deleteWidget('d1', 'w1')

      expect(global.fetch).toHaveBeenCalledWith('/api/dashboards/d1/widgets/w1', {
        method: 'DELETE',
      })
    })

    it('deleteWidget throws on error', async () => {
      mockFetchResponse({}, false, 404)

      await expect(api.deleteWidget('d1', 'invalid')).rejects.toThrow('HTTP error! status: 404')
    })
  })

  describe('request deduplication', () => {
    const series = { series: [] }

    it('keeps a shared request running when the first caller aborts', async () => {
      let resolveFetch: (value: Response) => void = () => {}
      vi.mocked(global.fetch).mockImplementationOnce(
        () => new Promise<Response>((resolve) => { resolveFetch = resolve })
      )
      const first = new AbortController()
      const params = { name: 'dedup.abort', from: 'a', to: 'b' }

      const firstRequest = api.getMetricSeries(params, { signal: first.signal })
      const secondRequest = api.getMetricSeries(params)
      first.abort()

      await expect(firstRequest).rejects.toMatchObject({ name: 'AbortError' })
      resolveFetch({ ok: true, status: 200, json: async () => series } as Response)
      await expect(secondRequest).resolves.toEqual(series)
      expect(global.fetch).toHaveBeenCalledTimes(1)
    })

    it('aborts the shared request when every caller aborted', async () => {
      let fetchSignal: AbortSignal | undefined
      vi.mocked(global.fetch).mockImplementationOnce((_url, init) => {
        fetchSignal = init?.signal ?? undefined
        return new Promise<Response>(() => {})
      })
      const a = new AbortController()
      const b = new AbortController()
      const params = { name: 'dedup.all', from: 'a', to: 'b' }

      const requests = [
        api.getMetricSeries(params, { signal: a.signal }),
        api.getMetricSeries(params, { signal: b.signal }),
      ]
      a.abort()
      expect(fetchSignal?.aborted).toBe(false)
      b.abort()

      await Promise.allSettled(requests)
      expect(fetchSignal?.aborted).toBe(true)
    })
  })

  describe('error handling', () => {
    it('explains how to sign in on 401', async () => {
      mockFetchResponse({}, false, 401)

      await expect(api.getStats()).rejects.toThrow('?token=')
      expect(global.fetch).toHaveBeenCalledTimes(1)
    })

    it('includes the server message of a failed change', async () => {
      mockFetchResponse({ error: 'Bad Request', message: 'name is required' }, false, 400)

      await expect(api.createDashboard({ name: '' })).rejects.toThrow('HTTP error! status: 400: name is required')
    })

    it('includes the server message of a failed query', async () => {
      mockFetchResponse(
        { error: 'Bad Request', message: 'invalid cursor: pass the nextCursor of the previous page' },
        false,
        400
      )

      await expect(api.getLogs({ cursor: 'stale' })).rejects.toThrow(
        'HTTP error! status: 400: invalid cursor: pass the nextCursor of the previous page'
      )
      expect(global.fetch).toHaveBeenCalledTimes(1)
    })

    it('includes the server message once retries are used up', async () => {
      for (let i = 0; i < 6; i++) {
        mockFetchResponse({ error: 'Internal Server Error', message: 'database is locked' }, false, 500)
      }
      vi.useFakeTimers()
      const request = api.getStats().catch((err: Error) => err)
      await vi.runAllTimersAsync()
      vi.useRealTimers()

      expect(await request).toHaveProperty('message', 'HTTP error! status: 500: database is locked')
    })

    it('throws on 404', async () => {
      mockFetchResponse({}, false, 404)

      await expect(api.getTrace('non-existent', 'otel_trace')).rejects.toThrow(
        'HTTP error! status: 404'
      )
    })

    it('throws on 400', async () => {
      mockFetchResponse({}, false, 400)

      await expect(api.getTraces({ limit: -1 })).rejects.toThrow(
        'HTTP error! status: 400'
      )
    })

    it('propagates network errors', async () => {
      vi.mocked(global.fetch).mockRejectedValueOnce(new Error('Network error'))

      await expect(api.getStats()).rejects.toThrow('Network error')
    })
  })
})
