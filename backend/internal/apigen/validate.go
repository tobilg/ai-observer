package apigen

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"
)

// Validate checks JSON the API wrote against the type with this TypeScript
// name, as the generated declarations describe it: required fields present,
// null only where allowed, no undeclared fields, values of the declared
// types and enums. It returns the problems found, by JSON path.
func (m *Model) Validate(name string, data []byte) []string {
	t := m.byName[name]
	if t == nil {
		return []string{"no type " + name}
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return []string{"invalid JSON: " + err.Error()}
	}
	var problems []string
	validateNamed(t, v, "$", &problems)
	return problems
}

func validateNamed(t *Type, v any, path string, problems *[]string) {
	if !t.IsObject() {
		validateRef(*t.Alias, v, path, problems)
		return
	}
	obj, ok := v.(map[string]any)
	if !ok {
		*problems = append(*problems, fmt.Sprintf("%s: expected a %s object, got %s", path, t.Name, describe(v)))
		return
	}
	declared := map[string]bool{}
	validateFields(t, obj, path, declared, problems)
	var extra []string
	for key := range obj {
		if !declared[key] {
			extra = append(extra, key)
		}
	}
	slices.Sort(extra)
	for _, key := range extra {
		*problems = append(*problems, fmt.Sprintf("%s.%s: not a field of %s", path, key, t.Name))
	}
}

func validateFields(t *Type, obj map[string]any, path string, declared map[string]bool, problems *[]string) {
	for _, base := range t.Extends {
		validateFields(base, obj, path, declared, problems)
	}
	for _, f := range t.Fields {
		declared[f.Name] = true
		optional, null := fieldShape(t, f)
		p := path + "." + f.Name
		v, present := obj[f.Name]
		switch {
		case !present:
			if !optional {
				*problems = append(*problems, p+": missing")
			}
		case v == nil:
			if !null && f.Ref.Kind != KindUnknown {
				*problems = append(*problems, p+": null")
			}
		default:
			validateRef(f.Ref.NonNil(), v, p, problems)
		}
	}
}

func validateRef(r Ref, v any, path string, problems *[]string) {
	if v == nil {
		if !r.Nilable && r.Kind != KindUnknown {
			*problems = append(*problems, path+": null")
		}
		return
	}
	bad := func(want string) {
		*problems = append(*problems, fmt.Sprintf("%s: expected %s, got %s", path, want, describe(v)))
	}
	switch r.Kind {
	case KindUnknown:
	case KindString, KindEnum:
		s, ok := v.(string)
		if !ok {
			bad("a string")
			return
		}
		if r.Kind == KindEnum && !slices.ContainsFunc(r.Enum.Members, func(m EnumMember) bool { return m.Value == s }) {
			*problems = append(*problems, fmt.Sprintf("%s: %q is not a %s", path, s, r.Enum.Name))
		}
		if r.Format == "date-time" {
			if _, err := time.Parse(time.RFC3339Nano, s); err != nil {
				*problems = append(*problems, fmt.Sprintf("%s: %q is not an RFC 3339 time", path, s))
			}
		}
	case KindNumber:
		n, ok := v.(json.Number)
		if !ok {
			bad("a number")
			return
		}
		if r.Integer && strings.ContainsAny(n.String(), ".eE") {
			if f, err := n.Float64(); err != nil || f != float64(int64(f)) {
				bad("an integer")
			}
		}
	case KindBoolean:
		if _, ok := v.(bool); !ok {
			bad("a boolean")
		}
	case KindArray:
		arr, ok := v.([]any)
		if !ok {
			bad("an array")
			return
		}
		for i, item := range arr {
			validateRef(*r.Elem, item, fmt.Sprintf("%s[%d]", path, i), problems)
		}
	case KindTuple:
		arr, ok := v.([]any)
		if !ok || len(arr) != len(r.Items) {
			bad(fmt.Sprintf("an array of %d", len(r.Items)))
			return
		}
		for i, item := range arr {
			validateRef(r.Items[i], item, fmt.Sprintf("%s[%d]", path, i), problems)
		}
	case KindMap:
		obj, ok := v.(map[string]any)
		if !ok {
			bad("an object")
			return
		}
		keys := make([]string, 0, len(obj))
		for k := range obj {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		for _, k := range keys {
			validateRef(*r.Elem, obj[k], path+"["+k+"]", problems)
		}
	case KindNamed:
		validateNamed(r.Named, v, path, problems)
	}
}

func describe(v any) string {
	switch v.(type) {
	case nil:
		return "null"
	case string:
		return "a string"
	case json.Number:
		return "a number"
	case bool:
		return "a boolean"
	case []any:
		return "an array"
	case map[string]any:
		return "an object"
	}
	return fmt.Sprintf("%T", v)
}
