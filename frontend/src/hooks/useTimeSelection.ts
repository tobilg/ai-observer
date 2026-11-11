import { useState, useMemo, useCallback } from 'react'
import { isAbsoluteTimeSelection, type TimeSelection } from '@/types/dashboard'
import {
  readStoredTimeSelection,
  relativeTimeSelection,
  storeTimeSelection,
  timeRangeSource,
  timeSelectionDurationSeconds,
  timeSelectionFromParams,
  timeSelectionIntervalSeconds,
  timeSelectionTickInterval,
  writeTimeSelectionParams,
  type RangeSource,
  type TimeSelectionKey,
} from '@/lib/timeUtils'
import type { UrlWriter } from '@/hooks/useUrlState'

export interface UseTimeSelectionOptions {
  /** localStorage key of the page; only relative selections are stored */
  storageKey: string
  /** Timeframe value used when neither URL nor storage name one (default: '7d') */
  defaultTimeframe?: string
  /**
   * Keep the selection in the URL (timeframe, or from and to) through the
   * page's writer (useUrlWriter, or useUrlState which passes it)
   */
  url?: UrlWriter
}

export interface UseTimeSelectionResult {
  timeSelection: TimeSelection
  /** Selects a range: relative ones are stored per page, both kinds written to the URL with `url` */
  setTimeSelection: (selection: TimeSelection) => void
  /** Identity of the selection for query keys (never moving timestamps) */
  timeKey: TimeSelectionKey
  /** { key, resolve } for the query hooks of lib/queries.ts; stable while the selection is */
  range: RangeSource
  /** The bounds to query, computed when called: a relative window ends now */
  resolveRange: () => { from: Date; to: Date }
  isAbsoluteRange: boolean
  /** Bucket size for series (from the timeframe, or derived from an absolute range's length) */
  intervalSeconds: number
  /** Tick interval for X-axis labels */
  tickInterval: number
  /** Length of the selected window */
  durationSeconds: number
}

/**
 * A page's time selection. Where it comes from: the URL wins; otherwise the
 * relative selection last stored under storageKey; otherwise
 * defaultTimeframe. Absolute selections are never stored (they live in the
 * URL only), so opening a page again never shows a stale fixed range.
 *
 * Queries put timeKey (or range.key) into their key and call
 * resolveRange() (range.resolve()) in their queryFn, so every fetch of a
 * relative selection queries the window ending at that moment.
 */
export function useTimeSelection({
  storageKey,
  defaultTimeframe = '7d',
  url,
}: UseTimeSelectionOptions): UseTimeSelectionResult {
  // Without URL parameters: the stored or default selection, and the
  // relative one chosen last
  const [localSelection, setLocalSelection] = useState<TimeSelection>(() => {
    return readStoredTimeSelection(storageKey) ?? relativeTimeSelection(defaultTimeframe)
  })

  const timeframeParam = url?.params.get('timeframe') ?? null
  const fromParam = url?.params.get('from') ?? null
  const toParam = url?.params.get('to') ?? null
  const urlSelection = useMemo(() => {
    const params = new URLSearchParams()
    if (timeframeParam) params.set('timeframe', timeframeParam)
    if (fromParam) params.set('from', fromParam)
    if (toParam) params.set('to', toParam)
    return timeSelectionFromParams(params)
  }, [timeframeParam, fromParam, toParam])

  const timeSelection = (url && urlSelection) || localSelection
  const write = url?.write

  const setTimeSelection = useCallback(
    (selection: TimeSelection) => {
      storeTimeSelection(storageKey, selection)
      // With the URL, an absolute range lives there only: removing it from
      // the URL returns to the last relative selection
      if (!write || !isAbsoluteTimeSelection(selection)) {
        setLocalSelection(selection)
      }
      write?.((params) => writeTimeSelectionParams(params, selection))
    },
    [storageKey, write],
  )

  const range = useMemo(() => timeRangeSource(timeSelection), [timeSelection])

  return {
    timeSelection,
    setTimeSelection,
    timeKey: range.key,
    range,
    resolveRange: range.resolve,
    isAbsoluteRange: isAbsoluteTimeSelection(timeSelection),
    intervalSeconds: timeSelectionIntervalSeconds(timeSelection),
    tickInterval: timeSelectionTickInterval(timeSelection),
    durationSeconds: timeSelectionDurationSeconds(timeSelection),
  }
}
