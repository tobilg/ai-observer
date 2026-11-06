import { describe, it, expect, beforeEach } from 'vitest'
import { act, renderHook } from '@testing-library/react'
import { usePagination } from '../usePagination'

describe('usePagination', () => {
  beforeEach(() => {
    localStorage.clear()
  })

  it('starts on the first page without a cursor', () => {
    const { result } = renderHook(() => usePagination({ defaultPageSize: 25 }))
    expect(result.current.page).toBe(1)
    expect(result.current.pageSize).toBe(25)
    expect(result.current.cursor).toBeUndefined()
    expect(result.current.hasNext).toBe(false)
  })

  it('moves forward with the next cursors and back through the visited ones', () => {
    const { result } = renderHook(() => usePagination())

    act(() => result.current.setNextCursor('c2'))
    expect(result.current.hasNext).toBe(true)
    act(() => result.current.nextPage())
    expect(result.current.page).toBe(2)
    expect(result.current.cursor).toBe('c2')
    // The next cursor is unknown until page 2 has loaded
    expect(result.current.hasNext).toBe(false)

    act(() => result.current.setNextCursor('c3'))
    act(() => result.current.nextPage())
    expect(result.current.page).toBe(3)
    expect(result.current.cursor).toBe('c3')

    act(() => result.current.previousPage())
    expect(result.current.page).toBe(2)
    expect(result.current.cursor).toBe('c2')

    act(() => result.current.resetToFirstPage())
    expect(result.current.page).toBe(1)
    expect(result.current.cursor).toBeUndefined()
  })

  it('does not move past the last page', () => {
    const { result } = renderHook(() => usePagination())
    act(() => result.current.setNextCursor(undefined))
    act(() => result.current.nextPage())
    expect(result.current.page).toBe(1)
  })

  it('starts over and persists a new page size', () => {
    const { result } = renderHook(() => usePagination({ storageKey: 'page-size' }))
    act(() => result.current.setNextCursor('c2'))
    act(() => result.current.nextPage())

    act(() => result.current.setPageSize(50))

    expect(result.current.page).toBe(1)
    expect(result.current.pageSize).toBe(50)
    expect(localStorage.getItem('page-size')).toBe('50')
    const { result: reloaded } = renderHook(() => usePagination({ storageKey: 'page-size' }))
    expect(reloaded.current.pageSize).toBe(50)
  })
})
