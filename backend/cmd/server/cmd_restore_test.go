package main

import (
	"testing"

	"github.com/tobilg/ai-observer/internal/storage"
)

// --map-path is repeatable, in order; a value without OLD=NEW is an error
func TestParseRestoreMapPaths(t *testing.T) {
	flags, err := parseRestoreFlags([]string{"--map-path", "/Users/old=/Users/new", "./export",
		"--map-path=/mnt/a/=/b", "--yes"})
	if err != nil {
		t.Fatal(err)
	}
	want := []storage.PathMapping{{From: "/Users/old", To: "/Users/new"}, {From: "/mnt/a", To: "/b"}}
	if len(flags.MapPaths) != len(want) || flags.MapPaths[0] != want[0] || flags.MapPaths[1] != want[1] || flags.Path != "./export" || !flags.Yes {
		t.Errorf("flags = %+v, want mappings %v", flags, want)
	}
	if _, err := parseRestoreFlags([]string{"./export", "--map-path", "/only-old"}); err == nil {
		t.Error("--map-path without NEW accepted")
	}
}
