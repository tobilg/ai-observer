import type { ReactNode } from 'react'
import { AlertCircle, RefreshCw } from 'lucide-react'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { errorText } from '@/lib/errors'
import { cn } from '@/lib/utils'

interface QueryErrorProps {
  // The failed query's error (query.error)
  error: unknown
  // What failed, e.g. "Couldn't load traces"
  title?: ReactNode
  // Shown when the error has no more specific message (errorText)
  fallback?: string
  // Tries again, e.g. () => query.refetch()
  onRetry?: () => void
  // Whether the retry is running (query.isFetching)
  retrying?: boolean
  className?: string
}

/**
 * Inline error of a failed query, in place of the data it would show, so a
 * failure does not look like an empty result:
 *
 *   if (query.isError) return <QueryError error={query.error} title="Couldn't load logs" onRetry={() => query.refetch()} />
 */
export function QueryError({
  error,
  title = 'Something went wrong',
  fallback,
  onRetry,
  retrying = false,
  className,
}: QueryErrorProps) {
  return (
    <Alert variant="destructive" className={cn('my-4', className)}>
      <AlertCircle />
      <AlertTitle>{title}</AlertTitle>
      <AlertDescription>
        <p>{errorText(error, fallback)}</p>
        {onRetry && (
          <Button variant="outline" size="sm" className="mt-2 gap-2" onClick={onRetry} disabled={retrying}>
            <RefreshCw className={cn('h-3 w-3', retrying && 'animate-spin')} />
            Retry
          </Button>
        )}
      </AlertDescription>
    </Alert>
  )
}
