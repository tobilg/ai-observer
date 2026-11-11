package apigen

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Spec describes the HTTP endpoints of an API for an OpenAPI document
type Spec struct {
	Title, Version, Description string
	// Servers are the base URLs; routes name the ones that serve them
	Servers []Server
	Routes  []Route
}

// Server is a base URL of the API, such as the API port
type Server struct {
	Name, URL, Description string
}

// Route is an endpoint
type Route struct {
	// Method is the HTTP method, or "*" for a handler of every method
	// (Methods then lists the ones to document)
	Method  string
	Methods []string
	// Path is the router's pattern, such as /api/traces/{traceId}
	Path string
	// Servers names the servers that serve the route (default: the first
	// of Spec.Servers)
	Servers []string
	// Hidden routes are not documented (the dashboard's static files)
	Hidden bool
	Tag    string
	// Summary is a one-line description, Description more
	Summary, Description string
	// PathParams describes the {parameters} of Path
	PathParams map[string]string
	Query      []Param
	// Body is a value of the request body's type; BodyMedia lists the
	// media types of a body that is not JSON
	Body         any
	BodyRequired bool
	BodyMedia    []string
	Responses    []Response
}

// Param is a query parameter
type Param struct {
	Name, Description string
	// Type is string (default), integer or boolean
	Type string
	// Enum names an enum of the model the value is from
	Enum     string
	Required bool
	Default  any
	Min, Max *int64
	// Explode: the parameter may be repeated (an array of values)
	Repeated bool
}

// Response is a status the route answers with
type Response struct {
	// Status is the HTTP status; 0 documents all other statuses (default)
	Status      int
	Description string
	// Body is a value of the body's type (JSON); nil for no body or Media
	Body any
	// Media is the media type of a body that is not JSON, described by
	// Description
	Media string
}

// Int64 returns a pointer to n, for Param bounds
func Int64(n int64) *int64 { return &n }

var pathParam = regexp.MustCompile(`\{([^}:]+)(:[^}]*)?\}`)

// PathParams returns the names of a pattern's {parameters}
func PathParams(pattern string) []string {
	var names []string
	for _, m := range pathParam.FindAllStringSubmatch(pattern, -1) {
		names = append(names, m[1])
	}
	return names
}

// OpenAPI writes an OpenAPI 3.1 document of spec, with the types of the
// model as components
func (m *Model) OpenAPI(spec Spec) ([]byte, error) {
	schemas := map[string]any{}
	for _, e := range m.Enums {
		s := map[string]any{"type": "string", "enum": enumValues(e)}
		addDescription(s, enumDoc(e))
		schemas[e.Name] = s
	}
	for _, t := range m.Types {
		schemas[t.Name] = m.typeSchema(t)
	}

	// The first server serves the routes that name no other
	if len(spec.Servers) == 0 {
		return nil, fmt.Errorf("no servers")
	}
	defaultServer := spec.Servers[0].Name
	servers := map[string]map[string]any{}
	for _, s := range spec.Servers {
		srv := map[string]any{"url": s.URL}
		addDescription(srv, s.Description)
		servers[s.Name] = srv
	}

	paths := map[string]map[string]any{}
	for _, r := range spec.Routes {
		if r.Hidden {
			continue
		}
		methods := []string{r.Method}
		if r.Method == "*" {
			methods = r.Methods
		}
		item := paths[r.Path]
		if item == nil {
			item = map[string]any{}
			paths[r.Path] = item
		}
		for _, method := range methods {
			op, err := m.operation(r, method)
			if err != nil {
				return nil, fmt.Errorf("%s %s: %w", method, r.Path, err)
			}
			if len(r.Servers) > 0 && !(len(r.Servers) == 1 && r.Servers[0] == defaultServer) {
				var opServers []any
				for _, name := range r.Servers {
					srv, ok := servers[name]
					if !ok {
						return nil, fmt.Errorf("%s %s: unknown server %q", method, r.Path, name)
					}
					opServers = append(opServers, srv)
				}
				op["servers"] = opServers
			}
			key := strings.ToLower(method)
			if _, dup := item[key]; dup {
				return nil, fmt.Errorf("%s %s is documented twice", method, r.Path)
			}
			item[key] = op
		}
	}

	info := map[string]any{"title": spec.Title, "version": spec.Version}
	addDescription(info, spec.Description)
	doc := map[string]any{
		"openapi":    "3.1.0",
		"info":       info,
		"servers":    []any{servers[defaultServer]},
		"paths":      paths,
		"components": map[string]any{"schemas": schemas},
	}
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(out, '\n'), nil
}

