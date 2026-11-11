package api

// HealthResponse is the answer of GET /health (liveness): always "ok" while
// the server answers
type HealthResponse struct {
	Status string `json:"status"`
}

// Readiness states of ReadyResponse
const (
	ReadyOK          = "ok"
	ReadyUnavailable = "unavailable"
)

// ReadyResponse is the answer of GET /ready (readiness): Status and Database
// are "ok" (200) when the database answers a query, else "unavailable"
// (503) with Error
type ReadyResponse struct {
	Status   string `json:"status"`
	Database string `json:"database"`
	Error    string `json:"error,omitempty"`
}
