package mcpserver

import (
	"context"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestAppViewTools(t *testing.T) {
	cs, _, _ := testSession(t)
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	withView := map[string]bool{"usage_summary": true, "query_metrics": true, "get_trace": true, "list_sessions": true}
	for _, tool := range res.Tools {
		ui, _ := tool.Meta["ui"].(map[string]any)
		if !withView[tool.Name] {
			if tool.Meta != nil {
				t.Errorf("%s: unexpected _meta %v", tool.Name, tool.Meta)
			}
			continue
		}
		if ui["resourceUri"] != appURI || tool.Meta["ui/resourceUri"] != appURI {
			t.Errorf("%s: _meta = %v, want the view %s", tool.Name, tool.Meta, appURI)
		}
		delete(withView, tool.Name)
	}
	if len(withView) != 0 {
		t.Errorf("tools without the view: %v", withView)
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