func (m *Model) operation(r Route, method string) (map[string]any, error) {
	op := map[string]any{"operationId": operationID(method, r.Path)}
	addDescription(op, r.Description)
	if r.Summary != "" {
		op["summary"] = r.Summary
	}
	if r.Tag != "" {
		op["tags"] = []string{r.Tag}
	}
	var params []any
	names := PathParams(r.Path)
	for _, name := range names {
		p := map[string]any{"name": name, "in": "path", "required": true, "schema": map[string]any{"type": "string"}}
		addDescription(p, r.PathParams[name])
		params = append(params, p)
	}
	for name := range r.PathParams {
		if !slices.Contains(names, name) {
			return nil, fmt.Errorf("path parameter %q is not in the path", name)
		}
	}
	for _, q := range r.Query {
		schema, err := m.paramSchema(q)
		if err != nil {
			return nil, err
		}
		p := map[string]any{"name": q.Name, "in": "query", "schema": schema}
		if q.Required {
			p["required"] = true
		}
		if q.Repeated {
			p["explode"] = true
		}
		addDescription(p, q.Description)
		params = append(params, p)
	}
	if len(params) > 0 {
		op["parameters"] = params
	}

	switch {
	case r.Body != nil:
		ref, err := m.schemaOf(r.Body)
		if err != nil {
			return nil, err
		}
		op["requestBody"] = map[string]any{
			"required": r.BodyRequired,
			"content":  map[string]any{"application/json": map[string]any{"schema": ref}},
		}
	case len(r.BodyMedia) > 0:
		content := map[string]any{}
		for _, media := range r.BodyMedia {
			content[media] = map[string]any{}
		}
		op["requestBody"] = map[string]any{"required": true, "content": content}
	}

	responses := map[string]any{}
	for _, resp := range r.Responses {
		key := "default"
		if resp.Status != 0 {
			key = strconv.Itoa(resp.Status)
		}
		desc := resp.Description
		if desc == "" {
			desc = http.StatusText(resp.Status)
		}
		out := map[string]any{"description": desc}
		switch {
		case resp.Body != nil:
			ref, err := m.schemaOf(resp.Body)
			if err != nil {
				return nil, err
			}
			out["content"] = map[string]any{"application/json": map[string]any{"schema": ref}}
		case resp.Media != "":
			out["content"] = map[string]any{resp.Media: map[string]any{}}
		}
		if _, dup := responses[key]; dup {
			return nil, fmt.Errorf("response %s is given twice", key)
		}
		responses[key] = out
	}
	if len(responses) == 0 {
		return nil, fmt.Errorf("no responses")
	}
	op["responses"] = responses
	return op, nil
}

// operationID is a name of an operation: the method and the path's words,
// such as getApiTracesByTraceId
func operationID(method, path string) string {
	var b strings.Builder
	b.WriteString(strings.ToLower(method))
	for _, part := range strings.FieldsFunc(path, func(r rune) bool { return r == '/' || r == '-' || r == '.' || r == '_' }) {
		if name, ok := strings.CutPrefix(part, "{"); ok {
			b.WriteString("By")
			part = strings.TrimSuffix(name, "}")
		}
		if part == "*" {
			part = "Any"
		}
		b.WriteString(strings.ToUpper(part[:1]) + part[1:])
	}
	return b.String()
}

func (m *Model) paramSchema(q Param) (map[string]any, error) {
	s := map[string]any{}
	switch {
	case q.Enum != "":
		if m.Enum(q.Enum) == nil {
			return nil, fmt.Errorf("parameter %s: unknown enum %s", q.Name, q.Enum)
		}
		s["$ref"] = "#/components/schemas/" + q.Enum
	case q.Type == "" || q.Type == "string":
		s["type"] = "string"
	case q.Type == "integer" || q.Type == "boolean":
		s["type"] = q.Type
	default:
		return nil, fmt.Errorf("parameter %s: unknown type %q", q.Name, q.Type)
	}
	if q.Default != nil {
		s["default"] = q.Default
	}
	if q.Min != nil {
		s["minimum"] = *q.Min
	}
	if q.Max != nil {
		s["maximum"] = *q.Max
	}
	if q.Repeated {
		return map[string]any{"type": "array", "items": s}, nil
	}
	return s, nil
}

