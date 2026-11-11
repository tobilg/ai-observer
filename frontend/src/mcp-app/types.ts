// Structured results of the MCP tools the view draws, as
// backend/internal/mcpserver returns them (snake_case JSON), generated from
// the Go types (types/mcp.gen.ts)
export type {
  UsageGroup,
  UsageRow,
  UsageSummary,
  MetricPoint,
  MetricSeries,
  MetricsResult,
  TraceSpan,
  TraceResult,
  SessionRow,
  SessionsResult,
} from '@/types/mcp.gen'

// Calls a tool of AI Observer's MCP server through the host and returns its
// structured result; throws the tool's error text
export type CallTool = (name: string, args: Record<string, unknown>) => Promise<unknown>
