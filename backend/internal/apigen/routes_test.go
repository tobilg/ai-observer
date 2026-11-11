package apigen

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/tobilg/ai-observer/internal/apigen/fixture"
)

func fixtureSpec(routes ...Route) Spec {
	return Spec{
		Title: "Test", Version: "1",
		Servers: []Server{{Name: "api", URL: "http://localhost:1"}, {Name: "other", URL: "http://localhost:2"}},
		Routes:  routes,
	}
}

func TestOpenAPI(t *testing.T) {
	m := buildFixture(t)
	spec := fixtureSpec(
		Route{Method: http.MethodGet, Path: "/things/{id}", Tag: "things", Summary: "A thing",
			PathParams: map[string]string{"id": "Thing ID"},
			Query: []Param{
				{Name: "limit", Type: "integer", Default: 10, Min: Int64(1), Max: Int64(100)},
				{Name: "shape", Enum: "Shape", Required: true},
				{Name: "tag", Repeated: true},
			},
			Responses: []Response{{Status: 200, Body: fixture.Thing{}}, {Description: "Error", Body: fixture.Inner{}}}},
		Route{Method: http.MethodPost, Path: "/things", Servers: []string{"other"}, Body: fixture.Request{}, BodyRequired: true,
			Responses: []Response{{Status: 204}}},
		Route{Method: "*", Methods: []string{http.MethodPost}, Path: "/rpc", BodyMedia: []string{"application/json"},
			Responses: []Response{{Status: 200, Media: "application/json", Description: "Reply"}}},
		Route{Method: "*", Path: "/*", Hidden: true},
	)
	data, err := m.OpenAPI(spec)
	if err != nil {
		t.Fatal(err)
	}
	again, _ := m.OpenAPI(spec)
	if string(data) != string(again) {
		t.Error("OpenAPI output is not deterministic")
	}
	var doc struct {
		OpenAPI    string                               `json:"openapi"`
		Servers    []map[string]any                     `json:"servers"`
		Paths      map[string]map[string]map[string]any `json:"paths"`
		Components struct {
			Schemas map[string]map[string]any `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.OpenAPI != "3.1.0" || len(doc.Servers) != 1 || doc.Servers[0]["url"] != "http://localhost:1" {
		t.Errorf("header = %s %v", doc.OpenAPI, doc.Servers)
	}
	if len(doc.Paths) != 3 || doc.Paths["/*"] != nil {
		t.Errorf("paths = %v", doc.Paths)
	}
	get := doc.Paths["/things/{id}"]["get"]
	if get["operationId"] != "getThingsById" || get["summary"] != "A thing" {
		t.Errorf("get = %v", get)
	}
	if post := doc.Paths["/things"]["post"]; post["servers"] == nil || post["requestBody"] == nil {
		t.Errorf("post = %v", post)
	}
	if doc.Paths["/rpc"]["post"] == nil {
		t.Errorf("rpc = %v", doc.Paths["/rpc"])
	}
	for _, name := range []string{"Thing", "Base", "Inner", "Counts", "Request", "Color", "Shape"} {
		if doc.Components.Schemas[name] == nil {
			t.Errorf("no schema %s", name)
		}
	}
	text := string(data)
	for _, want := range []string{`"$ref": "#/components/schemas/Base"`, `"prefixItems"`, `"format": "date-time"`} {
		if !strings.Contains(text, want) {
			t.Errorf("OpenAPI lacks %s", want)
		}
	}
	if enum := doc.Components.Schemas["Color"]["enum"].([]any); len(enum) != 2 || enum[0] != "red" || enum[1] != "green" {
		t.Errorf("Color = %v", doc.Components.Schemas["Color"])
	}
	thing := doc.Components.Schemas["Thing"]["allOf"].([]any)[1].(map[string]any)
	ptr := thing["properties"].(map[string]any)["ptr"].(map[string]any)["type"].([]any)
	if len(ptr) != 2 || ptr[0] != "integer" || ptr[1] != "null" {
		t.Errorf("ptr = %v, want integer or null", ptr)
	}
	required := thing["required"].([]any)
	if !containsAll(required, "name", "ptr", "nonNull", "struct", "pair", "counts") || containsAll(required, "opt") {
		t.Errorf("required = %v", required)
	}
}

func containsAll(list []any, values ...string) bool {
	for _, v := range values {
		found := false
		for _, item := range list {
			found = found || item == v
		}
		if !found {
			return false
		}
	}
	return true
}

func TestOpenAPIErrors(t *testing.T) {
	m := buildFixture(t)
	ok := []Response{{Status: 200}}
	tests := []struct {
		route Route
		want  string
	}{
		{Route{Method: "GET", Path: "/x", Query: []Param{{Name: "p", Enum: "Nope"}}, Responses: ok}, "unknown enum Nope"},
		{Route{Method: "GET", Path: "/x", Servers: []string{"nope"}, Responses: ok}, `unknown server "nope"`},
		{Route{Method: "GET", Path: "/x", Responses: []Response{{Status: 200, Body: fixture.WithPoint{}}}}, "not part of the model"},
		{Route{Method: "GET", Path: "/x", PathParams: map[string]string{"id": ""}, Responses: ok}, `path parameter "id" is not in the path`},
		{Route{Method: "GET", Path: "/x"}, "no responses"},
		{Route{Method: "GET", Path: "/x", Query: []Param{{Name: "p", Type: "float"}}, Responses: ok}, `unknown type "float"`},
	}
	for _, tt := range tests {
		_, err := m.OpenAPI(fixtureSpec(tt.route))
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%+v: err = %v, want %q", tt.route, err, tt.want)
		}
	}
	if _, err := m.OpenAPI(fixtureSpec(Route{Method: "GET", Path: "/x", Responses: ok}, Route{Method: "GET", Path: "/x", Responses: ok})); err == nil {
		t.Error("a route documented twice is no error")
	}
}

func TestPathParams(t *testing.T) {
	got := PathParams("/a/{id}/b/{widgetId:[0-9]+}")
	if strings.Join(got, ",") != "id,widgetId" {
		t.Errorf("PathParams = %v", got)
	}
}
