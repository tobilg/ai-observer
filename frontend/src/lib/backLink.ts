// The Back buttons of detail pages (useBackLink)

// Labels by the first path segment of the page to go back to
const LIST_LABELS: Record<string, string> = {
  '': 'Back to dashboard',
  traces: 'Back to traces',
  sessions: 'Back to sessions',
  logs: 'Back to logs',
  metrics: 'Back to metrics',
}

const DETAIL_LABELS: Record<string, string> = {
  traces: 'Back to trace',
  sessions: 'Back to session',
}

/**
 * The target and label of a Back button: from when it is a path of this app
 * (location state can hold anything; "//host" would leave the app),
 * otherwise fallback
 */
export function backLinkFor(from: unknown, fallback: string): { to: string; label: string } {
  const to = typeof from === 'string' && from.startsWith('/') && !from.startsWith('//') ? from : fallback
  const segments = to.split(/[?#]/, 1)[0].split('/').filter(Boolean)
  const label = segments.length > 1
    ? DETAIL_LABELS[segments[0]] ?? 'Back'
    : LIST_LABELS[segments[0] ?? ''] ?? 'Back'
  return { to, label }
}
