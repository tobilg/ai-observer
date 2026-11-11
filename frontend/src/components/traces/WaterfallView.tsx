import { useState, useMemo, useCallback, useDeferredValue, memo, type ReactNode } from 'react'
import { ChevronRight, ChevronDown, Search } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { CopyButton } from '@/components/common/copy-button'
import { HighlightedText } from '@/components/common/highlighted-text'
import { formatCount, formatDurationNanos } from '@/lib/format'
import { formatTimestamp, getSpanKindIcon, cn } from '@/lib/utils'
import type { TraceOverview, Span } from '@/types/traces'
import { AttributeList } from './AttributeList'
import {
  buildSpanTree,
  filterSpanTree,
  flattenTree,
  isErrorSpan,
  maxTreeDepth,
  parentSpanIds,
  spanSearchText,
  timeAxisTicks,
  traceExtent,
  type SpanNode,
} from './waterfall'

// This component is also part of the MCP Apps view (src/mcp-app), which has
// no router and no QueryClient: links to app pages come in as render props.

/** Renders a link to a session; without it, session IDs are plain text */
export type RenderSessionLink = (sessionId: string) => ReactNode

interface WaterfallViewProps {
  spans: Span[]
  trace?: TraceOverview
  /** When true, all span details are expanded by default */
  allExpanded?: boolean
  /** Shows a span filter, "Errors only" and expand/collapse all above the waterfall */
  filterable?: boolean
  renderSessionLink?: RenderSessionLink
}

// Indentation per tree level, and the width of the collapse button
const INDENT_PX = 16
const TOGGLE_PX = 16

const NO_TERMS: readonly string[] = []

// The searchable text of each span, built when a filter first needs it
// (spans are immutable query data; loading more pages keeps them)
const searchTexts = new WeakMap<Span, string>()

function cachedSearchText(span: Span): string {
  let text = searchTexts.get(span)
  if (text === undefined) {
    text = spanSearchText(span)
    searchTexts.set(span, text)
  }
  return text
}

/** "+12.34ms": a time since the trace started */
function formatOffset(ms: number): string {
  return `+${formatDurationNanos(Math.max(0, ms) * 1_000_000)}`
}

