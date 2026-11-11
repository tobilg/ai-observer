// Command apigen writes the TypeScript types of AI Observer's API
// (frontend/src/types/api.gen.ts, mcp.gen.ts) and its OpenAPI document
// (docs/openapi.json) from the Go types (internal/apigen/spec). Run it in
// the backend module: go run ./cmd/apigen (make api-types).
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/tobilg/ai-observer/internal/apigen/spec"
)

func main() {
	check := flag.Bool("check", false, "only report whether the files are up to date (exit status 1 if not)")
	flag.Parse()
	if err := run(*check); err != nil {
		fmt.Fprintln(os.Stderr, "apigen:", err)
		os.Exit(1)
	}
}

func run(check bool) error {
	root, err := spec.RepoRoot(".")
	if err != nil {
		return err
	}
	files, err := spec.Files(".")
	if err != nil {
		return err
	}
	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	slices.Sort(paths)
	stale := 0
	for _, path := range paths {
		target := filepath.Join(root, filepath.FromSlash(path))
		current, err := os.ReadFile(target)
		if err == nil && string(current) == string(files[path]) {
			continue
		}
		if check {
			fmt.Println("out of date:", path)
			stale++
			continue
		}
		if err := os.WriteFile(target, files[path], 0o644); err != nil {
			return err
		}
		fmt.Println("wrote", path)
	}
	if stale > 0 {
		return fmt.Errorf("%d file(s) out of date: run %s", stale, spec.RegenerateCommand)
	}
	return nil
}
