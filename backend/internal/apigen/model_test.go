package apigen

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/tobilg/ai-observer/internal/apigen/fixture"
)

const fixturePkg = "github.com/tobilg/ai-observer/internal/apigen/fixture"

func fixtureConfig() Config {
	return Config{
		Roots: []Root{
			{Value: fixture.Thing{}},
			{Value: fixture.Request{}, Request: true},
			{Value: fixture.Counts{}},
		},
		Enums: []EnumSpec{
			{Name: "Color", Package: fixturePkg, GoType: "Color"},
			{Name: "Shape", Package: fixturePkg, Prefix: "Shape"},
		},
		Fields: []FieldSpec{
			{Type: fixture.Thing{}, Field: "NonNull", NonNull: true},
			{Type: fixture.Thing{}, Field: "Shape", Enum: "Shape"},
			{Type: fixture.Thing{}, Field: "Shapes", Enum: "Shape"},
		},
	}
}

func buildFixture(t *testing.T) *Model {
	t.Helper()
	m, err := Build(fixtureConfig(), ".")
	if err != nil {
		t.Fatal(err)
	}
	return m
}

const wantFixtureTS = `// Generated for a test.

/** Color is a color */
export type Color =
  /** Red is warm */
  | 'red'
  /** Green is calm */
  | 'green'

/** Shapes of things */
export type Shape = 'round' | 'square'

/** Thing has a field of each kind */
export interface Thing extends Base {
  name: string
  /** opt may be missing */
  opt?: string
  ptr: number | null
  ptrOpt?: number
  List: string[] | null
  nonNull: Inner[]
  map?: Record<string, number>
  struct: Inner
  zero?: Inner
  when: string
  color: Color
  shape?: Shape
  shapes?: Shape[]
  pair: [number, number]
  any: unknown
  raw?: unknown
  bytes?: string
  ptrList?: (Inner | null)[]
  counts: Counts | null
  nested?: Record<string, number[] | null>
}

/** Base is embedded */
export interface Base {
  /** id identifies the thing */
  id: string
}

/** Inner is referenced */
export interface Inner {
  a: string
}

/** Counts is a named map */
export type Counts = Record<string, number>

/** Request is sent by clients */
export interface Request {
  name: string
  opt?: Inner
  items: string[]
  inner?: Inner
}
`

func TestTypeScript(t *testing.T) {
	got := string(buildFixture(t).TypeScript("Generated for a test."))
	if got != wantFixtureTS {
		t.Errorf("TypeScript:\n%s\nwant:\n%s", got, wantFixtureTS)
	}
}

func TestBuildDirections(t *testing.T) {
	m := buildFixture(t)
	inner := m.Type(fixture.Inner{})
	if inner == nil || !inner.Response || !inner.Request {
		t.Errorf("Inner = %+v, want used in both directions", inner)
	}
	if req := m.TypeNamed("Request"); req == nil || req.Response || !req.Request {
		t.Errorf("Request = %+v, want a request only", req)
	}
}

