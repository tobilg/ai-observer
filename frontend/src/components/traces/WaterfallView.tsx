import { useState, useMemo, memo } from 'react'
import { formatDuration, getSpanKindIcon, formatEventTime, cn } from '@/lib/utils'
import type { TraceOverview, Span } from '@/types/traces'
import { ChevronRight, ChevronDown } from 'lucide-react'

interface SpanNode {
  span: Span
  children: SpanNode[]
  depth: number
  // Visual extent includes children's times
  visualStart: number // ms timestamp
  visualEnd: number   // ms timestamp
}

function buildSpanTree(spans: Span[]): SpanNode[] {
  // Create a map of spanId -> span for quick lookup
  const spanMap = new Map<string, Span>()
  for (const span of spans) {
    spanMap.set(span.spanId, span)
  }

  // Create nodes for each span
  const nodeMap = new Map<string, SpanNode>()
  for (const span of spans) {
    const startMs = new Date(span.timestamp).getTime()
    const endMs = startMs + span.duration / 1_000_000
    nodeMap.set(span.spanId, {
      span,
      children: [],
      depth: 0,
      visualStart: startMs,
      visualEnd: endMs,
    })
  }

  // Build parent-child relationships
  const roots: SpanNode[] = []
  for (const span of spans) {
    const node = nodeMap.get(span.spanId)!
    if (span.parentSpanId && nodeMap.has(span.parentSpanId)) {
      const parentNode = nodeMap.get(span.parentSpanId)!
      parentNode.children.push(node)
    } else {
      // No parent in this trace, treat as root
      roots.push(node)
    }
  }

  // Sort children by timestamp and calculate depths
  // Each span keeps its own start/end times (visualStart/visualEnd set during node creation)
  function processNodes(nodes: SpanNode[], depth: number) {
    nodes.sort((a, b) => new Date(a.span.timestamp).getTime() - new Date(b.span.timestamp).getTime())
    for (const node of nodes) {
      node.depth = depth
      processNodes(node.children, depth + 1)
    }
  }
  processNodes(roots, 0)

  return roots
}

function flattenTree(nodes: SpanNode[], collapsedSpans: Set<string>): SpanNode[] {
  const result: SpanNode[] = []
  function traverse(nodes: SpanNode[]) {
    for (const node of nodes) {
      result.push(node)
      if (!collapsedSpans.has(node.span.spanId)) {
        traverse(node.children)
      }
    }
  }
  traverse(nodes)
  return result
}

interface WaterfallViewProps {
  spans: Span[]
  trace?: TraceOverview
  /** When true, all span details are expanded by default */
  allExpanded?: boolean
}

