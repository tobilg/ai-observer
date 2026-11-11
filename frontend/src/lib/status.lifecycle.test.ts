import { describe, it, expect } from 'vitest'
import { daysUntil, freeShare, isUnbounded, localDayRange } from './status'

describe('isUnbounded', () => {
  it('is set without retention and size limit', () => {
    expect(isUnbounded({ retentionDays: 0, maxDbSizeBytes: 0 })).toBe(true)
    expect(isUnbounded({ retentionDays: 30, maxDbSizeBytes: 0 })).toBe(false)
    expect(isUnbounded({ retentionDays: 0, maxDbSizeBytes: 20 * 1024 ** 3 })).toBe(false)
  })
})

describe('freeShare', () => {
  it('is the free part of the file', () => {
    expect(freeShare({ usedBytes: 750, freeBytes: 250 })).toBe(0.25)
    expect(freeShare({ usedBytes: 0, freeBytes: 0 })).toBeNull()
  })
})

describe('daysUntil', () => {
  it('divides the remaining space by the growth', () => {
    expect(daysUntil({ usedBytes: 1_000, growthBytesPerDay: 100 }, 1_550)).toBe(5)
    expect(daysUntil({ usedBytes: 2_000, growthBytesPerDay: 100 }, 1_550)).toBeNull()
    expect(daysUntil({ usedBytes: 1_000 }, 1_550)).toBeNull()
  })
})

describe('localDayRange', () => {
  it('spans whole local days', () => {
    const range = localDayRange('2026-11-01', '2026-11-03')
    expect(range).not.toBeNull()
    const from = new Date(range!.from)
    const to = new Date(range!.to)
    expect([from.getFullYear(), from.getMonth(), from.getDate(), from.getHours(), from.getMinutes()]).toEqual([2026, 10, 1, 0, 0])
    expect([to.getFullYear(), to.getMonth(), to.getDate(), to.getHours(), to.getMinutes(), to.getSeconds()]).toEqual([2026, 10, 3, 23, 59, 59])
  })

  it('accepts a single day', () => {
    const range = localDayRange('2026-11-01', '2026-11-01')
    expect(range && new Date(range.to).getTime() - new Date(range.from).getTime()).toBe(86_400_000 - 1)
  })

  it('rejects missing, invalid and reversed days', () => {
    expect(localDayRange('', '2026-11-01')).toBeNull()
    expect(localDayRange('2026-11-01', 'tomorrow')).toBeNull()
    expect(localDayRange('2026-11-03', '2026-11-01')).toBeNull()
  })
})
