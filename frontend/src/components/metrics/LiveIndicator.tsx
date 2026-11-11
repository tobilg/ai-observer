import { Badge } from '@/components/ui/badge'
import { cn } from '@/lib/utils'

// Whether the shown data follows new telemetry: relative time windows do,
// fixed (absolute) ranges show historical data and are not refreshed
export function LiveIndicator({ live, className }: { live: boolean; className?: string }) {
  return (
    <Badge
      variant="outline"
      className={cn('gap-1.5 font-normal text-muted-foreground', className)}
      title={live ? 'Refreshed when new telemetry arrives' : 'A fixed date range is not refreshed'}
    >
      <span
        aria-hidden="true"
        className={cn('h-2 w-2 rounded-full', live ? 'bg-green-500 motion-safe:animate-pulse' : 'bg-muted-foreground/50')}
      />
      {live ? 'Live' : 'Paused (fixed range)'}
    </Badge>
  )
}
