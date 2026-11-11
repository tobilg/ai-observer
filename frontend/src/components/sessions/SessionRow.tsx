import { memo } from 'react'
import { Link } from 'react-router-dom'
import { ChevronRight, GitBranch } from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { CopyButton } from '@/components/common/copy-button'
import { HighlightedText } from '@/components/common/highlighted-text'
import { formatCost, formatCount, formatTimeSpan, formatTokenUsage } from '@/lib/format'
import { sessionPath, type ListLinkState } from '@/lib/listLinks'
import { getServiceIcon } from '@/lib/metricMetadata'
import { cn, formatTimestamp } from '@/lib/utils'
import type { Session } from '@/types/sessions'

interface SessionRowProps {
  session: Session
  serviceName: string
  linkState: ListLinkState
  // Terms of the search the row was found with, marked in its snippet
  terms?: readonly string[]
}

/**
 * A session in the sessions list. The title (the first prompt, or the ID)
 * is the link to the transcript and covers the whole row, so the row opens
 * the transcript on click and in a new tab with cmd/ctrl-click; the copy
 * button sits above that link.
 */
export const SessionRow = memo(function SessionRow({ session, serviceName, linkState, terms }: SessionRowProps) {
  const ServiceIcon = getServiceIcon(session.serviceName)
  const title = session.title?.trim()
  const tokens = formatTokenUsage({ input: session.inputTokens, output: session.outputTokens })

  return (
    <li
      className={cn(
        'relative flex flex-col gap-3 rounded-lg border p-4 transition-colors hover:bg-accent/50',
        'focus-within:ring-2 focus-within:ring-ring/50 sm:flex-row sm:items-center sm:gap-4',
      )}
    >
      <ServiceIcon className="hidden h-8 w-8 shrink-0 text-muted-foreground sm:block" aria-hidden="true" />

      <div className="min-w-0 flex-1 space-y-1">
        <div className="flex min-w-0 flex-wrap items-center gap-2">
          <Link
            to={sessionPath(session.sessionId)}
            state={linkState}
            className={cn(
              'min-w-0 truncate font-medium outline-none after:absolute after:inset-0 after:content-[""]',
              !title && 'font-mono text-sm',
            )}
            title={title || session.sessionId}
          >
            {title || session.sessionId}
          </Link>
          {!title && <CopyButton value={session.sessionId} label="Copy session ID" className="relative z-10" />}
          <Badge variant="outline" className="shrink-0">
            {serviceName}
          </Badge>
        </div>

        {title && (
          <div className="flex min-w-0 items-center gap-1 font-mono text-xs text-muted-foreground">
            <span className="truncate">{session.sessionId}</span>
            <CopyButton value={session.sessionId} label="Copy session ID" className="relative z-10" />
          </div>
        )}

        {session.snippet && (
          <p className="line-clamp-2 text-sm break-words text-muted-foreground">
            <HighlightedText text={session.snippet} terms={terms} />
          </p>
        )}

        <div className="flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-muted-foreground">
          <span>{formatTimestamp(session.startTime)}</span>
          <span>Duration: {formatTimeSpan(session.startTime, session.lastTime)}</span>
          {session.model && (
            <span className="max-w-[16rem] truncate" title={session.model}>
              Model: {session.model}
            </span>
          )}
          {session.repository && (
            <span className="flex min-w-0 items-center gap-1" title={session.branch ? `${session.repository} (${session.branch})` : session.repository}>
              <GitBranch className="h-3 w-3 shrink-0" aria-hidden="true" />
              <span className="truncate">
                {session.repository}
                {session.branch && ` · ${session.branch}`}
              </span>
            </span>
          )}
          {tokens && <span>Tokens: {tokens}</span>}
          {session.matches !== undefined && session.matches > 0 && (
            <span>
              {formatCount(session.matches)} {session.matches === 1 ? 'match' : 'matches'}
              {session.matchedAt && <>, latest {formatTimestamp(session.matchedAt)}</>}
            </span>
          )}
        </div>
      </div>

      <dl className="flex shrink-0 gap-6 sm:flex-col sm:gap-1 sm:text-right">
        {/* Term below its value, in reading order for screen readers */}
        <div className="flex flex-col-reverse">
          <dt className="text-xs text-muted-foreground">{session.messageCount === 1 ? 'message' : 'messages'}</dt>
          <dd className="font-medium">{formatCount(session.messageCount)}</dd>
        </div>
        {session.costUsd !== undefined && (
          <div className="flex flex-col-reverse">
            <dt className="text-xs text-muted-foreground">cost</dt>
            <dd className="font-medium">{formatCost(session.costUsd)}</dd>
          </div>
        )}
      </dl>

      <ChevronRight className="hidden h-5 w-5 shrink-0 text-muted-foreground sm:block" aria-hidden="true" />
    </li>
  )
})
