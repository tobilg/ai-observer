// Errors of API requests and the messages pages show for them. This module
// has no dependencies, so modules that page tests mock (@/lib/api) can be
// replaced without losing these helpers.

export const AUTH_REQUIRED_MESSAGE =
  'Authentication required: open AI Observer once with ?token=<AI_OBSERVER_AUTH_TOKEN>'

const TIMEOUT_MESSAGE = 'The request took too long. Try a shorter time range or fewer filters.'
const NETWORK_MESSAGE = 'Cannot reach the AI Observer server.'

// A response with an error status. message keeps the format of earlier
// versions ("HTTP error! status: 400: <server message>"); serverMessage is
// the server's explanation alone ({"error", "message"} body), if any.
export class HTTPError extends Error {
  status: number
  serverMessage?: string

  constructor(status: number, message: string, serverMessage?: string) {
    super(message)
    this.name = 'HTTPError'
    this.status = status
    this.serverMessage = serverMessage
  }
}

// A request that the client stopped after its timeout (not an abort by the
// caller, which pages ignore)
export class TimeoutError extends Error {
  constructor(message: string, options?: ErrorOptions) {
    super(message, options)
    this.name = 'TimeoutError'
  }
}

export function httpError(status: number, serverMessage?: string): HTTPError {
  if (status === 401) {
    return new HTTPError(status, AUTH_REQUIRED_MESSAGE, serverMessage)
  }
  return new HTTPError(status, `HTTP error! status: ${status}${serverMessage ? `: ${serverMessage}` : ''}`, serverMessage)
}

export function isAbortError(error: unknown): boolean {
  return error instanceof Error && error.name === 'AbortError'
}

export function isHTTPError(error: unknown, status?: number): error is HTTPError {
  return error instanceof HTTPError && (status === undefined || error.status === status)
}

export function isAuthError(error: unknown): boolean {
  return isHTTPError(error, 401)
}

export function isTimeoutError(error: unknown): boolean {
  return error instanceof TimeoutError || isHTTPError(error, 504)
}

// fetch rejects with a TypeError when the server cannot be reached; the
// message differs by browser ("Failed to fetch", "Load failed", ...)
export function isNetworkError(error: unknown): boolean {
  return error instanceof TypeError
}

/**
 * Whether a failed read is worth repeating: network errors, 429 and 5xx,
 * except 503 (the server answers it when storage is busy; exporters retry
 * it, the dashboard would only pile up) and 504 (a query stopped by the
 * server's timeout would time out again). Never 4xx, aborts or timeouts.
 */
export function isRetryableError(error: unknown): boolean {
  if (isNetworkError(error)) {
    return true
  }
  if (!(error instanceof HTTPError)) {
    return false
  }
  if (error.status === 503 || error.status === 504) {
    return false
  }
  return error.status === 429 || error.status >= 500
}

/**
 * The message to show for a failed request: how to sign in on 401, a hint
 * on timeouts (client timeout or 504), the server's explanation when it sent
 * one, otherwise the caller's fallback (e.g. "Failed to load traces").
 */
export function errorText(error: unknown, fallback = 'Something went wrong. Try again.'): string {
  if (isAuthError(error)) {
    return AUTH_REQUIRED_MESSAGE
  }
  if (isTimeoutError(error)) {
    return TIMEOUT_MESSAGE
  }
  if (error instanceof HTTPError && error.serverMessage) {
    return error.serverMessage
  }
  if (isNetworkError(error)) {
    return NETWORK_MESSAGE
  }
  return fallback
}
