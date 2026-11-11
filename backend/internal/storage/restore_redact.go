package storage

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"sync"

	duckdb "github.com/duckdb/duckdb-go/v2"
	"github.com/tobilg/ai-observer/internal/redact"
)

// The SQL functions a restore redacts the text of restored records with, as
// ingestion does (insertLogsTx, spanRow, insertMetricsTx): text columns
// with redactTextFunction, attribute maps (JSON objects of strings, or
// arrays of them for span events and links) with redactAttributesFunction,
// which redacts their values and keeps their keys, and the resource and
// scope attributes that many records share with redactSharedFunction,
// which remembers its results (like writeTx.redactShared).
const (
	redactTextFunction       = "ai_observer_redact"
	redactAttributesFunction = "ai_observer_redact_attributes"
	redactSharedFunction     = "ai_observer_redact_shared"
)

// sharedCacheEntries and sharedCacheMaxLen bound what redactSharedFunction
// remembers: the documents of at most this many distinct maps, each at
// most this long. A restore of 3.2M spans had 8 distinct resource maps.
const (
	sharedCacheEntries = 4096
	sharedCacheMaxLen  = 4096
)

// registerRedactFunctions registers the redaction functions of r on the
// write transaction's connection. A function registered in a transaction
// is part of it: it is dropped when the transaction rolls back, and a later
// restore registers it again with its own redactor.
func registerRedactFunctions(w *writeTx, r *redact.Redactor) error {
	attributes := func(doc string) string { return redactAttributes(r, doc) }
	functions := map[string]func(string) string{
		redactTextFunction:       r.String,
		redactAttributesFunction: attributes,
		redactSharedFunction:     remembered(attributes),
	}
	for name, fn := range functions {
		if err := duckdb.RegisterScalarUDF(w.conn, name, &redactFunction{fn: fn}); err != nil {
			return fmt.Errorf("registering %s: %w", name, err)
		}
	}
	return nil
}

// remembered returns fn remembering its results for short inputs (it is
// called from several threads at once)
func remembered(fn func(string) string) func(string) string {
	var mu sync.RWMutex
	cache := make(map[string]string)
	return func(s string) string {
		if len(s) > sharedCacheMaxLen {
			return fn(s)
		}
		mu.RLock()
		out, ok := cache[s]
		mu.RUnlock()
		if ok {
			return out
		}
		out = fn(s)
		mu.Lock()
		if len(cache) < sharedCacheEntries {
			cache[s] = out
		}
		mu.Unlock()
		return out
	}
}

// redactFunction is a scalar SQL function VARCHAR -> VARCHAR. It returns
// NULL for a value fn leaves as it is, so DuckDB does not copy it back
// (callers write COALESCE(f(x), x): redactedExpr). DuckDB calls it from
// several threads at once: fn must be safe for concurrent use, which the
// redactor is (it holds no state).
type redactFunction struct {
	fn func(string) string
}

func (f *redactFunction) Config() duckdb.ScalarFuncConfig {
	varchar, err := duckdb.NewTypeInfo(duckdb.TYPE_VARCHAR)
	if err != nil {
		// TYPE_VARCHAR is a valid primitive type
		panic(err)
	}
	return duckdb.ScalarFuncConfig{
		InputTypeInfos: []duckdb.TypeInfo{varchar},
		ResultTypeInfo: varchar,
	}
}

func (f *redactFunction) Executor() duckdb.ScalarFuncExecutor {
	return duckdb.ScalarFuncExecutor{RowExecutor: func(values []driver.Value) (any, error) {
		s, ok := values[0].(string)
		if !ok {
			return nil, nil
		}
		if out := f.fn(s); out != s {
			return out, nil
		}
		return nil, nil
	}}
}

// redactAttributes returns the JSON document doc with the string values of
// its attribute maps redacted like Redactor.Map: an object of strings, an
// array of such objects (span events and links), or for other JSON every
// string value. Keys are kept. A document in which nothing changes is
// returned as it is, so restored records stay identical to the exported
// ones; a changed one is written as ingestion writes it (mapToString).
func redactAttributes(r *redact.Redactor, doc string) string {
	// Most documents hold nothing to redact: find that out without
	// decoding them (a restore of 3.2M spans decoded 900 MB of attributes)
	if unchanged, ok := valuesUnchanged(r, doc); ok && unchanged {
		return doc
	}
	trimmed := strings.TrimLeft(doc, " \t\r\n")
	if trimmed == "" {
		return doc
	}
	switch trimmed[0] {
	case '{':
		var m map[string]string
		if json.Unmarshal([]byte(doc), &m) == nil {
			redacted := r.Map(m)
			if sameMap(redacted, m) {
				return doc
			}
			return mapToString(redacted)
		}
	case '[':
		var maps []map[string]string
		if json.Unmarshal([]byte(doc), &maps) == nil {
			changed := false
			for i, m := range maps {
				if redacted := r.Map(m); !sameMap(redacted, m) {
					maps[i] = redacted
					changed = true
				}
			}
			if !changed {
				return doc
			}
			return mapArrayToString(maps)
		}
	}
	return redactJSONValues(r, doc)
}

