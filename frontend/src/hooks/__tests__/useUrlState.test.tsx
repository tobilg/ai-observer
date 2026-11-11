import { describe, expect, it, beforeEach } from 'vitest'
import { act } from '@testing-library/react'
import { useLocation, useNavigate, useNavigationType } from 'react-router-dom'
import { useUrlState } from '../useUrlState'
import { relativeTimeSelection } from '@/lib/timeUtils'
import { renderHookWithProviders } from '@/test/queryWrapper'

const DEFAULTS = { service: '', search: '', background: false }

function useTestState() {
  const state = useUrlState({ filters: DEFAULTS, time: { storageKey: 'test-url-time', defaultTimeframe: '24h' } })
  const location = useLocation()
  const navigate = useNavigate()
  return { ...state, search: location.search, key: location.key, navigate, navigationType: useNavigationType() }
}

describe('useUrlState', () => {
  beforeEach(() => localStorage.clear())

  it('reads filters from the URL and uses defaults for the rest', () => {
    const { result } = renderHookWithProviders(() => useTestState(), { route: '/traces?service=codex&background=true' })
    expect(result.current.filters).toEqual({ service: 'codex', search: '', background: true })
  })

  it('writes changed filters and leaves out defaults', () => {
    const { result } = renderHookWithProviders(() => useTestState(), { route: '/traces?service=codex' })

    act(() => result.current.setFilters({ search: 'error', background: true }))
    expect(result.current.search).toBe('?service=codex&search=error&background=true')

    act(() => result.current.setFilters({ service: '', background: false }))
    expect(result.current.search).toBe('?search=error')
    expect(result.current.filters).toEqual({ service: '', search: 'error', background: false })
  })

  it('keeps the filters object while other parameters change', () => {
    const { result } = renderHookWithProviders(() => useTestState(), { route: '/traces?service=codex' })
    const filters = result.current.filters

    act(() => result.current.time.setTimeSelection(relativeTimeSelection('1h')))
    expect(result.current.filters).toBe(filters)
  })

  it('combines filter and time changes made together', () => {
    const { result } = renderHookWithProviders(() => useTestState(), { route: '/traces' })

    act(() => {
      result.current.setFilters({ service: 'claude-code' })
      result.current.time.setTimeSelection(relativeTimeSelection('3h'))
      result.current.setFilters({ search: 'x' })
    })

    expect(new URLSearchParams(result.current.search)).toEqual(
      new URLSearchParams({ service: 'claude-code', timeframe: '3h', search: 'x' }),
    )
    expect(result.current.filters).toMatchObject({ service: 'claude-code', search: 'x' })
    expect(result.current.time.timeKey).toEqual({ timeframe: '3h' })
  })

  it('replaces the history entry and skips writes that change nothing', () => {
    const { result } = renderHookWithProviders(() => useTestState(), { route: '/traces?service=a' })
    const key = result.current.key

    act(() => result.current.setFilters({ service: 'a' }))
    expect(result.current.key).toBe(key)

    act(() => result.current.setFilters({ service: 'b' }))
    expect(result.current.search).toBe('?service=b')
    expect(result.current.navigationType).toBe('REPLACE')
  })

  it('follows navigation to other parameters', () => {
    const { result } = renderHookWithProviders(() => useTestState(), { route: '/traces?service=a&timeframe=1h' })

    act(() => result.current.navigate('/traces?search=y'))
    expect(result.current.filters).toEqual({ service: '', search: 'y', background: false })
    // Without time parameters: the stored (or default) selection
    expect(result.current.time.timeKey).toEqual({ timeframe: '24h' })
  })
})