// schemaOf returns a reference to the schema of a value's type
func (m *Model) schemaOf(v any) (map[string]any, error) {
	t := m.Type(v)
	if t == nil {
		return nil, fmt.Errorf("type %T is not part of the model: add it to the roots", v)
	}
	return map[string]any{"$ref": "#/components/schemas/" + t.Name}, nil
}

func (m *Model) typeSchema(t *Type) map[string]any {
	nulls := t.Response
	var s map[string]any
	if !t.IsObject() {
		s = refSchema(*t.Alias, nulls)
	} else {
		props := map[string]any{}
		var required []string
		for _, f := range t.Fields {
			optional, null := fieldShape(t, f)
			p := refSchema(f.Ref.NonNil(), nulls)
			if null {
				p = withNull(p)
			}
			addDescription(p, f.Doc)
			props[f.Name] = p
			if !optional {
				required = append(required, f.Name)
			}
		}
		s = map[string]any{"type": "object", "properties": props}
		if len(required) > 0 {
			s["required"] = required
		}
		if len(t.Extends) > 0 {
			var all []any
			for _, base := range t.Extends {
				all = append(all, map[string]any{"$ref": "#/components/schemas/" + base.Name})
			}
			s = map[string]any{"allOf": append(all, s)}
		}
	}
	addDescription(s, t.Doc)
	return s
}

func refSchema(r Ref, nulls bool) map[string]any {
	var s map[string]any
	switch r.Kind {
	case KindUnknown:
		return map[string]any{}
	case KindString:
		s = map[string]any{"type": "string"}
		if r.Format != "" {
			s["format"] = r.Format
		}
	case KindNumber:
		s = map[string]any{"type": "number"}
		if r.Integer {
			s["type"] = "integer"
		}
		if r.Format != "" {
			s["format"] = r.Format
		}
	case KindBoolean:
		s = map[string]any{"type": "boolean"}
	case KindNamed:
		s = map[string]any{"$ref": "#/components/schemas/" + r.Named.Name}
	case KindEnum:
		s = map[string]any{"$ref": "#/components/schemas/" + r.Enum.Name}
	case KindArray:
		s = map[string]any{"type": "array", "items": refSchema(*r.Elem, nulls)}
	case KindTuple:
		items := make([]any, len(r.Items))
		for i, item := range r.Items {
			items[i] = refSchema(item, nulls)
			if i < len(r.Labels) {
				items[i].(map[string]any)["title"] = r.Labels[i]
			}
		}
		s = map[string]any{"type": "array", "prefixItems": items, "minItems": len(items), "maxItems": len(items)}
	case KindMap:
		s = map[string]any{"type": "object", "additionalProperties": refSchema(*r.Elem, nulls)}
	}
	if nulls && r.Nilable {
		s = withNull(s)
	}
	return s
}

// withNull allows null besides the schema's values
func withNull(s map[string]any) map[string]any {
	if t, ok := s["type"].(string); ok {
		s["type"] = []string{t, "null"}
		return s
	}
	return map[string]any{"anyOf": []any{s, map[string]any{"type": "null"}}}
}

func addDescription(s map[string]any, doc string) {
	if doc = strings.TrimSpace(doc); doc != "" {
		s["description"] = doc
	}
}

func enumValues(e *Enum) []string {
	values := make([]string, len(e.Members))
	for i, m := range e.Members {
		values[i] = m.Value
	}
	return values
}

// enumDoc is an enum's doc with its members' comments
func enumDoc(e *Enum) string {
	lines := []string{e.Doc}
	for _, m := range e.Members {
		if m.Doc != "" {
			lines = append(lines, "- "+m.Value+": "+strings.ReplaceAll(m.Doc, "\n", " "))
		}
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}