// valuesUnchanged reports whether redaction leaves every value of doc, a
// JSON object of strings or an array of such objects, as it is, reading
// the values in place. ok is false for documents of another shape, which
// the caller decodes.
func valuesUnchanged(r *redact.Redactor, doc string) (unchanged, ok bool) {
	p := jsonScanner{doc: doc}
	p.space()
	if p.peek() == '[' {
		p.i++
		p.space()
		if p.peek() == ']' {
			p.i++
			return p.end(), p.end()
		}
		for {
			if p.peek() == 'n' && strings.HasPrefix(p.doc[p.i:], "null") {
				p.i += 4
			} else {
				changed, ok := p.object(r)
				if !ok || changed {
					return false, ok
				}
			}
			p.space()
			switch p.next() {
			case ',':
				p.space()
				continue
			case ']':
				return p.end(), p.end()
			}
			return false, false
		}
	}
	changed, ok := p.object(r)
	if !ok || changed {
		return false, ok
	}
	return p.end(), p.end()
}

// jsonScanner reads a JSON document in place
type jsonScanner struct {
	doc string
	i   int
}

func (p *jsonScanner) peek() byte {
	if p.i < len(p.doc) {
		return p.doc[p.i]
	}
	return 0
}

func (p *jsonScanner) next() byte {
	c := p.peek()
	p.i++
	return c
}

func (p *jsonScanner) space() {
	for p.i < len(p.doc) && strings.IndexByte(" \t\r\n", p.doc[p.i]) >= 0 {
		p.i++
	}
}

// end reports whether only white space follows
func (p *jsonScanner) end() bool {
	p.space()
	return p.i == len(p.doc)
}

// object reads an object of strings and reports whether redaction changes
// one of its values
func (p *jsonScanner) object(r *redact.Redactor) (changed, ok bool) {
	p.space()
	if p.next() != '{' {
		return false, false
	}
	p.space()
	if p.peek() == '}' {
		p.i++
		return false, true
	}
	for {
		p.space()
		if _, ok := p.str(); !ok {
			return false, false
		}
		p.space()
		if p.next() != ':' {
			return false, false
		}
		p.space()
		value, ok := p.str()
		if !ok {
			return false, false
		}
		if r.String(value) != value {
			return true, true
		}
		p.space()
		switch p.next() {
		case ',':
			continue
		case '}':
			return false, true
		}
		return false, false
	}
}

// str reads a string, unescaping it only when it holds escapes
func (p *jsonScanner) str() (string, bool) {
	if p.next() != '"' {
		return "", false
	}
	start, escaped := p.i, false
	for p.i < len(p.doc) {
		switch p.doc[p.i] {
		case '\\':
			escaped = true
			p.i += 2
			continue
		case '"':
			raw := p.doc[start:p.i]
			p.i++
			if !escaped {
				return raw, true
			}
			var s string
			if err := json.Unmarshal([]byte(p.doc[start-1:p.i]), &s); err != nil {
				return "", false
			}
			return s, true
		}
		p.i++
	}
	return "", false
}

// sameMap reports whether a and b are the same map: Redactor.Map returns
// its argument when it changes nothing
func sameMap(a, b map[string]string) bool {
	return reflect.ValueOf(a).UnsafePointer() == reflect.ValueOf(b).UnsafePointer()
}

// redactJSONValues redacts every string value of a JSON document of another
// shape (keys are kept); a document that is not JSON is returned unchanged
func redactJSONValues(r *redact.Redactor, doc string) string {
	dec := json.NewDecoder(strings.NewReader(doc))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return doc
	}
	changed := false
	var walk func(v any) any
	walk = func(v any) any {
		switch x := v.(type) {
		case string:
			if redacted := r.String(x); redacted != x {
				changed = true
				return redacted
			}
		case []any:
			for i := range x {
				x[i] = walk(x[i])
			}
		case map[string]any:
			for k := range x {
				x[k] = walk(x[k])
			}
		}
		return v
	}
	v = walk(v)
	if !changed {
		return doc
	}
	out, err := json.Marshal(v)
	if err != nil {
		return doc
	}
	return string(out)
}

// restoreRedaction returns the SQL expression that restores column col of
// t's rows (expr, the file's value) redacted, or expr when the column holds
// no text ingestion redacts. Empty maps and lists, most span events and
// links, are not passed to the functions.
func (t restoreTable) restoreRedaction(col, expr string) string {
	if slices.Contains(t.text, col) {
		return redactedExpr(redactTextFunction, expr)
	}
	function := ""
	switch {
	case slices.Contains(t.shared, col):
		function = redactSharedFunction
	case slices.Contains(t.attributes, col):
		function = redactAttributesFunction
	default:
		return expr
	}
	return "CASE WHEN CAST(" + expr + " AS VARCHAR) IN ('{}', '[]') THEN " + expr + " ELSE " + redactedExpr(function, expr) + " END"
}

// redactedExpr is the SQL expression of expr redacted with the redaction
// function named function, which returns NULL for unchanged values
func redactedExpr(function, expr string) string {
	return "COALESCE(" + function + "(" + expr + "), " + expr + ")"
}

// redactRestoredBaselines is the expression list of the attribute columns
// of restored metric baselines, redacted like the restored metric points'
// attributes so a baseline still names its series
func redactRestoredBaselines(enabled bool) string {
	if !enabled {
		return "Attributes, ResourceAttributes"
	}
	return redactedExpr(redactAttributesFunction, "Attributes") + ", " + redactedExpr(redactAttributesFunction, "ResourceAttributes")
}
