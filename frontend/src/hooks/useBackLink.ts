import { useLocation } from 'react-router-dom'
import { backLinkFor } from '@/lib/backLink'
import type { ListLinkState } from '@/lib/listLinks'

/**
 * Where a detail page's Back button leads: the page that opened it, with
 * its filters, when it passed a ListLinkState (useListLinkState),
 * otherwise fallback (the list page)
 */
export function useBackLink(fallback: string): { to: string; label: string } {
  // History state comes from anywhere: backLinkFor checks it is an app path
  const state = useLocation().state as Partial<ListLinkState> | null
  return backLinkFor(state?.from, fallback)
}
