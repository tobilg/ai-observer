import { useEffect, useRef, useState } from 'react'
import { usePageVisible } from '@/hooks/usePageVisible'
import { useWebSocketConnected } from '@/hooks/useWebSocket'

// While real-time notifications arrive, polling only keeps a relative time
// window moving; data changes are announced over the WebSocket
const CONNECTED_INTERVAL_MS = 60_000

interface UseLiveRefreshOptions {
  // false turns refreshing off, e.g. for absolute time ranges
  enabled: boolean
  // Polling interval while the WebSocket is disconnected
  intervalMs: number
  // Counter of real-time notifications for the shown data (telemetryStore)
  updateCount: number
  // Delay after a notification, so a burst of them refreshes once
  debounceMs?: number
  // Longest delay after the first notification of a burst, so a steady
  // stream of them still refreshes
  maxWaitMs?: number
}

/**
 * useLiveRefresh returns a key that increases whenever live data should be
 * fetched again; use it as an effect dependency. It refreshes after real-time
 * notifications (debounced, but at least every maxWaitMs while they keep
 * arriving), polls while the WebSocket is disconnected (and slowly while
 * connected, so relative time windows move), pauses while the tab is hidden
 * and refreshes once when it is shown again.
 */
export function useLiveRefresh({
  enabled,
  intervalMs,
  updateCount,
  debounceMs = 500,
  maxWaitMs = 2000,
}: UseLiveRefreshOptions): number {
  const [refreshKey, setRefreshKey] = useState(0)
  const pageVisible = usePageVisible()
  const connected = useWebSocketConnected()
  const active = enabled && pageVisible
  const lastCountRef = useRef(updateCount)
  // When the first notification that has not been refreshed for arrived
  const pendingSinceRef = useRef<number | null>(null)

  // Polling
  useEffect(() => {
    if (!active) {
      return
    }
    const ms = connected ? Math.max(intervalMs, CONNECTED_INTERVAL_MS) : intervalMs
    const interval = setInterval(() => setRefreshKey((key) => key + 1), ms)
    return () => clearInterval(interval)
  }, [active, connected, intervalMs])

  // Real-time notifications
  useEffect(() => {
    if (updateCount === lastCountRef.current) {
      return
    }
    lastCountRef.current = updateCount
    if (!active) {
      pendingSinceRef.current = null
      return // caught up when the tab is shown again
    }
    // Each notification restarts the debounce timer, so without a deadline
    // notifications arriving faster than debounceMs would never refresh
    const now = Date.now()
    pendingSinceRef.current ??= now
    const deadline = pendingSinceRef.current + maxWaitMs
    const timer = setTimeout(() => {
      pendingSinceRef.current = null
      setRefreshKey((key) => key + 1)
    }, Math.max(0, Math.min(debounceMs, deadline - now)))
    return () => clearTimeout(timer)
  }, [updateCount, active, debounceMs, maxWaitMs])

  // Showing the tab again catches up on what was missed while hidden
  const wasVisibleRef = useRef(pageVisible)
  useEffect(() => {
    if (pageVisible && !wasVisibleRef.current && enabled) {
      setRefreshKey((key) => key + 1)
    }
    wasVisibleRef.current = pageVisible
  }, [pageVisible, enabled])

  return refreshKey
}