// Memoized, so re-renders of the page do not rebuild the waterfall
export const WaterfallView = memo(function WaterfallView({
  spans,
  trace,
  allExpanded = false,
  filterable = false,
  renderSessionLink,
}: WaterfallViewProps) {
  const tree = useMemo(() => buildSpanTree(spans), [spans])

  // Span details expanded at first: all of them with allExpanded
  const [expandedSpans, setExpandedSpans] = useState<Set<string>>(() =>
    allExpanded ? new Set(spans.map((s) => s.spanId)) : new Set(),
  )
  const [collapsedSpans, setCollapsedSpans] = useState<Set<string>>(() => new Set())

  const [query, setQuery] = useState('')
  const [errorsOnly, setErrorsOnly] = useState(false)
  // Filtering thousands of spans must not delay typing
  const deferredQuery = useDeferredValue(query)

  const toggleSpanDetails = useCallback((spanId: string) => {
    setExpandedSpans((prev) => {
      const next = new Set(prev)
      if (next.has(spanId)) {
        next.delete(spanId)
      } else {
        next.add(spanId)
      }
      return next
    })
  }, [])

  const toggleSpanChildren = useCallback((spanId: string) => {
    setCollapsedSpans((prev) => {
      const next = new Set(prev)
      if (next.has(spanId)) {
        next.delete(spanId)
      } else {
        next.add(spanId)
      }
      return next
    })
  }, [])

  const filter = useMemo(
    () => (filterable ? filterSpanTree(tree, { query: deferredQuery, errorsOnly }, cachedSearchText) : null),
    [filterable, tree, deferredQuery, errorsOnly],
  )
  const terms = useMemo(() => (filter && deferredQuery.trim() ? [deferredQuery.trim()] : NO_TERMS), [filter, deferredQuery])

  const flatNodes = useMemo(() => flattenTree(tree, collapsedSpans, filter?.visible), [tree, collapsedSpans, filter])
  const parents = useMemo(() => parentSpanIds(tree), [tree])
  // From the whole tree (not the shown rows), so the columns do not move
  const maxDepth = useMemo(() => maxTreeDepth(tree), [tree])
  const { start: traceStart, duration: totalDuration } = useMemo(() => traceExtent(spans, trace), [spans, trace])
  const ticks = useMemo(() => timeAxisTicks(totalDuration), [totalDuration])

  // Name column: 120px for the name, the indentation and two icons
  const nameColumnWidth = 120 + maxDepth * INDENT_PX + 24
  const notLoaded = trace ? Math.max(0, trace.spanCount - spans.length) : 0

  return (
    <div className="space-y-1">
      {filterable && (
        <div className="mb-3 space-y-2">
          <div className="flex flex-wrap items-center gap-2">
            <div className="relative min-w-48 flex-1">
              <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" aria-hidden="true" />
              <Input
                type="search"
                value={query}
                onChange={(e) => setQuery(e.target.value)}
                placeholder="Filter spans by name, ID or attribute"
                aria-label="Filter spans"
                className="pl-9"
              />
            </div>
            <Button
              type="button"
              variant={errorsOnly ? 'secondary' : 'outline'}
              aria-pressed={errorsOnly}
              onClick={() => setErrorsOnly((e) => !e)}
            >
              Errors only
            </Button>
            <Button
              type="button"
              variant="outline"
              onClick={() => setCollapsedSpans(new Set())}
              disabled={filter !== null || collapsedSpans.size === 0}
            >
              Expand all
            </Button>
            <Button
              type="button"
              variant="outline"
              onClick={() => setCollapsedSpans(new Set(parents))}
              disabled={filter !== null || parents.size === 0 || collapsedSpans.size === parents.size}
            >
              Collapse all
            </Button>
          </div>
          {filter && (
            <p className="text-sm text-muted-foreground" role="status">
              {filter.matches.size === 0
                ? `No loaded span matches`
                : `${formatCount(filter.matches.size)} of ${formatCount(spans.length)} loaded spans match`}
              {notLoaded > 0 && ` · ${formatCount(notLoaded)} more spans are not loaded yet`}
            </p>
          )}
        </div>
      )}

      {/* Time axis: offsets from the start of the trace */}
      <div className="flex items-end gap-2 text-[10px] text-muted-foreground" aria-hidden="true" data-testid="time-axis">
        <div className="shrink-0" style={{ width: `${nameColumnWidth}px` }} />
        <div className="relative h-5 flex-1 border-b">
          {ticks.map((tick, index) => (
            <div key={tick.offsetMs} className="absolute bottom-0 h-1.5 border-l" style={{ left: `${tick.percent}%` }}>
              <span
                className={cn(
                  'absolute bottom-1.5 whitespace-nowrap tabular-nums',
                  index === 0 ? 'left-0' : tick.percent > 90 ? 'right-0' : '-translate-x-1/2',
                )}
              >
                {tick.label}
              </span>
            </div>
          ))}
        </div>
        <div className="w-20 shrink-0" />
      </div>

      {/* Top-level trace bar */}
      {trace && (
        <div className="mb-2 flex items-center gap-2 border-b py-1 pb-2 text-xs">
          <div className="flex shrink-0 items-center gap-1 overflow-hidden" style={{ width: `${nameColumnWidth}px` }}>
            <span className="flex w-4 items-center justify-center text-muted-foreground" aria-hidden="true">
              <svg className="h-3 w-3" viewBox="0 0 12 12" fill="currentColor">
                <circle cx="6" cy="6" r="5" />
              </svg>
            </span>
            <span className="truncate font-semibold text-foreground" title={trace.rootSpan}>
              {trace.rootSpan}
            </span>
          </div>
          <div className="relative h-5 flex-1 rounded bg-muted">
            <div
              className={cn('absolute h-full rounded', trace.status === 'ERROR' ? 'bg-error' : 'bg-primary')}
              style={{ left: '0%', width: '100%' }}
            />
          </div>
          <div className="w-20 shrink-0 text-right font-semibold">{formatDurationNanos(trace.duration)}</div>
        </div>
      )}

      {flatNodes.map((node) => (
        <SpanRow
          key={node.span.spanId}
          node={node}
          left={totalDuration > 0 ? ((node.start - traceStart) / totalDuration) * 100 : 0}
          width={totalDuration > 0 ? ((node.end - node.start) / totalDuration) * 100 : 100}
          offsetMs={node.start - traceStart}
          isExpanded={expandedSpans.has(node.span.spanId)}
          isCollapsed={collapsedSpans.has(node.span.spanId)}
          canCollapse={filter === null && node.children.length > 0}
          match={filter ? (filter.matches.has(node.span.spanId) ? 'match' : 'context') : undefined}
          terms={terms}
          nameColumnWidth={nameColumnWidth}
          onToggleDetails={toggleSpanDetails}
          onToggleChildren={toggleSpanChildren}
          renderSessionLink={renderSessionLink}
        />
      ))}
    </div>
  )
})

