package mcpserver

// AppOutputs are values of the structured outputs of the tools whose
// results the MCP Apps view draws (frontend/src/mcp-app): usage_summary,
// query_metrics, get_trace and list_sessions. internal/apigen/spec
// generates their TypeScript types (frontend/src/types/mcp.gen.ts) from
// them, so changing their fields changes the view's types.
func AppOutputs() []any {
	return []any{usageSummaryOut{}, queryMetricsOut{}, getTraceOut{}, listSessionsOut{}}
}

// UsageGroups are the values usage_summary takes as group_by (and returns)
var UsageGroups = []string{"tool", "model", "day", "repository"}