func TestBuildErrors(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
		want string
	}{
		{"own MarshalJSON", Config{Roots: []Root{{Value: fixture.WithPoint{}}}}, "describe its JSON in Config.Custom"},
		{"unexported embedded", Config{Roots: []Root{{Value: fixture.Hidden{}}}}, `name it in Config.Names as "fixture.hiddenBase"`},
		{"collision", Config{Roots: []Root{{Value: fixture.Collides{}}}}, `field "id" (Other) collides`},
		{"unknown field", Config{Roots: []Root{{Value: fixture.Inner{}}}, Fields: []FieldSpec{{Type: fixture.Inner{}, Field: "B"}}}, "no such field"},
		{"unused enum", Config{Roots: []Root{{Value: fixture.Inner{}}}, Enums: []EnumSpec{{Name: "Shape", Package: fixturePkg, Prefix: "Shape"}}}, "enum Shape is used by no field"},
		{"unused name", Config{Roots: []Root{{Value: fixture.Inner{}}}, Names: map[string]string{"fixture.gone": "Gone"}}, "name fixture.gone matches no type"},
		{"field of another type", Config{Roots: []Root{{Value: fixture.Inner{}}}, Fields: []FieldSpec{{Type: fixture.Base{}, Field: "ID", Enum: ""}}}, "the type is not part of the API"},
		{"no constants", Config{Roots: []Root{{Value: fixture.Inner{}}}, Enums: []EnumSpec{{Name: "X", Package: fixturePkg, Prefix: "Nothing"}}}, "no constants found"},
		{"enum on a number", Config{Roots: []Root{{Value: fixture.Thing{}}}, Enums: []EnumSpec{{Name: "Shape", Package: fixturePkg, Prefix: "Shape"}},
			Fields: []FieldSpec{{Type: fixture.Thing{}, Field: "Pair", Enum: "Shape"}}}, "applies to strings only"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Build(tt.cfg, ".")
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("err = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestBuildNamesAndCustom(t *testing.T) {
	cfg := Config{
		Roots:  []Root{{Value: fixture.Hidden{}}, {Value: fixture.WithPoint{}, Doc: "WithPoint has a point"}},
		Names:  map[string]string{"fixture.hiddenBase": "HiddenBase"},
		Custom: []CustomSpec{{Type: fixture.Point{}, Ref: &Ref{Kind: KindTuple, Labels: []string{"x", "y"}, Items: []Ref{Number, Number}}}},
	}
	m, err := Build(cfg, ".")
	if err != nil {
		t.Fatal(err)
	}
	got := string(m.TypeScript("x"))
	for _, want := range []string{
		"export interface Hidden extends HiddenBase {\n  extra: number\n}",
		"export interface HiddenBase {\n  inherited: string\n}",
		"export interface WithPoint {\n  p: [x: number, y: number]\n}",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("TypeScript lacks\n%s\nin\n%s", want, got)
		}
	}
	data, _ := json.Marshal(fixture.WithPoint{P: fixture.Point{X: 1, Y: 2}})
	if problems := m.Validate("WithPoint", data); len(problems) > 0 {
		t.Errorf("Validate(%s) = %v", data, problems)
	}
}

func TestValidate(t *testing.T) {
	m := buildFixture(t)
	n := 3
	valid := fixture.Thing{Base: fixture.Base{ID: "x"}, Ptr: &n, NonNull: []fixture.Inner{}, Color: fixture.Green, Shape: fixture.ShapeRound,
		Any: map[string]any{"k": []int{1}}, Raw: json.RawMessage(`{"a":1}`), Counts: fixture.Counts{"a": 1}}
	data, err := json.Marshal(valid)
	if err != nil {
		t.Fatal(err)
	}
	if problems := m.Validate("Thing", data); len(problems) > 0 {
		t.Fatalf("Validate(%s) = %v", data, problems)
	}

	var obj map[string]any
	if err := json.Unmarshal(data, &obj); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		change func(map[string]any)
		want   string
	}{
		{"null list", func(o map[string]any) { o["nonNull"] = nil }, "$.nonNull: null"},
		{"missing", func(o map[string]any) { delete(o, "name") }, "$.name: missing"},
		{"missing embedded", func(o map[string]any) { delete(o, "id") }, "$.id: missing"},
		{"undeclared", func(o map[string]any) { o["extra"] = 1 }, "$.extra: not a field of Thing"},
		{"enum", func(o map[string]any) { o["color"] = "blue" }, `$.color: "blue" is not a Color`},
		{"enum in list", func(o map[string]any) { o["shapes"] = []any{"round", "oval"} }, `$.shapes[1]: "oval" is not a Shape`},
		{"time", func(o map[string]any) { o["when"] = "yesterday" }, `$.when: "yesterday" is not an RFC 3339 time`},
		{"integer", func(o map[string]any) { o["ptr"] = 1.5 }, "$.ptr: expected an integer"},
		{"tuple", func(o map[string]any) { o["pair"] = []any{1} }, "$.pair: expected an array of 2"},
		{"nested", func(o map[string]any) { o["nonNull"] = []any{map[string]any{"a": 1}} }, "$.nonNull[0].a: expected a string"},
		{"map value", func(o map[string]any) { o["counts"] = map[string]any{"a": "x"} }, "$.counts[a]: expected a number"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			changed := map[string]any{}
			for k, v := range obj {
				changed[k] = v
			}
			tt.change(changed)
			data, _ := json.Marshal(changed)
			problems := m.Validate("Thing", data)
			if !strings.Contains(strings.Join(problems, "\n"), tt.want) {
				t.Errorf("problems = %v, want %q", problems, tt.want)
			}
		})
	}
	// null where allowed
	data, _ = json.Marshal(fixture.Thing{NonNull: []fixture.Inner{}, Color: fixture.Red})
	if problems := m.Validate("Thing", data); len(problems) > 0 {
		t.Errorf("zero Thing: %v", problems)
	}
}
