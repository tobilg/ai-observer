package api

// LogLevelCounts is the answer of GET /api/logs/levels: the number of log
// records per severity text, such as {"INFO": 120, "ERROR": 3} (records
// without one are not counted)
type LogLevelCounts map[string]int64

// OKResponse is the answer of requests that return no data, such as
// making a dashboard the default or moving widgets: {"status": "ok"}
type OKResponse struct {
	Status string `json:"status"`
}

// List returns s, or an empty slice when s is nil: lists in responses are
// written as [] when empty, never as null (as the generated TypeScript
// types and docs/openapi.json declare them)
func List[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}
