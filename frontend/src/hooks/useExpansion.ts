import { useCallback, useState } from 'react'

export interface Expansion {
  /** Whether part of item is expanded; fallback until the user toggles it */
  isExpanded(item: number, part: string, fallback?: boolean): boolean
  /** Toggles part of item, which shows as current */
  toggle(item: number, part: string, current: boolean): void
}

/**
 * What the user expanded or collapsed in a virtualized list, by item key
 * (e.g. a message's position in the transcript) and part (a tool block,
 * the rest of a long text): rows unmount when they scroll away and mount
 * again with the same state. Parts nobody toggled use their fallback, e.g.
 * expanded for the current search match.
 */
export function useExpansion(): Expansion {
  const [toggled, setToggled] = useState<ReadonlyMap<string, boolean>>(() => new Map())

  const isExpanded = useCallback(
    (item: number, part: string, fallback = false) => toggled.get(`${item}:${part}`) ?? fallback,
    [toggled],
  )
  const toggle = useCallback((item: number, part: string, current: boolean) => {
    setToggled((prev) => new Map(prev).set(`${item}:${part}`, !current))
  }, [])

  return { isExpanded, toggle }
}
