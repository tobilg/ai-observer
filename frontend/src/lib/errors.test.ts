import { describe, expect, it } from 'vitest'
import { AUTH_REQUIRED_MESSAGE, HTTPError, TimeoutError, errorText, httpError, isRetryableError } from './errors'

describe('errorText', () => {
  it('explains how to sign in on 401', () => {
    expect(errorText(httpError(401))).toBe(AUTH_REQUIRED_MESSAGE)
    expect(AUTH_REQUIRED_MESSAGE).toContain('?token=<AI_OBSERVER_AUTH_TOKEN>')
  })

  it('suggests a narrower query on timeouts', () => {
    expect(errorText(new TimeoutError('Request timed out after 30 seconds'))).toMatch(/took too long/)
    expect(errorText(httpError(504, 'query timeout'))).toMatch(/took too long/)
  })

  it("shows the server's message", () => {
    expect(errorText(httpError(400, 'invalid cursor'), 'Failed to load logs')).toBe('invalid cursor')
  })

  it('names a server that cannot be reached', () => {
    expect(errorText(new TypeError('Failed to fetch'))).toMatch(/Cannot reach/)
  })

  it("falls back to the caller's message", () => {
    expect(errorText(httpError(500), 'Failed to load logs')).toBe('Failed to load logs')
    expect(errorText(new Error('boom'), 'Failed to load logs')).toBe('Failed to load logs')
    expect(errorText(undefined)).toMatch(/Something went wrong/)
  })
})

describe('isRetryableError', () => {
  it('retries network errors, 429 and 5xx', () => {
    expect(isRetryableError(new TypeError('Failed to fetch'))).toBe(true)
    expect(isRetryableError(httpError(429))).toBe(true)
    expect(isRetryableError(httpError(500))).toBe(true)
    expect(isRetryableError(httpError(502))).toBe(true)
  })

  it('never retries 4xx, 503, 504, timeouts or aborts', () => {
    for (const status of [400, 401, 404, 413, 503, 504]) {
      expect(isRetryableError(httpError(status))).toBe(false)
    }
    expect(isRetryableError(new TimeoutError('timed out'))).toBe(false)
    const abort = new Error('aborted')
    abort.name = 'AbortError'
    expect(isRetryableError(abort)).toBe(false)
  })

  it('keeps the message format of earlier versions', () => {
    const error = httpError(400, 'name is required')
    expect(error).toBeInstanceOf(HTTPError)
    expect(error.message).toBe('HTTP error! status: 400: name is required')
    expect(error.serverMessage).toBe('name is required')
  })
})
