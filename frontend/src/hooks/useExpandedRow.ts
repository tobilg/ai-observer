import { useCallback, useState } from 'react'

/**
 * Which row of a list is expanded, by the row's stable identity (rowKeys),
 * so a refetch of the same page keeps it open. viewKey names what the list
 * shows (filters, time range, page): when it changes, nothing is expanded.
 *
 *   const [expanded, toggle] = useExpandedRow(JSON.stringify([filters, cursor]))
 *   <LogRow expanded={expanded === key} onToggle={() => toggle(key)} />
 */
export function useExpandedRow(viewKey: string): [string | null, (rowKey: string) => void] {
  const [state, setState] = useState<{ view: string; row: string | null }>({ view: viewKey, row: null })
  // Adjusting state while rendering: another page never shows the row open,
  // not even when coming back to the page it was opened on
  if (state.view !== viewKey) {
    setState({ view: viewKey, row: null })
  }

  const toggle = useCallback(
    (rowKey: string) =>
      setState((prev) => ({ view: viewKey, row: prev.view === viewKey && prev.row === rowKey ? null : rowKey })),
    [viewKey],
  )

  return [state.view === viewKey ? state.row : null, toggle]
}
