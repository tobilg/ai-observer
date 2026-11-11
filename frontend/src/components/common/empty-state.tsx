import type { ReactNode } from 'react'
import { Inbox, type LucideIcon } from 'lucide-react'
import { cn } from '@/lib/utils'

interface EmptyStateProps {
  // What is missing, e.g. "No traces in this time range"
  title: ReactNode
  // How to get some, e.g. a hint to widen the filters
  description?: ReactNode
  icon?: LucideIcon
  // E.g. a button that clears the filters
  action?: ReactNode
  className?: string
}

// A list or view without data (not an error: QueryError shows those)
export function EmptyState({ title, description, icon: Icon = Inbox, action, className }: EmptyStateProps) {
  return (
    <div className={cn('flex flex-col items-center justify-center gap-2 py-10 text-center', className)}>
      <Icon className="h-8 w-8 text-muted-foreground" aria-hidden="true" />
      <p className="font-medium">{title}</p>
      {description && <div className="max-w-md text-sm text-muted-foreground">{description}</div>}
      {action && <div className="mt-2">{action}</div>}
    </div>
  )
}
