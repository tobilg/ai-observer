import { useState, useCallback } from 'react'

interface UsePaginationOptions {
  defaultPageSize?: number
  storageKey?: string  // Optional localStorage key for persisting pageSize
  // Goes back to the first page whenever this value changes (compared with
  // Object.is, so pass a string such as JSON of the filters): cursors mark
  // positions in one filtered list. Covers filter changes from the URL too
  // (browser Back, links), not only those of the page's own handlers.
  resetOn?: unknown
}

interface UsePaginationReturn {
  page: number // 1-based
  pageSize: number
  // Cursor of the current page for the list request (undefined on page 1)
  cursor: string | undefined
  // Whether the loaded page has a next one (from setNextCursor)
  hasNext: boolean
  // Records the nextCursor of the loaded page
  setNextCursor: (next: string | undefined) => void
  // Goes to the next page: the given cursor (the loaded page's nextCursor),
  // or the one recorded with setNextCursor
  nextPage: (cursor?: string) => void
  previousPage: () => void
  setPageSize: (size: number) => void
  resetToFirstPage: () => void
}

/**
 * Read pageSize from localStorage if storageKey is provided
 */
function getInitialPageSize(storageKey: string | undefined, defaultPageSize: number): number {
  if (!storageKey) return defaultPageSize
  try {
    const stored = localStorage.getItem(storageKey)
    if (stored !== null) {
      const parsed = JSON.parse(stored)
      if (typeof parsed === 'number' && parsed > 0) {
        return parsed
      }
    }
  } catch {
    // Ignore parse errors, use default
  }
  return defaultPageSize
}

/**
 * Cursor pagination for the list APIs (logs, traces, sessions): each page is
 * requested with the nextCursor of the page before it, so pages do not shift
 * while new data arrives. The cursors of the pages visited so far are kept
 * to go back.
 *
 * With TanStack list queries, pass the loaded page's cursor to nextPage and
 * take hasNext from the query, ignoring placeholder data (whose nextCursor
 * belongs to the page shown before):
 *
 *   const hasNext = !query.isPlaceholderData && Boolean(query.data?.nextCursor)
 *   onNext={() => pagination.nextPage(query.data?.nextCursor)}
 */
export function usePagination(options: UsePaginationOptions = {}): UsePaginationReturn {
  const { defaultPageSize = 10, storageKey, resetOn } = options

  // cursors[i] requests page i + 2; the last one is the current page's
  const [cursors, setCursors] = useState<string[]>([])
  const [nextCursor, setNextCursorState] = useState<string | undefined>(undefined)
  const [pageSize, setPageSizeState] = useState(() =>
    getInitialPageSize(storageKey, defaultPageSize)
  )

  // Start over when the list changes. Adjusting state while rendering
  // avoids requesting the new list with an old cursor first.
  const [listKey, setListKey] = useState(resetOn)
  if (!Object.is(listKey, resetOn)) {
    setListKey(resetOn)
    setCursors([])
    setNextCursorState(undefined)
  }

  const setNextCursor = useCallback((next: string | undefined) => {
    setNextCursorState(next || undefined)
  }, [])

  const nextPage = useCallback((cursor?: string) => {
    const next = cursor || nextCursor
    if (!next) return
    setCursors((current) => [...current, next])
    // Unknown until the next page has loaded
    setNextCursorState(undefined)
  }, [nextCursor])

  const previousPage = useCallback(() => {
    setCursors((current) => current.slice(0, -1))
    setNextCursorState(undefined)
  }, [])

  const resetToFirstPage = useCallback(() => {
    setCursors((current) => (current.length === 0 ? current : []))
  }, [])

  const setPageSize = useCallback((newSize: number) => {
    // Cursors mark positions for the old page size: start over
    setPageSizeState(newSize)
    setCursors([])
    setNextCursorState(undefined)

    // Persist to localStorage if storageKey provided
    if (storageKey) {
      try {
        localStorage.setItem(storageKey, JSON.stringify(newSize))
      } catch {
        // Ignore storage errors
      }
    }
  }, [storageKey])

  return {
    page: cursors.length + 1,
    pageSize,
    cursor: cursors[cursors.length - 1],
    hasNext: Boolean(nextCursor),
    setNextCursor,
    nextPage,
    previousPage,
    setPageSize,
    resetToFirstPage,
  }
}
