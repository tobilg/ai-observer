import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { renderHook, act, waitFor } from '@testing-library/react'
import { useWebSocket, webSocketManager } from './useWebSocket'
import { useTelemetryStore } from '@/stores/telemetryStore'

interface MockSocket extends WebSocket {
  simulateMessage(data: unknown): void
  simulateError(): void
}

type WebSocketManagerWithMock = typeof webSocketManager & {
  ws: MockSocket | null
}

function getMockWebSocket(): MockSocket {
  const ws = (webSocketManager as WebSocketManagerWithMock).ws
  if (!ws) {
    throw new Error('Expected mock WebSocket instance')
  }
  return ws
}

describe('useWebSocket', () => {
  beforeEach(() => {
    // Reset store
    useTelemetryStore.setState({
      recentSpans: [],
      recentMetrics: [],
      recentLogs: [],
    })

    // Disconnect any existing connection
    webSocketManager.disconnect()
  })

  afterEach(() => {
    webSocketManager.disconnect()
  })

  describe('connection', () => {
    it('connects on mount', async () => {
      const { result } = renderHook(() => useWebSocket('/ws'))

      // Wait for connection
      await waitFor(() => {
        expect(result.current.isConnected).toBe(true)
      })
    })

    it('returns error as null when connected', async () => {
      const { result } = renderHook(() => useWebSocket('/ws'))

      await waitFor(() => {
        expect(result.current.isConnected).toBe(true)
      })

      expect(result.current.error).toBeNull()
    })

    it('uses custom URL', async () => {
      const { result } = renderHook(() => useWebSocket('/custom-ws'))

      await waitFor(() => {
        expect(result.current.isConnected).toBe(true)
      })
    })
  })

  describe('message handling', () => {
    it('routes trace messages to store', async () => {
      renderHook(() => useWebSocket('/ws'))

      await waitFor(() => {
        expect(webSocketManager.isConnected).toBe(true)
      })

      // Get the mock WebSocket instance
      const mockWs = getMockWebSocket()

      // Simulate receiving a traces message
      act(() => {
        mockWs.simulateMessage({
          type: 'traces',
          timestamp: new Date().toISOString(),
          payload: [
            {
              traceId: 'trace-1',
              spanId: 'span-1',
              spanName: 'test',
              serviceName: 'service',
              timestamp: new Date().toISOString(),
              duration: 1000,
            },
          ],
        })
      })

      expect(useTelemetryStore.getState().recentSpans).toHaveLength(1)
    })

    it('routes metrics messages to store', async () => {
      renderHook(() => useWebSocket('/ws'))

      await waitFor(() => {
        expect(webSocketManager.isConnected).toBe(true)
      })

      const mockWs = getMockWebSocket()

      act(() => {
        mockWs.simulateMessage({
          type: 'metrics',
          timestamp: new Date().toISOString(),
          payload: [
            {
              metricName: 'test_metric',
              serviceName: 'service',
              metricType: 'gauge',
              timestamp: new Date().toISOString(),
              value: 42,
            },
          ],
        })
      })

      expect(useTelemetryStore.getState().recentMetrics).toHaveLength(1)
    })

    it('routes logs messages to store', async () => {
      renderHook(() => useWebSocket('/ws'))

      await waitFor(() => {
        expect(webSocketManager.isConnected).toBe(true)
      })

      const mockWs = getMockWebSocket()

      act(() => {
        mockWs.simulateMessage({
          type: 'logs',
          timestamp: new Date().toISOString(),
          payload: [
            {
              body: 'Test log',
              serviceName: 'service',
              severityText: 'INFO',
              timestamp: new Date().toISOString(),
            },
          ],
        })
      })

      expect(useTelemetryStore.getState().recentLogs).toHaveLength(1)
    })
  })
})

