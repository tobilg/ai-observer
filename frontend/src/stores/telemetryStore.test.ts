import { describe, it, expect, beforeEach } from 'vitest'
import { MAX_RECENT_ITEMS, useTelemetryStore, type StoredNotification } from './telemetryStore'

describe('telemetryStore', () => {
  beforeEach(() => {
    // Reset store state before each test using the store's clearRecentData action
    useTelemetryStore.getState().clearRecentData()
  })

  const notification = (...timestamps: number[]): StoredNotification => ({
    count: timestamps.length,
    services: ['test-service'],
    timestamps,
  })

  describe('initial state', () => {
    it('has empty buffers and no received metrics', () => {
      const state = useTelemetryStore.getState()
      expect(state.recentSpanTimes).toEqual([])
      expect(state.recentLogTimes).toEqual([])
      expect(state.metricsReceivedCount).toBe(0)
    })
  })

  describe('addSpans', () => {
    it('records the timestamps of stored spans', () => {
      useTelemetryStore.getState().addSpans(notification(1, 2))

      expect(useTelemetryStore.getState().recentSpanTimes).toEqual([1, 2])
    })

    it('prepends newer notifications', () => {
      useTelemetryStore.getState().addSpans(notification(1))
      useTelemetryStore.getState().addSpans(notification(2))

      expect(useTelemetryStore.getState().recentSpanTimes).toEqual([2, 1])
    })

    it('keeps at most MAX_RECENT_ITEMS timestamps, newest first', () => {
      const times = Array.from({ length: MAX_RECENT_ITEMS + 5 }, (_, i) => i)
      useTelemetryStore.getState().addSpans(notification(...times.slice(0, 5)))
      useTelemetryStore.getState().addSpans(notification(...times.slice(5)))

      const state = useTelemetryStore.getState()
      expect(state.recentSpanTimes).toHaveLength(MAX_RECENT_ITEMS)
      expect(state.recentSpanTimes[0]).toBe(5)
    })

    it('increments the update counter for every notification', () => {
      const before = useTelemetryStore.getState().spansUpdateCount

      useTelemetryStore.getState().addSpans(notification(1))

      expect(useTelemetryStore.getState().spansUpdateCount).toBe(before + 1)
    })
  })

  describe('addMetrics', () => {
    it('counts received metrics without buffering them', () => {
      useTelemetryStore.getState().addMetrics(notification(1, 2))
      useTelemetryStore.getState().addMetrics(notification(3))

      const state = useTelemetryStore.getState()
      expect(state.metricsReceivedCount).toBe(3)
      expect('recentMetrics' in state).toBe(false)
    })

    it('increments the update counter for every batch', () => {
      const before = useTelemetryStore.getState().metricsUpdateCount

      useTelemetryStore.getState().addMetrics(notification(1))
      useTelemetryStore.getState().addMetrics(notification(2))

      expect(useTelemetryStore.getState().metricsUpdateCount).toBe(before + 2)
    })
  })

  describe('addLogs', () => {
    it('records the timestamps of stored logs, newest first', () => {
      useTelemetryStore.getState().addLogs(notification(1))
      useTelemetryStore.getState().addLogs(notification(2, 3))

      expect(useTelemetryStore.getState().recentLogTimes).toEqual([2, 3, 1])
    })

    it('tolerates notifications without timestamps', () => {
      useTelemetryStore.getState().addLogs({ count: 0, services: [] } as unknown as StoredNotification)

      expect(useTelemetryStore.getState().recentLogTimes).toEqual([])
    })
  })

  describe('clearRecentData', () => {
    it('clears all data', () => {
      useTelemetryStore.getState().addSpans(notification(1))
      useTelemetryStore.getState().addMetrics(notification(1))
      useTelemetryStore.getState().addLogs(notification(1))

      useTelemetryStore.getState().clearRecentData()

      const state = useTelemetryStore.getState()
      expect(state.recentSpanTimes).toEqual([])
      expect(state.recentLogTimes).toEqual([])
      expect(state.metricsReceivedCount).toBe(0)
    })

    it('allows adding data after clearing', () => {
      useTelemetryStore.getState().addSpans(notification(1))
      useTelemetryStore.getState().clearRecentData()
      useTelemetryStore.getState().addSpans(notification(2))

      expect(useTelemetryStore.getState().recentSpanTimes).toEqual([2])
    })
  })

  describe('independence of data types', () => {
    it('adding spans does not affect metrics or logs', () => {
      useTelemetryStore.getState().addMetrics(notification(1))
      useTelemetryStore.getState().addLogs(notification(1))
      useTelemetryStore.getState().addSpans(notification(1))

      const state = useTelemetryStore.getState()
      expect(state.recentSpanTimes).toHaveLength(1)
      expect(state.metricsReceivedCount).toBe(1)
      expect(state.recentLogTimes).toHaveLength(1)
    })
  })
})
