// Pure logic of the trace waterfall: the span tree, the span filter, the
// time axis and the display of attribute values.
import type { Span, TraceOverview } from '@/types/traces'

export interface SpanNode {
  span: Span
  children: SpanNode[]
  depth: number
  // Start and end in Unix milliseconds
  start: number
  end: number
}

export function spanStartMs(span: Span): number {
  return new Date(span.timestamp).getTime()
}

/**
 * The spans as a tree: children under their parents, sorted by start time.
 * Spans whose parent is not among spans (not loaded yet, or in another
 * trace) are roots.
 */
export function buildSpanTree(spans: Span[]): SpanNode[] {
  const nodes = new Map<string, SpanNode>()
  for (const span of spans) {
    const start = spanStartMs(span)
    nodes.set(span.spanId, { span, children: [], depth: 0, start, end: start + span.duration / 1_000_000 })
  }

  const roots: SpanNode[] = []
  for (const span of spans) {
    const node = nodes.get(span.spanId)!
    const parent = span.parentSpanId ? nodes.get(span.parentSpanId) : undefined
    if (parent && parent !== node) {
      parent.children.push(node)
    } else {
      roots.push(node)
    }
  }

  // Iterative, so a deep trace cannot overflow the stack
  const stack: Array<[SpanNode[], number]> = [[roots, 0]]
  while (stack.length > 0) {
    const [list, depth] = stack.pop()!
    list.sort((a, b) => a.start - b.start)
    for (const node of list) {
      node.depth = depth
      if (node.children.length > 0) {
        stack.push([node.children, depth + 1])
      }
    }
  }
  return roots
}

/**
 * The rows of the waterfall in tree order. With visible (a filter is
 * active), only those spans, and every one of them: collapsing is ignored
 * so no match is hidden. Otherwise the children of collapsed spans are left
 * out.
 */
export function flattenTree(roots: SpanNode[], collapsed: ReadonlySet<string>, visible?: ReadonlySet<string> | null): SpanNode[] {
  const result: SpanNode[] = []
  const stack = [...roots].reverse()
  while (stack.length > 0) {
    const node = stack.pop()!
    if (visible && !visible.has(node.span.spanId)) {
      continue
    }
    result.push(node)
    if (visible || !collapsed.has(node.span.spanId)) {
      for (let i = node.children.length - 1; i >= 0; i--) {
        stack.push(node.children[i])
      }
    }
  }
  return result
}

export function maxTreeDepth(roots: SpanNode[]): number {
  let max = 0
  const stack = [...roots]
  while (stack.length > 0) {
    const node = stack.pop()!
    if (node.depth > max) max = node.depth
    stack.push(...node.children)
  }
  return max
}

/** The IDs of the spans that have children (to collapse them all) */
export function parentSpanIds(roots: SpanNode[]): Set<string> {
  const ids = new Set<string>()
  const stack = [...roots]
  while (stack.length > 0) {
    const node = stack.pop()!
    if (node.children.length > 0) {
      ids.add(node.span.spanId)
      stack.push(...node.children)
    }
  }
  return ids
}

/**
 * The time the waterfall shows (Unix ms): the extent of the spans, and of
 * the whole trace when only its first spans are loaded
 */
export function traceExtent(spans: Span[], trace?: TraceOverview): { start: number; duration: number } {
  if (spans.length === 0) {
    return { start: 0, duration: 0 }
  }
  let start = Number.MAX_SAFE_INTEGER
  let end = Number.MIN_SAFE_INTEGER
  for (const span of spans) {
    const s = spanStartMs(span)
    const e = s + span.duration / 1_000_000
    if (s < start) start = s
    if (e > end) end = e
  }
  const overviewStart = trace ? new Date(trace.startTime).getTime() : NaN
  if (trace && !Number.isNaN(overviewStart)) {
    start = Math.min(start, overviewStart)
    end = Math.max(end, overviewStart + trace.duration / 1_000_000)
  }
  return { start, duration: end - start }
}

// ============================================================================
// Time axis
// ============================================================================

export interface AxisTick {
  // Time since the trace started
  offsetMs: number
  // Position on the axis (0-100)
  percent: number
  label: string
}

// A step of 1, 2 or 5 times a power of ten, at least rough
function niceStep(rough: number): number {
  const power = Math.pow(10, Math.floor(Math.log10(rough)))
  const fraction = rough / power
  const nice = fraction <= 1 ? 1 : fraction <= 2 ? 2 : fraction <= 5 ? 5 : 10
  return nice * power
}

/**
 * A time since the trace started, as an axis label: the unit follows the
 * axis length (maxMs), the decimals the step between ticks ("0", "250ms",
 * "1.5s", "2m")
 */
