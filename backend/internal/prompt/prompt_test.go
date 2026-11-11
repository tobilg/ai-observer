package prompt

import (
	"strings"
	"testing"
)

func TestConfirmed(t *testing.T) {
	for in, want := range map[string]bool{
		"y\n": true, "yes\n": true, "YES\n": true, " Y \n": true, "y": true,
		"\n": false, "": false, "n\n": false, "no\n": false, "yep\n": false, "\ny\n": false,
	} {
		if got := Confirmed(strings.NewReader(in)); got != want {
			t.Errorf("Confirmed(%q) = %v, want %v", in, got, want)
		}
	}
}
