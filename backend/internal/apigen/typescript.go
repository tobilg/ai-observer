package apigen

import (
	"fmt"
	"regexp"
	"strings"
)

// TypeScript writes the model as TypeScript declarations, after header (a
// comment without comment markers, written as // lines). Types are written
// in the order the model reached them, enums first.
//
// Mapping: strings, numbers and booleans as such; time.Time as string (RFC
// 3339); slices as T[]; arrays as tuples; maps as Record<string, V>;
// interfaces and json.RawMessage as unknown; enums as unions of string
// literals. In types the API writes, a field encoding/json may leave out
// (omitempty on a type it applies to, omitzero) is optional, and a pointer,
// slice or map field that may be nil is `T | null` unless the field is
// declared NonNull. In types only clients send, fields with omitempty are
// optional and none is null.
func (m *Model) TypeScript(header string) []byte {
	var w tsWriter
	for line := range strings.SplitSeq(strings.TrimRight(header, "\n"), "\n") {
		w.line(strings.TrimRight("// "+line, " "))
	}
	for _, e := range m.Enums {
		w.blank()
		w.doc("", e.Doc, "", "")
		w.enum(e)
	}
	for _, t := range m.Types {
		w.blank()
		w.typeDecl(t)
	}
	return []byte(w.b.String())
}

type tsWriter struct {
	b strings.Builder
}

func (w *tsWriter) line(s string) {
	w.b.WriteString(s)
	w.b.WriteByte('\n')
}

func (w *tsWriter) blank() { w.b.WriteByte('\n') }

// doc writes a TSDoc comment; a leading Go name in the text is replaced by
// the TypeScript name
func (w *tsWriter) doc(indent, text, goName, name string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	if goName != "" && goName != name {
		if rest, ok := strings.CutPrefix(text, goName); ok && (rest == "" || rest[0] == ' ') {
			text = name + rest
		}
	}
	text = strings.ReplaceAll(text, "*/", "*\\/")
	lines := strings.Split(text, "\n")
	if len(lines) == 1 {
		w.line(indent + "/** " + lines[0] + " */")
		return
	}
	w.line(indent + "/**")
	for _, l := range lines {
		w.line(strings.TrimRight(indent+" * "+l, " "))
	}
	w.line(indent + " */")
}

func (w *tsWriter) enum(e *Enum) {
	documented := false
	for _, m := range e.Members {
		documented = documented || m.Doc != ""
	}
	if !documented {
		values := make([]string, len(e.Members))
		for i, m := range e.Members {
			values[i] = tsString(m.Value)
		}
		w.line("export type " + e.Name + " = " + strings.Join(values, " | "))
		return
	}
	w.line("export type " + e.Name + " =")
	for _, m := range e.Members {
		w.doc("  ", m.Doc, "", "")
		w.line("  | " + tsString(m.Value))
	}
}

func (w *tsWriter) typeDecl(t *Type) {
	w.doc("", t.Doc, t.GoType.Name(), t.Name)
	nulls := t.Response
	if !t.IsObject() {
		w.line("export type " + t.Name + " = " + tsType(*t.Alias, nulls))
		return
	}
	bases := make([]string, len(t.Extends))
	for i, base := range t.Extends {
		bases[i] = base.Name
	}
	switch {
	case len(t.Fields) == 0 && len(bases) == 0:
		w.line("export type " + t.Name + " = Record<string, never>")
		return
	case len(t.Fields) == 0:
		w.line("export type " + t.Name + " = " + strings.Join(bases, " & "))
		return
	}
	head := "export interface " + t.Name
	if len(bases) > 0 {
		head += " extends " + strings.Join(bases, ", ")
	}
	w.line(head + " {")
	for _, f := range t.Fields {
		w.doc("  ", f.Doc, f.GoName, f.Name)
		optional, null := fieldShape(t, f)
		typ := tsType(f.Ref.NonNil(), nulls)
		if null {
			typ += " | null"
		}
		name := tsPropertyName(f.Name)
		if optional {
			name += "?"
		}
		w.line("  " + name + ": " + typ)
	}
	w.line("}")
}

// fieldShape returns whether a field may be missing and whether it may be
// null (unknown includes null)
func fieldShape(t *Type, f *Field) (optional, null bool) {
	if !t.Response {
		return f.OmitEmpty, false
	}
	return f.Omitted, !f.Omitted && f.Ref.Nilable && !f.NonNull && f.Ref.Kind != KindUnknown
}

// tsType writes a JSON type; nulls says whether nil values inside it are
// written as null (types the API writes)
func tsType(r Ref, nulls bool) string {
	var s string
	switch r.Kind {
	case KindString:
		s = "string"
	case KindNumber:
		s = "number"
	case KindBoolean:
		s = "boolean"
	case KindUnknown:
		return "unknown"
	case KindNamed:
		s = r.Named.Name
	case KindEnum:
		s = r.Enum.Name
	case KindArray:
		elem := tsType(*r.Elem, nulls)
		if nulls && r.Elem.Nilable && r.Elem.Kind != KindUnknown {
			elem = "(" + elem + ")" // (T | null)[]
		}
		s = elem + "[]"
	case KindTuple:
		items := make([]string, len(r.Items))
		for i, item := range r.Items {
			items[i] = tsType(item, nulls)
			if i < len(r.Labels) {
				items[i] = r.Labels[i] + ": " + items[i]
			}
		}
		s = "[" + strings.Join(items, ", ") + "]"
	case KindMap:
		s = "Record<string, " + tsType(*r.Elem, nulls) + ">"
	default:
		panic(fmt.Sprintf("apigen: unknown kind %d", r.Kind))
	}
	if nulls && r.Nilable {
		s += " | null"
	}
	return s
}

var tsIdentifier = regexp.MustCompile(`^[A-Za-z_$][A-Za-z0-9_$]*$`)

func tsPropertyName(name string) string {
	if tsIdentifier.MatchString(name) {
		return name
	}
	return tsString(name)
}

func tsString(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `'`, `\'`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	return "'" + s + "'"
}
