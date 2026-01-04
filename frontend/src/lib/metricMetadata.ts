// Metric Metadata System for AI Observer
// Provides human-readable names, descriptions, and formatting for metrics from
// Claude Code, Gemini CLI, and Codex CLI

// ============================================================================
// Type Definitions
// ============================================================================

export type MetricType = 'counter' | 'gauge' | 'histogram' | 'summary'
export type SourceTool = 'claude_code' | 'gemini_cli' | 'codex_cli_rs' | 'unknown'
export type UnitFormatter = 'number' | 'duration' | 'bytes' | 'currency' | 'percentage' | 'tokens' | 'ratio'

export interface UnitFormat {
  unit: string // Raw unit string from OTLP (e.g., "1", "s", "By", "USD")
  displayUnit: string // Human-readable (e.g., "count", "seconds", "bytes", "$")
  formatter: UnitFormatter
  scale?: number // Optional scale factor (e.g., 1000 for ms -> s)
}

export interface AttributeBreakdown {
  attributeKey: string // e.g., "type", "model", "decision"
  displayName: string // e.g., "Type", "Model", "Decision"
  showInLegend: boolean // Whether to show in chart legend
  knownValues?: Record<string, string> // Optional mapping of known values to labels
}

export interface MetricMetadata {
  name: string // Exact metric name (e.g., "claude_code.token.usage")
  displayName: string // Human-readable name (e.g., "Token Usage")
  description: string // Detailed description
  source: SourceTool // Which tool emits this metric
  metricType: MetricType // counter, gauge, histogram, summary
  unit: UnitFormat // Unit and formatting info
  isMonotonic: boolean // For counters: true = cumulative, false = delta
  breakdowns?: AttributeBreakdown[] // Attributes to show as multiple series
}

// ============================================================================
// Unit Format Presets
// ============================================================================

export const UNIT_FORMATS: Record<string, UnitFormat> = {
  count: { unit: '1', displayUnit: '', formatter: 'number' },
  tokens: { unit: 'tokens', displayUnit: 'tokens', formatter: 'tokens' },
  seconds: { unit: 's', displayUnit: 's', formatter: 'duration' },
  milliseconds: { unit: 'ms', displayUnit: 'ms', formatter: 'duration' },
  bytes: { unit: 'By', displayUnit: 'bytes', formatter: 'bytes' },
  usd: { unit: 'USD', displayUnit: '$', formatter: 'currency' },
  percent: { unit: '%', displayUnit: '%', formatter: 'percentage' },
  ratio: { unit: 'ratio', displayUnit: '', formatter: 'ratio' },
  score: { unit: 'score', displayUnit: '', formatter: 'number' },
}

// ============================================================================
// Metric Catalog
// ============================================================================

