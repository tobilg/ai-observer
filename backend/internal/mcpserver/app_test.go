package mcpserver

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestAppViewTools(t *testing.T) {
	server := httptest.NewServer(NewHandler(failingStore{}, "test"))
	t.Cleanup(server.Close)
	ctx := t.Context()
	for _, version := range []string{"2025-11-25", "2026-07-28"} {
		for _, mime := range []string{appMIMEType, "", "text/html"} {
			t.Run(version+"/"+mime, func(t *testing.T) {
				caps := &mcp.ClientCapabilities{}
				if mime != "" {
					caps.AddExtension(appExtension, map[string]any{"mimeTypes": []string{mime}})
				}
				client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, &mcp.ClientOptions{Capabilities: caps})
				cs, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: server.URL}, &mcp.ClientSessionOptions{ProtocolVersion: version})
				if err != nil {
					t.Fatal(err)
				}
				defer cs.Close()
				if cs.InitializeResult().ProtocolVersion != version {
					t.Fatalf("negotiated protocol %s, want %s", cs.InitializeResult().ProtocolVersion, version)
				}
				if (cs.ID() != "") != (version == "2025-11-25" && mime == appMIMEType) {
					t.Fatalf("unexpected session ID %q", cs.ID())
				}
				res, err := cs.ListTools(ctx, nil)
				if err != nil {
					t.Fatal(err)
				}
				withView := map[string]bool{"usage_summary": true, "query_metrics": true, "get_trace": true, "list_sessions": true}
				for _, tool := range res.Tools {
					ui, _ := tool.Meta["ui"].(map[string]any)
					if withView[tool.Name] && mime == appMIMEType {
						if ui["resourceUri"] != appURI || tool.Meta["ui/resourceUri"] != appURI {
							t.Errorf("%s: missing view in %v", tool.Name, tool.Meta)
						}
					} else if ui != nil || tool.Meta["ui/resourceUri"] != nil {
						t.Errorf("%s: unexpected view in %v", tool.Name, tool.Meta)
					}
					delete(withView, tool.Name)
				}
				if len(withView) > 0 {
					t.Errorf("missing tools: %v", withView)
				}
			})
		}
	}
}

func TestAppViewResource(t *testing.T) {
	cs, _, _ := testSession(t)
	ctx := context.Background()
	list, err := cs.ListResources(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range list.Resources {
		if strings.HasPrefix(r.URI, "ui://") {
			t.Errorf("resources/list lists %s", r.URI)
		}
	}

	res, err := cs.ReadResource(ctx, &mcp.ReadResourceParams{URI: appURI})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Contents) != 1 {
		t.Fatalf("contents = %+v", res.Contents)
	}
	c := res.Contents[0]
	ui, _ := c.Meta["ui"].(map[string]any)
	if c.URI != appURI || c.MIMEType != appMIMEType || !strings.Contains(c.Text, "<html") || ui["prefersBorder"] != true {
		t.Errorf("view = %s %s %v, %d characters", c.URI, c.MIMEType, c.Meta, len(c.Text))
	}
	if c.Meta["openai/widgetDescription"] != appModelDescription {
		t.Errorf("openai/widgetDescription = %v", c.Meta["openai/widgetDescription"])
	}
	perms, _ := ui["permissions"].(map[string]any)
	if _, ok := perms["clipboardWrite"].(map[string]any); !ok {
		t.Errorf("missing clipboardWrite permission in %v", perms)
	}
	// Self-contained: hosts give the page no network access
	if strings.Contains(c.Text, `src="/`) || strings.Contains(c.Text, `href="/`) {
		t.Error("the view loads files instead of inlining them")
	}
}

func TestHideAppResources(t *testing.T) {
	next := func(context.Context, string, mcp.Request) (mcp.Result, error) {
		return &mcp.ListResourcesResult{Resources: []*mcp.Resource{{URI: "ui://ai-observer/view.html"}, {URI: "ai-observer://other"}}}, nil
	}
	res, err := hideAppResources(next)(context.Background(), "resources/list", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := res.(*mcp.ListResourcesResult).Resources; len(got) != 1 || got[0].URI != "ai-observer://other" {
		t.Errorf("resources/list = %+v", got)
	}
	// Only the listing is filtered
	res, _ = hideAppResources(next)(context.Background(), "other/method", nil)
	if got := res.(*mcp.ListResourcesResult).Resources; len(got) != 2 {
		t.Errorf("other method = %+v", got)
	}
}
