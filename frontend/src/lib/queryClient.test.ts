import { afterEach, describe, expect, it, vi } from 'vitest'
import { MAX_RETRIES, createQueryClient, retryDelay, shouldRetry } from './queryClient'
import { httpError } from './errors'
import { useAuthStore } from '@/stores/authStore'

describe('retry policy', () => {
  it('retries retryable errors up to MAX_RETRIES times', () => {
    const error = httpError(500)
    expect(shouldRetry(0, error)).toBe(true)
    expect(shouldRetry(MAX_RETRIES - 1, error)).toBe(true)
    expect(shouldRetry(MAX_RETRIES, error)).toBe(false)
  })

  it('does not retry client errors and server timeouts', () => {
    expect(shouldRetry(0, httpError(400))).toBe(false)
    expect(shouldRetry(0, httpError(503))).toBe(false)
    expect(shouldRetry(0, httpError(504))).toBe(false)
  })

  it('backs off exponentially up to 4 seconds', () => {
    vi.spyOn(Math, 'random').mockReturnValue(0.5) // no jitter
    expect(retryDelay(0)).toBe(500)
    expect(retryDelay(1)).toBe(1000)
    expect(retryDelay(2)).toBe(2000)
    expect(retryDelay(5)).toBe(4000)
    vi.restoreAllMocks()
  })
})

describe('auth tracking', () => {
  afterEach(() => {
    useAuthStore.setState({ authRequired: false })
  })

  it('flags a 401 of any query and clears it when a query succeeds', async () => {
    const client = createQueryClient()

    await client
      .fetchQuery({ queryKey: ['a'], queryFn: () => Promise.reject(httpError(401)), retry: false })
      .catch(() => {})
    expect(useAuthStore.getState().authRequired).toBe(true)

    await client.fetchQuery({ queryKey: ['b'], queryFn: () => Promise.resolve(1) })
    expect(useAuthStore.getState().authRequired).toBe(false)
  })

  it('ignores other errors', async () => {
    const client = createQueryClient()

    await client
      .fetchQuery({ queryKey: ['c'], queryFn: () => Promise.reject(httpError(500)), retry: false })
      .catch(() => {})

    expect(useAuthStore.getState().authRequired).toBe(false)
  })

  it('does not retry mutations', async () => {
    const client = createQueryClient()
    const mutationFn = vi.fn().mockRejectedValue(httpError(500))

    await client.getMutationCache().build(client, { mutationFn }).execute(undefined).catch(() => {})

    expect(mutationFn).toHaveBeenCalledTimes(1)
  })
})
