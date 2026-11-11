import { useCallback, useEffect, useMemo, useRef } from 'react'
import { useSearchParams } from 'react-router-dom'
import { useTimeSelection, type UseTimeSelectionOptions, type UseTimeSelectionResult } from '@/hooks/useTimeSelection'

/**
 * The one writer of a page's URL parameters. Hooks that keep state in the
 * URL (useUrlFilters, useTimeSelection with `url`) write through the same
 * writer, so their changes in one event handler are combined:
 * react-router's setSearchParams builds on the parameters of the last
 * render, so two calls in a row would drop the first one's change.
 * Writes replace the history entry.
 */
export interface UrlWriter {
  params: URLSearchParams
  write: (mutate: (params: URLSearchParams) => void) => void
}

export function useUrlWriter(): UrlWriter {
  const [searchParams, setSearchParams] = useSearchParams()
  // The parameters written since the last render, which later writes in the
  // same tick build on
  const pendingRef = useRef<URLSearchParams | null>(null)
  useEffect(() => {
    pendingRef.current = null
  }, [searchParams])

  const write = useCallback(
    (mutate: (params: URLSearchParams) => void) => {
      const base = pendingRef.current ?? searchParams
      const next = new URLSearchParams(base)
      mutate(next)
      if (next.toString() === base.toString()) {
        return
      }
      pendingRef.current = next
      setSearchParams(next, { replace: true })
    },
    [searchParams, setSearchParams],
  )

  return useMemo(() => ({ params: searchParams, write }), [searchParams, write])
}

// Filters kept in the URL: strings (empty string usually means "all") and
// booleans. The defaults name the parameters and their types; parameters
// equal to their default are left out of the URL. Don't name filters
// timeframe, from or to (the time selection's parameters).
export type UrlFilterValues = Record<string, string | boolean>

function readFilter(params: URLSearchParams, key: string, fallback: string | boolean): string | boolean {
  const value = params.get(key)
  if (value === null) {
    return fallback
  }
  if (typeof fallback === 'boolean') {
    return value === 'true' ? true : value === 'false' ? false : fallback
  }
  return value
}

/**
 * Filters read from and written to the URL through writer. setFilters
 * changes only the given filters.
 */
export function useUrlFilters<F extends UrlFilterValues>(
  defaults: F,
  writer: UrlWriter,
): [F, (updates: Partial<F>) => void] {
  // Callers usually pass an object literal: depend on its content
  const defaultsKey = JSON.stringify(defaults)
  const stableDefaults = useMemo(() => JSON.parse(defaultsKey) as F, [defaultsKey])
  const { params, write } = writer

  // A new object only when a filter's value changes (not when other
  // parameters do), so it can be an effect or memo dependency
  const values: Record<string, string | boolean> = {}
  for (const [key, fallback] of Object.entries(stableDefaults)) {
    values[key] = readFilter(params, key, fallback)
  }
  const valuesKey = JSON.stringify(values)
  const filters = useMemo(() => JSON.parse(valuesKey) as F, [valuesKey])

  const setFilters = useCallback(
    (updates: Partial<F>) => {
      write((next) => {
        for (const [key, value] of Object.entries(updates)) {
          if (value === undefined || value === stableDefaults[key]) {
            next.delete(key)
          } else {
            next.set(key, String(value))
          }
        }
      })
    },
    [write, stableDefaults],
  )

  return [filters, setFilters]
}

export interface UseUrlStateOptions<F extends UrlFilterValues> {
  // Parameters and their defaults, e.g. { service: '', search: '', background: false }
  filters: F
  // The page's time selection, also kept in the URL (UseTimeSelectionOptions without url)
  time: Omit<UseTimeSelectionOptions, 'url'>
}

export interface UseUrlStateResult<F extends UrlFilterValues> {
  filters: F
  setFilters: (updates: Partial<F>) => void
  time: UseTimeSelectionResult
}

/**
 * A list page's filters and time selection, kept in the URL with one writer:
 *
 *   const { filters, setFilters, time } = useUrlState({
 *     filters: { service: '', search: '' },
 *     time: { storageKey: 'ai-observer-logs-timeselection', defaultTimeframe: '24h' },
 *   })
 *   setFilters({ service: 'claude-code' })  // ?service=claude-code&timeframe=...
 *   time.setTimeSelection(selection)        // same URL, filters kept
 */
export function useUrlState<F extends UrlFilterValues>({ filters: defaults, time: timeOptions }: UseUrlStateOptions<F>): UseUrlStateResult<F> {
  const url = useUrlWriter()
  const [filters, setFilters] = useUrlFilters(defaults, url)
  const time = useTimeSelection({ ...timeOptions, url })
  return { filters, setFilters, time }
}
