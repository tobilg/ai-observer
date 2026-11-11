import { describe, expect, it, vi } from 'vitest'
import { act, waitFor } from '@testing-library/react'
import { useNewItems } from '../useNewItems'
import { renderHookWithProviders } from '@/test/queryWrapper'

type Props = { recentTimes: number[]; anchor: number; enabled?: boolean; filters?: object }

function setup(initialProps: Props) {
  const count = vi.fn().mockResolvedValue(3)
  const onSeen = vi.fn()
  const rendered = renderHookWithProviders(
    ({ recentTimes, anchor, enabled = true, filters = { service: 'a' } }: Props) =>
      useNewItems({
        name: 'traces',
        filters,
        count,
        anchor,
        recentTimes,
        enabled,
        refetch: ['traces', 'list'],
        onSeen,
        debounceMs: 0,
      }),
    { initialProps },
  )
  return { ...rendered, count, onSeen }
}

describe('useNewItems', () => {
  const anchor = 1_000_000

  it('counts nothing until records newer than the list arrive', async () => {
    const { result, count } = setup({ recentTimes: [anchor - 5], anchor })
    await new Promise((resolve) => setTimeout(resolve, 10))
    expect(result.current.count).toBe(0)
    expect(count).not.toHaveBeenCalled()
  })

  it("asks the API how many match the list's filters", async () => {
    const { result, count } = setup({ recentTimes: [anchor + 10, anchor + 5, anchor - 1], anchor })

    await waitFor(() => expect(result.current.count).toBe(3))
    const [range] = count.mock.calls[0]
    expect(range.from.getTime()).toBe(anchor)
    expect(range.to.getTime()).toBeGreaterThanOrEqual(anchor)
  })

  it('counts again when more records arrive, with the count shown meanwhile', async () => {
    const { result, count, rerender } = setup({ recentTimes: [anchor + 10], anchor })
    await waitFor(() => expect(result.current.count).toBe(3))

    count.mockResolvedValue(4)
    rerender({ recentTimes: [anchor + 20, anchor + 10], anchor })
    expect(result.current.count).toBe(3)
    await waitFor(() => expect(result.current.count).toBe(4))
    expect(count).toHaveBeenCalledTimes(2)
  })

  it('refetches the list and hides the count when seen', async () => {
    const { result, onSeen, queryClient } = setup({ recentTimes: [anchor + 10], anchor })
    await waitFor(() => expect(result.current.count).toBe(3))
    const invalidate = vi.spyOn(queryClient, 'invalidateQueries')

    act(() => result.current.markSeen())

    expect(result.current.count).toBe(0)
    expect(onSeen).toHaveBeenCalled()
    expect(invalidate).toHaveBeenCalledWith({ queryKey: ['traces', 'list'] })
  })

  it('is off while the list has not loaded or when disabled', async () => {
    const { result, count, rerender } = setup({ recentTimes: [anchor + 10], anchor: 0 })
    rerender({ recentTimes: [anchor + 10], anchor, enabled: false })
    await new Promise((resolve) => setTimeout(resolve, 10))
    expect(result.current.count).toBe(0)
    expect(count).not.toHaveBeenCalled()
  })
})
