import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { act, renderHook } from '@testing-library/react'
import { useLiveRefresh } from '../useLiveRefresh'

const state = vi.hoisted(() => ({ visible: true, connected: false }))
vi.mock('@/hooks/usePageVisible', () => ({ usePageVisible: () => state.visible }))
vi.mock('@/hooks/useWebSocket', () => ({ useWebSocketConnected: () => state.connected }))

type Props = { enabled: boolean; updateCount: number }

function render(props: Props = { enabled: true, updateCount: 0 }) {
  return renderHook((p: Props) => useLiveRefresh({ ...p, intervalMs: 10_000 }), { initialProps: props })
}

describe('useLiveRefresh', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    state.visible = true
    state.connected = false
  })
  afterEach(() => {
    vi.useRealTimers()
  })

  it('polls at the interval while the WebSocket is disconnected', () => {
    const { result } = render()
    expect(result.current).toBe(0)
    act(() => vi.advanceTimersByTime(10_000))
    expect(result.current).toBe(1)
    act(() => vi.advanceTimersByTime(20_000))
    expect(result.current).toBe(3)
  })

  it('refreshes on notifications while connected and polls only to move the window', () => {
    state.connected = true
    const { result, rerender } = render()

    act(() => vi.advanceTimersByTime(30_000))
    expect(result.current).toBe(0)

    // A burst of notifications refreshes once, after the debounce
    rerender({ enabled: true, updateCount: 1 })
    rerender({ enabled: true, updateCount: 2 })
    act(() => vi.advanceTimersByTime(499))
    expect(result.current).toBe(0)
    act(() => vi.advanceTimersByTime(1))
    expect(result.current).toBe(1)

    act(() => vi.advanceTimersByTime(60_000))
    expect(result.current).toBe(2)
  })

  it('pauses while the tab is hidden and refreshes when it is shown again', () => {
    state.visible = false
    const { result, rerender } = render()
    rerender({ enabled: true, updateCount: 5 })
    act(() => vi.advanceTimersByTime(60_000))
    expect(result.current).toBe(0)

    state.visible = true
    rerender({ enabled: true, updateCount: 5 })
    expect(result.current).toBe(1)
  })

  it('does nothing when disabled', () => {
    const { result, rerender } = render({ enabled: false, updateCount: 0 })
    rerender({ enabled: false, updateCount: 3 })
    act(() => vi.advanceTimersByTime(60_000))
    expect(result.current).toBe(0)
  })
})
