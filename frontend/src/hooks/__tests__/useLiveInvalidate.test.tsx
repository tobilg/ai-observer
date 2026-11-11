import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { act } from '@testing-library/react'
import { useQuery } from '@tanstack/react-query'
import { useLiveInvalidate } from '../useLiveInvalidate'
import { renderHookWithProviders } from '@/test/queryWrapper'

const state = vi.hoisted(() => ({ visible: true, connected: true }))
vi.mock('@/hooks/usePageVisible', () => ({ usePageVisible: () => state.visible }))
vi.mock('@/hooks/useWebSocket', () => ({ useWebSocketConnected: () => state.connected }))

type Props = { enabled: boolean; updateCount: number }

describe('useLiveInvalidate', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    state.visible = true
    state.connected = true
  })
  afterEach(() => {
    vi.useRealTimers()
  })

  function setup(initialProps: Props = { enabled: true, updateCount: 0 }) {
    const queryFn = vi.fn().mockResolvedValue('data')
    const other = vi.fn().mockResolvedValue('other')
    const rendered = renderHookWithProviders(
      (props: Props) => {
        useQuery({ queryKey: ['logs', 'list', 1], queryFn })
        useQuery({ queryKey: ['traces', 'list'], queryFn: other })
        useLiveInvalidate({ queryKey: ['logs', 'list'], intervalMs: 10_000, ...props })
      },
      { initialProps },
    )
    return { ...rendered, queryFn, other }
  }

  it('refetches the given queries after real-time notifications', async () => {
    const { queryFn, other, rerender } = setup()
    await act(() => vi.advanceTimersByTimeAsync(0))
    expect(queryFn).toHaveBeenCalledTimes(1)

    rerender({ enabled: true, updateCount: 1 })
    await act(() => vi.advanceTimersByTimeAsync(500))

    expect(queryFn).toHaveBeenCalledTimes(2)
    expect(other).toHaveBeenCalledTimes(1)
  })

  it('polls while the WebSocket is disconnected', async () => {
    state.connected = false
    const { queryFn } = setup()
    await act(() => vi.advanceTimersByTimeAsync(0))

    await act(() => vi.advanceTimersByTimeAsync(10_000))
    expect(queryFn).toHaveBeenCalledTimes(2)
  })

  it('does nothing while disabled', async () => {
    state.connected = false
    const { queryFn, rerender } = setup({ enabled: false, updateCount: 0 })
    await act(() => vi.advanceTimersByTimeAsync(0))

    rerender({ enabled: false, updateCount: 3 })
    await act(() => vi.advanceTimersByTimeAsync(30_000))
    expect(queryFn).toHaveBeenCalledTimes(1)
  })
})
