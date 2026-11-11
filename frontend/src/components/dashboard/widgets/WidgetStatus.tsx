import { AlertCircle, RefreshCw } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { cn } from '@/lib/utils'

interface WidgetErrorProps {
  // What went wrong (errorText of the failed query)
  message: string
  onRetry?: () => void
  retrying?: boolean
  className?: string
}

// A widget's data could not be loaded: a short message with a Retry button,
// in the space the data would take
export function WidgetError({ message, onRetry, retrying = false, className }: WidgetErrorProps) {
  return (
    <div role="alert" className={cn('flex flex-col items-start gap-1 text-sm text-destructive', className)}>
      <span className="flex items-start gap-1.5">
        <AlertCircle className="mt-0.5 h-3.5 w-3.5 shrink-0" aria-hidden="true" />
        <span className="line-clamp-3">{message}</span>
      </span>
      {onRetry && (
        <Button
          type="button"
          variant="ghost"
          size="sm"
          className="h-7 gap-1.5 px-2 text-xs"
          onClick={onRetry}
          disabled={retrying}
        >
          <RefreshCw className={cn('h-3 w-3', retrying && 'animate-spin')} aria-hidden="true" />
          Retry
        </Button>
      )}
    </div>
  )
}

// A widget's first load
export function WidgetLoading({ className }: { className?: string }) {
  return (
    <div role="status" aria-busy="true" aria-label="Loading" className={className}>
      <Skeleton className="h-full min-h-8 w-full" />
    </div>
  )
}
