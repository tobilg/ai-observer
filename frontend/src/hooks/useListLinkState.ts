import { useMemo } from 'react'
import { useLocation } from 'react-router-dom'
import type { ListLinkState } from '@/lib/listLinks'

/** The state to pass to links from this list page to detail pages */
export function useListLinkState(): ListLinkState {
  const { pathname, search } = useLocation()
  return useMemo(() => ({ from: pathname + search }), [pathname, search])
}
