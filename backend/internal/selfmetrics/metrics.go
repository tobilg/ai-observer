package selfmetrics

import (
	"net/http"
	"sync/atomic"
	"time"
)

// The metrics of AI Observer. Label values are short fixed words (signal
// names, results, route patterns, tool names), never request data.
var (
	OTLPRequests = Default.NewCounter("ai_observer_otlp_requests_total",
		"OTLP export requests by transport (http, grpc), signal (traces, metrics, logs) and result (ok, invalid, too_large, unauthorized, unavailable, error)",
		"transport", "signal", "result")
	RecordsStored = Default.NewCounter("ai_observer_records_stored_total",
		"Records stored from OTLP exports (HTTP and gRPC) by signal (spans, logs, metrics; metrics include derived ones)", "signal")
	RecordsRejected = Default.NewCounter("ai_observer_records_rejected_total",
		"Records of OTLP exports rejected as invalid (reported to the exporter as partial success), by signal (spans, metrics)", "signal")
	BatchesDeduplicated = Default.NewCounter("ai_observer_batches_deduplicated_total",
		"OTLP log and metric exports acknowledged without storing because the same body was stored in the last 15 minutes, by signal (logs, metrics)", "signal")
	NoiseSpansDropped = Default.NewCounter("ai_observer_noise_spans_dropped_total",
		"Spans left out as noise (tools.Provider.NoiseSpans)")

	WriteTransactionSeconds = Default.NewHistogram("ai_observer_write_transaction_seconds",
		"Duration of storage write transactions, from acquiring the write lock to commit or rollback", DurationBuckets)
	WriteLockWaitSeconds = Default.NewHistogram("ai_observer_write_lock_wait_seconds",
		"Time writers waited for the storage write lock (writes are serialized)", DurationBuckets)

	APIRequestSeconds = Default.NewHistogram("ai_observer_api_request_seconds",
		"Duration of API requests by route pattern", DurationBuckets, "route")
	APITimeouts = Default.NewCounter("ai_observer_api_timeouts_total",
		"API requests answered 504 because their query exceeded the request timeout, by route pattern", "route")
	AuthFailures = Default.NewCounter("ai_observer_auth_failures_total",
		"Requests refused for missing or wrong credentials (unauthorized) or from an address blocked after failed attempts (blocked), by transport (http: OTLP/HTTP and the API, grpc: OTLP/gRPC)",
		"transport", "result")

	WebSocketDroppedBroadcasts = Default.NewCounter("ai_observer_websocket_dropped_broadcasts_total",
		"Real-time notifications dropped because the hub's broadcast queue was full")
	WebSocketSlowClients = Default.NewCounter("ai_observer_websocket_slow_clients_disconnected_total",
		"WebSocket clients disconnected because their send buffer was full")

	WatcherFilesProcessed = Default.NewCounter("ai_observer_watcher_files_processed_total",
		"Session files processed by watch mode, by tool", "tool")
	WatcherErrors = Default.NewCounter("ai_observer_watcher_errors_total",
		"Session files watch mode failed to process, by tool", "tool")

	RetentionDeletedRows = Default.NewCounter("ai_observer_retention_deleted_rows_total",
		"Rows deleted by retention (AI_OBSERVER_RETENTION_DAYS), by signal (spans, logs, metrics)", "signal")
)

// Transports of the transport label: OTLP/HTTP and the API, and OTLP/gRPC
const (
	TransportHTTP = "http"
	TransportGRPC = "grpc"
)

// webSocketClients returns the number of connected WebSocket clients
// (SetWebSocketClients)
var webSocketClients atomic.Pointer[func() int]

func init() {
	Default.NewGaugeFunc("ai_observer_websocket_clients", "Connected WebSocket clients", func() int64 {
		if fn := webSocketClients.Load(); fn != nil {
			return int64((*fn)())
		}
		return 0
	})
}

// SetWebSocketClients sets the function that counts the connected WebSocket
// clients (the server's hub)
func SetWebSocketClients(fn func() int) { webSocketClients.Store(&fn) }

// OTLPResult is the result label of an OTLP request answered with status
func OTLPResult(status int) string {
	switch {
	case status < 300:
		return "ok"
	case status == http.StatusUnauthorized || status == http.StatusForbidden || status == http.StatusTooManyRequests:
		return "unauthorized"
	case status == http.StatusRequestEntityTooLarge:
		return "too_large"
	case status < 500:
		return "invalid"
	case status == http.StatusServiceUnavailable:
		return "unavailable"
	default:
		return "error"
	}
}

// WriteTimer measures a write transaction: the wait for the write lock and
// the transaction. In storage:
//
//	wt := selfmetrics.StartWrite()
//	s.mu.Lock()
//	defer s.mu.Unlock()
//	defer wt.Locked().Done()
type WriteTimer struct{ start, locked time.Time }

// StartWrite starts measuring before the write lock is taken
func StartWrite() *WriteTimer { return &WriteTimer{start: time.Now()} }

// Locked records the wait for the lock, which was just acquired
func (t *WriteTimer) Locked() *WriteTimer {
	t.locked = time.Now()
	WriteLockWaitSeconds.With().Observe(t.locked.Sub(t.start).Seconds())
	return t
}

// Done records the transaction's duration
func (t *WriteTimer) Done() {
	WriteTransactionSeconds.With().ObserveSince(t.locked)
}
