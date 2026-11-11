package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// settingName matches the names of AI Observer's environment variables
var settingName = regexp.MustCompile(`^AI_OBSERVER_[A-Z0-9_]+$`)

// readSettings returns the AI_OBSERVER_* names that the backend's code (not
// its tests) writes as string literals: the environment variables it reads
func readSettings(t *testing.T) map[string]string {
	t.Helper()
	found := map[string]string{}
	err := filepath.WalkDir("../..", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if name := d.Name(); name == "node_modules" || name == "dist" || strings.HasPrefix(name, ".") && path != "../.." {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, src, 0)
		if err != nil {
			return err
		}
		ast.Inspect(file, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			if value, err := strconv.Unquote(lit.Value); err == nil && settingName.MatchString(value) {
				found[value] = path
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return found
}

// Every environment variable the code reads is in the help, and the help
// lists no variable the code does not read
func TestHelpListsEverySetting(t *testing.T) {
	read := readSettings(t)
	if len(read) < 20 {
		t.Fatalf("found only %d settings in the code: %v", len(read), read)
	}
	help := captureOutput(printHelp)
	listed := map[string]bool{}
	for _, g := range settings {
		for _, s := range g.settings {
			if listed[s.name] {
				t.Errorf("%s is listed twice", s.name)
			}
			listed[s.name] = true
			if _, ok := read[s.name]; !ok {
				t.Errorf("the help lists %s, which no code reads", s.name)
			}
			if s.help == "" {
				t.Errorf("%s has no description", s.name)
			}
		}
	}
	for name, path := range read {
		if !listed[name] {
			t.Errorf("%s (read in %s) is missing from the settings of the help (cmd/server/settings.go)", name, path)
		}
		if !strings.Contains(help, name) {
			t.Errorf("ai-observer --help does not show %s", name)
		}
	}
}

// Every command answers --help with its usage and no error
func TestCommandHelpIsNoError(t *testing.T) {
	parsers := map[string]func([]string) error{
		"import": func(a []string) error { _, err := parseImportFlags(a); return err },
		"export": func(a []string) error { _, err := parseExportFlags(a); return err },
		"delete": func(a []string) error { _, err := parseDeleteFlags(a); return err },
		"watch":  func(a []string) error { _, err := parseWatchArgs(a); return err },
		"serve":  func(a []string) error { _, err := parseServeArgs(a); return err },
		"setup":  runSetup,
	}
	for name, parse := range parsers {
		var err error
		out := captureOutput(func() { err = parse([]string{"--help"}) })
		if !strings.Contains(out, "Usage: ai-observer "+name) {
			t.Errorf("%s --help printed no usage:\n%s", name, out)
		}
		exitOnError(err) // a --help request must not end the test process
	}
}
