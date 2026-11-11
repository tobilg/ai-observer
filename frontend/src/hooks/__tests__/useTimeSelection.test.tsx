import { beforeEach, describe, expect, it } from 'vitest'
import { act, renderHook } from '@testing-library/react'
import { useLocation } from 'react-router-dom'
import { useTimeSelection } from '../useTimeSelection'
import { useUrlWriter } from '../useUrlState'
import { absoluteTimeSelection, relativeTimeSelection } from '@/lib/timeUtils'
import { renderHookWithProviders } from '@/test/queryWrapper'

const KEY = 'test-timeselection'
const from = new Date('2026-01-01T00:00:00.000Z')
const to = new Date('2026-01-02T00:00:00.000Z')

function useUrlTimeSelection(defaultTimeframe?: string) {
  const url = useUrlWriter()
  const time = useTimeSelection({ storageKey: KEY, defaultTimeframe, url })
  return { time, search: useLocation().search }
}

describe('useTimeSelection without URL', () => {
  beforeEach(() => localStorage.clear())

  it('starts with the default timeframe', () => {
    const { result } = renderHook(() => useTimeSelection({ storageKey: KEY, defaultTimeframe: '24h' }))
    expect(result.current.timeKey).toEqual({ timeframe: '24h' })
    expect(result.current.isAbsoluteRange).toBe(false)
    expect(result.current.intervalSeconds).toBe(3600)
    expect(result.current.durationSeconds).toBe(86400)
  })

  it('restores the stored relative selection', () => {
    localStorage.setItem(KEY, JSON.stringify({ type: 'relative', timeframeValue: '3h' }))
    const { result } = renderHook(() => useTimeSelection({ storageKey: KEY }))
    expect(result.current.timeKey).toEqual({ timeframe: '3h' })
  })

  it('resolves the window when asked, with a stable key and range', async () => {
    const { result, rerender } = renderHook(() => useTimeSelection({ storageKey: KEY, defaultTimeframe: '1h' }))
    const range = result.current.range
    const first = result.current.resolveRange()
    await new Promise((resolve) => setTimeout(resolve, 5))
    rerender()

    expect(result.current.range).toBe(range)
    const second = result.current.resolveRange()
    expect(second.to.getTime()).toBeGreaterThan(first.to.getTime())
    expect(second.to.getTime() - second.from.getTime()).toBe(3600_000)
  })

  it('stores relative selections, never absolute ones', () => {
    const { result } = renderHook(() => useTimeSelection({ storageKey: KEY }))

    act(() => result.current.setTimeSelection(relativeTimeSelection('6h')))
    expect(JSON.parse(localStorage.getItem(KEY)!)).toEqual({ type: 'relative', timeframeValue: '6h' })

    act(() => result.current.setTimeSelection(absoluteTimeSelection(from, to)))
    expect(result.current.isAbsoluteRange).toBe(true)
    expect(result.current.resolveRange()).toEqual({ from, to })
    expect(JSON.parse(localStorage.getItem(KEY)!)).toEqual({ type: 'relative', timeframeValue: '6h' })
  })
})

describe('useTimeSelection with URL', () => {
  beforeEach(() => localStorage.clear())

  it('prefers the URL over storage', () => {
    localStorage.setItem(KEY, JSON.stringify({ type: 'relative', timeframeValue: '3h' }))
    const { result } = renderHookWithProviders(() => useUrlTimeSelection(), { route: '/logs?timeframe=30m' })
    expect(result.current.time.timeKey).toEqual({ timeframe: '30m' })
  })

  it('reads absolute ranges from the URL', () => {
    const { result } = renderHookWithProviders(() => useUrlTimeSelection(), {
      route: `/logs?from=${from.toISOString()}&to=${to.toISOString()}`,
    })
    expect(result.current.time.isAbsoluteRange).toBe(true)
    expect(result.current.time.timeKey).toEqual({ from: from.toISOString(), to: to.toISOString() })
  })

  it('writes selections to the URL and stores only relative ones', () => {
    const { result } = renderHookWithProviders(() => useUrlTimeSelection(), { route: '/logs?service=a' })

    act(() => result.current.time.setTimeSelection(relativeTimeSelection('12h')))
    expect(result.current.search).toBe('?service=a&timeframe=12h')
    expect(JSON.parse(localStorage.getItem(KEY)!)).toEqual({ type: 'relative', timeframeValue: '12h' })

    act(() => result.current.time.setTimeSelection(absoluteTimeSelection(from, to)))
    expect(result.current.time.isAbsoluteRange).toBe(true)
    expect(new URLSearchParams(result.current.search).get('timeframe')).toBeNull()
    expect(new URLSearchParams(result.current.search).get('from')).toBe(from.toISOString())
    expect(JSON.parse(localStorage.getItem(KEY)!)).toEqual({ type: 'relative', timeframeValue: '12h' })
  })

  it('does not restore an absolute range left in storage by earlier versions', () => {
    localStorage.setItem(KEY, JSON.stringify({ type: 'absolute', from: from.toISOString(), to: to.toISOString() }))
    const { result } = renderHookWithProviders(() => useUrlTimeSelection('24h'), { route: '/logs' })
    expect(result.current.time.timeKey).toEqual({ timeframe: '24h' })
  })
})