export const METRIC_CATALOG: MetricMetadata[] = [
  // ==========================================================================
  // CLAUDE CODE METRICS
  // ==========================================================================
  {
    name: 'claude_code.session.count',
    displayName: 'Sessions',
    description: 'Number of Claude Code CLI sessions started',
    source: 'claude_code',
    metricType: 'counter',
    unit: UNIT_FORMATS.count,
    isMonotonic: true,
  },
  {
    name: 'claude_code.lines_of_code.count',
    displayName: 'Lines of Code',
    description: 'Number of lines of code modified (added or removed)',
    source: 'claude_code',
    metricType: 'counter',
    unit: UNIT_FORMATS.count,
    isMonotonic: true,
    breakdowns: [
      {
        attributeKey: 'type',
        displayName: 'Change Type',
        showInLegend: true,
        knownValues: {
          added: 'Lines Added',
          removed: 'Lines Removed',
        },
      },
    ],
  },
  {
    name: 'claude_code.pull_request.count',
    displayName: 'Pull Requests',
    description: 'Number of pull requests created',
    source: 'claude_code',
    metricType: 'counter',
    unit: UNIT_FORMATS.count,
    isMonotonic: true,
  },
  {
    name: 'claude_code.commit.count',
    displayName: 'Commits',
    description: 'Number of git commits created',
    source: 'claude_code',
    metricType: 'counter',
    unit: UNIT_FORMATS.count,
    isMonotonic: true,
  },
  {
    name: 'claude_code.cost.usage',
    displayName: 'Cost',
    description: 'Total cost in USD for Claude Code usage',
    source: 'claude_code',
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
  {
    name: 'claude_code.cost.usage_user_facing',
    displayName: 'Cost (User-Facing)',
    description: 'Cost for user-facing API calls only (excludes tool-routing)',
    source: 'claude_code',
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
  {
    name: 'claude_code.token.usage',
    displayName: 'Token Usage',
    description: 'Number of tokens consumed by Claude Code',
    source: 'claude_code',
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
          cacheCreation: 'Cache Creation',
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
    name: 'claude_code.token.usage_user_facing',
    displayName: 'Token Usage (User-Facing)',
    description: 'Tokens consumed by user-facing API calls only (excludes tool-routing)',
    source: 'claude_code',
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
          cacheCreation: 'Cache Creation',
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
    name: 'claude_code.code_edit_tool.decision',
    displayName: 'Edit Decisions',
    description: 'Count of code editing tool permission decisions',
    source: 'claude_code',
    metricType: 'counter',
    unit: UNIT_FORMATS.count,
    isMonotonic: true,
    breakdowns: [
      {
        attributeKey: 'decision',
        displayName: 'Decision',
        showInLegend: true,
        knownValues: {
          accept: 'Accepted',
          reject: 'Rejected',
        },
      },
      {
        attributeKey: 'tool',
        displayName: 'Tool',
        showInLegend: true,
        knownValues: {
          Edit: 'Edit',
          Write: 'Write',
          NotebookEdit: 'Notebook Edit',
        },
      },
      {
        attributeKey: 'language',
        displayName: 'Language',
        showInLegend: true,
      },
    ],
  },
  {
    name: 'claude_code.active_time.total',
    displayName: 'Active Time',
    description: 'Total active time in seconds (actual usage, not idle time)',
    source: 'claude_code',
    metricType: 'counter',
    unit: UNIT_FORMATS.seconds,
    isMonotonic: true,
  },

  // ==========================================================================
  // CODEX CLI METRICS
  // ==========================================================================

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

  // ==========================================================================
  // GEMINI CLI METRICS
  // ==========================================================================
  {
    name: 'gemini_cli.cost.usage',
    displayName: 'Cost',
    description: 'Total cost in USD for Gemini CLI usage',
    source: 'gemini_cli',
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
  {
    name: 'gemini_cli.session.count',
    displayName: 'Sessions (Cumulative)',
    description: 'Number of Gemini CLI sessions started (cumulative counter)',
    source: 'gemini_cli',
    metricType: 'counter',
    unit: UNIT_FORMATS.count,
    isMonotonic: true,
  },
  {
    name: 'gemini_cli.session.count.delta',
    displayName: 'Sessions',
    description: 'Number of Gemini CLI sessions started (delta per interval)',
    source: 'gemini_cli',
    metricType: 'counter',
    unit: UNIT_FORMATS.count,
    isMonotonic: false,
  },
  {
    name: 'gemini_cli.tool.call.count',
    displayName: 'Tool Calls',
    description: 'Number of tool invocations',
    source: 'gemini_cli',
    metricType: 'counter',
    unit: UNIT_FORMATS.count,
    isMonotonic: true,
    breakdowns: [
      {
        attributeKey: 'function_name',
        displayName: 'Function',
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
      {
        attributeKey: 'decision',
        displayName: 'Decision',
        showInLegend: true,
        knownValues: {
          accept: 'Accepted',
          reject: 'Rejected',
          modify: 'Modified',
          auto_accept: 'Auto-Accepted',
        },
      },
      {
        attributeKey: 'tool_type',
        displayName: 'Tool Type',
        showInLegend: true,
        knownValues: {
          mcp: 'MCP',
          native: 'Native',
        },
      },
    ],
  },
  {
    name: 'gemini_cli.tool.call.latency',
    displayName: 'Tool Latency',
    description: 'Tool execution duration in milliseconds',
    source: 'gemini_cli',
    metricType: 'histogram',
    unit: UNIT_FORMATS.milliseconds,
    isMonotonic: false,
    breakdowns: [
      {
        attributeKey: 'function_name',
        displayName: 'Function',
        showInLegend: true,
      },
    ],
  },
  {
    name: 'gemini_cli.api.request.count',
    displayName: 'API Requests (Cumulative)',
    description: 'Number of API calls to model providers (cumulative counter)',
    source: 'gemini_cli',
    metricType: 'counter',
    unit: UNIT_FORMATS.count,
    isMonotonic: true,
    breakdowns: [
      {
        attributeKey: 'model',
        displayName: 'Model',
        showInLegend: true,
      },
      {
        attributeKey: 'status_code',
        displayName: 'Status Code',
        showInLegend: true,
      },
      {
        attributeKey: 'error_type',
        displayName: 'Error Type',
        showInLegend: true,
      },
    ],
  },
  {
    name: 'gemini_cli.api.request.count.delta',
    displayName: 'API Requests',
    description: 'Number of API calls to model providers (delta per interval)',
    source: 'gemini_cli',
    metricType: 'counter',
    unit: UNIT_FORMATS.count,
    isMonotonic: false,
    breakdowns: [
      {
        attributeKey: 'model',
        displayName: 'Model',
        showInLegend: true,
      },
      {
        attributeKey: 'status_code',
        displayName: 'Status Code',
        showInLegend: true,
      },
      {
        attributeKey: 'error_type',
        displayName: 'Error Type',
        showInLegend: true,
      },
    ],
  },
  {
    name: 'gemini_cli.api.request.latency',
    displayName: 'API Latency',
    description: 'API response time in milliseconds',
    source: 'gemini_cli',
    metricType: 'histogram',
    unit: UNIT_FORMATS.milliseconds,
    isMonotonic: false,
    breakdowns: [
      {
        attributeKey: 'model',
        displayName: 'Model',
        showInLegend: true,
      },
    ],
  },
  {
    name: 'gemini_cli.token.usage',
    displayName: 'Token Usage (Cumulative)',
    description: 'Tokens consumed by Gemini CLI operations (cumulative counter)',
    source: 'gemini_cli',
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
          thought: 'Thought',
          cache: 'Cached',
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
    name: 'gemini_cli.token.usage.delta',
    displayName: 'Token Usage',
    description: 'Tokens consumed by Gemini CLI operations (delta per interval)',
    source: 'gemini_cli',
    metricType: 'counter',
    unit: UNIT_FORMATS.tokens,
    isMonotonic: false,
    breakdowns: [
      {
        attributeKey: 'type',
        displayName: 'Token Type',
        showInLegend: true,
        knownValues: {
          input: 'Input',
          output: 'Output',
          thought: 'Thought',
          cache: 'Cached',
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
    name: 'gemini_cli.file.operation.count',
    displayName: 'File Operations (Cumulative)',
    description: 'File system operations tracked (cumulative counter)',
    source: 'gemini_cli',
    metricType: 'counter',
    unit: UNIT_FORMATS.count,
    isMonotonic: true,
    breakdowns: [
      {
        attributeKey: 'operation',
        displayName: 'Operation',
        showInLegend: true,
        knownValues: {
          create: 'Create',
          read: 'Read',
          update: 'Update',
        },
      },
      {
        attributeKey: 'programming_language',
        displayName: 'Language',
        showInLegend: true,
      },
      {
        attributeKey: 'extension',
        displayName: 'Extension',
        showInLegend: true,
      },
    ],
  },
  {
    name: 'gemini_cli.file.operation.count.delta',
    displayName: 'File Operations',
    description: 'File system operations tracked (delta per interval)',
    source: 'gemini_cli',
    metricType: 'counter',
    unit: UNIT_FORMATS.count,
    isMonotonic: false,
    breakdowns: [
      {
        attributeKey: 'operation',
        displayName: 'Operation',
        showInLegend: true,
        knownValues: {
          create: 'Create',
          read: 'Read',
          update: 'Update',
        },
      },
      {
        attributeKey: 'programming_language',
        displayName: 'Language',
        showInLegend: true,
      },
      {
        attributeKey: 'extension',
        displayName: 'Extension',
        showInLegend: true,
      },
    ],
  },
  {
    name: 'gemini_cli.lines.changed',
    displayName: 'Lines Changed',
    description: 'Diff-based line modifications',
    source: 'gemini_cli',
    metricType: 'counter',
    unit: UNIT_FORMATS.count,
    isMonotonic: true,
    breakdowns: [
      {
        attributeKey: 'type',
        displayName: 'Change Type',
        showInLegend: true,
        knownValues: {
          added: 'Lines Added',
          removed: 'Lines Removed',
        },
      },
      {
        attributeKey: 'function_name',
        displayName: 'Function',
        showInLegend: true,
      },
    ],
  },
  {
    name: 'gemini_cli.agent.run.count',
    displayName: 'Agent Runs',
    description: 'Number of agent execution runs',
    source: 'gemini_cli',
    metricType: 'counter',
    unit: UNIT_FORMATS.count,
    isMonotonic: true,
    breakdowns: [
      {
        attributeKey: 'agent_name',
        displayName: 'Agent',
        showInLegend: true,
      },
      {
        attributeKey: 'terminate_reason',
        displayName: 'Termination Reason',
        showInLegend: true,
      },
    ],
  },
  {
    name: 'gemini_cli.agent.duration',
    displayName: 'Agent Duration',
    description: 'Agent execution time in milliseconds',
    source: 'gemini_cli',
    metricType: 'histogram',
    unit: UNIT_FORMATS.milliseconds,
    isMonotonic: false,
    breakdowns: [
      {
        attributeKey: 'agent_name',
        displayName: 'Agent',
        showInLegend: true,
      },
    ],
  },
  {
    name: 'gemini_cli.agent.turns',
    displayName: 'Agent Turns',
    description: 'Number of interaction iterations per agent run',
    source: 'gemini_cli',
    metricType: 'histogram',
    unit: UNIT_FORMATS.count,
    isMonotonic: false,
    breakdowns: [
      {
        attributeKey: 'agent_name',
        displayName: 'Agent',
        showInLegend: true,
      },
    ],
  },
  {
    name: 'gemini_cli.chat_compression',
    displayName: 'Chat Compression',
    description: 'Context compression events',
    source: 'gemini_cli',
    metricType: 'counter',
    unit: UNIT_FORMATS.count,
    isMonotonic: true,
  },
  {
    name: 'gemini_cli.chat.invalid_chunk.count',
    displayName: 'Invalid Chunks',
    description: 'Malformed stream data count',
    source: 'gemini_cli',
    metricType: 'counter',
    unit: UNIT_FORMATS.count,
    isMonotonic: true,
  },
  {
    name: 'gemini_cli.chat.content_retry.count',
    displayName: 'Content Retries',
    description: 'Recovery attempt count',
    source: 'gemini_cli',
    metricType: 'counter',
    unit: UNIT_FORMATS.count,
    isMonotonic: true,
  },
  {
    name: 'gemini_cli.chat.content_retry_failure.count',
    displayName: 'Retry Failures',
    description: 'Exhausted retry attempts count',
    source: 'gemini_cli',
    metricType: 'counter',
    unit: UNIT_FORMATS.count,
    isMonotonic: true,
  },
  {
    name: 'gemini_cli.slash_command.model.call_count',
    displayName: 'Model Commands',
    description: 'Model selections via slash commands',
    source: 'gemini_cli',
    metricType: 'counter',
    unit: UNIT_FORMATS.count,
    isMonotonic: true,
    breakdowns: [
      {
        attributeKey: 'slash_command.model.model_name',
        displayName: 'Model',
        showInLegend: true,
      },
    ],
  },
  {
    name: 'gemini_cli.model_routing.latency',
    displayName: 'Routing Latency',
    description: 'Router decision timing in milliseconds',
    source: 'gemini_cli',
    metricType: 'histogram',
    unit: UNIT_FORMATS.milliseconds,
    isMonotonic: false,
    breakdowns: [
      {
        attributeKey: 'routing.decision_model',
        displayName: 'Decision Model',
        showInLegend: true,
      },
      {
        attributeKey: 'routing.decision_source',
        displayName: 'Decision Source',
        showInLegend: true,
      },
    ],
  },
  {
    name: 'gemini_cli.model_routing.failure.count',
    displayName: 'Routing Failures',
    description: 'Model routing failure count',
    source: 'gemini_cli',
    metricType: 'counter',
    unit: UNIT_FORMATS.count,
    isMonotonic: true,
    breakdowns: [
      {
        attributeKey: 'routing.decision_source',
        displayName: 'Decision Source',
        showInLegend: true,
      },
    ],
  },
  {
    name: 'gemini_cli.ui.flicker.count',
    displayName: 'UI Flicker',
    description: 'Rendering instability events',
    source: 'gemini_cli',
    metricType: 'counter',
    unit: UNIT_FORMATS.count,
    isMonotonic: true,
  },
  {
    name: 'gemini_cli.startup.duration',
    displayName: 'Startup Duration',
    description: 'Initialization time in milliseconds',
    source: 'gemini_cli',
    metricType: 'histogram',
    unit: UNIT_FORMATS.milliseconds,
    isMonotonic: false,
    breakdowns: [
      {
        attributeKey: 'phase',
        displayName: 'Phase',
        showInLegend: true,
      },
    ],
  },
  {
    name: 'gemini_cli.memory.usage',
    displayName: 'Memory Usage',
    description: 'Memory consumption in bytes',
    source: 'gemini_cli',
    metricType: 'histogram',
    unit: UNIT_FORMATS.bytes,
    isMonotonic: false,
    breakdowns: [
      {
        attributeKey: 'memory_type',
        displayName: 'Memory Type',
        showInLegend: true,
        knownValues: {
          heap_used: 'Heap Used',
          heap_total: 'Heap Total',
          external: 'External',
          rss: 'RSS',
        },
      },
      {
        attributeKey: 'component',
        displayName: 'Component',
        showInLegend: true,
      },
    ],
  },
  {
    name: 'gemini_cli.cpu.usage',
    displayName: 'CPU Usage',
    description: 'Processor utilization percentage',
    source: 'gemini_cli',
    metricType: 'histogram',
    unit: UNIT_FORMATS.percent,
    isMonotonic: false,
    breakdowns: [
      {
        attributeKey: 'component',
        displayName: 'Component',
        showInLegend: true,
      },
    ],
  },
  {
    name: 'gemini_cli.tool.queue.depth',
    displayName: 'Tool Queue Depth',
    description: 'Number of pending tools in queue',
    source: 'gemini_cli',
    metricType: 'histogram',
    unit: UNIT_FORMATS.count,
    isMonotonic: false,
  },
  {
    name: 'gemini_cli.tool.execution.breakdown',
    displayName: 'Tool Execution Breakdown',
    description: 'Phase-level tool execution durations in milliseconds',
    source: 'gemini_cli',
    metricType: 'histogram',
    unit: UNIT_FORMATS.milliseconds,
    isMonotonic: false,
    breakdowns: [
      {
        attributeKey: 'function_name',
        displayName: 'Function',
        showInLegend: true,
      },
      {
        attributeKey: 'phase',
        displayName: 'Phase',
        showInLegend: true,
        knownValues: {
          validation: 'Validation',
          preparation: 'Preparation',
          execution: 'Execution',
          result_processing: 'Result Processing',
        },
      },
    ],
  },
  {
    name: 'gemini_cli.api.request.breakdown',
    displayName: 'API Request Breakdown',
    description: 'Request phase analysis in milliseconds',
    source: 'gemini_cli',
    metricType: 'histogram',
    unit: UNIT_FORMATS.milliseconds,
    isMonotonic: false,
    breakdowns: [
      {
        attributeKey: 'model',
        displayName: 'Model',
        showInLegend: true,
      },
      {
        attributeKey: 'phase',
        displayName: 'Phase',
        showInLegend: true,
        knownValues: {
          request_preparation: 'Request Preparation',
          network_latency: 'Network Latency',
          response_processing: 'Response Processing',
          token_processing: 'Token Processing',
        },
      },
    ],
  },
  {
    name: 'gemini_cli.token.efficiency',
    displayName: 'Token Efficiency',
    description: 'Output quality metrics ratio',
    source: 'gemini_cli',
    metricType: 'histogram',
    unit: UNIT_FORMATS.ratio,
    isMonotonic: false,
    breakdowns: [
      {
        attributeKey: 'model',
        displayName: 'Model',
        showInLegend: true,
      },
      {
        attributeKey: 'metric',
        displayName: 'Metric',
        showInLegend: true,
      },
    ],
  },
  {
    name: 'gemini_cli.performance.score',
    displayName: 'Performance Score',
    description: 'Composite performance benchmark score',
    source: 'gemini_cli',
    metricType: 'histogram',
    unit: UNIT_FORMATS.score,
    isMonotonic: false,
    breakdowns: [
      {
        attributeKey: 'category',
        displayName: 'Category',
        showInLegend: true,
      },
    ],
  },
  {
    name: 'gemini_cli.performance.regression',
    displayName: 'Performance Regressions',
    description: 'Performance degradation detection count',
    source: 'gemini_cli',
    metricType: 'counter',
    unit: UNIT_FORMATS.count,
    isMonotonic: true,
    breakdowns: [
      {
        attributeKey: 'metric',
        displayName: 'Metric',
        showInLegend: true,
      },
      {
        attributeKey: 'severity',
        displayName: 'Severity',
        showInLegend: true,
        knownValues: {
          low: 'Low',
          medium: 'Medium',
          high: 'High',
        },
      },
    ],
  },
  {
    name: 'gemini_cli.performance.regression.percentage_change',
    displayName: 'Regression Percentage',
    description: 'Performance variance magnitude',
    source: 'gemini_cli',
    metricType: 'histogram',
    unit: UNIT_FORMATS.percent,
    isMonotonic: false,
    breakdowns: [
      {
        attributeKey: 'metric',
        displayName: 'Metric',
        showInLegend: true,
      },
      {
        attributeKey: 'severity',
        displayName: 'Severity',
        showInLegend: true,
      },
    ],
  },
  {
    name: 'gemini_cli.performance.baseline.comparison',
    displayName: 'Baseline Comparison',
    description: 'Performance baseline drift percentage',
    source: 'gemini_cli',
    metricType: 'histogram',
    unit: UNIT_FORMATS.percent,
    isMonotonic: false,
    breakdowns: [
      {
        attributeKey: 'metric',
        displayName: 'Metric',
        showInLegend: true,
      },
      {
        attributeKey: 'category',
        displayName: 'Category',
        showInLegend: true,
      },
    ],
  },
  {
    name: 'gen_ai.client.token.usage',
    displayName: 'GenAI Token Usage (Cumulative)',
    description: 'Input/output token consumption per operation (OpenTelemetry semantic convention, cumulative)',
    source: 'gemini_cli',
    metricType: 'histogram',
    unit: UNIT_FORMATS.tokens,
    isMonotonic: false,
    breakdowns: [
      {
        attributeKey: 'gen_ai.token.type',
        displayName: 'Token Type',
        showInLegend: true,
        knownValues: {
          input: 'Input',
          output: 'Output',
        },
      },
      {
        attributeKey: 'gen_ai.request.model',
        displayName: 'Model',
        showInLegend: true,
      },
    ],
  },
  {
    name: 'gen_ai.client.token.usage.delta',
    displayName: 'GenAI Token Usage',
    description: 'Input/output token consumption per operation (OpenTelemetry semantic convention, delta per interval)',
    source: 'gemini_cli',
    metricType: 'counter',
    unit: UNIT_FORMATS.tokens,
    isMonotonic: false,
    breakdowns: [
      {
        attributeKey: 'gen_ai.token.type',
        displayName: 'Token Type',
        showInLegend: true,
        knownValues: {
          input: 'Input',
          output: 'Output',
        },
      },
      {
        attributeKey: 'gen_ai.request.model',
        displayName: 'Model',
        showInLegend: true,
      },
    ],
  },
  {
    name: 'gen_ai.client.operation.duration',
    displayName: 'GenAI Operation Duration',
    description: 'Operation completion timing in seconds (OpenTelemetry semantic convention)',
    source: 'gemini_cli',
    metricType: 'histogram',
    unit: UNIT_FORMATS.seconds,
    isMonotonic: false,
    breakdowns: [
      {
        attributeKey: 'gen_ai.operation.name',
        displayName: 'Operation',
        showInLegend: true,
      },
      {
        attributeKey: 'gen_ai.request.model',
        displayName: 'Model',
        showInLegend: true,
      },
    ],
  },

  // ==========================================================================
  // CODEX CLI EVENTS (from codex_cli_rs service)
  // ==========================================================================
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

// ============================================================================
// Lookup Map for O(1) Access
// ============================================================================

export const METRIC_METADATA_MAP: Map<string, MetricMetadata> = new Map(
  METRIC_CATALOG.map((m) => [m.name, m])
)

// ============================================================================
// Utility Functions
// ============================================================================

/**
 * Get metadata for a metric, with graceful fallback for unknown metrics
 */
export function getMetricMetadata(metricName: string): MetricMetadata {
  const metadata = METRIC_METADATA_MAP.get(metricName)
  if (metadata) return metadata

  // Infer source from metric prefix
  let source: SourceTool = 'unknown'
  if (metricName.startsWith('claude_code.')) source = 'claude_code'
  else if (metricName.startsWith('gemini_cli.')) source = 'gemini_cli'
  else if (metricName.startsWith('codex.') || metricName.startsWith('codex_cli_rs.')) source = 'codex_cli_rs'
  else if (metricName.startsWith('gen_ai.')) source = 'gemini_cli'

  // Return fallback metadata
  return {
    name: metricName,
    displayName: formatMetricName(metricName),
    description: '',
    source,
    metricType: 'counter',
    unit: UNIT_FORMATS.count,
    isMonotonic: true,
  }
}

/**
 * Convert raw metric name to human-readable format
 * e.g., "claude_code.token.usage" -> "Token Usage"
 */
export function formatMetricName(name: string): string {
  // Remove common prefixes
  const prefixes = ['claude_code.', 'gemini_cli.', 'codex_cli_rs.', 'codex.', 'gen_ai.']
  let cleaned = name
  for (const prefix of prefixes) {
    if (cleaned.startsWith(prefix)) {
      cleaned = cleaned.slice(prefix.length)
      break
    }
  }

  // Convert snake_case and dots to Title Case
  return cleaned
    .replace(/[._]/g, ' ')
    .replace(/\b\w/g, (c) => c.toUpperCase())
    .trim()
}

/**
 * Format a metric value based on its unit type
 */
export function formatMetricValue(value: number | null, unit: UnitFormat): string {
  if (value === null || value === undefined) return '—'

  const scaledValue = unit.scale ? value / unit.scale : value

  switch (unit.formatter) {
    case 'duration':
      return formatDuration(scaledValue, unit.displayUnit)
    case 'bytes':
      return formatBytes(scaledValue)
    case 'currency':
      return formatCurrency(scaledValue)
    case 'percentage':
      return `${scaledValue.toFixed(1)}%`
    case 'tokens':
      return formatNumber(scaledValue)
    case 'ratio':
      return scaledValue.toFixed(3)
    case 'number':
    default:
      return formatNumber(scaledValue)
  }
}

/**
 * Format a number with K/M abbreviations
 */
export function formatNumber(n: number): string {
  if (Math.abs(n) >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`
  if (Math.abs(n) >= 1_000) return `${(n / 1_000).toFixed(1)}K`
  return Number.isInteger(n) ? n.toString() : n.toFixed(2)
}

/**
 * Format a duration value
 */
export function formatDuration(value: number, unit: string): string {
  // If value is in milliseconds
  if (unit === 'ms') {
    if (value >= 60000) return `${(value / 60000).toFixed(1)}m`
    if (value >= 1000) return `${(value / 1000).toFixed(2)}s`
    return `${Math.round(value)}ms`
  }

  // If value is in seconds
  if (value >= 3600) return `${(value / 3600).toFixed(1)}h`
  if (value >= 60) return `${(value / 60).toFixed(1)}m`
  if (value >= 1) return `${value.toFixed(1)}s`
  return `${Math.round(value * 1000)}ms`
}

/**
 * Format bytes to human-readable format
 */
export function formatBytes(bytes: number): string {
  if (bytes >= 1_073_741_824) return `${(bytes / 1_073_741_824).toFixed(1)} GB`
  if (bytes >= 1_048_576) return `${(bytes / 1_048_576).toFixed(1)} MB`
  if (bytes >= 1_024) return `${(bytes / 1_024).toFixed(1)} KB`
  return `${bytes} B`
}

/**
 * Format currency (USD)
 */
export function formatCurrency(amount: number): string {
  return new Intl.NumberFormat('en-US', {
    style: 'currency',
    currency: 'USD',
    minimumFractionDigits: 2,
    maximumFractionDigits: 4,
  }).format(amount)
}

/**
 * Get the display label for a series based on its labels and metadata
 */
export function getSeriesLabel(
  labels: Record<string, string> | undefined,
  metadata: MetricMetadata,
  breakdownAttribute?: string
): string {
  if (!labels) {
    return metadata.displayName || 'value'
  }

  // If a specific breakdown attribute is requested, use it
  if (breakdownAttribute && labels[breakdownAttribute]) {
    const breakdown = metadata.breakdowns?.find((b) => b.attributeKey === breakdownAttribute)
    if (breakdown) {
      const value = labels[breakdownAttribute]
      return breakdown.knownValues?.[value] || value
    }
    return labels[breakdownAttribute]
  }

  // Otherwise, use the first breakdown with a value
  if (metadata.breakdowns) {
    for (const breakdown of metadata.breakdowns) {
      const value = labels[breakdown.attributeKey]
      if (value) {
        return breakdown.knownValues?.[value] || value
      }
    }
  }

  // Fall back to service name or type
  if (labels.type) {
    const serviceName = labels.service ? getServiceDisplayName(labels.service) : ''
    return serviceName ? `${serviceName}: ${labels.type}` : labels.type
  }

  return labels.service ? getServiceDisplayName(labels.service) : 'value'
}

/**
 * Get available breakdown attributes for a metric
 */
export function getAvailableBreakdowns(metricName: string): AttributeBreakdown[] {
  const metadata = getMetricMetadata(metricName)
  return metadata.breakdowns || []
}

/**
 * Get metrics grouped by source tool
 */
export function getMetricsBySource(): Record<SourceTool, MetricMetadata[]> {
  const grouped: Record<SourceTool, MetricMetadata[]> = {
    claude_code: [],
    gemini_cli: [],
    codex_cli_rs: [],
    unknown: [],
  }

  for (const metric of METRIC_CATALOG) {
    grouped[metric.source].push(metric)
  }

  return grouped
}

/**
 * Get display name for a source tool
 */
export function getSourceDisplayName(source: SourceTool): string {
  switch (source) {
    case 'claude_code':
      return 'Claude Code'
    case 'gemini_cli':
      return 'Gemini CLI'
    case 'codex_cli_rs':
      return 'Codex CLI'
    default:
      return 'Other'
  }
}

/**
 * Get display name for a service name (from API/backend)
 */
export function getServiceDisplayName(serviceName: string): string {
  switch (serviceName) {
    case 'claude-code':
      return 'Claude Code'
    case 'gemini-cli':
      return 'Gemini CLI'
    case 'codex_cli_rs':
      return 'Codex CLI'
    default:
      return serviceName // fallback to original
  }
}
