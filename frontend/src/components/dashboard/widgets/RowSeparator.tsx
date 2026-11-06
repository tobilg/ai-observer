import { Plus } from 'lucide-react'
import { cn } from '@/lib/utils'

interface RowSeparatorProps {
  gridRow: number // The actual grid row for this separator
  onClick: () => void
}

export function RowSeparator({ gridRow, onClick }: RowSeparatorProps) {
  return (
    <div
      onClick={onClick}
      style={{
        gridColumn: '1 / -1',
        gridRow,
      }}
      className={cn(
        'relative flex items-center justify-center cursor-pointer group'
      )}
    >
      {/* The horizontal dashed line - always visible */}
      <div
        className={cn(
          'absolute inset-x-4 top-1/2 border-t border-dashed',
          'border-border group-hover:border-primary/50 transition-colors'
        )}
      />

      {/* The centered button - always visible but subtle */}
      <button
        type="button"
        aria-label="Add row"
        className={cn(
          'relative z-10 flex items-center gap-1 px-2.5 py-1 rounded-full',
          'bg-muted border border-dashed border-border text-xs text-muted-foreground',
          'group-hover:bg-background group-hover:border-primary/50 group-hover:text-primary',
          'group-hover:border-solid transition-all'
        )}
      >
        <Plus className="h-3 w-3" />
        <span className="hidden group-hover:inline">Add row</span>
      </button>
    </div>
  )
}
