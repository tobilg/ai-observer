import { describe, expect, it } from 'vitest'
import { act, renderHook } from '@testing-library/react'
import { useExpandedRow } from '../useExpandedRow'

describe('useExpandedRow', () => {
  it('toggles a row and closes it when the view changes, also when coming back', () => {
    const { result, rerender } = renderHook(({ view }) => useExpandedRow(view), { initialProps: { view: 'page-1' } })

    act(() => result.current[1]('row-a'))
    expect(result.current[0]).toBe('row-a')
    act(() => result.current[1]('row-b'))
    expect(result.current[0]).toBe('row-b')
    act(() => result.current[1]('row-b'))
    expect(result.current[0]).toBeNull()

    act(() => result.current[1]('row-a'))
    rerender({ view: 'page-2' })
    expect(result.current[0]).toBeNull()
    rerender({ view: 'page-1' })
    expect(result.current[0]).toBeNull()
  })
})
