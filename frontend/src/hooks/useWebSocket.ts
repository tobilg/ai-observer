import { useEffect, useState, useSyncExternalStore, useCallback } from 'react'
import { useTelemetryStore, type StoredNotification } from '@/stores/telemetryStore'

interface WebSocketMessage {
  type: 'traces' | 'metrics' | 'logs'
  timestamp: string
  payload: StoredNotification
}

// Reconnect delays grow from 1 s to 30 s while the server cannot be reached
// (or rejects the connection, e.g. without the auth cookie)
const RECONNECT_BASE_MS = 1000
const RECONNECT_MAX_MS = 30000

// Singleton WebSocket manager
class WebSocketManager {
  private ws: WebSocket | null = null
  private url: string = ''
  private reconnectTimeout: number | null = null
  private reconnectAttempts = 0
  private listeners: Set<() => void> = new Set()
  private _isConnected: boolean = false
  private _error: string | null = null
  private messageHandler: ((message: WebSocketMessage) => void) | null = null

  get isConnected() {
    return this._isConnected
  }

  get error() {
    return this._error
  }

  subscribe(listener: () => void) {
    this.listeners.add(listener)
    return () => {
      this.listeners.delete(listener)
    }
  }

  private notify() {
    this.listeners.forEach((listener) => listener())
  }

  setMessageHandler(handler: (message: WebSocketMessage) => void) {
    this.messageHandler = handler
  }

  connect(url: string) {
    // Don't reconnect if already connected to the same URL
    if (this.ws && this.url === url && this.ws.readyState === WebSocket.OPEN) {
      return
    }

    // Close existing connection if any
    this.disconnect()

    this.url = url

    try {
      const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
      const wsUrl = url.startsWith('/')
        ? `${protocol}//${window.location.host}${url}`
        : url

      this.ws = new WebSocket(wsUrl)

      this.ws.onopen = () => {
        this.reconnectAttempts = 0
        this._isConnected = true
        this._error = null
        this.notify()
      }

      this.ws.onclose = (event) => {
        this._isConnected = false
        this.notify()

        // Only reconnect if not a clean close
        if (event.code !== 1000) {
          this.scheduleReconnect()
        }
      }

      this.ws.onerror = () => {
        this._error = 'WebSocket connection error'
        console.error('WebSocket error')
        this.notify()
      }

      this.ws.onmessage = (event) => {
        try {
          const message: WebSocketMessage = JSON.parse(event.data)
          if (this.messageHandler) {
            this.messageHandler(message)
          }
        } catch (err) {
          console.error('Failed to parse WebSocket message:', err)
        }
      }
    } catch (err) {
      console.error('Failed to create WebSocket:', err)
      this._error = 'Failed to create WebSocket connection'
      this.notify()
    }
  }

  private scheduleReconnect() {
    if (this.reconnectTimeout) {
      clearTimeout(this.reconnectTimeout)
    }
    const delay = Math.min(RECONNECT_BASE_MS * 2 ** this.reconnectAttempts, RECONNECT_MAX_MS)
    this.reconnectAttempts++
    // Up to 20% jitter, so tabs do not reconnect in lockstep
    this.reconnectTimeout = window.setTimeout(() => {
      this.connect(this.url)
    }, delay * (1 + Math.random() * 0.2))
  }

  disconnect() {
    if (this.reconnectTimeout) {
      clearTimeout(this.reconnectTimeout)
      this.reconnectTimeout = null
    }
    if (this.ws) {
      // Remove handlers before closing to prevent reconnect
      this.ws.onclose = null
      this.ws.onerror = null
      this.ws.onmessage = null
      this.ws.onopen = null

      if (this.ws.readyState === WebSocket.OPEN || this.ws.readyState === WebSocket.CONNECTING) {
        this.ws.close(1000, 'Client disconnect')
      }
      this.ws = null
    }
    this._isConnected = false
    this.notify()
  }
}

// Single global instance
const wsManager = new WebSocketManager()

export function useWebSocket(url: string = '/ws') {
  const addSpans = useTelemetryStore((state) => state.addSpans)
  const addMetrics = useTelemetryStore((state) => state.addMetrics)
  const addLogs = useTelemetryStore((state) => state.addLogs)

  // Use useSyncExternalStore for proper React 18+ subscription
  const isConnected = useSyncExternalStore(
    (callback) => wsManager.subscribe(callback),
    () => wsManager.isConnected
  )

  const [error, setError] = useState<string | null>(null)

  // Memoize the message handler to prevent accumulation
  const handleMessage = useCallback((message: WebSocketMessage) => {
    switch (message.type) {
      case 'traces':
        addSpans(message.payload)
        break
      case 'metrics':
        addMetrics(message.payload)
        break
      case 'logs':
        addLogs(message.payload)
        break
    }
  }, [addSpans, addMetrics, addLogs])

  useEffect(() => {
    // Set up message handler
    wsManager.setMessageHandler(handleMessage)

    // Connect
    wsManager.connect(url)

    // Update error state
    const unsubscribe = wsManager.subscribe(() => {
      setError(wsManager.error)
    })

    return () => {
      unsubscribe()
      // Don't disconnect on unmount - keep connection alive
      // The connection is shared across the app
    }
  }, [url, handleMessage])

  return { isConnected, error }
}

// useWebSocketConnected reports whether the shared WebSocket is connected,
// without connecting it (Layout's useWebSocket does that)
export function useWebSocketConnected(): boolean {
  return useSyncExternalStore(
    (callback) => wsManager.subscribe(callback),
    () => wsManager.isConnected
  )
}

// Export for manual control if needed
export const webSocketManager = wsManager
