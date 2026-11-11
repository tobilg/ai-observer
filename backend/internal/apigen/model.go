// Package apigen describes the JSON of an HTTP API from its Go types and
// writes it as TypeScript declarations and an OpenAPI document.
//
// The types are read with reflect, so the JSON they describe is what
// encoding/json writes: field names and omitempty from the json tags,
// embedded structs promoted, nil pointers, slices and maps written as null
// unless omitted. Doc comments and the constants of string enums come from
// the Go source files of the types' packages (go/ast), so the generator runs
// in the module's source tree.
package apigen

import (
	"encoding"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"
)

// Config describes an API: its root types and how to name and refine them
type Config struct {
	// Roots are the request and response bodies (and other messages) of
	// the API; the types they reference are included
	Roots []Root
	// Names gives TypeScript names to types reached from roots that have no
	// exported Go name, keyed by "<package name>.<Go type name>" (such as
	// "mcpserver.usageRow"); also renames exported types
	Names map[string]string
	// Enums are string types whose values are Go constants
	Enums []EnumSpec
	// Fields refine single fields
	Fields []FieldSpec
	// Custom describes the JSON of types with their own MarshalJSON or
	// MarshalText (time.Time and json.RawMessage are known). A type with
	// either method that is not listed is an error, since its JSON is not
	// what its fields say.
	Custom []CustomSpec
}

// Root is a type of the API
type Root struct {
	// Value is a value of the type (its zero value)
	Value any
	// Name is the TypeScript name; default: the Go type name
	Name string
	// Request marks types that clients send. Types reached only from
	// requests are written as the client should send them: fields with
	// omitempty are optional, others required, and none is null.
	Request bool
	// Doc describes a type whose Go declaration has no doc comment
	Doc string
}

// EnumSpec declares a closed set of string values: the string constants of
// a Go package with a name prefix or of a named type, or literal values
type EnumSpec struct {
	// Name is the TypeScript type name
	Name string
	// Doc describes the enum; default: the doc comment of the Go type or
	// of the constants' declaration group
	Doc string
	// Package is the import path of the package declaring the constants
	Package string
	// Prefix selects the constants whose names are the prefix followed by
	// an upper-case letter or digit (TraceKind selects TraceKindOTelTrace)
	Prefix string
	// GoType selects the constants of a named string type of Package, and
	// makes fields of that type use the enum
	GoType string
	// Values lists the values instead of constants
	Values []string
}

// FieldSpec refines a field of a struct type
type FieldSpec struct {
	// Type is a value of the struct type (or its reflect.Type), Field the
	// Go name of the field
	Type  any
	Field string
	// Enum names an EnumSpec the field's string values (or elements) are
	// from
	Enum string
	// NonNull marks a slice or map field that is never nil where the API
	// writes the type: it is written as [] or {} when empty, never null
	NonNull bool
	// Override replaces the field's type
	Override *Ref
	// As is a value of the Go type the field holds, for interface fields
	// that always hold one type
	As any
}

// CustomSpec describes the JSON of a type with its own marshaling
type CustomSpec struct {
	// Type is a value of the type (or its reflect.Type)
	Type any
	// Ref is the JSON type; nil when AsStruct
	Ref *Ref
	// AsStruct says the JSON has the fields of the struct as encoding/json
	// would write them (adjusted by FieldSpecs)
	AsStruct bool
}

// Model is the API's types, ready to be written
type Model struct {
	// Types are the named types in the order they were reached
	Types []*Type
	// Enums are the enums in declaration order
	Enums []*Enum

	byGo   map[reflect.Type]*Type
	byName map[string]*Type
}

// Type is a named type of the API: an object, or an alias of another type
// (a named map such as Record<string, number>)
type Type struct {
	Name   string
	GoType reflect.Type
	Doc    string
	// Extends are the embedded structs whose fields are promoted
	Extends []*Type
	// Fields of an object in Go order (promoted fields are in Extends)
	Fields []*Field
	// Alias is the aliased type of a named non-struct type
	Alias *Ref
	// Response is set when the type appears in something the API writes,
	// Request when in something clients send
	Response, Request bool
}

