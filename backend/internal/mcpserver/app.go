package mcpserver

import (
	"context"
	"slices"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/tobilg/ai-observer/internal/frontend"
)

// The tools' view for hosts that render MCP Apps (Claude Desktop and web,
// VS Code, Cursor and others; SEP-1865): a tool's _meta.ui.resourceUri names
// the page, the host reads it with resources/read, shows it in a sandboxed
// iframe and passes it the tool's arguments and result, from whose
// structured content it draws charts and tables. The page calls tools
// through the host only (it has no network access), so the API's Host,
// Origin and token checks still apply. Clients without MCP Apps support
// ignore the field and use the text result as before.
const (
	appURI      = "ui://ai-observer/view.html"
	appMIMEType = "text/html;profile=mcp-app"
)

// withApp marks a tool as shown by the view. The legacy flat key is set
// too, as the official SDK's registerAppTool does, for hosts that read only
// it.
func withApp(tool *mcp.Tool) *mcp.Tool {
	tool.Meta = mcp.Meta{
		"ui":             map[string]any{"resourceUri": appURI},
		"ui/resourceUri": appURI,
	}
	return tool
}

// addAppResource serves the view's page. It needs no content security
// policy entries: scripts and styles are inlined, and data comes from tool
// calls through the host.
func addAppResource(server *mcp.Server) {
	html := frontend.MCPAppHTML()
	server.AddResource(&mcp.Resource{
		URI:         appURI,
		Name:        "ai-observer-view",
		Title:       "AI Observer view",
		Description: "Charts and tables for the results of usage_summary, query_metrics, get_trace and list_sessions, for hosts that render MCP Apps",
		MIMEType:    appMIMEType,
	}, func(context.Context, *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{
			URI:      appURI,
			MIMEType: appMIMEType,
			Text:     html,
			Meta:     mcp.Meta{"ui": map[string]any{"prefersBorder": true}},
		}}}, nil
	})
}

// hideAppResources leaves ui:// resources out of resources/list, which the
// MCP Apps specification allows: hosts that render them find them through the
// tools' metadata, while agents that browse resources (Claude Code and Codex
// have tools to list and read them) would otherwise pull the page's bundle
// into their context. resources/read still serves them.
func hideAppResources(next mcp.MethodHandler) mcp.MethodHandler {
	return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
		res, err := next(ctx, method, req)
		if list, ok := res.(*mcp.ListResourcesResult); ok && err == nil && method == "resources/list" {
			// A copy: the result's slice may share the server's storage
			list.Resources = slices.DeleteFunc(slices.Clone(list.Resources), func(r *mcp.Resource) bool {
				return strings.HasPrefix(r.URI, "ui://")
			})
		}
		return res, err
	}
}
