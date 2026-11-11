import { useEffect, useRef } from 'react'
import { useQueryClient, type QueryKey } from '@tanstack/react-query'
import { useLiveRefresh } from '@/hooks/useLiveRefresh'

interface UseLiveInvalidateOptions {
  // The queries to refresh: a key prefix (queryKeys.logs.lists) or several
  queryKey: QueryKey | QueryKey[]
  // false turns refreshing off, e.g. for absolute time ranges
  enabled: boolean
  // Polling interval while the WebSocket is disconnected
  intervalMs: number
  // Counter of real-time notifications for the shown data (telemetryStore)
  updateCount: number
  debounceMs?: number
  maxWaitMs?: number
}

function keyList(queryKey: QueryKey | QueryKey[]): QueryKey[] {
  return queryKey.length > 0 && queryKey.every(Array.isArray) ? (queryKey as QueryKey[]) : [queryKey as QueryKey]
}

/**
 * Live refresh for TanStack queries: invalidates the given queries whenever
 * useLiveRefresh ticks (debounced real-time notifications, polling while
 * the WebSocket is disconnected, catching up when the tab is shown again).
 * Active queries refetch and keep showing their data meanwhile
 * (isFetching); queryFns resolve relative time windows anew, so the window
 * moves with every refresh.
 *
 *   useLiveInvalidate({
 *     queryKey: queryKeys.sessions.lists,
 *     enabled: !time.isAbsoluteRange,
 *     intervalMs: 30_000,
 *     updateCount: useTelemetryStore((s) => s.logsUpdateCount),
 *   })
 */
export function useLiveInvalidate({ queryKey, enabled, intervalMs, updateCount, debounceMs, maxWaitMs }: UseLiveInvalidateOptions): void {
  const queryClient = useQueryClient()
  const refreshKey = useLiveRefresh({ enabled, intervalMs, updateCount, debounceMs, maxWaitMs })

  // Keys are usually written inline: use the latest without re-running
  const keysRef = useRef(keyList(queryKey))
  useEffect(() => {
    keysRef.current = keyList(queryKey)
  })

  useEffect(() => {
    if (refreshKey === 0) {
      return // nothing to catch up on yet
    }
    for (const key of keysRef.current) {
      void queryClient.invalidateQueries({ queryKey: key })
    }
  }, [refreshKey, queryClient])
}