// IsObject reports whether the type is an object (not an alias)
func (t *Type) IsObject() bool { return t.Alias == nil }

// Field is a field of an object
type Field struct {
	Name   string // JSON name
	GoName string
	Doc    string
	Ref    Ref
	// OmitEmpty: the tag has omitempty or omitzero. Omitted: encoding/json
	// leaves the field out for some values (omitempty on a type it applies
	// to, or omitzero), so it may be missing from responses.
	OmitEmpty, Omitted bool
	// NonNull: a slice or map field the API never writes as null
	NonNull bool
}

// Enum is a closed set of strings
type Enum struct {
	Name    string
	Doc     string
	Members []EnumMember
}

// EnumMember is a value of an enum, with its constant's comment
type EnumMember struct {
	Value string
	Doc   string
}

// Kind is the kind of a JSON type
type Kind int

const (
	KindUnknown Kind = iota // any JSON value
	KindString
	KindNumber
	KindBoolean
	KindArray
	KindTuple
	KindMap
	KindNamed // a Type
	KindEnum
)

// Ref is a JSON type
type Ref struct {
	Kind Kind
	// Integer: a number without fraction (Go integer types)
	Integer bool
	// Format is the OpenAPI format, such as date-time or int64
	Format string
	// Nilable: Go may write null here (nil pointer, slice, map or
	// interface)
	Nilable bool
	// Elem is the element type of arrays and the value type of maps
	Elem *Ref
	// Items and Labels are the element types and their names of tuples
	Items  []Ref
	Labels []string
	Named  *Type
	Enum   *Enum
}

// Common JSON types for overrides
var (
	String  = Ref{Kind: KindString}
	Number  = Ref{Kind: KindNumber}
	Unknown = Ref{Kind: KindUnknown, Nilable: true}
)

// Nullable returns r marked as possibly null
func (r Ref) Nullable() Ref {
	r.Nilable = true
	return r
}

// NonNil returns r marked as never null
func (r Ref) NonNil() Ref {
	r.Nilable = false
	return r
}

// ArrayOf is an array of elem
func ArrayOf(elem Ref) Ref { return Ref{Kind: KindArray, Elem: &elem} }

// TupleOf is a fixed-length array whose elements have these names and types
func TupleOf(labels []string, items ...Ref) Ref {
	return Ref{Kind: KindTuple, Labels: labels, Items: items}
}

// Build reads the types of cfg. Sources are read from the module that
// contains dir (the directory of its go.mod or one below it).
func Build(cfg Config, dir string) (*Model, error) {
	src, err := newSourceReader(dir)
	if err != nil {
		return nil, err
	}
	b := &builder{
		cfg:    cfg,
		src:    src,
		model:  &Model{byGo: map[reflect.Type]*Type{}, byName: map[string]*Type{}},
		enums:  map[string]*Enum{},
		enumTy: map[reflect.Type]*Enum{},
		custom: map[reflect.Type]CustomSpec{},
		fields: map[reflect.Type]map[string]FieldSpec{},
		names:  map[reflect.Type]string{},
		used:   map[string]bool{},
	}
	if err := b.prepare(); err != nil {
		return nil, err
	}
	for _, root := range cfg.Roots {
		t := reflect.TypeOf(root.Value)
		if t == nil {
			return nil, fmt.Errorf("root value is nil")
		}
		name := root.Name
		if name == "" {
			// Named non-struct roots (such as a named map) are written as
			// aliases wherever they are used
			if name, err = b.typeName(t); err != nil {
				return nil, err
			}
		}
		if prev, ok := b.names[t]; ok && prev != name {
			return nil, fmt.Errorf("%s is named both %s and %s", t, prev, name)
		}
		b.names[t] = name
	}
	for _, root := range cfg.Roots {
		t := reflect.TypeOf(root.Value)
		named, err := b.namedType(t)
		if err != nil {
			return nil, err
		}
		if named.Doc == "" {
			named.Doc = root.Doc
		}
		b.mark(named, !root.Request, map[*Type]bool{})
	}
	if err := b.checkUnused(); err != nil {
		return nil, err
	}
	return b.model, nil
}

// Type returns the named type of a Go value or type
func (m *Model) Type(v any) *Type {
	return m.byGo[typeOf(v)]
}