interface SpanRowProps {
  node: SpanNode
  left: number
  width: number
  // Start of the span since the start of the trace
  offsetMs: number
  isExpanded: boolean
  isCollapsed: boolean
  // Whether the span's children can be hidden (not while filtering)
  canCollapse: boolean
  // With a filter: whether the span matches, or is shown as a match's ancestor
  match?: 'match' | 'context'
  terms: readonly string[]
  nameColumnWidth: number
  onToggleDetails: (spanId: string) => void
  onToggleChildren: (spanId: string) => void
  renderSessionLink?: RenderSessionLink
}

// One row of the waterfall. Traces have thousands of spans: rows are
// memoized, so toggling one span renders only that row, and the browser
// skips layout and paint of rows outside the viewport (content-visibility).
const SpanRow = memo(function SpanRow({
  node, left, width, offsetMs, isExpanded, isCollapsed, canCollapse, match, terms, nameColumnWidth,
  onToggleDetails, onToggleChildren, renderSessionLink,
}: SpanRowProps) {
  const span = node.span
  const status = span.statusCode || 'OK'
  const duration = formatDurationNanos(span.duration)
  const indent = node.depth * INDENT_PX + TOGGLE_PX
  const detailsId = `span-details-${span.spanId}`

  return (
    <div className="group" style={{ contentVisibility: 'auto', containIntrinsicSize: 'auto 24px' }}>
      <div
        className={cn(
          'flex items-center rounded text-xs transition-colors hover:bg-accent',
          isExpanded && 'bg-accent',
          match === 'match' && 'bg-warning/15',
          match === 'context' && 'opacity-60',
        )}
        data-match={match}
      >
        {/* Indentation and the button that hides or shows the children */}
        <div className="flex shrink-0 justify-end" style={{ width: `${indent}px` }}>
          {canCollapse && (
            <button
              type="button"
              className="flex h-5 w-4 items-center justify-center rounded text-muted-foreground hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
              aria-expanded={!isCollapsed}
              aria-label={`${isCollapsed ? 'Show' : 'Hide'} the ${node.children.length} child spans of ${span.spanName}`}
              onClick={() => onToggleChildren(span.spanId)}
            >
              {isCollapsed ? <ChevronRight className="h-3 w-3" /> : <ChevronDown className="h-3 w-3" />}
            </button>
          )}
        </div>

        {/* Name, bar and duration: shows or hides the span's details */}
        <button
          type="button"
          className="flex min-w-0 flex-1 cursor-pointer items-center gap-2 rounded py-0.5 text-left focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
          aria-expanded={isExpanded}
          aria-controls={isExpanded ? detailsId : undefined}
          aria-label={`${span.spanName}, ${duration}, ${status}`}
          title={`${span.spanName}\nStarts ${formatOffset(offsetMs)} · ${duration}`}
          onClick={() => onToggleDetails(span.spanId)}
        >
          <span className="flex shrink-0 items-center gap-1 overflow-hidden" style={{ width: `${nameColumnWidth - indent}px` }}>
            <span className="w-4 shrink-0 text-center text-muted-foreground" aria-hidden="true">
              {getSpanKindIcon(span.spanKind)}
            </span>
            <span className="truncate font-medium">
              <HighlightedText text={span.spanName} terms={terms} />
            </span>
          </span>

          {/* Duration bar - starts at the same position in all rows */}
          <span className="relative h-5 flex-1 rounded bg-muted">
            <span
              className={cn('absolute h-full rounded', isErrorSpan(span) ? 'bg-error' : 'bg-info')}
              style={{ left: `${left}%`, width: `${Math.max(width, 0.5)}%` }}
            />
          </span>

          <span className="w-20 shrink-0 text-right text-muted-foreground">{duration}</span>
        </button>
      </div>

      {isExpanded && (
        <SpanDetails
          span={span}
          id={detailsId}
          depth={node.depth}
          offsetMs={offsetMs}
          terms={terms}
          renderSessionLink={renderSessionLink}
        />
      )}
    </div>
  )
})

interface SpanDetailsProps {
  span: Span
  id?: string
  depth?: number
  // Start of the span since the start of the trace
  offsetMs?: number
  terms?: readonly string[]
  renderSessionLink?: RenderSessionLink
}

function hasEntries(attributes?: Record<string, string>): attributes is Record<string, string> {
  return attributes !== undefined && Object.keys(attributes).length > 0
}

function IdValue({ value, label }: { value: string; label: string }) {
  return (
    <span className="flex min-w-0 items-center gap-1">
      <span className="break-all font-mono">{value}</span>
      <CopyButton value={value} label={label} className="-my-1 size-6 shrink-0" />
    </span>
  )
}

function Section({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section>
      <h4 className="mb-1 font-medium">{title}</h4>
      {children}
    </section>
  )
}

