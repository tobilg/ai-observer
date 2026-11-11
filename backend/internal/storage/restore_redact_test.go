package storage

import (
	"strings"
	"testing"

	"github.com/tobilg/ai-observer/internal/redact"
)

// fakeKey builds an Anthropic-shaped key at runtime, so secret scanners do
// not flag the repository
func fakeKey() string {
	return "sk-ant-api03-" + strings.Repeat("Ab3x", 11)
}

func TestRedactAttributes(t *testing.T) {
	r := redact.New(redact.Secrets)
	key := fakeKey()
	cases := []struct {
		name, in string
		// want is the result; "" means the input unchanged
		want string
	}{
		{"map unchanged keeps the text", `{"b": "1",  "a": "x"}`, ""},
		{"map", `{"prompt":"use ` + key + `","z":"1"}`, mapToString(map[string]string{"prompt": "use [REDACTED:anthropic_key]", "z": "1"})},
		{"key kept", `{"` + key + `":"1"}`, ""},
		{"array unchanged", `[{"k":"v"},{}]`, ""},
		{"array of maps", `[{"k":"v"},{"out":"` + key + `"}]`,
			mapArrayToString([]map[string]string{{"k": "v"}, {"out": "[REDACTED:anthropic_key]"}})},
		{"other JSON", `{"n":12345678901234567890,"list":["` + key + `"]}`, `{"list":["[REDACTED:anthropic_key]"],"n":12345678901234567890}`},
		{"not JSON", `not json ` + key, ""},
		{"empty", ``, ""},
	}
	for _, c := range cases {
		got := redactAttributes(r, c.in)
		want := c.want
		if want == "" {
			want = c.in
		}
		if got != want {
			t.Errorf("%s: %q -> %q, want %q", c.name, c.in, got, want)
		}
	}
	// Redacting redacted text changes nothing, so restoring an export of a
	// redacting database keeps its records as they are
	once := redactAttributes(r, `{"prompt":"use `+key+`"}`)
	if again := redactAttributes(r, once); again != once {
		t.Errorf("redacting again changed %q to %q", once, again)
	}
}

// valuesUnchanged reads objects of strings and arrays of them in place,
// and leaves other shapes to the decoding path
func TestValuesUnchanged(t *testing.T) {
	r := redact.New(redact.Secrets)
	key := fakeKey()
	cases := []struct {
		doc           string
		unchanged, ok bool
	}{
		{`{}`, true, true},
		{` { "a" : "b" , "c":"d" } `, true, true},
		{`{"a":"b\"c\\","e\"":"\u003cx\u003e"}`, true, true},
		{`{"a":"` + key + `"}`, false, true},
		{`{"a":"x","b":"` + key + `"}`, false, true},
		// An escaped secret is unescaped before it is checked
		{`{"a":"\u0073` + key[1:] + `"}`, false, true},
		{`[]`, true, true},
		{`[{"a":"b"}, null, {}]`, true, true},
		{`[{"a":"b"},{"c":"` + key + `"}]`, false, true},
		{`{"a":1}`, false, false},
		{`{"a":{"b":"c"}}`, false, false},
		{`["a"]`, false, false},
		{`{"a":"b"} x`, false, false},
		{`{"a":"b"`, false, false},
		{`{"a":"b\`, false, false},
		{``, false, false},
	}
	for _, c := range cases {
		unchanged, ok := valuesUnchanged(r, c.doc)
		if unchanged != c.unchanged || ok != c.ok {
			t.Errorf("valuesUnchanged(%q) = %v, %v; want %v, %v", c.doc, unchanged, ok, c.unchanged, c.ok)
		}
	}
}

func TestParsePathMapping(t *testing.T) {
	m, err := ParsePathMapping("/Users/old/=/home/new")
	if err != nil || m.From != "/Users/old" || m.To != "/home/new" {
		t.Errorf("ParsePathMapping = %+v, %v", m, err)
	}
	for _, bad := range []string{"", "/a", "=/b", "/a=", "/=/b"} {
		if _, err := ParsePathMapping(bad); err == nil {
			t.Errorf("ParsePathMapping(%q) accepted", bad)
		}
	}
}

func TestMapPathExpr(t *testing.T) {
	store, err := NewDuckDBStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	mappings := []PathMapping{{From: "/Users/old", To: "/home/new"}, {From: `C:\Users\o'ld`, To: "/win"}}
	cases := map[string]string{
		"/Users/old/.claude/x.jsonl": "/home/new/.claude/x.jsonl",
		"/Users/old":                 "/home/new",
		"/Users/older/x.jsonl":       "/Users/older/x.jsonl",
		`C:\Users\o'ld\x.jsonl`:      `/win\x.jsonl`,
		"/other/x.jsonl":             "/other/x.jsonl",
	}
	for in, want := range cases {
		var got string
		if err := store.DB().QueryRow("SELECT "+mapPathExpr("p", mappings)+" FROM (SELECT ?::VARCHAR AS p)", in).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Errorf("%s mapped to %s, want %s", in, got, want)
		}
	}
	if expr := mapPathExpr("p", nil); expr != "p" {
		t.Errorf("no mappings: %s", expr)
	}
}
