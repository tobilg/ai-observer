import type { ReactNode } from 'react'
import { useDocumentTitle } from '@/hooks/useDocumentTitle'
import { cn } from '@/lib/utils'

interface PageHeaderProps {
  title: string
  description?: ReactNode
  // Buttons on the right, e.g. the "N new traces" button
  actions?: ReactNode
  // The tab title, when it should differ from title (e.g. a session ID)
  documentTitle?: string
  className?: string
}

// A page's heading; also sets the tab title ("<title> · AI Observer")
export function PageHeader({ title, description, actions, documentTitle, className }: PageHeaderProps) {
  useDocumentTitle(documentTitle ?? title)
  return (
    <div className={cn('flex flex-wrap items-start justify-between gap-4', className)}>
      <div className="min-w-0">
        <h1 className="text-3xl font-bold tracking-tight">{title}</h1>
        {description && <p className="text-muted-foreground">{description}</p>}
      </div>
      {actions && <div className="flex shrink-0 items-center gap-2">{actions}</div>}
    </div>
  )
}
