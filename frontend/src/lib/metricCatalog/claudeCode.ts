import { UNIT_FORMATS, type MetricMetadata } from '@/lib/metricTypes'

// Claude Code metrics
export const CLAUDE_CODE_METRICS: MetricMetadata[] = [
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
]