// TypeNamed returns the type with this TypeScript name
func (m *Model) TypeNamed(name string) *Type { return m.byName[name] }

// Enum returns the enum with this name
func (m *Model) Enum(name string) *Enum {
	for _, e := range m.Enums {
		if e.Name == name {
			return e
		}
	}
	return nil
}

func typeOf(v any) reflect.Type {
	if t, ok := v.(reflect.Type); ok {
		return t
	}
	return reflect.TypeOf(v)
}

type builder struct {
	cfg    Config
	src    *sourceReader
	model  *Model
	enums  map[string]*Enum
	enumTy map[reflect.Type]*Enum // named Go string types with constants
	custom map[reflect.Type]CustomSpec
	fields map[reflect.Type]map[string]FieldSpec
	names  map[reflect.Type]string
	// used records the Names, Fields and enum type bindings that were
	// applied, so stale configuration is reported
	used map[string]bool
}

var (
	timeType       = reflect.TypeFor[time.Time]()
	rawMessageType = reflect.TypeFor[json.RawMessage]()
	marshalerType  = reflect.TypeFor[json.Marshaler]()
	textMarshaler  = reflect.TypeFor[encoding.TextMarshaler]()
)

func (b *builder) prepare() error {
	for _, spec := range b.cfg.Enums {
		if _, dup := b.enums[spec.Name]; dup {
			return fmt.Errorf("enum %s is declared twice", spec.Name)
		}
		e, err := b.src.enum(spec)
		if err != nil {
			return err
		}
		b.enums[spec.Name] = e
		b.model.Enums = append(b.model.Enums, e)
	}
	for _, c := range b.cfg.Custom {
		t := typeOf(c.Type)
		if c.Ref == nil && !c.AsStruct {
			return fmt.Errorf("custom type %s needs a Ref or AsStruct", t)
		}
		b.custom[t] = c
	}
	for _, f := range b.cfg.Fields {
		t := typeOf(f.Type)
		if t.Kind() != reflect.Struct {
			return fmt.Errorf("field spec %s.%s: not a struct type", t, f.Field)
		}
		sf, ok := t.FieldByName(f.Field)
		if !ok || len(sf.Index) != 1 {
			return fmt.Errorf("field spec %s.%s: no such field", t, f.Field)
		}
		if f.Enum != "" && b.enums[f.Enum] == nil {
			return fmt.Errorf("field spec %s.%s: unknown enum %s", t, f.Field, f.Enum)
		}
		if f.NonNull && sf.Type.Kind() != reflect.Slice && sf.Type.Kind() != reflect.Map {
			return fmt.Errorf("field spec %s.%s: NonNull applies to slices and maps", t, f.Field)
		}
		if b.fields[t] == nil {
			b.fields[t] = map[string]FieldSpec{}
		}
		if _, dup := b.fields[t][f.Field]; dup {
			return fmt.Errorf("field spec %s.%s is given twice", t, f.Field)
		}
		b.fields[t][f.Field] = f
	}
	return nil
}

// checkUnused reports configuration that matched nothing
func (b *builder) checkUnused() error {
	var problems []string
	for key := range b.cfg.Names {
		if !b.used["name:"+key] {
			problems = append(problems, "name "+key+" matches no type")
		}
	}
	for t, specs := range b.fields {
		if b.model.byGo[t] == nil {
			for name := range specs {
				problems = append(problems, fmt.Sprintf("field spec %s.%s: the type is not part of the API", t, name))
			}
		}
	}
	for _, e := range b.model.Enums {
		if !b.used["enum:"+e.Name] {
			problems = append(problems, "enum "+e.Name+" is used by no field")
		}
	}
	if len(problems) > 0 {
		slices.Sort(problems)
		return fmt.Errorf("stale configuration: %s", strings.Join(problems, "; "))
	}
	return nil
}

