import { describe, it, expect, vi, beforeEach } from 'vitest'
import { ADMIN_TIMEOUT_MS, REQUEST_TIMEOUT_MS, api } from './api'
import { TimeoutError } from './errors'

describe('admin api', () => {
  beforeEach(() => {
    vi.mocked(global.fetch).mockReset()
  })

  const respond = (data: unknown) =>
    vi.mocked(global.fetch).mockResolvedValueOnce({ ok: true, status: 200, json: async () => data } as Response)

  it('posts deletions as JSON', async () => {
    respond({ dryRun: true, logs: 1 })
    const req = { from: '2026-11-01T00:00:00.000Z', to: '2026-11-02T00:00:00.000Z', scope: 'all' as const, dryRun: true }
    await api.adminDelete(req)
    expect(global.fetch).toHaveBeenCalledWith(
      '/api/admin/delete',
      expect.objectContaining({ method: 'POST', body: JSON.stringify(req), headers: { 'Content-Type': 'application/json' } }),
    )
  })

  it('posts checkpoints and backups', async () => {
    respond({ database: {}, durationMs: 1 })
    await api.adminCheckpoint()
    expect(global.fetch).toHaveBeenLastCalledWith('/api/admin/checkpoint', expect.objectContaining({ method: 'POST' }))

    respond({ path: '/b' })
    await api.adminBackup('mine.duckdb')
    expect(global.fetch).toHaveBeenLastCalledWith('/api/admin/backup', expect.objectContaining({ body: '{"name":"mine.duckdb"}' }))
    respond({ path: '/b' })
    await api.adminBackup()
    expect(global.fetch).toHaveBeenLastCalledWith('/api/admin/backup', expect.objectContaining({ body: '{}' }))
  })

  it('waits longer than other requests: a backup takes minutes', async () => {
    vi.useFakeTimers()
    try {
      vi.mocked(global.fetch).mockImplementationOnce((_url, init) => new Promise((_resolve, reject) => {
        init?.signal?.addEventListener('abort', () => {
          const error = new Error('aborted')
          error.name = 'AbortError'
          reject(error)
        })
      }))
      let settled = false
      const result = api.adminBackup()
      result.catch(() => {}).finally(() => { settled = true })
      await vi.advanceTimersByTimeAsync(REQUEST_TIMEOUT_MS + 1_000)
      expect(settled).toBe(false)
      const assertion = expect(result).rejects.toThrow(TimeoutError)
      await vi.advanceTimersByTimeAsync(ADMIN_TIMEOUT_MS)
      await assertion
    } finally {
      vi.useRealTimers()
    }
  })
})
