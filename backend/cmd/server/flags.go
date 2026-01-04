package main

import (
	"flag"
	"fmt"
	"strings"
)

// printFlags prints flag definitions with double-dash prefix (--flag)
// instead of Go's default single-dash (-flag)
func printFlags(fs *flag.FlagSet) {
	fs.VisitAll(func(f *flag.Flag) {
		// Format: --name type
		//             description (default: value)
		var typeStr string
		switch f.DefValue {
		case "false", "true":
			typeStr = "" // boolean flags don't show type
		default:
			typeStr = " " + flagTypeName(f)
		}

		fmt.Printf("  --%s%s\n", f.Name, typeStr)
		fmt.Printf("      %s", f.Usage)
		if f.DefValue != "" && f.DefValue != "false" {
			fmt.Printf(" (default: %s)", f.DefValue)
		}
		fmt.Println()
	})
}

// flagTypeName returns a human-readable type name for the flag
func flagTypeName(f *flag.Flag) string {
	// Check the default value to infer type
	if f.DefValue == "0" || strings.HasPrefix(f.DefValue, "-") || isNumeric(f.DefValue) {
		return "int"
	}
	if f.DefValue == "0s" || strings.HasSuffix(f.DefValue, "s") || strings.HasSuffix(f.DefValue, "m") || strings.HasSuffix(f.DefValue, "h") {
		return "duration"
	}
	return "string"
}

func isNumeric(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return len(s) > 0
}