// The details of a span: its IDs and timing, then its attributes, resource,
// events and links in full
export const SpanDetails = memo(function SpanDetails({
  span, id, depth = 0, offsetMs, terms, renderSessionLink,
}: SpanDetailsProps) {
  const events = span.events ?? []
  const links = span.links ?? []
  const scope = span.scopeName ? `${span.scopeName}${span.scopeVersion ? ` ${span.scopeVersion}` : ''}` : undefined
  const spanStart = new Date(span.timestamp).getTime()

  const overview: Array<[string, ReactNode]> = [
    ['Span ID', <IdValue value={span.spanId} label="Copy span ID" />],
    ...(span.parentSpanId ? [['Parent span ID', <IdValue value={span.parentSpanId} label="Copy parent span ID" />] as [string, ReactNode]] : []),
    ['Trace ID', <IdValue value={span.traceId} label="Copy trace ID" />],
    ['Service', span.serviceName],
    ['Kind', span.spanKind || 'Unspecified'],
    ...(scope ? [['Scope', scope] as [string, ReactNode]] : []),
    ['Start', `${formatTimestamp(span.timestamp)}${offsetMs !== undefined ? ` (${formatOffset(offsetMs)})` : ''}`],
    ['Duration', formatDurationNanos(span.duration)],
    ['Status', span.statusCode || 'Unset'],
    ...(span.sessionId
      ? [['Session', renderSessionLink ? renderSessionLink(span.sessionId) : <IdValue value={span.sessionId} label="Copy session ID" />] as [string, ReactNode]]
      : []),
  ]

  return (
    <div
      id={id}
      className="mb-2 mt-1 min-w-0 space-y-3 rounded-lg border bg-muted/50 p-3 text-xs"
      style={{ marginLeft: `${depth * INDENT_PX + 32}px` }}
    >
      <dl className="grid grid-cols-1 gap-x-4 gap-y-1 sm:grid-cols-[minmax(8rem,16rem)_minmax(0,1fr)]">
        {overview.map(([label, value]) => (
          <div key={label} className="contents">
            <dt className="text-muted-foreground">{label}</dt>
            <dd className="min-w-0">{value}</dd>
          </div>
        ))}
      </dl>

      {span.statusMessage && (
        <Section title={isErrorSpan(span) ? 'Error' : 'Status message'}>
          <p className={cn('whitespace-pre-wrap break-words', isErrorSpan(span) ? 'text-error' : 'text-muted-foreground')}>
            {span.statusMessage}
          </p>
        </Section>
      )}

      {hasEntries(span.spanAttributes) && (
        <Section title="Attributes">
          <AttributeList attributes={span.spanAttributes} terms={terms} />
        </Section>
      )}

      {hasEntries(span.resourceAttributes) && (
        <Section title="Resource">
          <AttributeList attributes={span.resourceAttributes} terms={terms} />
        </Section>
      )}

      {events.length > 0 && (
        <Section title={`Events (${formatCount(events.length)})`}>
          <ol className="space-y-2">
            {events.map((event, index) => (
              <li key={index} className="space-y-1">
                <div className="flex flex-wrap items-baseline gap-2">
                  <span className="tabular-nums text-muted-foreground">
                    {formatOffset(new Date(event.timestamp).getTime() - spanStart)}
                  </span>
                  <span className="font-medium">
                    <HighlightedText text={event.name} terms={terms} />
                  </span>
                  <span className="text-muted-foreground">{formatTimestamp(event.timestamp)}</span>
                </div>
                {hasEntries(event.attributes) && (
                  <div className="pl-4">
                    <AttributeList attributes={event.attributes} terms={terms} />
                  </div>
                )}
              </li>
            ))}
          </ol>
        </Section>
      )}

      {links.length > 0 && (
        <Section title={`Links (${formatCount(links.length)})`}>
          <ol className="space-y-2">
            {links.map((link, index) => (
              <li key={index} className="space-y-1">
                <dl className="grid grid-cols-1 gap-x-4 gap-y-1 sm:grid-cols-[minmax(8rem,16rem)_minmax(0,1fr)]">
                  <dt className="text-muted-foreground">Trace ID</dt>
                  <dd className="min-w-0"><IdValue value={link.traceId} label="Copy linked trace ID" /></dd>
                  <dt className="text-muted-foreground">Span ID</dt>
                  <dd className="min-w-0"><IdValue value={link.spanId} label="Copy linked span ID" /></dd>
                  {link.traceState && (
                    <>
                      <dt className="text-muted-foreground">Trace state</dt>
                      <dd className="min-w-0 break-all font-mono">{link.traceState}</dd>
                    </>
                  )}
                </dl>
                {hasEntries(link.attributes) && (
                  <div className="pl-4">
                    <AttributeList attributes={link.attributes} terms={terms} />
                  </div>
                )}
              </li>
            ))}
          </ol>
        </Section>
      )}
    </div>
  )
})
