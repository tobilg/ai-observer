import { memo, type ReactNode } from 'react'
import { Link } from 'react-router-dom'
import { ChevronDown, MessageSquare } from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { CopyButton } from '@/components/common/copy-button'
import { otelTracePath, sessionPath, type ListLinkState } from '@/lib/listLinks'
import { cn, formatTimestamp, getSeverityColor } from '@/lib/utils'
import type { LogRecord } from '@/types/logs'

interface LogRowProps {
  log: LogRecord
  // DOM id of the details panel (aria-controls)
  detailsId: string
  expanded: boolean
  onToggle: () => void
  serviceName: string
  linkState: ListLinkState
}

function Field({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="flex min-w-0 flex-wrap items-center gap-x-2">
      <dt className="text-muted-foreground">{label}</dt>
      <dd className="flex min-w-0 items-center gap-1 break-all">{children}</dd>
    </div>
  )
}

function Attributes({ title, attributes }: { title: string; attributes?: Record<string, string> }) {
  const entries = attributes ? Object.entries(attributes) : []
  if (entries.length === 0) {
    return null
  }
  return (
    <div className="text-xs">
      <p className="mb-1 text-muted-foreground">{title}</p>
      <dl className="space-y-1 rounded bg-muted p-2">
        {entries.map(([key, value]) => (
          <div key={key} className="break-all">
            <dt className="inline text-muted-foreground">{key}: </dt>
            <dd className="inline">{value}</dd>
          </div>
        ))}
      </dl>
    </div>
  )
}

/**
 * A log record in the logs list: the summary is a button that shows the
 * details (body, IDs with links to the trace and session, attributes).
 * Below md the summary stacks: time, level and service above the body.
 */
export const LogRow = memo(function LogRow({ log, detailsId, expanded, onToggle, serviceName, linkState }: LogRowProps) {
  const severity = log.severityText || 'UNKNOWN'
  return (
    <li className="border-b last:border-0">
      <div className="flex items-start gap-1">
        <Button
          type="button"
          variant="ghost"
          // No size: the row sizes itself (the variants set a fixed height)
          size={null}
          onClick={onToggle}
          aria-expanded={expanded}
          aria-controls={detailsId}
          className={cn(
            'flex min-w-0 flex-1 shrink flex-col items-stretch justify-start gap-1 rounded px-2 py-2 text-left font-normal whitespace-normal',
            'hover:bg-accent/50 md:flex-row md:items-start md:gap-3',
            expanded && 'bg-accent/50',
          )}
        >
          <span className="flex min-w-0 items-center gap-2 md:contents">
            <span className="shrink-0 text-xs text-muted-foreground md:w-40 md:pt-0.5">{formatTimestamp(log.timestamp)}</span>
            <Badge className={cn('shrink-0', getSeverityColor(log.severityText || ''))}>{severity}</Badge>
            <span className="min-w-0 truncate text-muted-foreground md:w-28 md:shrink-0">{serviceName}</span>
          </span>
          <span className="min-w-0 flex-1 truncate">{log.body}</span>
          <ChevronDown
            aria-hidden="true"
            className={cn('hidden h-4 w-4 shrink-0 self-center text-muted-foreground transition-transform md:block', expanded && 'rotate-180')}
          />
        </Button>
        {log.sessionId && (
          <Button asChild variant="ghost" size="icon-sm" className="mt-1 shrink-0 text-muted-foreground">
            <Link to={sessionPath(log.sessionId)} state={linkState} aria-label="Open session" title="Open session">
              <MessageSquare className="h-3.5 w-3.5" />
            </Link>
          </Button>
        )}
      </div>

      {expanded && (
        <div id={detailsId} className="space-y-3 px-2 pb-4 md:pl-44">
          <div className="relative rounded bg-muted p-3 pr-10">
            <p className="whitespace-pre-wrap break-all">{log.body}</p>
            {log.body && <CopyButton value={log.body} label="Copy log body" className="absolute right-1 top-1" />}
          </div>

          <dl className="space-y-1 text-xs">
            {log.traceId && (
              <Field label="Trace ID">
                <Link to={otelTracePath(log.traceId)} state={linkState} className="font-mono underline-offset-4 hover:underline">
                  {log.traceId}
                </Link>
                <CopyButton value={log.traceId} label="Copy trace ID" />
              </Field>
            )}
            {log.spanId && (
              <Field label="Span ID">
                <span className="font-mono">{log.spanId}</span>
                <CopyButton value={log.spanId} label="Copy span ID" />
              </Field>
            )}
            {log.sessionId && (
              <Field label="Session">
                <Link to={sessionPath(log.sessionId)} state={linkState} className="font-mono underline-offset-4 hover:underline">
                  {log.sessionId}
                </Link>
                <CopyButton value={log.sessionId} label="Copy session ID" />
              </Field>
            )}
            {log.scopeName && (
              <Field label="Scope">
                <span className="font-mono">
                  {log.scopeName}
                  {log.scopeVersion && ` ${log.scopeVersion}`}
                </span>
              </Field>
            )}
          </dl>

          <Attributes title="Attributes" attributes={log.logAttributes} />
          <Attributes title="Resource attributes" attributes={log.resourceAttributes} />
        </div>
      )}
    </li>
  )
})
