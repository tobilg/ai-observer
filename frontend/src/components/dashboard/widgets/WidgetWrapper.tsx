import { useSortable } from '@dnd-kit/sortable'
import { GripVertical, X } from 'lucide-react'
import { cn } from '@/lib/utils'
import type { DashboardWidget } from '@/types/dashboard'

interface WidgetWrapperProps {
  widget: DashboardWidget
  isEditMode: boolean
  onRemove: (widgetId: string) => void
  children: React.ReactNode
  gridRowOverride?: number
  gridRowSpanOverride?: number
  maxColumns?: number
}

export function WidgetWrapper({
  widget,
  isEditMode,
  onRemove,
  children,
  gridRowOverride,
  gridRowSpanOverride,
  maxColumns = 4,
}: WidgetWrapperProps) {
  const {
    attributes,
    listeners,
    setNodeRef,
    transform,
    transition,
    isDragging,
  } = useSortable({
    id: widget.id,
    disabled: !isEditMode,
    data: {
      type: 'widget',
      widget: {
        id: widget.id,
        colSpan: widget.colSpan,
        rowSpan: widget.rowSpan,
        gridColumn: widget.gridColumn,
        gridRow: widget.gridRow,
      },
    },
  })

  const actualGridRow = gridRowOverride ?? widget.gridRow
  const actualRowSpan = gridRowSpanOverride ?? widget.rowSpan
  // Clamp colSpan to maxColumns for responsive layout
  const effectiveColSpan = Math.min(widget.colSpan, maxColumns)

  // On mobile (maxColumns < 4), let widgets flow naturally without explicit positioning
  const isMobileLayout = maxColumns < 4

  const style = {
    // On desktop, use explicit column positioning; on mobile, let widgets flow naturally
    gridColumn: isMobileLayout ? `span ${effectiveColSpan}` : `${widget.gridColumn} / span ${effectiveColSpan}`,
    // Only set explicit row position on desktop; let mobile flow naturally
    gridRow: isMobileLayout ? `span ${widget.rowSpan}` : `${actualGridRow} / span ${actualRowSpan}`,
    transform: transform ? `translate3d(${transform.x}px, ${transform.y}px, 0)` : undefined,
    transition,
  }

  return (
    <div
      ref={setNodeRef}
      style={style}
      className={cn(
        'relative rounded-lg border bg-card text-card-foreground shadow-sm',
        isDragging && 'z-50 opacity-90 shadow-lg',
        isEditMode && 'outline outline-2 outline-offset-[-2px] outline-primary/30'
      )}
    >
      {isEditMode && (
        <div className="flex items-center justify-between px-2 pt-1 -mb-2">
          <button
            {...attributes}
            {...listeners}
            className="cursor-grab rounded p-0.5 text-muted-foreground hover:bg-accent hover:text-accent-foreground active:cursor-grabbing"
          >
            <GripVertical className="h-4 w-4" />
          </button>
          <button
            onClick={() => onRemove(widget.id)}
            className="rounded p-0.5 text-muted-foreground hover:bg-destructive hover:text-destructive-foreground"
          >
            <X className="h-4 w-4" />
          </button>
        </div>
      )}
      {children}
    </div>
  )
}