// mark records in which direction t and the types it references are used
func (b *builder) mark(t *Type, response bool, seen map[*Type]bool) {
	if seen[t] {
		return
	}
	seen[t] = true
	if response {
		t.Response = true
	} else {
		t.Request = true
	}
	var visit func(r *Ref)
	visit = func(r *Ref) {
		if r == nil {
			return
		}
		if r.Named != nil {
			b.mark(r.Named, response, seen)
		}
		visit(r.Elem)
		for i := range r.Items {
			visit(&r.Items[i])
		}
	}
	for _, base := range t.Extends {
		b.mark(base, response, seen)
	}
	for _, f := range t.Fields {
		visit(&f.Ref)
	}
	visit(t.Alias)
}

// typeName returns the TypeScript name of a named Go type
func (b *builder) typeName(t reflect.Type) (string, error) {
	if name, ok := b.names[t]; ok {
		return name, nil
	}
	key := packageName(t.PkgPath()) + "." + t.Name()
	if name, ok := b.cfg.Names[key]; ok {
		b.used["name:"+key] = true
		return name, nil
	}
	if t.Name() == "" || !isExported(t.Name()) {
		return "", fmt.Errorf("type %s has no exported name: name it in Config.Names as %q", t, key)
	}
	return t.Name(), nil
}

// namedType returns the Type of a named struct or root type, reading it the
// first time
func (b *builder) namedType(t reflect.Type) (*Type, error) {
	if nt, ok := b.model.byGo[t]; ok {
		return nt, nil
	}
	name, err := b.typeName(t)
	if err != nil {
		return nil, err
	}
	if other, dup := b.model.byName[name]; dup {
		return nil, fmt.Errorf("types %s and %s are both named %s", other.GoType, t, name)
	}
	nt := &Type{Name: name, GoType: t, Doc: b.src.typeDoc(t)}
	b.model.byGo[t] = nt
	b.model.byName[name] = nt
	b.model.Types = append(b.model.Types, nt)

	if t.Kind() == reflect.Struct && b.isStructJSON(t) {
		if err := b.readStruct(nt, t); err != nil {
			return nil, err
		}
		return nt, nil
	}
	// A named non-struct type, or a struct with its own JSON: an alias of
	// its JSON type
	var ref Ref
	if t.Kind() == reflect.Struct {
		ref, err = b.refOf(t)
	} else {
		ref, err = b.kindRef(t)
	}
	if err != nil {
		return nil, err
	}
	ref.Nilable = false // roots and named types are written as values
	nt.Alias = &ref
	return nt, nil
}

// isStructJSON reports whether a struct type is written as its fields
func (b *builder) isStructJSON(t reflect.Type) bool {
	if c, ok := b.custom[t]; ok {
		return c.AsStruct
	}
	return t != timeType && !hasMarshaler(t)
}

func hasMarshaler(t reflect.Type) bool {
	pt := reflect.PointerTo(t)
	return t.Implements(marshalerType) || pt.Implements(marshalerType) ||
		t.Implements(textMarshaler) || pt.Implements(textMarshaler)
}

// jsonTag is a parsed json struct tag
type jsonTag struct {
	name                string
	skip                bool
	omitEmpty, omitZero bool
	asString            bool
}

func parseTag(f reflect.StructField) jsonTag {
	tag, ok := f.Tag.Lookup("json")
	if !ok {
		return jsonTag{}
	}
	if tag == "-" {
		return jsonTag{skip: true}
	}
	name, opts, _ := strings.Cut(tag, ",")
	jt := jsonTag{name: name}
	for opt := range strings.SplitSeq(opts, ",") {
		switch opt {
		case "omitempty":
			jt.omitEmpty = true
		case "omitzero":
			jt.omitZero = true
		case "string":
			jt.asString = true
		}
	}
	return jt
}

