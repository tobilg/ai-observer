import { useMemo } from 'react'
import { Badge } from '@/components/ui/badge'
import { WaterfallView } from '@/components/traces/WaterfallView'
import { traceSpans } from '../data'
import type { TraceResult } from '../types'
import { ViewHeader } from './parts'

// get_trace: the trace page's waterfall; a span shows its attributes when
// clicked
export function TraceView({ data }: { data: TraceResult }) {
  const spans = useMemo(() => traceSpans(data), [data])
  return (
    <div className="space-y-4">
      <ViewHeader
        title="Trace"
        subtitle={<span className="font-mono">{data.id}</span>}
      >
        <Badge variant="outline">{data.kind === 'codex_operation' ? 'Codex operation' : 'Trace'}</Badge>
        <Badge variant="secondary">
          {data.truncated ? `${data.spans.length} of ${data.total_spans} spans` : `${data.total_spans} spans`}
        </Badge>
      </ViewHeader>
      <WaterfallView spans={spans} />
    </div>
  )
}
