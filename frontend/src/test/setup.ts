import '@testing-library/jest-dom'
import { vi } from 'vitest'

// Suppress React act() warnings that occur with async state updates
// These warnings don't indicate actual test failures and are common in tests with async operations
const originalConsoleError = console.error
console.error = (...args: unknown[]) => {
  const message = args[0]
  if (
    typeof message === 'string' &&
    message.includes('not wrapped in act')
  ) {
    return // Suppress act() warnings
  }
  originalConsoleError.apply(console, args)
}

// Mock WebSocket
class MockWebSocket {
  static CONNECTING = 0
  static OPEN = 1
  static CLOSING = 2
  static CLOSED = 3

  readyState = MockWebSocket.CONNECTING
  url: string
  onopen: ((event: Event) => void) | null = null
  onclose: ((event: CloseEvent) => void) | null = null
  onerror: ((event: Event) => void) | null = null
  onmessage: ((event: MessageEvent) => void) | null = null

  constructor(url: string) {
    this.url = url
    // Simulate async connection with queueMicrotask for faster, more predictable timing
    // This runs after the current task but before the next event loop tick
    queueMicrotask(() => {
      if (this.readyState === MockWebSocket.CONNECTING) {
        this.readyState = MockWebSocket.OPEN
        if (this.onopen) {
          this.onopen(new Event('open'))
        }
      }
    })
  }

  send = vi.fn()

  close(code?: number, reason?: string) {
    this.readyState = MockWebSocket.CLOSED
    if (this.onclose) {
      this.onclose(new CloseEvent('close', { code: code ?? 1000, reason }))
    }
  }

  // Helper to simulate receiving a message
  simulateMessage(data: unknown) {
    if (this.onmessage) {
      this.onmessage(new MessageEvent('message', { data: JSON.stringify(data) }))
    }
  }

  // Helper to simulate an error
  simulateError() {
    if (this.onerror) {
      this.onerror(new Event('error'))
    }
  }
}

// Assign mock to global
vi.stubGlobal('WebSocket', MockWebSocket)

// Mock fetch
global.fetch = vi.fn()

// Mock window.location
Object.defineProperty(window, 'location', {
  value: {
    protocol: 'http:',
    host: 'localhost:5173',
    hostname: 'localhost',
    port: '5173',
    href: 'http://localhost:5173/',
    origin: 'http://localhost:5173',
    pathname: '/',
    search: '',
    hash: '',
  },
  writable: true,
})

// Mock ResizeObserver
global.ResizeObserver = vi.fn().mockImplementation(() => ({
  observe: vi.fn(),
  unobserve: vi.fn(),
  disconnect: vi.fn(),
}))

// Mock matchMedia
Object.defineProperty(window, 'matchMedia', {
  writable: true,
  value: vi.fn().mockImplementation((query: string) => ({
    matches: false,
    media: query,
    onchange: null,
    addListener: vi.fn(),
    removeListener: vi.fn(),
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
    dispatchEvent: vi.fn(),
  })),
})

// Reset all mocks before each test
beforeEach(() => {
  vi.clearAllMocks()
})

export { MockWebSocket }
