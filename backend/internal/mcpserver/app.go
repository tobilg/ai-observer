package mcpserver

import (
	"context"
	"maps"
	"slices"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/tobilg/ai-observer/internal/frontend"
)

// The tools' view for hosts that render MCP Apps (the Codex app, VS Code,
// Cursor and others; SEP-1865; Claude Desktop renders none for servers from
// its config file, and reaches URL servers from the cloud, so it shows no
// view of a local AI Observer): a tool's _meta.ui.resourceUri names
// the page, the host reads it with resources/read, shows it in a sandboxed
// iframe and passes it the tool's arguments and result, from whose
// structured content it draws charts and tables. The page calls tools
// through the host only (it has no network access), so the API's Host,
// Origin and token checks still apply. Clients without MCP Apps support
// ignore the field and use the text result as before.
const (
	appURI       = "ui://ai-observer/view.html"
	appMIMEType  = "text/html;profile=mcp-app"
	appExtension = "io.modelcontextprotocol/ui"
)

// appModelDescription tells the model what the user sees in the view, so it
// does not restate the values below it. The MCP Apps specification has no
// field for this; "openai/widgetDescription" is OpenAI's (ChatGPT, Codex),
// "surfaced to the model when the component loads", and other hosts ignore
// it.
const appModelDescription = "The view summarizes usage, charts metric series, shows a trace waterfall, or lists loaded sessions. " +
	"Ranked lists initially show up to 20 rows. Answer the user's question using the tool result and avoid repeating values already visible."

func supportsApp(caps *mcp.ClientCapabilities) bool {
	if caps == nil {
		return false
	}
	settings, _ := caps.Extensions[appExtension].(map[string]any)
	types, _ := settings["mimeTypes"].([]any)
	for _, mime := range types {
		if mime == appMIMEType {
			return true
		}
	}
	return false
}

// Only advertise the HTML view to clients that negotiated its MIME profile.
// The server's registered tools are shared by concurrent sessions.
func negotiateAppTools(next mcp.MethodHandler) mcp.MethodHandler {
	return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
		res, err := next(ctx, method, req)
		if method != "tools/list" || err != nil || supportsApp(req.(*mcp.ListToolsRequest).ClientCapabilities()) {
			return res, err
		}
		list := *res.(*mcp.ListToolsResult)
		list.Tools = slices.Clone(list.Tools)
		for i, tool := range list.Tools {
			if tool.Meta["ui/resourceUri"] == appURI {
				plain := *tool
				plain.Meta = maps.Clone(tool.Meta)
				delete(plain.Meta, "ui")
				delete(plain.Meta, "ui/resourceUri")
				list.Tools[i] = &plain
			}
		}
		return &list, nil
	}
}

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
			Meta: mcp.Meta{
				"ui":                       map[string]any{"prefersBorder": true, "permissions": map[string]any{"clipboardWrite": map[string]any{}}},
				"openai/widgetDescription": appModelDescription,
			},
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
