import { beforeEach, describe, expect, it, vi } from 'vitest'
import { act, waitFor } from '@testing-library/react'
import { api } from '@/lib/api'
import { queryKeys, useLogs, useProviders, useServices, useTraceSpans, countLogs } from './queries'
import { relativeTimeSelection, timeRangeSource } from './timeUtils'
import { getServiceDisplayName } from './metricMetadata'
import { useProviderStore } from '@/stores/providerStore'
import { renderHookWithProviders } from '@/test/queryWrapper'

vi.mock('@/lib/api', () => ({
  api: {
    getProviders: vi.fn(),
    getServices: vi.fn(),
    getLogs: vi.fn(),
    getTraceSpans: vi.fn(),
  },
}))

describe('query keys', () => {
  it('hold the selection identity, not moving bounds, and drop empty filters', () => {
    const range = timeRangeSource(relativeTimeSelection('1h'))
    expect(queryKeys.logs.list({ service: '', severity: 'ERROR' }, range, { limit: 10 })).toEqual([
      'logs',
      'list',
      { severity: 'ERROR' },
      { timeframe: '1h' },
      { limit: 10 },
    ])
    expect(queryKeys.traces.list({ background: false }, range, { limit: 10 })).toEqual(
      queryKeys.traces.list({}, range, { limit: 10 }),
    )
  })
})

describe('query hooks', () => {
  beforeEach(() => {
    useProviderStore.setState({ displayNames: {}, toolServices: {}, loaded: false })
  })

  it('resolve a relative window when they fetch', async () => {
    vi.mocked(api.getLogs).mockResolvedValue({ logs: [], total: 0, hasMore: false })
    const range = timeRangeSource(relativeTimeSelection('1h'))

    const { result, queryClient } = renderHookWithProviders(() =>
      useLogs({ service: 'claude-code', search: '' }, range, { limit: 25, cursor: 'c1' }),
    )
    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    const [params, options] = vi.mocked(api.getLogs).mock.calls[0]
    expect(params).toMatchObject({ service: 'claude-code', limit: 25, cursor: 'c1' })
    expect(params).not.toHaveProperty('search')
    expect(new Date(params!.to!).getTime() - new Date(params!.from!).getTime()).toBe(3600_000)
    expect(options?.signal).toBeInstanceOf(AbortSignal)

    // A refetch queries the window ending at that moment
    await new Promise((resolve) => setTimeout(resolve, 5))
    await act(() => queryClient.invalidateQueries({ queryKey: queryKeys.logs.all }))
    const [later] = vi.mocked(api.getLogs).mock.calls[1]
    expect(new Date(later!.to!).getTime()).toBeGreaterThan(new Date(params!.to!).getTime())
  })

  it('share the services list', async () => {
    vi.mocked(api.getServices).mockResolvedValue({ services: ['a', 'b'] })

    const { result } = renderHookWithProviders(() => [useServices(), useServices()])
    await waitFor(() => expect(result.current[0].data).toEqual(['a', 'b']))
    expect(result.current[1].data).toEqual(['a', 'b'])
    expect(api.getServices).toHaveBeenCalledTimes(1)
  })

  it('page through trace spans with the cursor', async () => {
    vi.mocked(api.getTraceSpans)
      .mockResolvedValueOnce({ spans: [], total: 0, hasMore: true, nextCursor: 'next' })
      .mockResolvedValueOnce({ spans: [], total: 0, hasMore: false })

    const { result } = renderHookWithProviders(() => useTraceSpans('t1', 'otel_trace'))
    await waitFor(() => expect(result.current.hasNextPage).toBe(true))
    await act(() => result.current.fetchNextPage())

    await waitFor(() => expect(result.current.data?.pages).toHaveLength(2))
    expect(vi.mocked(api.getTraceSpans).mock.calls[1][3]).toBe('next')
    expect(result.current.hasNextPage).toBe(false)
  })

  it('count items in a range with the list filters and limit 1', async () => {
    vi.mocked(api.getLogs).mockResolvedValue({ logs: [], total: 7, hasMore: true })
    const from = new Date('2026-01-01T00:00:00Z')
    const to = new Date('2026-01-01T01:00:00Z')

    const total = await countLogs({ severity: 'ERROR', service: '' })({ from, to }, new AbortController().signal)

    expect(total).toBe(7)
    expect(vi.mocked(api.getLogs).mock.calls[0][0]).toEqual({
      severity: 'ERROR',
      from: from.toISOString(),
      to: to.toISOString(),
      limit: 1,
    })
  })
})

describe('useProviders', () => {
  beforeEach(() => {
    useProviderStore.setState({ displayNames: {}, toolServices: {}, loaded: false })
  })

  it('feeds the provider names to getServiceDisplayName', async () => {
    vi.mocked(api.getProviders).mockResolvedValue({
      providers: [{ tool: 'new-tool', serviceName: 'new_tool_svc', displayName: 'New Tool', sessionFiles: false }],
    })

    const { result } = renderHookWithProviders(() => useProviders())
    await waitFor(() => expect(result.current.isSuccess).toBe(true))

    await waitFor(() => expect(getServiceDisplayName('new_tool_svc')).toBe('New Tool'))
    expect(useProviderStore.getState().loaded).toBe(true)
  })

  it('loads the providers once for all users', async () => {
    vi.mocked(api.getProviders).mockResolvedValue({ providers: [] })

    const { result } = renderHookWithProviders(() => [useProviders(), useProviders()])
    await waitFor(() => expect(result.current[0].isSuccess).toBe(true))

    expect(api.getProviders).toHaveBeenCalledTimes(1)
  })

  it('keeps the fallback names when the request fails', async () => {
    vi.mocked(api.getProviders).mockRejectedValue(new Error('offline'))

    const { result } = renderHookWithProviders(() => useProviders())
    await waitFor(() => expect(result.current.isError).toBe(true))

    expect(useProviderStore.getState().loaded).toBe(false)
    expect(getServiceDisplayName('copilot-chat')).toBe('GitHub Copilot (VS Code)')
  })
})
