import { create } from 'zustand'

// Payload of a real-time message: the server announces stored records without
// their content, and pages re-query the API for the data
export interface StoredNotification {
  count: number
  services: string[]
  timestamps: number[] // Unix milliseconds of each record
}

interface TelemetryState {
  // Timestamps (Unix ms) of records received in real time, newest first, used
  // to count items newer than a page's anchor time
  recentSpanTimes: number[]
  recentLogTimes: number[]

  // Update counters (always increment, used to trigger refreshes)
  spansUpdateCount: number
  metricsUpdateCount: number
  logsUpdateCount: number

  // Metric data points received in real time. Metrics are only counted: every
  // consumer re-queries the API when they change.
  metricsReceivedCount: number

  // Actions
  addSpans: (notification: StoredNotification) => void
  addMetrics: (notification: StoredNotification) => void
  addLogs: (notification: StoredNotification) => void
  clearRecentData: () => void
  clearRecentLogs: () => void
  clearRecentSpans: () => void
}

// Every update copies the buffer (zustand needs a new array) and subscribers
// scan it, so keep it small; it only feeds "new items" indicators.
export const MAX_RECENT_ITEMS = 1000

function prependTimes(times: number[], notification: StoredNotification): number[] {
  return [...(notification.timestamps ?? []), ...times].slice(0, MAX_RECENT_ITEMS)
}

export const useTelemetryStore = create<TelemetryState>((set) => ({
  recentSpanTimes: [],
  recentLogTimes: [],
  spansUpdateCount: 0,
  metricsUpdateCount: 0,
  logsUpdateCount: 0,
  metricsReceivedCount: 0,

  addSpans: (notification) =>
    set((state) => ({
      recentSpanTimes: prependTimes(state.recentSpanTimes, notification),
      spansUpdateCount: state.spansUpdateCount + 1,
    })),

  addMetrics: (notification) =>
    set((state) => ({
      metricsUpdateCount: state.metricsUpdateCount + 1,
      metricsReceivedCount: state.metricsReceivedCount + (notification.count ?? 0),
    })),

  addLogs: (notification) =>
    set((state) => ({
      recentLogTimes: prependTimes(state.recentLogTimes, notification),
      logsUpdateCount: state.logsUpdateCount + 1,
    })),

  clearRecentData: () =>
    set({
      recentSpanTimes: [],
      recentLogTimes: [],
      metricsReceivedCount: 0,
    }),

  clearRecentLogs: () =>
    set({
      recentLogTimes: [],
    }),

  clearRecentSpans: () =>
    set({
      recentSpanTimes: [],
    }),
}))
