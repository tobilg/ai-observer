import { useDroppable } from '@dnd-kit/core'
import { cn } from '@/lib/utils'

interface EmptyCellPlaceholderProps {
  gridRow: number // Visual grid row for CSS positioning
  gridColumn: number
  colSpan?: number
  logicalRow?: number // Logical row for drop data (defaults to gridRow)
  onClick: () => void
  maxColumns?: number
}

export function EmptyCellPlaceholder({
  gridRow,
  gridColumn,
  colSpan = 1,
  logicalRow,
  onClick,
  maxColumns = 4,
}: EmptyCellPlaceholderProps) {
  const dataRow = logicalRow ?? gridRow
  // Clamp colSpan to maxColumns for responsive layout
  const effectiveColSpan = Math.min(colSpan, maxColumns)
  const { setNodeRef, isOver } = useDroppable({
    id: `empty-${dataRow}-${gridColumn}-${effectiveColSpan}`,
    data: { type: 'empty', gridRow: dataRow, gridColumn, colSpan: effectiveColSpan },
  })

  return (
    <button
      type="button"
      ref={setNodeRef}
      onClick={onClick}
      aria-label={`Add a widget in row ${dataRow}, column ${gridColumn}`}
      style={{
        gridColumn: `${gridColumn} / span ${effectiveColSpan}`,
        gridRow: `${gridRow} / span 1`,
      }}
      className={cn(
        'flex items-center justify-center rounded-lg border-2 border-dashed',
        'text-muted-foreground text-sm cursor-pointer transition-colors min-h-[100px]',
        'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring',
        isOver
          ? 'border-primary bg-primary/10'
          : 'border-muted-foreground/30 hover:border-primary/50 hover:bg-accent/30'
      )}
    >
      <span aria-hidden="true">+ Add widget</span>
    </button>
  )
}
