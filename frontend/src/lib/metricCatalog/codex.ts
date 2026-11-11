import { UNIT_FORMATS, type MetricMetadata } from '@/lib/metricTypes'

// The Codex app and the CLI (both send conversations as codex-app-server
// since Codex 0.167, with originator Codex_Desktop or codex-tui)
const ORIGINATOR_BREAKDOWN = {
  attributeKey: 'originator',
  displayName: 'Client',
  showInLegend: true,
  knownValues: {
    Codex_Desktop: 'Codex app',
    'codex-tui': 'Codex CLI',
  },
}

const MODEL_BREAKDOWN = {
  attributeKey: 'model',
  displayName: 'Model',
  showInLegend: true,
}

// Codex metrics: token usage and cost derived by AI Observer, and Codex's
// own per-turn metrics (Codex 0.167 and later)
export const CODEX_METRICS: MetricMetadata[] = [
  {
    name: 'codex_cli_rs.token.usage',
    displayName: 'Token Usage',
    description: 'Tokens consumed by Codex (input includes cached input and cache writes, output includes reasoning)',
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
          cacheWrite: 'Cache Write',
          reasoning: 'Reasoning',
        },
      },
      MODEL_BREAKDOWN,
      ORIGINATOR_BREAKDOWN,
    ],
  },
  {
    name: 'codex_cli_rs.cost.usage',
    displayName: 'Cost',
    description: 'Cost in USD of Codex usage, calculated from built-in prices',
    source: 'codex_cli_rs',
    metricType: 'counter',
    unit: UNIT_FORMATS.usd,
    isMonotonic: true,
    breakdowns: [MODEL_BREAKDOWN, ORIGINATOR_BREAKDOWN],
  },
  {
    name: 'codex.turn.token_usage',
    displayName: 'Turn Tokens',
    description:
      "Tokens per turn as Codex reports them (total is input plus output). AI Observer's Token Usage counts the same tokens per response",
    source: 'codex_cli_rs',
    metricType: 'histogram',
    unit: UNIT_FORMATS.tokens,
    isMonotonic: false,
    breakdowns: [
      {
        attributeKey: 'token_type',
        displayName: 'Token Type',
        showInLegend: true,
        knownValues: {
          input: 'Input',
          output: 'Output',
          cached_input: 'Cache Read',
          cache_write_input: 'Cache Write',
          reasoning_output: 'Reasoning',
          total: 'Total',
        },
      },
      MODEL_BREAKDOWN,
      ORIGINATOR_BREAKDOWN,
    ],
  },
  {
    name: 'codex.conversation.turn.count',
    displayName: 'Turns',
    description: 'Conversation turns',
    source: 'codex_cli_rs',
    metricType: 'counter',
    unit: UNIT_FORMATS.count,
    isMonotonic: true,
    breakdowns: [MODEL_BREAKDOWN, ORIGINATOR_BREAKDOWN],
  },
  {
    name: 'codex.turn.e2e_duration_ms',
    displayName: 'Turn Duration',
    description: 'Duration of a turn from the prompt to its end, in milliseconds',
    source: 'codex_cli_rs',
    metricType: 'histogram',
    unit: UNIT_FORMATS.milliseconds,
    isMonotonic: false,
    breakdowns: [MODEL_BREAKDOWN, ORIGINATOR_BREAKDOWN],
  },
  {
    name: 'codex.turn.ttft.duration_ms',
    displayName: 'Time to First Token',
    description: 'Time from the prompt to the first token of the response, in milliseconds',
    source: 'codex_cli_rs',
    metricType: 'histogram',
    unit: UNIT_FORMATS.milliseconds,
    isMonotonic: false,
    breakdowns: [MODEL_BREAKDOWN, ORIGINATOR_BREAKDOWN],
  },
]

// Codex events (logs of codex_cli_rs and codex-app-server)
export const CODEX_EVENT_METRICS: MetricMetadata[] = [
  {
    name: 'codex.conversation_starts',
    displayName: 'Sessions',
    description: 'Codex session initialization events',
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