func (b *builder) readStruct(nt *Type, t reflect.Type) error {
	docs := b.src.fieldDocs(t)
	seen := map[string]string{} // JSON name → where it comes from
	for i := range t.NumField() {
		f := t.Field(i)
		tag := parseTag(f)
		if tag.skip {
			continue
		}
		if f.Anonymous && tag.name == "" {
			ft := f.Type
			if ft.Kind() == reflect.Pointer {
				return fmt.Errorf("%s embeds the pointer %s: not supported (its fields are missing when nil)", t, ft)
			}
			if ft.Kind() != reflect.Struct {
				if !f.IsExported() {
					continue
				}
				return fmt.Errorf("%s embeds the non-struct %s: not supported", t, ft)
			}
			base, err := b.namedType(ft)
			if err != nil {
				return err
			}
			if !base.IsObject() {
				return fmt.Errorf("%s embeds %s, which has its own JSON: not supported", t, ft)
			}
			for _, name := range objectFieldNames(base) {
				if prev, dup := seen[name]; dup {
					return fmt.Errorf("%s: field %q of embedded %s collides with %s", t, name, ft, prev)
				}
				seen[name] = "embedded " + ft.String()
			}
			nt.Extends = append(nt.Extends, base)
			continue
		}
		if !f.IsExported() {
			continue
		}
		if tag.asString {
			return fmt.Errorf("%s.%s: the json tag option string is not supported", t, f.Name)
		}
		name := tag.name
		if name == "" {
			name = f.Name
		}
		if prev, dup := seen[name]; dup {
			return fmt.Errorf("%s: field %q (%s) collides with %s", t, name, f.Name, prev)
		}
		seen[name] = f.Name

		spec, hasSpec := b.fields[t][f.Name]
		field := &Field{
			Name:      name,
			GoName:    f.Name,
			Doc:       docs[f.Name],
			OmitEmpty: tag.omitEmpty || tag.omitZero,
			Omitted:   tag.omitZero || (tag.omitEmpty && omitEmptyApplies(f.Type)),
			NonNull:   hasSpec && spec.NonNull,
		}
		var err error
		switch {
		case hasSpec && spec.Override != nil:
			field.Ref = *spec.Override
		case hasSpec && spec.As != nil:
			if f.Type.Kind() != reflect.Interface {
				return fmt.Errorf("%s.%s: As applies to interface fields", t, f.Name)
			}
			field.Ref, err = b.refOf(typeOf(spec.As))
			if err != nil {
				return fmt.Errorf("%s.%s: %w", t, f.Name, err)
			}
		default:
			field.Ref, err = b.refOf(f.Type)
			if err != nil {
				return fmt.Errorf("%s.%s: %w", t, f.Name, err)
			}
		}
		if hasSpec && spec.Enum != "" {
			if err := b.applyEnum(&field.Ref, b.enums[spec.Enum]); err != nil {
				return fmt.Errorf("%s.%s: %w", t, f.Name, err)
			}
		}
		nt.Fields = append(nt.Fields, field)
	}
	return nil
}

// objectFieldNames lists the JSON names of an object's fields, promoted
// ones included
func objectFieldNames(t *Type) []string {
	var names []string
	for _, base := range t.Extends {
		names = append(names, objectFieldNames(base)...)
	}
	for _, f := range t.Fields {
		names = append(names, f.Name)
	}
	return names
}

// applyEnum makes a string type (or the elements of an array or map of
// strings) an enum
func (b *builder) applyEnum(r *Ref, e *Enum) error {
	switch r.Kind {
	case KindString:
		r.Kind, r.Enum = KindEnum, e
	case KindArray, KindMap:
		return b.applyEnum(r.Elem, e)
	default:
		return fmt.Errorf("enum %s applies to strings only", e.Name)
	}
	b.used["enum:"+e.Name] = true
	return nil
}

// omitEmptyApplies reports whether omitempty can leave out a field of this
// type: encoding/json never leaves out struct values
func omitEmptyApplies(t reflect.Type) bool {
	switch t.Kind() {
	case reflect.Struct:
		return false
	case reflect.Array:
		return t.Len() == 0
	}
	return true
}

// refOf returns the JSON type of a Go type
func (b *builder) refOf(t reflect.Type) (Ref, error) {
	if c, ok := b.custom[t]; ok && c.Ref != nil {
		return *c.Ref, nil
	}
	switch {
	case t == timeType:
		return Ref{Kind: KindString, Format: "date-time"}, nil
	case t == rawMessageType:
		return Unknown, nil
	}
	if _, ok := b.custom[t]; !ok && t.Kind() != reflect.Pointer && t.Kind() != reflect.Interface && hasMarshaler(t) {
		return Ref{}, fmt.Errorf("%s has its own MarshalJSON or MarshalText: describe its JSON in Config.Custom", t)
	}
	if _, named := b.names[t]; named && t.Kind() != reflect.Struct {
		// A named non-struct type of the API, written as an alias
		nt, err := b.namedType(t)
		if err != nil {
			return Ref{}, err
		}
		return Ref{Kind: KindNamed, Named: nt, Nilable: nilableKind(t.Kind())}, nil
	}
	return b.kindRef(t)
}

