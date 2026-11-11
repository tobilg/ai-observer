package apigen

import "testing"

func TestHasNamePrefix(t *testing.T) {
	for name, want := range map[string]bool{"StatP95": true, "StatAvg": true, "Status": false, "Stat": false, "Stat9": true, "XStatA": false} {
		if got := hasNamePrefix(name, "Stat"); got != want {
			t.Errorf("hasNamePrefix(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestFindModuleRoot(t *testing.T) {
	root, module, err := FindModuleRoot(".")
	if err != nil || module != "github.com/tobilg/ai-observer" || root == "" {
		t.Errorf("FindModuleRoot = %q, %q, %v", root, module, err)
	}
}
