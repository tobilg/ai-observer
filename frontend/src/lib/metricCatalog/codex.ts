import { UNIT_FORMATS, type MetricMetadata } from '@/lib/metricTypes'

// Codex CLI metrics
export const CODEX_METRICS: MetricMetadata[] = [
  {
    name: 'codex_cli_rs.token.usage',
    displayName: 'Token Usage',
    description: 'Number of tokens consumed by Codex CLI',
    source: 'codex_cli_rs',
    metricType: 'counter',
    unit: UNIT_FORMATS.tokens,
    isMonotonic: true,
    breakdowns: [
      {
        attributeKey: 'type',
        displayName: 'Token Type',
        showInLegend: true,
        knownValues: {
          input: 'Input',
          output: 'Output',
          cacheRead: 'Cache Read',
          reasoning: 'Reasoning',
          tool: 'Tool',
        },
      },
      {
        attributeKey: 'model',
        displayName: 'Model',
        showInLegend: true,
      },
    ],
  },
  {
    name: 'codex_cli_rs.cost.usage',
    displayName: 'Cost',
    description: 'Total cost in USD for Codex CLI usage',
    source: 'codex_cli_rs',
    metricType: 'counter',
    unit: UNIT_FORMATS.usd,
    isMonotonic: true,
    breakdowns: [
      {
        attributeKey: 'model',
        displayName: 'Model',
        showInLegend: true,
      },
    ],
  },
]

// Codex CLI events (from the codex_cli_rs service)
export const CODEX_EVENT_METRICS: MetricMetadata[] = [
  {
    name: 'codex.conversation_starts',
    displayName: 'Sessions',
    description: 'Codex CLI session initialization events',
    source: 'codex_cli_rs',
    metricType: 'counter',
    unit: UNIT_FORMATS.count,
    isMonotonic: true,
  },
  {
    name: 'codex.api_request',
    displayName: 'API Requests',
    description: 'Outbound API calls to model providers',
    source: 'codex_cli_rs',
    metricType: 'counter',
    unit: UNIT_FORMATS.count,
    isMonotonic: true,
    breakdowns: [
      {
        attributeKey: 'http.response.status_code',
        displayName: 'Status Code',
        showInLegend: true,
      },
    ],
  },
  {
    name: 'codex.sse_event',
    displayName: 'SSE Events',
    description: 'Streaming response lifecycle events',
    source: 'codex_cli_rs',
    metricType: 'counter',
    unit: UNIT_FORMATS.count,
    isMonotonic: true,
    breakdowns: [
      {
        attributeKey: 'event.kind',
        displayName: 'Event Kind',
        showInLegend: true,
      },
    ],
  },
  {
    name: 'codex.user_prompt',
    displayName: 'User Prompts',
    description: 'User input telemetry events',
    source: 'codex_cli_rs',
    metricType: 'counter',
    unit: UNIT_FORMATS.count,
    isMonotonic: true,
  },
  {
    name: 'codex.tool_decision',
    displayName: 'Tool Decisions',
    description: 'Tool approval workflow decisions',
    source: 'codex_cli_rs',
    metricType: 'counter',
    unit: UNIT_FORMATS.count,
    isMonotonic: true,
    breakdowns: [
      {
        attributeKey: 'tool_name',
        displayName: 'Tool',
        showInLegend: true,
      },
      {
        attributeKey: 'decision',
        displayName: 'Decision',
        showInLegend: true,
        knownValues: {
          approved: 'Approved',
          approved_execpolicy_amendment: 'Approved (Policy)',
          approved_for_session: 'Approved (Session)',
          denied: 'Denied',
          abort: 'Aborted',
        },
      },
      {
        attributeKey: 'source',
        displayName: 'Source',
        showInLegend: true,
        knownValues: {
          config: 'Config',
          user: 'User',
        },
      },
    ],
  },
  {
    name: 'codex.tool_result',
    displayName: 'Tool Results',
    description: 'Tool execution outcomes',
    source: 'codex_cli_rs',
    metricType: 'counter',
    unit: UNIT_FORMATS.count,
    isMonotonic: true,
    breakdowns: [
      {
        attributeKey: 'tool_name',
        displayName: 'Tool',
        showInLegend: true,
      },
      {
        attributeKey: 'success',
        displayName: 'Success',
        showInLegend: true,
        knownValues: {
          true: 'Success',
          false: 'Failed',
        },
      },
    ],
  },
]
