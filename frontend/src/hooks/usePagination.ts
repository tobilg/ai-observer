import { useState, useCallback } from 'react'

interface UsePaginationOptions {
  defaultPageSize?: number
  storageKey?: string  // Optional localStorage key for persisting pageSize
}

interface UsePaginationReturn {
  page: number // 1-based
  pageSize: number
  // Cursor of the current page for the list request (undefined on page 1)
  cursor: string | undefined
  // Whether the loaded page has a next one
  hasNext: boolean
  // Records the nextCursor of the loaded page
  setNextCursor: (next: string | undefined) => void
  nextPage: () => void
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
 */
export function usePagination(options: UsePaginationOptions = {}): UsePaginationReturn {
  const { defaultPageSize = 10, storageKey } = options

  // cursors[i] requests page i + 2; the last one is the current page's
  const [cursors, setCursors] = useState<string[]>([])
  const [nextCursor, setNextCursorState] = useState<string | undefined>(undefined)
  const [pageSize, setPageSizeState] = useState(() =>
    getInitialPageSize(storageKey, defaultPageSize)
  )

  const setNextCursor = useCallback((next: string | undefined) => {
    setNextCursorState(next || undefined)
  }, [])

  const nextPage = useCallback(() => {
    if (!nextCursor) return
    setCursors((current) => [...current, nextCursor])
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
