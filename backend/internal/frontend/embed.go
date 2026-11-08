package frontend

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed dist/*
var distFS embed.FS

// mcpAppHTML is the view MCP Apps hosts render for the results of the MCP
// endpoint's tools: one self-contained page (scripts and styles inlined),
// built from frontend/mcp-app.html with `vite build --mode mcp-app`. It is
// not served by the SPA handler.
//
//go:embed mcpapp/mcp-app.html
var mcpAppHTML string

// MCPAppHTML returns the page of the MCP tools' view (MCP Apps)
func MCPAppHTML() string {
	return mcpAppHTML
}

// GetFileSystem returns an http.FileSystem rooted at dist/
func GetFileSystem() (http.FileSystem, error) {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		return nil, err
	}
	return http.FS(sub), nil
}