export function formatAxisTime(ms: number, stepMs: number, maxMs: number = ms): string {
  if (ms === 0) {
    return '0'
  }
  const units: Array<[string, number]> = [['µs', 0.001], ['ms', 1], ['s', 1000], ['m', 60_000]]
  let [unit, size] = units[0]
  for (const [u, s] of units) {
    if (maxMs >= s) {
      unit = u
      size = s
    }
  }
  const decimals = Math.min(3, Math.max(0, -Math.floor(Math.log10(stepMs / size) + 1e-9)))
  return `${Number((ms / size).toFixed(decimals))}${unit}`
}

/** Ticks for an axis of durationMs: 0, then steps of a round size */
export function timeAxisTicks(durationMs: number, maxTicks = 6): AxisTick[] {
  if (!(durationMs > 0) || maxTicks < 2) {
    return [{ offsetMs: 0, percent: 0, label: '0' }]
  }
  const step = niceStep(durationMs / (maxTicks - 1))
  const ticks: AxisTick[] = []
  for (let i = 0; i * step <= durationMs * (1 + 1e-9); i++) {
    const offsetMs = i * step
    ticks.push({ offsetMs, percent: (offsetMs / durationMs) * 100, label: formatAxisTime(offsetMs, step, durationMs) })
  }
  return ticks
}

// ============================================================================
// Span filter
// ============================================================================

export interface SpanFilter {
  // Text to find in the span's name, IDs, status message, or the keys and
  // values of its attributes, resource attributes and events
  query: string
  errorsOnly: boolean
}

export interface SpanFilterResult {
  // Spans that match
  matches: Set<string>
  // Matches and their ancestors, which keep them in place in the tree
  visible: Set<string>
}

function addAttributes(parts: string[], attributes?: Record<string, string>) {
  if (!attributes) return
  for (const [key, value] of Object.entries(attributes)) {
    parts.push(key, value)
  }
}

/** The text the span filter searches (lowercase) */
export function spanSearchText(span: Span): string {
  const parts = [span.spanName, span.spanId, span.serviceName, span.statusMessage ?? '']
  addAttributes(parts, span.spanAttributes)
  addAttributes(parts, span.resourceAttributes)
  for (const event of span.events ?? []) {
    parts.push(event.name)
    addAttributes(parts, event.attributes)
  }
  return parts.join('\n').toLowerCase()
}

export function isErrorSpan(span: Span): boolean {
  return span.statusCode?.toUpperCase() === 'ERROR'
}

/** The spans of the tree that pass filter; null when nothing is filtered */
export function filterSpanTree(
  roots: SpanNode[],
  filter: SpanFilter,
  searchText: (span: Span) => string = spanSearchText,
): SpanFilterResult | null {
  const query = filter.query.trim().toLowerCase()
  if (!query && !filter.errorsOnly) {
    return null
  }
  const matches = new Set<string>()
  const visible = new Set<string>()
  const isMatch = (span: Span) =>
    (!filter.errorsOnly || isErrorSpan(span)) && (!query || searchText(span).includes(query))

  // Post-order: a span is visible when it or a descendant matches
  const stack: Array<{ node: SpanNode; visited: boolean }> = roots.map((node) => ({ node, visited: false }))
  while (stack.length > 0) {
    const entry = stack.pop()!
    const { node } = entry
    if (!entry.visited) {
      stack.push({ node, visited: true })
      for (const child of node.children) {
        stack.push({ node: child, visited: false })
      }
      continue
    }
    const id = node.span.spanId
    if (isMatch(node.span)) {
      matches.add(id)
      visible.add(id)
    } else if (node.children.some((child) => visible.has(child.span.spanId))) {
      visible.add(id)
    }
  }
  return { matches, visible }
}

// ============================================================================
// Attribute values
// ============================================================================

// Values longer than this, or with more lines, are shortened until expanded
export const LONG_VALUE_CHARS = 300
export const LONG_VALUE_LINES = 5

/** A value to show: JSON objects and arrays pretty-printed, others as they are */
export function formatAttributeValue(value: string): { text: string; json: boolean } {
  const trimmed = value.trim()
  if ((trimmed.startsWith('{') && trimmed.endsWith('}')) || (trimmed.startsWith('[') && trimmed.endsWith(']'))) {
    try {
      return { text: JSON.stringify(JSON.parse(trimmed), null, 2), json: true }
    } catch {
      // not JSON after all
    }
  }
  return { text: value, json: false }
}

export function isLongValue(text: string): boolean {
  if (text.length > LONG_VALUE_CHARS) {
    return true
  }
  let lines = 1
  for (let i = text.indexOf('\n'); i !== -1; i = text.indexOf('\n', i + 1)) {
    if (++lines > LONG_VALUE_LINES) return true
  }
  return false
}

/** The start of a long value: its first lines, at most LONG_VALUE_CHARS characters */
export function valuePreview(text: string): string {
  const lines = text.split('\n', LONG_VALUE_LINES).join('\n')
  const preview = lines.length > LONG_VALUE_CHARS ? lines.slice(0, LONG_VALUE_CHARS) : lines
  return `${preview.trimEnd()}…`
}
