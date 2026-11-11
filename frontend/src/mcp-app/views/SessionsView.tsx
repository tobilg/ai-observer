import { useState } from 'react'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { formatCost } from '@/lib/format'
import { formatRange, sessionCounts, sessionPlace, sessionsCost, toolDisplayName } from '../data'
import type { CallTool, SessionsResult } from '../types'
import { ErrorText, Notice, ViewHeader } from './parts'

function formatTime(value: string): string {
  return new Date(value).toLocaleString([], { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })
}

// list_sessions: sessions per tool and the session table; more pages are
// read with the same arguments and the next cursor
export function SessionsView({ data, args, callTool }: { data: SessionsResult; args: Record<string, unknown>; callTool: CallTool }) {
  const [page, setPage] = useState(data)
  const [pending, setPending] = useState(false)
  const [error, setError] = useState<string>()

  const loadMore = async () => {
    setPending(true)
    setError(undefined)
    try {
      const next = (await callTool('list_sessions', { ...args, from: page.from, to: page.to, cursor: page.next_cursor })) as SessionsResult
      setPage({ ...next, sessions: [...page.sessions, ...next.sessions] })
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setPending(false)
    }
  }

  const cost = sessionsCost(page.sessions)

  return (
    <div className="space-y-4">
      <ViewHeader
        title="Sessions"
        subtitle={`${page.sessions.length} of ${page.total} · ${formatRange(page.from, page.to)}`}
      >
        {sessionCounts(page.sessions).map(({ tool, count }) => (
          <Badge key={tool} variant="secondary">
            {toolDisplayName(tool)}: {count}
          </Badge>
        ))}
        {cost != null && <Badge variant="outline">Cost: {formatCost(cost)}</Badge>}
      </ViewHeader>

      {page.sessions.length === 0 ? (
        <Notice text="No sessions in this range." />
      ) : (
        <div className="overflow-x-auto">
          <Table className="text-xs">
            <TableHeader>
              <TableRow>
                <TableHead>Session</TableHead>
                <TableHead>Tool</TableHead>
                <TableHead>Model</TableHead>
                <TableHead>Started</TableHead>
                <TableHead>Last activity</TableHead>
                <TableHead className="text-right">Messages</TableHead>
                <TableHead className="text-right">Cost</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {page.sessions.map((s) => (
                <TableRow key={`${s.tool}/${s.session_id}`}>
                  <TableCell className="max-w-72">
                    <div className="truncate" title={s.title}>
                      {s.title || <span className="text-muted-foreground">Untitled</span>}
                    </div>
                    <div className="truncate font-mono text-muted-foreground" title={s.session_id}>
                      {sessionPlace(s) ?? s.session_id}
                    </div>
                  </TableCell>
                  <TableCell className="whitespace-nowrap">{toolDisplayName(s.tool)}</TableCell>
                  <TableCell className="max-w-48 truncate" title={s.model}>
                    {s.model || '—'}
                  </TableCell>
                  <TableCell className="whitespace-nowrap">{formatTime(s.start_time)}</TableCell>
                  <TableCell className="whitespace-nowrap">{formatTime(s.last_time)}</TableCell>
                  <TableCell className="text-right tabular-nums">{s.messages}</TableCell>
                  <TableCell className="text-right tabular-nums">
                    {s.cost_usd != null ? formatCost(s.cost_usd) : '—'}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}

      <ErrorText text={error} />
      {page.next_cursor && (
        <Button variant="outline" size="sm" disabled={pending} onClick={() => void loadMore()}>
          {pending ? 'Loading…' : 'Load more'}
        </Button>
      )}
    </div>
  )
}
