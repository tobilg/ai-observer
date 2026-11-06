package attrval

import "testing"

func TestInt(t *testing.T) {
	attrs := map[string]string{"empty": "", "count": "123", "float": "45.9", "spaced": " 7 ", "bad": "abc"}

	tests := []struct {
		name string
		keys []string
		want int
	}{
		{"integer", []string{"count"}, 123},
		{"float is truncated", []string{"float"}, 45},
		{"surrounding spaces", []string{"spaced"}, 7},
		{"unparsable", []string{"bad"}, 0},
		{"missing", []string{"missing"}, 0},
		{"skips empty and missing keys", []string{"missing", "empty", "count"}, 123},
		{"first non-empty key wins", []string{"float", "count"}, 45},
	}
	for _, tc := range tests {
		if got := Int(attrs, tc.keys...); got != tc.want {
			t.Errorf("%s: Int(%v) = %d, want %d", tc.name, tc.keys, got, tc.want)
		}
	}
}

func TestFloat(t *testing.T) {
	attrs := map[string]string{"cost": "0.0123", "bad": "n/a"}

	if got := Float(attrs, "missing", "cost"); got != 0.0123 {
		t.Errorf("Float = %v, want 0.0123", got)
	}
	if got := Float(attrs, "bad"); got != 0 {
		t.Errorf("Float of unparsable value = %v, want 0", got)
	}
}

func TestBool(t *testing.T) {
	attrs := map[string]string{"yes": "true", "one": "1", "no": "false"}

	for key, want := range map[string]bool{"yes": true, "one": true, "no": false} {
		got := Bool(attrs, key)
		if got == nil || *got != want {
			t.Errorf("Bool(%q) = %v, want %v", key, got, want)
		}
	}
	if got := Bool(attrs, "missing"); got != nil {
		t.Errorf("Bool of missing key = %v, want nil", *got)
	}
}

func TestString(t *testing.T) {
	attrs := map[string]string{"empty": "  ", "a": " value with spaces ", "b": "second"}
	if got := String(attrs, "missing", "empty", "a", "b"); got != " value with spaces " {
		t.Errorf("String = %q, want the first non-empty value unchanged", got)
	}
	if got := String(attrs, "missing", "empty"); got != "" {
		t.Errorf("String = %q, want empty", got)
	}
}
