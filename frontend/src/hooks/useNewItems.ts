import { useCallback, useMemo, useState } from 'react'
import { useQuery, useQueryClient, type QueryKey } from '@tanstack/react-query'
import { useDebounce } from '@/hooks/useDebounce'
import { queryKeys, type CountBetween } from '@/lib/queries'

interface UseNewItemsOptions {
  // Names the list in the cache key ('traces', 'logs', 'sessions')
  name: string
  // The list's filters: the count is cached per filters
  filters: object
  // Counts the items matching the list's filters in a range: countTraces,
  // countLogs or countSessions of lib/queries.ts with the same filters
  count: CountBetween
  // When the shown list was fetched (its query's dataUpdatedAt); 0 while
  // it has not loaded. Items after it are new.
  anchor: number
  // Timestamps (Unix ms) of records announced in real time (telemetryStore
  // recentSpanTimes or recentLogTimes): the count is asked for again when
  // more of them are newer than the anchor
  recentTimes: number[]
  // false turns the indicator off, e.g. for absolute time ranges
  enabled?: boolean
  // Queries markSeen refetches, so the list shows the new items
  // (queryKeys.traces.lists)
  refetch?: QueryKey
  // Called by markSeen, e.g. to go back to the first page
  onSeen?: () => void
  // Delay before counting, so a burst of notifications counts once
  debounceMs?: number
}

interface UseNewItemsResult {
  // Items matching the filters that arrived after the shown list was fetched
  count: number
  // Shows them: refetches the list (and calls onSeen); the count drops to 0
  markSeen: () => void
}

/**
 * The "N new items" indicator of a list that does not refresh by itself
 * (logs, traces): real-time notifications tell that records arrived, and
 * the list endpoint, asked with the list's filters, limit 1 and the range
 * since the list was fetched, tells how many of them the list would show.
 *
 *   const traces = useTraces(filters, time.range, page)
 *   const newTraces = useNewItems({
 *     name: 'traces',
 *     filters,
 *     count: countTraces(filters),
 *     anchor: traces.dataUpdatedAt,
 *     recentTimes: useTelemetryStore((s) => s.recentSpanTimes),
 *     enabled: !time.isAbsoluteRange,
 *     refetch: queryKeys.traces.lists,
 *     onSeen: resetToFirstPage,
 *   })
 *   {newTraces.count > 0 && <Button onClick={newTraces.markSeen}>{newTraces.count} new traces</Button>}
 */
export function useNewItems({
  name,
  filters,
  count,
  anchor,
  recentTimes,
  enabled = true,
  refetch,
  onSeen,
  debounceMs = 250,
}: UseNewItemsOptions): UseNewItemsResult {
  const queryClient = useQueryClient()
  // When the user last asked to see the new items: hides the count until the
  // refetched list moves the anchor
  const [seenAt, setSeenAt] = useState(0)
  const since = Math.max(anchor, seenAt)
  const active = enabled && anchor > 0

  // The timestamps are newest first per notification but not sorted overall
  const announced = useMemo(
    () => (active ? recentTimes.reduce((n, time) => (time > since ? n + 1 : n), 0) : 0),
    [active, recentTimes, since],
  )
  const debouncedAnnounced = useDebounce(announced, debounceMs)

  const query = useQuery({
    queryKey: queryKeys.newItems(name, filters, since, debouncedAnnounced),
    queryFn: ({ signal }) => count({ from: new Date(since), to: new Date() }, signal),
    enabled: active && debouncedAnnounced > 0,
    staleTime: Infinity,
    retry: false,
    // Keep the count shown while it is asked for again, for the same anchor
    placeholderData: (previous, previousQuery) => (previousQuery?.queryKey[3] === since ? previous : undefined),
  })

  const markSeen = useCallback(() => {
    setSeenAt(Date.now())
    if (refetch) {
      void queryClient.invalidateQueries({ queryKey: refetch })
    }
    onSeen?.()
  }, [queryClient, refetch, onSeen])

  return { count: announced > 0 ? (query.data ?? 0) : 0, markSeen }
}
