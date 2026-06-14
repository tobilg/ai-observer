package storage

import (
	"errors"
	"regexp"
	"strings"
)

var (
	// ErrInvalidAttributeKey indicates a user-provided JSON attribute key cannot
	// be safely used to build a DuckDB JSONPath.
	ErrInvalidAttributeKey = errors.New("invalid attribute key")

	jsonAttributeKeyPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)
)

func normalizePagination(limit, offset int) (int, int) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 1000 {
		limit = 1000
	}
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}

func normalizeIntervalSeconds(intervalSeconds int64) int64 {
	if intervalSeconds <= 0 {
		return 60
	}
	if intervalSeconds > 86400 {
		return 86400
	}
	return intervalSeconds
}

func appendLimitOffset(query string, args []interface{}, limit, offset int) (string, []interface{}) {
	limit, offset = normalizePagination(limit, offset)
	return query + " LIMIT ? OFFSET ?", append(args, limit, offset)
}

func appendLimit(query string, args []interface{}, limit int) (string, []interface{}) {
	limit, _ = normalizePagination(limit, 0)
	return query + " LIMIT ?", append(args, limit)
}

func metricAttributeJSONPath(attribute string) (string, error) {
	attribute = strings.TrimSpace(attribute)
	if attribute == "" || !jsonAttributeKeyPattern.MatchString(attribute) {
		return "", ErrInvalidAttributeKey
	}
	return `$."` + attribute + `"`, nil
}