describe('webSocketManager', () => {
  beforeEach(() => {
    webSocketManager.disconnect()
  })

  afterEach(() => {
    webSocketManager.disconnect()
  })

  describe('connect', () => {
    it('creates WebSocket connection', async () => {
      webSocketManager.connect('/ws')

      await waitFor(() => {
        expect(webSocketManager.isConnected).toBe(true)
      })
    })

    it('does not reconnect if already connected to same URL', async () => {
      webSocketManager.connect('/ws')

      await waitFor(() => {
        expect(webSocketManager.isConnected).toBe(true)
      })

      const ws1 = getMockWebSocket()

      webSocketManager.connect('/ws')

      expect(getMockWebSocket()).toBe(ws1)
    })

    it('disconnects and reconnects for different URL', async () => {
      webSocketManager.connect('/ws1')

      await waitFor(() => {
        expect(webSocketManager.isConnected).toBe(true)
      })

      const ws1 = getMockWebSocket()

      webSocketManager.connect('/ws2')

      // The old WebSocket should be closed
      expect(ws1.readyState).toBe(WebSocket.CLOSED)
    })
  })

  describe('disconnect', () => {
    it('closes WebSocket connection', async () => {
      webSocketManager.connect('/ws')

      await waitFor(() => {
        expect(webSocketManager.isConnected).toBe(true)
      })

      webSocketManager.disconnect()

      expect(webSocketManager.isConnected).toBe(false)
    })

    it('handles disconnect when not connected', () => {
      expect(() => webSocketManager.disconnect()).not.toThrow()
    })
  })

  describe('subscription', () => {
    it('notifies listeners on connection', async () => {
      const listener = vi.fn()
      const unsubscribe = webSocketManager.subscribe(listener)

      webSocketManager.connect('/ws')

      await waitFor(() => {
        expect(listener).toHaveBeenCalled()
      })

      unsubscribe()
    })

    it('notifies listeners on disconnect', async () => {
      const listener = vi.fn()

      webSocketManager.connect('/ws')

      await waitFor(() => {
        expect(webSocketManager.isConnected).toBe(true)
      })

      const unsubscribe = webSocketManager.subscribe(listener)
      listener.mockClear()

      webSocketManager.disconnect()

      expect(listener).toHaveBeenCalled()

      unsubscribe()
    })

    it('unsubscribe removes listener', async () => {
      const listener = vi.fn()
      const unsubscribe = webSocketManager.subscribe(listener)

      unsubscribe()
      listener.mockClear()

      webSocketManager.connect('/ws')

      await waitFor(() => {
        expect(webSocketManager.isConnected).toBe(true)
      })

      // The listener should not be called after unsubscribe
      // Note: this is tricky to test because the connection itself triggers a notification
      // We check that after unsubscribe, new events don't trigger the listener
      expect(listener).not.toHaveBeenCalled()
    })
  })

  describe('message handler', () => {
    it('calls message handler with parsed message', async () => {
      const handler = vi.fn()
      webSocketManager.setMessageHandler(handler)

      webSocketManager.connect('/ws')

      await waitFor(() => {
        expect(webSocketManager.isConnected).toBe(true)
      })

      const mockWs = getMockWebSocket()
      const testMessage = {
        type: 'traces',
        timestamp: new Date().toISOString(),
        payload: [],
      }

      mockWs.simulateMessage(testMessage)

      expect(handler).toHaveBeenCalledWith(testMessage)
    })

    it('handles invalid JSON gracefully', async () => {
      const handler = vi.fn()
      const consoleSpy = vi.spyOn(console, 'error').mockImplementation(() => {})

      webSocketManager.setMessageHandler(handler)
      webSocketManager.connect('/ws')

      await waitFor(() => {
        expect(webSocketManager.isConnected).toBe(true)
      })

      const mockWs = getMockWebSocket()

      // Trigger onmessage with invalid JSON
      if (mockWs.onmessage) {
        mockWs.onmessage(new MessageEvent('message', { data: 'invalid json' }))
      }

      expect(handler).not.toHaveBeenCalled()
      expect(consoleSpy).toHaveBeenCalled()

      consoleSpy.mockRestore()
    })
  })

  describe('error handling', () => {
    it('sets error state on WebSocket error', async () => {
      webSocketManager.connect('/ws')

      await waitFor(() => {
        expect(webSocketManager.isConnected).toBe(true)
      })

      const mockWs = getMockWebSocket()

      // Simulate error
      mockWs.simulateError()

      expect(webSocketManager.error).toBe('WebSocket connection error')
    })
  })
})
