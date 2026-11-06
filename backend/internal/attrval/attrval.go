// Package attrval reads typed values from telemetry attribute maps, where
// every value is stored as a string. Different tools use different keys for
// the same value, so each function takes the candidate keys in priority order.
package attrval

import (
	"strconv"
	"strings"
)

// String returns the value of the first key with a non-empty value, unchanged,
// or "" if there is none
func String(attrs map[string]string, keys ...string) string {
	for _, key := range keys {
		if val := attrs[key]; strings.TrimSpace(val) != "" {
			return val
		}
	}
	return ""
}

// Int returns the value of the first key with a non-empty value, parsed as an
// integer. Float values are truncated; unparsable values yield 0.
func Int(attrs map[string]string, keys ...string) int {
	val, ok := first(attrs, keys)
	if !ok {
		return 0
	}
	if i, err := strconv.ParseInt(val, 10, 64); err == nil {
		return int(i)
	}
	if f, err := strconv.ParseFloat(val, 64); err == nil {
		return int(f)
	}
	return 0
}

// Float returns the value of the first key with a non-empty value, parsed as a
// float. Unparsable values yield 0.
func Float(attrs map[string]string, keys ...string) float64 {
	val, ok := first(attrs, keys)
	if !ok {
		return 0
	}
	f, err := strconv.ParseFloat(val, 64)
	if err != nil {
		return 0
	}
	return f
}

// Bool returns whether the first present key is "true" or "1", or nil if none
// of the keys is present.
func Bool(attrs map[string]string, keys ...string) *bool {
	for _, key := range keys {
		if val, ok := attrs[key]; ok {
			result := val == "true" || val == "1"
			return &result
		}
	}
	return nil
}

func first(attrs map[string]string, keys []string) (string, bool) {
	for _, key := range keys {
		if val := strings.TrimSpace(attrs[key]); val != "" {
			return val, true
		}
	}
	return "", false
}
