package apigen

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"unicode"
)

// sourceReader reads doc comments and constants from the Go files of the
// module's packages
type sourceReader struct {
	root, module string
	packages     map[string]*sourcePackage // by import path
}

type sourcePackage struct {
	types     map[string]string            // doc comments of types
	fields    map[string]map[string]string // doc comments of struct fields, by type
	constants []sourceConst
}

type sourceConst struct {
	name, typ, value string
	ok               bool   // value is a string literal
	doc, groupDoc    string // the constant's comment, its group's doc
}

// FindModuleRoot returns the directory of the go.mod that contains dir and
// the module path it declares
func FindModuleRoot(dir string) (root, module string, err error) {
	dir, err = filepath.Abs(dir)
	if err != nil {
		return "", "", err
	}
	for d := dir; ; d = filepath.Dir(d) {
		data, err := os.ReadFile(filepath.Join(d, "go.mod"))
		if err == nil {
			for line := range strings.SplitSeq(string(data), "\n") {
				if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "module "); ok {
					return d, strings.Trim(strings.TrimSpace(rest), `"`), nil
				}
			}
			return "", "", fmt.Errorf("%s/go.mod declares no module", d)
		}
		if filepath.Dir(d) == d {
			return "", "", fmt.Errorf("no go.mod in %s or above", dir)
		}
	}
}

func newSourceReader(dir string) (*sourceReader, error) {
	root, module, err := FindModuleRoot(dir)
	if err != nil {
		return nil, err
	}
	return &sourceReader{root: root, module: module, packages: map[string]*sourcePackage{}}, nil
}

// pkg parses the package with this import path; packages outside the
// module (the standard library) have no docs
func (s *sourceReader) pkg(path string) (*sourcePackage, error) {
	if p, ok := s.packages[path]; ok {
		return p, nil
	}
	p := &sourcePackage{types: map[string]string{}, fields: map[string]map[string]string{}}
	s.packages[path] = p
	rel, ok := strings.CutPrefix(path, s.module)
	if !ok || (rel != "" && rel[0] != '/') {
		return p, nil
	}
	dir := filepath.Join(s.root, filepath.FromSlash(strings.TrimPrefix(rel, "/")))
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading the sources of %s: %w", path, err)
	}
	fset := token.NewFileSet()
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, parser.ParseComments|parser.SkipObjectResolution)
		if err != nil {
			return nil, fmt.Errorf("parsing %s: %w", name, err)
		}
		p.read(file)
	}
	return p, nil
}

func (p *sourcePackage) read(file *ast.File) {
	for _, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok {
			continue
		}
		switch gd.Tok {
		case token.TYPE:
			for _, spec := range gd.Specs {
				ts := spec.(*ast.TypeSpec)
				doc := ts.Doc
				if doc == nil && len(gd.Specs) == 1 {
					doc = gd.Doc
				}
				p.types[ts.Name.Name] = docText(doc)
				if st, ok := ts.Type.(*ast.StructType); ok {
					fields := map[string]string{}
					for _, f := range st.Fields.List {
						text := joinDocs(docText(f.Doc), docText(f.Comment))
						for _, n := range f.Names {
							fields[n.Name] = text
						}
						if len(f.Names) == 0 {
							fields[embeddedName(f.Type)] = text
						}
					}
					p.fields[ts.Name.Name] = fields
				}
			}
		case token.CONST:
			groupDoc := docText(gd.Doc)
			for _, spec := range gd.Specs {
				vs := spec.(*ast.ValueSpec)
				typ := ""
				if id, ok := vs.Type.(*ast.Ident); ok {
					typ = id.Name
				}
				doc := joinDocs(docText(vs.Doc), docText(vs.Comment))
				for i, n := range vs.Names {
					c := sourceConst{name: n.Name, typ: typ, doc: doc, groupDoc: groupDoc}
					if i < len(vs.Values) {
						if lit, ok := vs.Values[i].(*ast.BasicLit); ok && lit.Kind == token.STRING {
							if v, err := strconv.Unquote(lit.Value); err == nil {
								c.value, c.ok = v, true
							}
						}
					}
					p.constants = append(p.constants, c)
				}
			}
		}
	}
}

func embeddedName(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return embeddedName(t.X)
	case *ast.SelectorExpr:
		return t.Sel.Name
	}
	return ""
}

func docText(g *ast.CommentGroup) string {
	if g == nil {
		return ""
	}
	return strings.TrimSpace(g.Text())
}

func joinDocs(docs ...string) string {
	var parts []string
	for _, d := range docs {
		if d != "" {
			parts = append(parts, d)
		}
	}
	return strings.Join(parts, "\n")
}

// typeDoc returns the doc comment of a named type
func (s *sourceReader) typeDoc(t reflect.Type) string {
	p, err := s.pkg(t.PkgPath())
	if err != nil || t.Name() == "" {
		return ""
	}
	return p.types[t.Name()]
}

// fieldDocs returns the doc comments of a struct type's fields
func (s *sourceReader) fieldDocs(t reflect.Type) map[string]string {
	p, err := s.pkg(t.PkgPath())
	if err != nil {
		return nil
	}
	return p.fields[t.Name()]
}

// enum reads the members of an enum
func (s *sourceReader) enum(spec EnumSpec) (*Enum, error) {
	e := &Enum{Name: spec.Name, Doc: spec.Doc}
	if len(spec.Values) > 0 {
		if spec.Prefix != "" || spec.GoType != "" {
			return nil, fmt.Errorf("enum %s: give Values or constants (Prefix, GoType), not both", spec.Name)
		}
		for _, v := range spec.Values {
			e.Members = append(e.Members, EnumMember{Value: v})
		}
		return e, nil
	}
	if spec.Package == "" || (spec.Prefix == "") == (spec.GoType == "") {
		return nil, fmt.Errorf("enum %s: give Package and either Prefix or GoType", spec.Name)
	}
	p, err := s.pkg(spec.Package)
	if err != nil {
		return nil, err
	}
	if spec.GoType != "" && e.Doc == "" {
		e.Doc = p.types[spec.GoType]
	}
	seen := map[string]bool{}
	for _, c := range p.constants {
		if spec.GoType != "" && c.typ != spec.GoType {
			continue
		}
		if spec.Prefix != "" && !hasNamePrefix(c.name, spec.Prefix) {
			continue
		}
		if !c.ok {
			return nil, fmt.Errorf("enum %s: constant %s is not a string literal", spec.Name, c.name)
		}
		if e.Doc == "" {
			e.Doc = c.groupDoc
		}
		if seen[c.value] {
			continue
		}
		seen[c.value] = true
		e.Members = append(e.Members, EnumMember{Value: c.value, Doc: c.doc})
	}
	if len(e.Members) == 0 {
		return nil, fmt.Errorf("enum %s: no constants found in %s", spec.Name, spec.Package)
	}
	return e, nil
}

// hasNamePrefix reports whether name is prefix followed by an upper-case
// letter or digit (Stat matches StatP95, not Status)
func hasNamePrefix(name, prefix string) bool {
	rest, ok := strings.CutPrefix(name, prefix)
	if !ok || rest == "" {
		return false
	}
	r := []rune(rest)[0]
	return unicode.IsUpper(r) || unicode.IsDigit(r)
}
