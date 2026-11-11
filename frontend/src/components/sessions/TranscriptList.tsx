import { useEffect, useRef, useState, type ReactNode } from 'react'
import { useWindowVirtualizer } from '@tanstack/react-virtual'
import { cn } from '@/lib/utils'

interface TranscriptListProps {
  count: number
  // A stable key of each item: its position in the whole transcript, so
  // measured heights and React state stay with the message when filters
  // change the list
  itemKey: (index: number) => number
  renderItem: (index: number) => ReactNode
  // Height of an item before it was measured
  estimateSize: number
  // Space below each item (part of its measured height)
  gapClassName?: string
  // Scrolls to the item at revealIndex whenever revealKey changes (search
  // navigation); an item not in the list yet is scrolled to once it is
  revealIndex?: number
  revealKey?: string
  className?: string
}

// How many items are rendered beyond the viewport, above and below
const OVERSCAN = 6

/**
 * The messages of a transcript, virtualized in the window's scroll (the
 * page scrolls, not a box): only the items near the viewport are rendered,
 * and their heights are measured as they render.
 */
export function TranscriptList({
  count,
  itemKey,
  renderItem,
  estimateSize,
  gapClassName = 'pb-4',
  revealIndex,
  revealKey,
  className,
}: TranscriptListProps) {
  // The list's node, to place it in the page: the virtualizer needs the
  // distance from the top of the document (read on each render, as the
  // header above the list changes height)
  const [node, setNode] = useState<HTMLDivElement | null>(null)
  const scrollMargin = node ? node.getBoundingClientRect().top + window.scrollY : 0

  const virtualizer = useWindowVirtualizer({
    count,
    estimateSize: () => estimateSize,
    overscan: OVERSCAN,
    scrollMargin,
    getItemKey: itemKey,
  })

  const revealed = useRef<string | undefined>(undefined)
  useEffect(() => {
    if (revealKey === undefined || revealIndex === undefined || revealIndex < 0 || revealIndex >= count) {
      return
    }
    if (revealed.current === revealKey) {
      return
    }
    revealed.current = revealKey
    virtualizer.scrollToIndex(revealIndex, { align: 'center' })
  }, [revealKey, revealIndex, count, virtualizer])

  const items = virtualizer.getVirtualItems()

  return (
    <div ref={setNode} className={cn('w-full min-w-0', className)}>
      <div className="relative w-full" style={{ height: `${virtualizer.getTotalSize()}px` }}>
        <div
          className="absolute left-0 top-0 w-full"
          style={{ transform: `translateY(${(items[0]?.start ?? 0) - virtualizer.options.scrollMargin}px)` }}
        >
          {items.map((item) => (
            <div key={item.key} data-index={item.index} ref={virtualizer.measureElement} className={gapClassName}>
              {renderItem(item.index)}
            </div>
          ))}
        </div>
      </div>
    </div>
  )
}
