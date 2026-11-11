import type { ReactNode } from 'react'
import { Skeleton } from '@/components/ui/skeleton'
import { cn } from '@/lib/utils'

interface ListSkeletonProps {
  // Placeholder rows
  rows?: number
  // Height of a row (Tailwind class)
  rowClassName?: string
  className?: string
  label?: string
}

/**
 * Placeholder of a list's first load (query.isPending). Later loads keep the
 * shown rows: wrap them in Refreshing instead of showing this again.
 */
export function ListSkeleton({ rows = 5, rowClassName = 'h-14', className, label = 'Loading' }: ListSkeletonProps) {
  return (
    <div role="status" aria-busy="true" aria-label={label} className={cn('space-y-2', className)}>
      {Array.from({ length: rows }, (_, index) => (
        <Skeleton key={index} className={cn('w-full', rowClassName)} />
      ))}
    </div>
  )
}

interface RefreshingProps {
  // Whether the shown data is being replaced: query.isPlaceholderData (a
  // new page or filter loads, the previous result is shown) or, for live
  // refreshes, query.isFetching
  active: boolean
  children: ReactNode
  className?: string
}

/**
 * The pattern for data that is reloading while shown: keep the rows, dim
 * them a little and mark them busy, instead of blanking the list:
 *
 *   {query.isPending ? <ListSkeleton /> : (
 *     <Refreshing active={query.isPlaceholderData}>…rows…</Refreshing>
 *   )}
 */
export function Refreshing({ active, children, className }: RefreshingProps) {
  return (
    <div aria-busy={active} className={cn('transition-opacity duration-200', active && 'opacity-60', className)}>
      {children}
    </div>
  )
}
