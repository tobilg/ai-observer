import { useSortable } from '@dnd-kit/sortable'
import { GripVertical, Pencil, X } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'
import type { DashboardWidget } from '@/types/dashboard'

interface WidgetWrapperProps {
  widget: DashboardWidget
  isEditMode: boolean
  onRemove: (widgetId: string) => void
  onEdit: (widgetId: string) => void
  children: React.ReactNode
  gridRowOverride?: number
  gridRowSpanOverride?: number
  maxColumns?: number
}

export function WidgetWrapper({
  widget,
  isEditMode,
  onRemove,
  onEdit,
  children,
  gridRowOverride,
  gridRowSpanOverride,
  maxColumns = 4,
}: WidgetWrapperProps) {
  const {
    attributes,
    listeners,
    setNodeRef,
    setActivatorNodeRef,
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
        'relative min-w-0 rounded-lg border bg-card text-card-foreground shadow-sm',
        isDragging && 'z-50 opacity-90 shadow-lg',
        isEditMode && 'outline outline-2 outline-offset-[-2px] outline-primary/30'
      )}
    >
      {isEditMode && (
        <div className="flex items-center justify-between px-1 pt-1 -mb-2">
          <Button
            ref={setActivatorNodeRef}
            type="button"
            variant="ghost"
            size="icon-sm"
            {...attributes}
            {...listeners}
            aria-label={`Move ${widget.title}`}
            className="size-7 cursor-grab text-muted-foreground active:cursor-grabbing"
          >
            <GripVertical className="h-4 w-4" />
          </Button>
          <div className="flex items-center gap-0.5">
            <Button
              type="button"
              variant="ghost"
              size="icon-sm"
              onClick={() => onEdit(widget.id)}
              aria-label={`Edit ${widget.title}`}
              className="size-7 text-muted-foreground"
            >
              <Pencil className="h-4 w-4" />
            </Button>
            <Button
              type="button"
              variant="ghost"
              size="icon-sm"
              onClick={() => onRemove(widget.id)}
              aria-label={`Remove ${widget.title}`}
              className="size-7 text-muted-foreground hover:bg-destructive hover:text-white"
            >
              <X className="h-4 w-4" />
            </Button>
          </div>
        </div>
      )}
      {children}
    </div>
  )
}