// kindRef returns the JSON type of a Go type by its kind
func (b *builder) kindRef(t reflect.Type) (Ref, error) {
	switch t.Kind() {
	case reflect.Bool:
		return Ref{Kind: KindBoolean}, nil
	case reflect.Int, reflect.Int64, reflect.Uint, reflect.Uint64, reflect.Uintptr:
		return Ref{Kind: KindNumber, Integer: true, Format: "int64"}, nil
	case reflect.Int8, reflect.Int16, reflect.Int32, reflect.Uint8, reflect.Uint16, reflect.Uint32:
		return Ref{Kind: KindNumber, Integer: true, Format: "int32"}, nil
	case reflect.Float32:
		return Ref{Kind: KindNumber, Format: "float"}, nil
	case reflect.Float64:
		return Ref{Kind: KindNumber, Format: "double"}, nil
	case reflect.String:
		if e := b.enumOfType(t); e != nil {
			b.used["enum:"+e.Name] = true
			return Ref{Kind: KindEnum, Enum: e}, nil
		}
		return Ref{Kind: KindString}, nil
	case reflect.Interface:
		return Unknown, nil
	case reflect.Pointer:
		r, err := b.refOf(t.Elem())
		if err != nil {
			return Ref{}, err
		}
		r.Nilable = true
		return r, nil
	case reflect.Slice:
		if t.Elem().Kind() == reflect.Uint8 && !hasMarshaler(t.Elem()) {
			// encoding/json writes []byte as base64
			return Ref{Kind: KindString, Format: "byte", Nilable: true}, nil
		}
		e, err := b.refOf(t.Elem())
		if err != nil {
			return Ref{}, err
		}
		return Ref{Kind: KindArray, Elem: &e, Nilable: true}, nil
	case reflect.Array:
		e, err := b.refOf(t.Elem())
		if err != nil {
			return Ref{}, err
		}
		items := make([]Ref, t.Len())
		for i := range items {
			items[i] = e
		}
		return Ref{Kind: KindTuple, Items: items}, nil
	case reflect.Map:
		switch t.Key().Kind() {
		case reflect.String, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
			reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		default:
			return Ref{}, fmt.Errorf("map key type %s is not supported", t.Key())
		}
		v, err := b.refOf(t.Elem())
		if err != nil {
			return Ref{}, err
		}
		return Ref{Kind: KindMap, Elem: &v, Nilable: true}, nil
	case reflect.Struct:
		if t.Name() == "" {
			return Ref{}, fmt.Errorf("anonymous struct types are not supported: declare a named type")
		}
		nt, err := b.namedType(t)
		if err != nil {
			return Ref{}, err
		}
		return Ref{Kind: KindNamed, Named: nt}, nil
	}
	return Ref{}, fmt.Errorf("type %s (%s) is not supported", t, t.Kind())
}

func nilableKind(k reflect.Kind) bool {
	switch k {
	case reflect.Pointer, reflect.Slice, reflect.Map, reflect.Interface:
		return true
	}
	return false
}

// enumOfType returns the enum declared for a named string type
func (b *builder) enumOfType(t reflect.Type) *Enum {
	if t.Name() == "" || t.PkgPath() == "" {
		return nil
	}
	if e, ok := b.enumTy[t]; ok {
		return e
	}
	var found *Enum
	for _, spec := range b.cfg.Enums {
		if spec.GoType == t.Name() && spec.Package == t.PkgPath() {
			found = b.enums[spec.Name]
		}
	}
	b.enumTy[t] = found
	return found
}

func packageName(pkgPath string) string {
	return pkgPath[strings.LastIndex(pkgPath, "/")+1:]
}

func isExported(name string) bool {
	return name != "" && name[0] >= 'A' && name[0] <= 'Z'
}
