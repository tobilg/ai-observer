package storage

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// Helper functions
func mapToString(m map[string]string) string {
	if len(m) == 0 {
		return "{}"
	}
	b, err := json.Marshal(m)
	if err != nil {
		return "{}"
	}
	return string(b)
}

func stringArrayToString(arr []string) string {
	if len(arr) == 0 {
		return "[]"
	}
	b, err := json.Marshal(arr)
	if err != nil {
		return "[]"
	}
	return string(b)
}

func timestampArrayToString(arr []time.Time) string {
	if len(arr) == 0 {
		return "[]"
	}
	strs := make([]string, len(arr))
	for i, t := range arr {
		strs[i] = t.Format(time.RFC3339Nano)
	}
	b, err := json.Marshal(strs)
	if err != nil {
		return "[]"
	}
	return string(b)
}

func mapArrayToString(arr []map[string]string) string {
	if len(arr) == 0 {
		return "[]"
	}
	b, err := json.Marshal(arr)
	if err != nil {
		return "[]"
	}
	return string(b)
}

// scanJSONToMap scans a JSON column that DuckDB returns as map[string]interface{}
// and converts it to map[string]string. Errors are logged but not returned since
// partial results may still be useful for display purposes.
func scanJSONToMap(v interface{}) map[string]string {
	result := make(map[string]string)
	if v == nil {
		return result
	}

	switch val := v.(type) {
	case map[string]interface{}:
		for k, v := range val {
			if s, ok := v.(string); ok {
				result[k] = s
			} else if v != nil {
				// Convert non-string values to JSON
				b, err := json.Marshal(v)
				if err != nil {
					// Log but continue - use empty string for this key
					result[k] = fmt.Sprintf("<error: %v>", err)
					continue
				}
				result[k] = string(b)
			}
		}
	case string:
		// If it's a string, try to parse it as JSON
		if err := json.Unmarshal([]byte(val), &result); err != nil {
			// Log error but return empty map - caller can handle missing data
			return result
		}
	}
	return result
}

// uint64ArrayToString serializes counts as a JSON array
func uint64ArrayToString(arr []uint64) string {
	if len(arr) == 0 {
		return "[]"
	}
	var b strings.Builder
	b.WriteByte('[')
	for i, v := range arr {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(strconv.FormatUint(v, 10))
	}
	b.WriteByte(']')
	return b.String()
}

// float64ArrayToString serializes values as a JSON array in their shortest
// exact form. JSON has no NaN or infinity, so those become null instead of
// failing the whole insert.
func float64ArrayToString(arr []float64) string {
	if len(arr) == 0 {
		return "[]"
	}
	var b strings.Builder
	b.WriteByte('[')
	for i, v := range arr {
		if i > 0 {
			b.WriteString(", ")
		}
		if math.IsNaN(v) || math.IsInf(v, 0) {
			b.WriteString("null")
			continue
		}
		// Like encoding/json: plain notation except for very small or large values
		format := byte('f')
		if abs := math.Abs(v); abs != 0 && (abs < 1e-6 || abs >= 1e21) {
			format = 'e'
		}
		b.WriteString(strconv.FormatFloat(v, format, -1, 64))
	}
	b.WriteByte(']')
	return b.String()
}
