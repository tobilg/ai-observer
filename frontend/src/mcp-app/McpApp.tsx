import { errorOf, structuredOf, viewOf } from './data'
import type { MetricsResult, SessionsResult, TraceResult, UsageSummary } from './types'
import { useHost } from './useHost'
import { MetricsView } from './views/MetricsView'
import { Notice } from './views/parts'
import { SessionsView } from './views/SessionsView'
import { TraceView } from './views/TraceView'
import { UsageView } from './views/UsageView'

// The view MCP Apps hosts show for the results of usage_summary,
// query_metrics, get_trace and list_sessions
export function McpApp() {
  const host = useHost()
  return <div className="bg-background p-4 text-sm text-foreground">{content(host)}</div>
}

function content(host: ReturnType<typeof useHost>) {
  if (host.status === 'failed') {
    return <Notice title="Not connected to the host" text={host.error} />
  }
  if (!host.result) {
    return <Notice text={host.status === 'connecting' ? 'Connecting…' : 'Waiting for the result…'} />
  }
  const error = errorOf(host.result)
  if (error) {
    return <Notice title="The tool call failed" text={error} />
  }

  const data = structuredOf(host.result)
  // A new key for each pushed result resets the views' own state
  const key = host.version
  switch (viewOf(host.toolName, data)) {
    case 'usage':
      return <UsageView key={key} data={data as UsageSummary} args={host.args} callTool={host.callTool} />
    case 'metrics':
      return <MetricsView key={key} data={data as MetricsResult} />
    case 'trace':
      return <TraceView key={key} data={data as TraceResult} />
    case 'sessions':
      return <SessionsView key={key} data={data as SessionsResult} args={host.args} callTool={host.callTool} />
    default:
      return <Notice text="This result has no view." />
  }
}