// Memoized WaterfallView component to avoid re-renders when parent updates
export const WaterfallView = memo(function WaterfallView({ spans, trace, allExpanded = false }: WaterfallViewProps) {
  // Build tree first to get all span IDs for initial expanded state
  const tree = useMemo(() => buildSpanTree(spans), [spans])

  // Get all span IDs for initial expanded state when allExpanded is true
  const allSpanIds = useMemo(() => {
    if (!allExpanded) return new Set<string>()
    const ids = new Set<string>()
    function collectIds(nodes: SpanNode[]) {
      for (const node of nodes) {
        ids.add(node.span.spanId)
        collectIds(node.children)
      }
    }
    collectIds(tree)
    return ids
  }, [tree, allExpanded])

  const [expandedSpans, setExpandedSpans] = useState<Set<string>>(allSpanIds)
  const [collapsedSpans, setCollapsedSpans] = useState<Set<string>>(new Set())

  const toggleSpanDetails = (spanId: string) => {
    setExpandedSpans((prev) => {
      const next = new Set(prev)
      if (next.has(spanId)) {
        next.delete(spanId)
      } else {
        next.add(spanId)
      }
      return next
    })
  }

  const toggleSpanChildren = (spanId: string) => {
    setCollapsedSpans((prev) => {
      const next = new Set(prev)
      if (next.has(spanId)) {
        next.delete(spanId)
      } else {
        next.add(spanId)
      }
      return next
    })
  }

  const flatNodes = useMemo(() => flattenTree(tree, collapsedSpans), [tree, collapsedSpans])

  // Calculate max depth from full tree (not flatNodes) for consistent column width
  const maxDepth = useMemo(() => {
    function getMaxDepth(nodes: SpanNode[]): number {
      let max = 0
      for (const node of nodes) {
        if (node.depth > max) max = node.depth
        const childMax = getMaxDepth(node.children)
        if (childMax > max) max = childMax
      }
      return max
    }
    return getMaxDepth(tree)
  }, [tree])

  // Calculate timing for duration bars from all spans
  const { traceStart, totalDuration } = useMemo(() => {
    if (spans.length === 0) return { traceStart: 0, traceEnd: 0, totalDuration: 0 }

    let minStart = Number.MAX_SAFE_INTEGER
    let maxEnd = Number.MIN_SAFE_INTEGER

    for (const span of spans) {
      const start = new Date(span.timestamp).getTime()
      const end = start + span.duration / 1_000_000
      if (start < minStart) minStart = start
      if (end > maxEnd) maxEnd = end
    }

    return {
      traceStart: minStart,
      traceEnd: maxEnd,
      totalDuration: maxEnd - minStart
    }
  }, [spans])

  // Fixed width for the name column based on max depth
  // Base: 120px for name + 16px per depth level + 24px for icons
  const nameColumnWidth = 120 + (maxDepth * 16) + 24

  return (
    <div className="space-y-1">
      {/* Top-level trace bar */}
      {trace && (
        <div className="flex items-center gap-2 text-xs py-1 border-b mb-2 pb-2">
          {/* Name column - same width as span rows */}
          <div
            className="flex items-center gap-1 shrink-0 overflow-hidden"
            style={{ width: `${nameColumnWidth}px` }}
          >
            <span className="w-4 flex items-center justify-center text-muted-foreground" aria-hidden="true">
              <svg className="h-3 w-3" viewBox="0 0 12 12" fill="currentColor">
                <circle cx="6" cy="6" r="5" />
              </svg>
            </span>
            <span className="truncate font-semibold text-foreground" title={trace.rootSpan}>
              {trace.rootSpan}
            </span>
          </div>
          {/* Bar column */}
          <div className="flex-1 h-5 bg-muted rounded relative">
            <div
              className={cn(
                'absolute h-full rounded',
                trace.status === 'ERROR' ? 'bg-error' : 'bg-primary'
              )}
              style={{ left: '0%', width: '100%' }}
            />
          </div>
          {/* Duration column */}
          <div className="w-20 text-right font-semibold shrink-0">
            {formatDuration(trace.duration)}
          </div>
        </div>
      )}
      {flatNodes.map((node) => {
        const span = node.span
        // Calculate bar position and width using visual extents
        const left = totalDuration > 0 ? ((node.visualStart - traceStart) / totalDuration) * 100 : 0
        const width = totalDuration > 0 ? ((node.visualEnd - node.visualStart) / totalDuration) * 100 : 100
        const isExpanded = expandedSpans.has(span.spanId)
        const isCollapsed = collapsedSpans.has(span.spanId)
        const hasChildren = node.children.length > 0
        const hasDetails = span.spanAttributes && Object.keys(span.spanAttributes).length > 0 ||
                          span.events && span.events.length > 0 ||
                          span.statusMessage

        return (
          <div key={span.spanId} className="group">
            <div
              className={cn(
                'flex items-center gap-2 text-xs cursor-pointer rounded py-0.5',
                'hover:bg-accent transition-colors',
                isExpanded && 'bg-accent'
              )}
              onClick={() => toggleSpanDetails(span.spanId)}
              role="button"
              tabIndex={0}
              aria-expanded={hasDetails ? isExpanded : undefined}
              aria-controls={hasDetails ? `span-details-${span.spanId}` : undefined}
              aria-label={`${span.spanName}, ${formatDuration(span.duration)}, ${span.statusCode || 'OK'}`}
              onKeyDown={(e) => {
                if (e.key === 'Enter' || e.key === ' ') {
                  e.preventDefault()
                  toggleSpanDetails(span.spanId)
                }
              }}
            >
              {/* Name column with indentation - fixed width */}
              <div
                className="flex items-center gap-1 shrink-0 overflow-hidden"
                style={{
                  width: `${nameColumnWidth}px`,
                  paddingLeft: `${node.depth * 16}px`
                }}
              >
                {/* Tree expand/collapse indicator */}
                <div
                  className="w-4 shrink-0 text-muted-foreground"
                  aria-hidden="true"
                  onClick={(e) => {
                    if (hasChildren) {
                      e.stopPropagation()
                      toggleSpanChildren(span.spanId)
                    }
                  }}
                >
                  {hasChildren ? (
                    isCollapsed ? <ChevronRight className="h-3 w-3" /> : <ChevronDown className="h-3 w-3" />
                  ) : (
                    <span className="w-3" />
                  )}
                </div>

                {/* SpanKind icon */}
                <span className="w-4 shrink-0 text-center text-muted-foreground" title={span.spanKind || 'Unknown'} aria-hidden="true">
                  {getSpanKindIcon(span.spanKind)}
                </span>

                {/* Span name */}
                <span className="truncate font-medium" title={span.spanName}>{span.spanName}</span>
              </div>

              {/* Duration bar - starts at same position for all rows */}
              <div className="flex-1 h-5 bg-muted rounded relative">
                <div
                  className={cn(
                    'absolute h-full rounded',
                    span.statusCode === 'ERROR' ? 'bg-error' : 'bg-info'
                  )}
                  style={{
                    left: `${left}%`,
                    width: `${Math.max(width, 0.5)}%`,
                  }}
                />
              </div>

              {/* Duration text */}
              <div className="w-20 text-right text-muted-foreground shrink-0">
                {formatDuration(span.duration)}
              </div>
            </div>

            {/* Expanded details */}
            {isExpanded && hasDetails && (
              <SpanDetails span={span} id={`span-details-${span.spanId}`} depth={node.depth} />
            )}
          </div>
        )
      })}
    </div>
  )
})

