import { beforeEach, describe, expect, it } from 'vitest'
import {
  absoluteTimeSelection,
  readStoredTimeSelection,
  relativeTimeSelection,
  resolveTimeRange,
  storeTimeSelection,
  timeRangeSource,
  timeSelectionFromParams,
  timeSelectionKey,
  writeTimeSelectionParams,
} from './timeUtils'

const from = new Date('2026-01-01T00:00:00.000Z')
const to = new Date('2026-01-03T00:00:00.000Z')

describe('time selections', () => {
  it('resolves a relative window when asked, an absolute one as fixed', () => {
    const relative = relativeTimeSelection('1h')
    const now = new Date('2026-05-01T12:00:00Z')
    expect(resolveTimeRange(relative, now)).toEqual({ from: new Date('2026-05-01T11:00:00Z'), to: now })

    const absolute = absoluteTimeSelection(from, to)
    expect(resolveTimeRange(absolute, now)).toEqual({ from, to })
  })

  it('moves a relative window with every resolve', async () => {
    const range = timeRangeSource(relativeTimeSelection('5m'))
    const first = range.resolve()
    await new Promise((resolve) => setTimeout(resolve, 5))
    expect(range.resolve().to.getTime()).toBeGreaterThan(first.to.getTime())
  })

  it('keys selections by identity, not by moving bounds', () => {
    expect(timeSelectionKey(relativeTimeSelection('24h'))).toEqual({ timeframe: '24h' })
    expect(timeSelectionKey(absoluteTimeSelection(from, to))).toEqual({ from: from.toISOString(), to: to.toISOString() })
  })

  it('derives bucketing of absolute ranges from their length', () => {
    const selection = absoluteTimeSelection(from, to)
    expect(selection).toMatchObject({ type: 'absolute', range: { intervalSeconds: 21600, tickInterval: 1 } })
  })

  it('falls back to the default timeframe for unknown values', () => {
    expect(relativeTimeSelection('nope')).toMatchObject({ timeframe: { value: '7d' } })
    expect(relativeTimeSelection('nope', '1h')).toMatchObject({ timeframe: { value: '1h' } })
  })
})

describe('URL parameters', () => {
  it('reads absolute ranges before timeframes', () => {
    const params = new URLSearchParams({ timeframe: '1h', from: from.toISOString(), to: to.toISOString() })
    expect(timeSelectionFromParams(params)).toMatchObject({ type: 'absolute', range: { from, to } })
    expect(timeSelectionFromParams(new URLSearchParams({ timeframe: '1h' }))).toMatchObject({ timeframe: { value: '1h' } })
  })

  it('ignores invalid parameters', () => {
    expect(timeSelectionFromParams(new URLSearchParams({ timeframe: 'x' }))).toBeNull()
    expect(timeSelectionFromParams(new URLSearchParams({ from: 'x', to: 'y' }))).toBeNull()
    expect(timeSelectionFromParams(new URLSearchParams({ from: to.toISOString(), to: from.toISOString() }))).toBeNull()
  })

  it('writes one kind and removes the other', () => {
    const params = new URLSearchParams({ service: 'a', timeframe: '1h' })
    writeTimeSelectionParams(params, absoluteTimeSelection(from, to))
    expect(params.toString()).toBe(`service=a&from=${encodeURIComponent(from.toISOString())}&to=${encodeURIComponent(to.toISOString())}`)

    writeTimeSelectionParams(params, relativeTimeSelection('24h'))
    expect(params.toString()).toBe('service=a&timeframe=24h')
  })
})

describe('storage', () => {
  beforeEach(() => localStorage.clear())

  it('stores and restores relative selections', () => {
    storeTimeSelection('k', relativeTimeSelection('3h'))
    expect(readStoredTimeSelection('k')).toMatchObject({ type: 'relative', timeframe: { value: '3h' } })
  })

  it('never stores or restores absolute selections', () => {
    storeTimeSelection('k', absoluteTimeSelection(from, to))
    expect(localStorage.getItem('k')).toBeNull()

    // Stored by earlier versions
    localStorage.setItem('k', JSON.stringify({ type: 'absolute', from: from.toISOString(), to: to.toISOString() }))
    expect(readStoredTimeSelection('k')).toBeNull()
  })

  it('ignores unreadable values', () => {
    localStorage.setItem('k', '{not json')
    expect(readStoredTimeSelection('k')).toBeNull()
  })
})