// Memoized SpanDetails component
export const SpanDetails = memo(function SpanDetails({ span, id, depth = 0 }: { span: Span; id?: string; depth?: number }) {
  const hasAttributes = span.spanAttributes && Object.keys(span.spanAttributes).length > 0
  const hasEvents = span.events && span.events.length > 0

  return (
    <div id={id} className="mt-1 mb-2 p-3 bg-muted/50 rounded-lg border text-xs space-y-3" style={{ marginLeft: `${depth * 16 + 32}px` }}>
      {/* Status message for errors */}
      {span.statusCode === 'ERROR' && span.statusMessage && (
        <div>
          <div className="font-medium text-error mb-1">Error</div>
          <div className="text-muted-foreground">{span.statusMessage}</div>
        </div>
      )}

      {/* Span attributes */}
      {hasAttributes && (
        <div>
          <div className="font-medium mb-1">Attributes</div>
          <div className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-0.5">
            {Object.entries(span.spanAttributes!).map(([key, value]) => (
              <div key={key} className="contents">
                <span className="text-muted-foreground">{key}</span>
                <span className="font-mono truncate">{value}</span>
              </div>
            ))}
          </div>
        </div>
      )}

      {/* Events */}
      {hasEvents && (
        <div>
          <div className="font-medium mb-1">Events ({span.events!.length})</div>
          <div className="space-y-1">
            {span.events!.map((event, idx) => (
              <div key={idx} className="flex items-start gap-2">
                <span className="text-muted-foreground w-12 shrink-0">
                  +{formatEventTime(event.timestamp, span.timestamp)}
                </span>
                <span className="font-medium">{event.name}</span>
                {event.attributes && Object.keys(event.attributes).length > 0 && (
                  <span className="text-muted-foreground truncate">
                    {Object.entries(event.attributes).map(([k, v]) => `${k}=${v}`).join(', ')}
                  </span>
                )}
              </div>
            ))}
          </div>
        </div>
      )}

      {/* Show message if no details */}
      {!hasAttributes && !hasEvents && !span.statusMessage && (
        <div className="text-muted-foreground">No additional details</div>
      )}
    </div>
  )
})
